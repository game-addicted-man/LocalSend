package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxHistory = 500

// Config는 %APPDATA%\LocalDrop\config.json 에 저장됩니다.
type Config struct {
	Port     int    `json:"port"`
	Token    string `json:"token"`             // 폰 연결용 비밀 코드 (QR에 들어감)
	SaveDir  string `json:"saveDir"`           // 받은 파일 저장 위치
	PCName   string `json:"pcName"`            // 폰 화면에 보이는 PC 이름
	AutoCopy bool   `json:"autoCopy"`          // 폰에서 보낸 글을 PC 클립보드에 자동 복사
	Notify   bool   `json:"notify"`            // 받으면 Windows 알림
	LinkExe  string `json:"linkExe,omitempty"` // 바로가기(보내기 메뉴·자동 실행)가 가리키는 exe 경로
}

// Item은 타임라인에 쌓이는 한 건(파일 또는 글)입니다.
type Item struct {
	ID       string    `json:"id"`
	From     string    `json:"from"`               // "pc" 또는 "phone"
	DeviceID string    `json:"deviceId,omitempty"` // 보낸 폰의 기기 ID
	Device   string    `json:"device,omitempty"`   // 보낸 폰 이름
	Kind     string    `json:"kind"`               // "file" 또는 "text"
	Name     string    `json:"name,omitempty"`
	Size     int64     `json:"size,omitempty"`
	Mime     string    `json:"mime,omitempty"`
	Text     string    `json:"text,omitempty"`
	Time     time.Time `json:"time"`
	CID      string    `json:"cid,omitempty"`  // 보낸 쪽 화면이 붙인 임시 ID
	Path     string    `json:"path,omitempty"` // PC 안의 실제 경로 (폰에는 보내지 않음)
	IsDir    bool      `json:"isDir,omitempty"`
	Outbox   bool      `json:"outbox,omitempty"`  // PC 화면에 끌어다 놓아 복사해 둔 파일
	Expired  bool      `json:"expired,omitempty"` // 이전 실행에서 공유했던 PC 파일
}

// ItemView는 화면에 보내는 형태입니다.
type ItemView struct {
	Item
	Available bool `json:"available"`
}

type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	IP       string    `json:"ip"`
	LastSeen time.Time `json:"lastSeen"`
	Online   bool      `json:"online"`
	conns    int
}

type App struct {
	mu            sync.Mutex
	cfg           Config
	dataDir       string
	outboxDir     string
	port          int
	adminToken    string
	instanceToken string

	items   []*Item
	byID    map[string]*Item
	devices map[string]*Device
	uploads map[string]*uploadState

	hub        *Hub
	uiMu       sync.RWMutex
	ui         uiHooks
	quit       chan struct{}
	quitOnce   sync.Once
	saveTimer  *time.Timer
	notifier   *notifier
	browserUI  bool // PC 화면을 일반 브라우저로 열었는지 (종료 버튼 표시용)
	sendToPath string
}

// uiHooks는 운영체제별 창 기능입니다. 없으면 nil로 둡니다.
type uiHooks struct {
	show         func()         // 창을 앞으로 가져오기
	isForeground func() bool    // 지금 창을 보고 있는지
	owner        func() uintptr // 대화상자의 부모 창
}

func newApp(dataDir string) (*App, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	a := &App{
		dataDir:       dataDir,
		adminToken:    randomToken(18),
		instanceToken: randomToken(18),
		byID:          map[string]*Item{},
		devices:       map[string]*Device{},
		uploads:       map[string]*uploadState{},
		hub:           newHub(),
		quit:          make(chan struct{}),
	}
	cacheBase, err := os.UserCacheDir()
	if err != nil {
		cacheBase = dataDir
	}
	a.outboxDir = filepath.Join(cacheBase, appName, "outbox")
	// 이전 실행에서 끌어다 놓았던 복사본은 지웁니다 (프로그램이 켜져 있는 동안만 공유).
	_ = os.RemoveAll(a.outboxDir)

	a.loadConfig()
	a.loadHistory()
	a.notifier = newNotifier(a)
	return a, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func newID() string { return randomToken(9) }

// ---------- 설정 ----------

func (a *App) configPath() string  { return filepath.Join(a.dataDir, "config.json") }
func (a *App) historyPath() string { return filepath.Join(a.dataDir, "history.json") }

func (a *App) loadConfig() {
	cfg := Config{Port: defaultPort, AutoCopy: true, Notify: true}
	if b, err := os.ReadFile(a.configPath()); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			log.Printf("config.json을 읽지 못해 기본값을 씁니다: %v", err)
		}
	}
	changed := false
	if len(cfg.Token) < 12 {
		cfg.Token = randomToken(12)
		changed = true
	}
	if cfg.SaveDir == "" {
		cfg.SaveDir = filepath.Join(downloadsDir(), appName)
		changed = true
	}
	if strings.TrimSpace(cfg.PCName) == "" {
		cfg.PCName = defaultPCName()
		changed = true
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = defaultPort
	}
	a.cfg = cfg
	if changed {
		a.saveConfig()
	}
}

func (a *App) saveConfig() {
	a.mu.Lock()
	b, _ := json.MarshalIndent(a.cfg, "", "  ")
	a.mu.Unlock()
	if err := writeFileAtomic(a.configPath(), b); err != nil {
		log.Printf("설정 저장 실패: %v", err)
	}
}

func (a *App) saveDir() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.SaveDir
}

func defaultPCName() string {
	if u := strings.TrimSpace(defaultUserName()); u != "" {
		return cleanShort(u, 30) + "의 PC"
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return cleanShort(h, 40)
	}
	return "내 PC"
}

func downloadsDir() string {
	if d := platformDownloadsDir(); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads")
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------- 기록 ----------

func (a *App) loadHistory() {
	b, err := os.ReadFile(a.historyPath())
	if err != nil {
		return
	}
	var saved struct {
		Items []*Item `json:"items"`
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		log.Printf("history.json을 읽지 못했어요: %v", err)
		return
	}
	for _, it := range saved.Items {
		if it == nil || it.ID == "" {
			continue
		}
		if it.From == "pc" && it.Kind == "file" {
			it.Expired = true // PC에서 공유한 파일은 실행하는 동안만 받을 수 있어요
		}
		a.items = append(a.items, it)
		a.byID[it.ID] = it
	}
	sort.SliceStable(a.items, func(i, j int) bool { return a.items[i].Time.Before(a.items[j].Time) })
}

// scheduleSave는 잦은 변경을 모아 1초 뒤에 한 번만 저장합니다. a.mu를 잡은 상태에서 호출합니다.
func (a *App) scheduleSave() {
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}
	a.saveTimer = time.AfterFunc(time.Second, a.saveHistoryNow)
}

func (a *App) saveHistoryNow() {
	a.mu.Lock()
	items := make([]*Item, 0, len(a.items))
	for _, it := range a.items {
		c := *it
		items = append(items, &c)
	}
	a.mu.Unlock()
	b, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return
	}
	if err := writeFileAtomic(a.historyPath(), b); err != nil {
		log.Printf("기록 저장 실패: %v", err)
	}
}

func (a *App) addItem(it *Item) {
	a.mu.Lock()
	a.items = append(a.items, it)
	a.byID[it.ID] = it
	for len(a.items) > maxHistory {
		old := a.items[0]
		a.items = a.items[1:]
		delete(a.byID, old.ID)
		if old.Outbox && old.Path != "" {
			_ = os.Remove(old.Path)
		}
	}
	a.scheduleSave()
	a.mu.Unlock()
	a.broadcastItem(it)
}

func (a *App) getItem(id string) *Item {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.byID[id]
}

func (a *App) removeItem(id string) bool {
	a.mu.Lock()
	it, ok := a.byID[id]
	if !ok {
		a.mu.Unlock()
		return false
	}
	delete(a.byID, id)
	for i, x := range a.items {
		if x.ID == id {
			a.items = append(a.items[:i], a.items[i+1:]...)
			break
		}
	}
	a.scheduleSave()
	a.mu.Unlock()
	// 받은 파일은 지우지 않고 목록에서만 뺍니다. 끌어다 놓아 만든 복사본만 지웁니다.
	if it.Outbox && it.Path != "" {
		_ = os.Remove(it.Path)
	}
	a.hub.send("remove", map[string]string{"id": id}, true, true)
	return true
}

func (a *App) clearItems() {
	a.mu.Lock()
	var outbox []string
	for _, it := range a.items {
		if it.Outbox && it.Path != "" {
			outbox = append(outbox, it.Path)
		}
	}
	a.items = nil
	a.byID = map[string]*Item{}
	a.scheduleSave()
	a.mu.Unlock()
	for _, p := range outbox {
		_ = os.Remove(p)
	}
	a.hub.send("clear", map[string]any{}, true, true)
}

func (a *App) view(it *Item, admin bool) ItemView {
	v := ItemView{Item: *it}
	if it.Kind == "file" {
		v.Available = !it.Expired && pathExists(it.Path)
	} else {
		v.Available = true
	}
	if !admin {
		v.Path = "" // PC 안의 경로는 폰에 알려주지 않습니다.
		v.Outbox = false
	}
	return v
}

func (a *App) itemViews(admin bool) []ItemView {
	a.mu.Lock()
	items := append([]*Item(nil), a.items...)
	a.mu.Unlock()
	out := make([]ItemView, 0, len(items))
	for _, it := range items {
		out = append(out, a.view(it, admin))
	}
	return out
}

func (a *App) broadcastItem(it *Item) {
	a.hub.sendFunc("item", func(admin bool) any { return a.view(it, admin) }, true, true)
}

func pathExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// ---------- 기기 ----------

func (a *App) touchDevice(id, name, ip string, connDelta int) {
	if id == "" {
		return
	}
	a.mu.Lock()
	d, ok := a.devices[id]
	if !ok {
		d = &Device{ID: id}
		a.devices[id] = d
	}
	if name != "" {
		d.Name = name
	}
	if d.Name == "" {
		d.Name = "휴대폰"
	}
	d.IP = ip
	d.LastSeen = time.Now()
	d.conns += connDelta
	if d.conns < 0 {
		d.conns = 0
	}
	wasOnline := d.Online
	d.Online = d.conns > 0
	changed := !ok || connDelta != 0 || wasOnline != d.Online
	a.mu.Unlock()
	if changed {
		a.hub.send("devices", a.deviceList(), true, false)
	}
}

func (a *App) deviceList() []Device {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Device, 0, len(a.devices))
	for _, d := range a.devices {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

// ---------- 창 연결 ----------

func (a *App) hooks() uiHooks {
	a.uiMu.RLock()
	defer a.uiMu.RUnlock()
	return a.ui
}

func (a *App) setHooks(h uiHooks) {
	a.uiMu.Lock()
	a.ui = h
	a.uiMu.Unlock()
}

func (a *App) setBrowserUI(on bool) {
	a.uiMu.Lock()
	a.browserUI = on
	a.uiMu.Unlock()
}

func (a *App) isBrowserUI() bool {
	a.uiMu.RLock()
	defer a.uiMu.RUnlock()
	return a.browserUI
}

// ---------- 종료 ----------

func (a *App) requestQuit() {
	a.quitOnce.Do(func() { close(a.quit) })
}

func (a *App) shutdown() {
	a.mu.Lock()
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}
	a.mu.Unlock()
	a.saveHistoryNow()
	a.removeInstanceFile()
	_ = os.RemoveAll(a.outboxDir)
}

// ---------- 표시용 도우미 ----------

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

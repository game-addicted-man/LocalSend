package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/atotto/clipboard"
)

//go:embed web
var webFS embed.FS

var (
	pageTmpl   = template.Must(template.ParseFS(webFS, "web/phone.html", "web/pc.html", "web/notice.html"))
	staticETag = map[string]string{}
)

func init() {
	// Windows 레지스트리의 MIME 설정이 이상해도 스크립트·글꼴이 제대로 열리도록 고정합니다.
	for ext, t := range map[string]string{
		".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8",
		".svg": "image/svg+xml", ".png": "image/png", ".woff2": "font/woff2",
		".webmanifest": "application/manifest+json", ".ico": "image/x-icon", ".txt": "text/plain; charset=utf-8",
	} {
		_ = mime.AddExtensionType(ext, t)
	}
	_ = fs.WalkDir(webFS, "web/static", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, err := webFS.ReadFile(p); err == nil {
				sum := sha256.Sum256(b)
				staticETag[p] = `"` + hex.EncodeToString(sum[:8]) + `"`
			}
		}
		return nil
	})
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()

	// 화면
	mux.HandleFunc("GET /{$}", a.handlePhoneIndex)
	mux.HandleFunc("GET /pc", a.handlePCIndex)
	mux.HandleFunc("GET /static/{file...}", handleStatic)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/static/favicon.png"
		handleStatic(w, r)
	})
	mux.HandleFunc("GET /manifest.webmanifest", a.handleManifest)

	// 폰이 쓰는 API (QR의 비밀 코드 필요)
	mux.HandleFunc("GET /api/state", a.phone(a.handlePhoneState))
	mux.HandleFunc("GET /api/events", a.phone(a.handlePhoneEvents))
	mux.HandleFunc("POST /api/upload", a.phoneMut(a.handlePhoneUpload))
	mux.HandleFunc("POST /api/text", a.phoneMut(a.handlePhoneText))
	mux.HandleFunc("GET /api/file/{id}", a.handleFile) // 폰 또는 PC

	// PC 화면이 쓰는 API (이 PC 안에서만)
	mux.HandleFunc("GET /api/pc/state", a.admin(a.handlePCState))
	mux.HandleFunc("GET /api/pc/events", a.admin(a.handlePCEvents))
	mux.HandleFunc("GET /api/pc/qr.svg", a.admin(a.handleQR))
	mux.HandleFunc("POST /api/pc/share", a.adminMut(a.handlePCShareUpload))
	mux.HandleFunc("POST /api/pc/pick", a.adminMut(a.handlePCPick))
	mux.HandleFunc("POST /api/pc/text", a.adminMut(a.handlePCText))
	mux.HandleFunc("POST /api/pc/open", a.adminMut(a.handlePCOpen))
	mux.HandleFunc("POST /api/pc/remove", a.adminMut(a.handlePCRemove))
	mux.HandleFunc("POST /api/pc/clear", a.adminMut(a.handlePCClear))
	mux.HandleFunc("POST /api/pc/settings", a.adminMut(a.handlePCSettings))
	mux.HandleFunc("POST /api/pc/pickfolder", a.adminMut(a.handlePCPickFolder))
	mux.HandleFunc("POST /api/pc/reset", a.adminMut(a.handlePCReset))
	mux.HandleFunc("POST /api/pc/firewall", a.adminMut(a.handlePCFirewall))
	mux.HandleFunc("POST /api/pc/copy", a.adminMut(a.handlePCCopy))
	mux.HandleFunc("POST /api/pc/quit", a.adminMut(a.handlePCQuit))

	// 두 번째로 실행된 LocalDrop이 첫 번째에게 보내는 요청
	mux.HandleFunc("POST /api/instance", a.handleInstance)

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer") // 주소의 비밀 코드가 외부 사이트로 새지 않게
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

const pageCSP = "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'self' 'unsafe-inline'; " +
	"script-src 'self'; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// ---------- 인증 ----------

func tokenEqual(a, b string) bool {
	return a != "" && len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (a *App) currentToken() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Token
}

// requestToken은 폰이 보낸 비밀 코드를 헤더, 주소, 쿠키 순서로 찾습니다.
func requestToken(r *http.Request) string {
	if t := r.Header.Get("X-LD-Token"); t != "" {
		return t
	}
	if t := r.URL.Query().Get("t"); t != "" {
		return t
	}
	if c, err := r.Cookie("ld_t"); err == nil {
		return c.Value
	}
	return ""
}

func (a *App) phoneAuthed(r *http.Request) bool {
	return tokenEqual(requestToken(r), a.currentToken())
}

func hostIsLocal(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// adminAuthed는 이 PC 안에서 LocalDrop 창이 보낸 요청인지 확인합니다.
// 다른 웹사이트가 몰래 보내는 요청(CSRF, DNS 리바인딩)을 막기 위해 주소·Host·비밀 코드를 모두 봅니다.
func (a *App) adminAuthed(r *http.Request, requireHeader bool) bool {
	if !isLoopbackAddr(r.RemoteAddr) || !hostIsLocal(r.Host) {
		return false
	}
	if t := r.Header.Get("X-LD-Admin"); t != "" {
		return tokenEqual(t, a.adminToken)
	}
	if requireHeader {
		return false
	}
	if c, err := r.Cookie("ld_a"); err == nil {
		return tokenEqual(c.Value, a.adminToken)
	}
	return false
}

func (a *App) phone(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.phoneAuthed(r) {
			jsonError(w, http.StatusUnauthorized, "연결 정보가 맞지 않아요. PC 화면의 QR 코드를 다시 찍어 주세요.")
			return
		}
		h(w, r)
	}
}

func (a *App) phoneMut(h http.HandlerFunc) http.HandlerFunc {
	return a.phone(func(w http.ResponseWriter, r *http.Request) {
		// 사용자 정의 헤더를 요구하면 다른 사이트가 폼으로 몰래 보내는 요청이 막힙니다.
		if r.Header.Get("X-LD") != "1" {
			jsonError(w, http.StatusForbidden, "잘못된 요청이에요")
			return
		}
		h(w, r)
	})
}

func (a *App) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.adminAuthed(r, false) {
			jsonError(w, http.StatusForbidden, "LocalDrop 창에서만 쓸 수 있어요")
			return
		}
		h(w, r)
	}
}

func (a *App) adminMut(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.adminAuthed(r, true) {
			jsonError(w, http.StatusForbidden, "LocalDrop 창에서만 쓸 수 있어요")
			return
		}
		h(w, r)
	}
}

// ---------- 공통 응답 ----------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readJSON(r *http.Request, limit int64, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(v)
}

func renderPage(w http.ResponseWriter, name string, data any, status int) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", pageCSP)
	w.WriteHeader(status)
	if err := pageTmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("화면 그리기 오류 %s: %v", name, err)
	}
}

type noticeData struct {
	Title   string
	Message string
	Version string
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/static/")), "/")
	p := "web/static/" + name
	b, err := webFS.ReadFile(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		h.Set("Content-Type", t)
	}
	if et := staticETag[p]; et != "" {
		h.Set("ETag", et)
		if r.Header.Get("If-None-Match") == et {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	h.Set("Cache-Control", "public, max-age=604800")
	_, _ = w.Write(b)
}

// ---------- 폰 화면 ----------

type phonePage struct {
	Token   string
	PCName  string
	Version string
}

func (a *App) handlePhoneIndex(w http.ResponseWriter, r *http.Request) {
	tok := requestToken(r)
	if !tokenEqual(tok, a.currentToken()) {
		msg := "PC에서 LocalDrop을 켜고, 화면에 보이는 QR 코드를 휴대폰 카메라로 찍어 주세요."
		if tok != "" {
			msg = "이 연결 코드는 더 이상 쓸 수 없어요. PC의 LocalDrop 화면에 있는 QR 코드를 다시 찍어 주세요."
		}
		renderPage(w, "notice.html", noticeData{Title: "QR 코드를 찍어 주세요", Message: msg, Version: appVersion}, http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "ld_t", Value: tok, Path: "/", MaxAge: 400 * 24 * 3600,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	a.mu.Lock()
	pcName := a.cfg.PCName
	a.mu.Unlock()
	renderPage(w, "phone.html", phonePage{Token: tok, PCName: pcName, Version: appVersion}, http.StatusOK)
}

func (a *App) handleManifest(w http.ResponseWriter, r *http.Request) {
	start := "/"
	if tok := requestToken(r); tokenEqual(tok, a.currentToken()) {
		start = "/?t=" + url.QueryEscape(tok)
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name": "LocalDrop", "short_name": "LocalDrop", "start_url": start, "display": "browser",
		"background_color": "#EDF1EF", "theme_color": "#0B7A57", "lang": "ko",
		"icons": []map[string]string{
			{"src": "/static/icon-192.png", "sizes": "192x192", "type": "image/png"},
			{"src": "/static/icon-512.png", "sizes": "512x512", "type": "image/png"},
		},
	})
}

func deviceFromRequest(r *http.Request) (id, name string) {
	id = r.Header.Get("X-LD-Device-Id")
	name = r.Header.Get("X-LD-Device")
	if id == "" {
		id = r.URL.Query().Get("d")
	}
	if name == "" {
		name = r.URL.Query().Get("n")
	} else if n, err := url.QueryUnescape(name); err == nil {
		name = n
	}
	return cleanShort(id, 40), cleanShort(name, 40)
}

func cleanShort(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}

func cleanCID(s string) string {
	if len(s) > 64 {
		s = s[:64]
	}
	for _, c := range s {
		if !(c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return ""
		}
	}
	return s
}

func (a *App) handlePhoneState(w http.ResponseWriter, r *http.Request) {
	id, name := deviceFromRequest(r)
	a.touchDevice(id, name, clientIP(r.RemoteAddr), 0)
	writeJSON(w, a.phoneState())
}

func (a *App) phoneState() map[string]any {
	a.mu.Lock()
	pcName := a.cfg.PCName
	a.mu.Unlock()
	return map[string]any{"pcName": pcName, "version": appVersion, "items": a.itemViews(false)}
}

func (a *App) handlePhoneEvents(w http.ResponseWriter, r *http.Request) {
	id, name := deviceFromRequest(r)
	ip := clientIP(r.RemoteAddr)
	c := a.hub.add(false, requestToken(r))
	defer a.hub.remove(c)
	a.touchDevice(id, name, ip, +1)
	defer a.touchDevice(id, name, ip, -1)
	serveSSE(w, r, c, a.phoneState())
}

func (a *App) handlePhoneUpload(w http.ResponseWriter, r *http.Request) {
	id, name := deviceFromRequest(r)
	a.touchDevice(id, name, clientIP(r.RemoteAddr), 0)
	a.receiveFile(w, r, "phone", id, name)
}

func (a *App) handlePhoneText(w http.ResponseWriter, r *http.Request) {
	devID, devName := deviceFromRequest(r)
	a.touchDevice(devID, devName, clientIP(r.RemoteAddr), 0)
	var req struct {
		Text string `json:"text"`
		CID  string `json:"cid"`
	}
	if err := readJSON(r, 512<<10, &req); err != nil || strings.TrimSpace(req.Text) == "" {
		jsonError(w, http.StatusBadRequest, "보낼 글이 없어요")
		return
	}
	it := &Item{ID: newID(), From: "phone", DeviceID: devID, Device: devName, Kind: "text",
		Text: req.Text, Time: time.Now(), CID: cleanCID(req.CID)}
	a.addItem(it)
	a.mu.Lock()
	autoCopy := a.cfg.AutoCopy
	a.mu.Unlock()
	if autoCopy {
		go func() {
			if err := clipboard.WriteAll(req.Text); err != nil {
				log.Printf("클립보드 복사 실패: %v", err)
			}
		}()
	}
	a.notifier.textReceived(devName, autoCopy)
	writeJSON(w, a.view(it, false))
}

// ---------- 파일 받기 (폰 → PC, PC 화면 끌어다 놓기) ----------

type uploadState struct {
	ID       string `json:"id"`
	CID      string `json:"cid,omitempty"`
	Name     string `json:"name"`
	Total    int64  `json:"total"`
	Received int64  `json:"received"`
	Device   string `json:"device,omitempty"`
	DeviceID string `json:"deviceId,omitempty"`
	Speed    int64  `json:"speed"`
	started  time.Time
	got      *atomic.Int64
}

type countingReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

type writerOnly struct{ io.Writer }

func (a *App) receiveFile(w http.ResponseWriter, r *http.Request, from, devID, devName string) {
	q := r.URL.Query()
	name := sanitizeFileName(q.Get("name"))
	size, _ := strconv.ParseInt(q.Get("size"), 10, 64)
	if size <= 0 && r.ContentLength > 0 {
		size = r.ContentLength
	}
	mtimeMs, _ := strconv.ParseInt(q.Get("mtime"), 10, 64)
	cid := cleanCID(q.Get("cid"))

	dir := a.saveDir()
	if from == "pc" {
		dir = a.outboxDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		jsonError(w, http.StatusInternalServerError, "저장 폴더를 만들 수 없어요: "+err.Error())
		return
	}
	tmp, err := os.CreateTemp(dir, partialPrefix+"*.part")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "저장 폴더에 쓸 수 없어요: "+err.Error())
		return
	}

	up := &uploadState{ID: newID(), CID: cid, Name: name, Total: size, Device: devName, DeviceID: devID,
		started: time.Now(), got: new(atomic.Int64)}
	track := from == "phone"
	stop := make(chan struct{})
	if track {
		a.mu.Lock()
		a.uploads[up.ID] = up
		a.mu.Unlock()
		go a.reportProgress(up, stop)
	}

	buf := make([]byte, 1<<20)
	n, copyErr := io.CopyBuffer(writerOnly{tmp}, &countingReader{r: r.Body, n: up.got}, buf)
	closeErr := tmp.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil && size > 0 && n != size {
		copyErr = fmt.Errorf("%s 중 %s만 받았어요", humanSize(size), humanSize(n))
	}

	var final string
	if copyErr == nil {
		final, copyErr = placeFile(tmp.Name(), dir, name)
	}
	if track {
		close(stop)
		a.mu.Lock()
		delete(a.uploads, up.ID)
		a.mu.Unlock()
		a.hub.send("upload-end", map[string]any{"id": up.ID, "ok": copyErr == nil}, true, false)
	}
	if copyErr != nil {
		_ = os.Remove(tmp.Name())
		log.Printf("받기 실패 %q: %v", name, copyErr)
		jsonError(w, http.StatusBadRequest, "파일을 다 받지 못했어요: "+copyErr.Error())
		return
	}
	if mtimeMs > 0 {
		t := time.UnixMilli(mtimeMs)
		if t.Year() > 1980 && t.Before(time.Now().Add(48*time.Hour)) {
			_ = os.Chtimes(final, t, t)
		}
	}
	it := &Item{ID: newID(), From: from, DeviceID: devID, Device: devName, Kind: "file",
		Name: filepath.Base(final), Size: n, Mime: mimeByName(final), Time: time.Now(),
		CID: cid, Path: final, Outbox: from == "pc"}
	a.addItem(it)
	if from == "phone" {
		log.Printf("받음: %s (%s) ← %s", it.Name, humanSize(n), devName)
		a.notifier.fileReceived(devName)
	}
	writeJSON(w, a.view(it, from == "pc"))
}

func (a *App) reportProgress(up *uploadState, stop chan struct{}) {
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	var lastBytes int64
	lastAt := time.Now()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			got := up.got.Load()
			dt := now.Sub(lastAt).Seconds()
			speed := int64(0)
			if dt > 0 {
				speed = int64(float64(got-lastBytes) / dt)
			}
			lastBytes, lastAt = got, now
			a.mu.Lock()
			up.Received = got
			up.Speed = speed
			snapshot := uploadState{ID: up.ID, CID: up.CID, Name: up.Name, Total: up.Total, Received: got,
				Device: up.Device, DeviceID: up.DeviceID, Speed: speed}
			a.mu.Unlock()
			a.hub.send("progress", &snapshot, true, false)
		}
	}
}

func (a *App) uploadList() []uploadState {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]uploadState, 0, len(a.uploads))
	for _, u := range a.uploads {
		c := uploadState{ID: u.ID, CID: u.CID, Name: u.Name, Total: u.Total, Received: u.got.Load(),
			Device: u.Device, DeviceID: u.DeviceID, Speed: u.Speed}
		out = append(out, c)
	}
	return out
}

// ---------- 파일 보내기 (다운로드) ----------

func (a *App) handleFile(w http.ResponseWriter, r *http.Request) {
	admin := a.adminAuthed(r, false)
	if !admin && !a.phoneAuthed(r) {
		http.Error(w, "QR 코드를 다시 찍어 주세요", http.StatusUnauthorized)
		return
	}
	it := a.getItem(r.PathValue("id"))
	if it == nil || it.Kind != "file" {
		http.Error(w, "목록에 없는 파일이에요", http.StatusNotFound)
		return
	}
	if it.Expired {
		http.Error(w, "프로그램을 다시 켜서 만료된 파일이에요. PC에서 다시 보내 주세요.", http.StatusGone)
		return
	}
	if !admin {
		id, name := deviceFromRequest(r)
		a.touchDevice(id, name, clientIP(r.RemoteAddr), 0)
	}
	if it.IsDir {
		serveFolderZip(w, it.Path, strings.TrimSuffix(it.Name, ".zip"))
		return
	}
	f, err := os.Open(it.Path)
	if err != nil {
		http.Error(w, "PC에서 파일을 찾을 수 없어요 (옮겼거나 지웠을 수 있어요)", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.Error(w, "파일을 열 수 없어요", http.StatusNotFound)
		return
	}
	ctype := mimeByName(it.Name)
	disp := "attachment"
	if r.URL.Query().Get("inline") == "1" && inlineSafe(ctype) {
		disp = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", contentDisposition(disp, it.Name))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'unsafe-inline'")
	h.Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// ---------- PC 화면 ----------

type pcPage struct {
	Admin   string
	Version string
}

func (a *App) handlePCIndex(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackAddr(r.RemoteAddr) || !hostIsLocal(r.Host) {
		renderPage(w, "notice.html", noticeData{Title: "PC에서만 열 수 있어요",
			Message: "이 화면은 LocalDrop이 실행 중인 PC에서만 열려요.", Version: appVersion}, http.StatusForbidden)
		return
	}
	if q := r.URL.Query().Get("a"); q != "" {
		if tokenEqual(q, a.adminToken) {
			http.SetCookie(w, &http.Cookie{Name: "ld_a", Value: q, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/pc", http.StatusSeeOther)
			return
		}
	}
	if c, err := r.Cookie("ld_a"); err != nil || !tokenEqual(c.Value, a.adminToken) {
		renderPage(w, "notice.html", noticeData{Title: "LocalDrop 창에서 열어 주세요",
			Message: "주소가 바뀌었어요. 작업 표시줄이나 시작 메뉴에서 LocalDrop을 다시 실행하면 이 화면이 열려요.", Version: appVersion}, http.StatusForbidden)
		return
	}
	renderPage(w, "pc.html", pcPage{Admin: a.adminToken, Version: appVersion}, http.StatusOK)
}

type pcSettings struct {
	SaveDir   string `json:"saveDir"`
	PCName    string `json:"pcName"`
	AutoCopy  bool   `json:"autoCopy"`
	Notify    bool   `json:"notify"`
	SendTo    bool   `json:"sendTo"`
	AutoStart bool   `json:"autoStart"`
}

func (a *App) pcState() map[string]any {
	a.mu.Lock()
	s := pcSettings{SaveDir: a.cfg.SaveDir, PCName: a.cfg.PCName, AutoCopy: a.cfg.AutoCopy, Notify: a.cfg.Notify}
	a.mu.Unlock()
	s.SendTo = sendToEnabled()
	s.AutoStart = autoStartEnabled()
	return map[string]any{
		"app": appName, "version": appVersion, "platform": runtime.GOOS, "port": a.port,
		"addresses": a.addresses(), "settings": s, "items": a.itemViews(true),
		"devices": a.deviceList(), "uploads": a.uploadList(), "browserMode": a.isBrowserUI(),
		"features": map[string]bool{
			"pickFiles": dialogsSupported(), "pickFolder": dialogsSupported(),
			"sendTo": sendToSupported(), "firewall": firewallSupported(), "autoStart": autoStartSupported(),
		},
	}
}

func (a *App) handlePCState(w http.ResponseWriter, r *http.Request) {
	a.markUIWorking()
	writeJSON(w, a.pcState())
}

func (a *App) handlePCEvents(w http.ResponseWriter, r *http.Request) {
	c := a.hub.add(true, "")
	defer a.hub.remove(c)
	serveSSE(w, r, c, map[string]any{"version": appVersion})
}

func (a *App) handleQR(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	ok := false
	for _, ad := range lanAddresses() {
		if ad.IP == ip {
			ok = true
			break
		}
	}
	if !ok {
		http.Error(w, "unknown address", http.StatusBadRequest)
		return
	}
	svg, err := qrSVG(a.phoneURL(ip))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, svg)
}

func (a *App) handlePCShareUpload(w http.ResponseWriter, r *http.Request) {
	a.receiveFile(w, r, "pc", "", "")
}

func (a *App) handlePCPick(w http.ResponseWriter, r *http.Request) {
	paths, err := pickFiles(a.ownerWindow())
	switch {
	case errors.Is(err, errCanceled):
		writeJSON(w, map[string]any{"items": []ItemView{}})
		return
	case err != nil:
		jsonError(w, http.StatusNotImplemented, "파일 선택 창을 열 수 없어요")
		return
	}
	writeJSON(w, map[string]any{"items": a.sharePaths(paths)})
}

// sharePaths는 PC의 파일을 복사하지 않고 그 자리에서 폰에 공유합니다.
func (a *App) sharePaths(paths []string) []ItemView {
	out := []ItemView{}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		st, err := os.Stat(abs)
		if err != nil {
			log.Printf("공유할 파일을 찾지 못함: %s (%v)", abs, err)
			continue
		}
		it := &Item{ID: newID(), From: "pc", Kind: "file", Name: filepath.Base(abs), Path: abs, Time: time.Now()}
		if st.IsDir() {
			it.IsDir = true
			it.Name = filepath.Base(abs) + ".zip"
			it.Size = folderSize(abs)
			it.Mime = "application/zip"
		} else {
			it.Size = st.Size()
			it.Mime = mimeByName(abs)
		}
		a.addItem(it)
		out = append(out, a.view(it, true))
	}
	return out
}

func (a *App) handlePCText(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
		CID  string `json:"cid"`
	}
	if err := readJSON(r, 512<<10, &req); err != nil || strings.TrimSpace(req.Text) == "" {
		jsonError(w, http.StatusBadRequest, "보낼 글이 없어요")
		return
	}
	it := &Item{ID: newID(), From: "pc", Kind: "text", Text: req.Text, Time: time.Now(), CID: cleanCID(req.CID)}
	a.addItem(it)
	writeJSON(w, a.view(it, true))
}

func (a *App) handlePCOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID  string `json:"id"`
		How string `json:"how"` // open, reveal, savedir
	}
	if err := readJSON(r, 4096, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "잘못된 요청이에요")
		return
	}
	if req.How == "savedir" {
		dir := a.saveDir()
		_ = os.MkdirAll(dir, 0o755)
		if err := openPath(dir); err != nil {
			jsonError(w, http.StatusInternalServerError, "폴더를 열 수 없어요")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	it := a.getItem(req.ID)
	if it == nil || it.Kind != "file" || it.Path == "" {
		jsonError(w, http.StatusNotFound, "목록에 없는 파일이에요")
		return
	}
	if !pathExists(it.Path) {
		jsonError(w, http.StatusNotFound, "파일이 없어요. 옮겼거나 지웠을 수 있어요.")
		return
	}
	var err error
	if req.How == "reveal" {
		err = revealPath(it.Path)
	} else {
		err = openPath(it.Path)
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "열 수 없어요: "+err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handlePCRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, 4096, &req); err != nil || !a.removeItem(req.ID) {
		jsonError(w, http.StatusNotFound, "목록에 없어요")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handlePCClear(w http.ResponseWriter, r *http.Request) {
	a.clearItems()
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handlePCSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SaveDir   *string `json:"saveDir"`
		PCName    *string `json:"pcName"`
		AutoCopy  *bool   `json:"autoCopy"`
		Notify    *bool   `json:"notify"`
		SendTo    *bool   `json:"sendTo"`
		AutoStart *bool   `json:"autoStart"`
	}
	if err := readJSON(r, 64<<10, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "잘못된 요청이에요")
		return
	}
	if req.SaveDir != nil {
		dir := strings.TrimSpace(*req.SaveDir)
		if dir == "" || !filepath.IsAbs(dir) {
			jsonError(w, http.StatusBadRequest, "폴더 경로를 전체 경로로 적어 주세요 (예: D:\\받은파일)")
			return
		}
		if err := checkWritableDir(dir); err != nil {
			jsonError(w, http.StatusBadRequest, "그 폴더에는 저장할 수 없어요: "+err.Error())
			return
		}
		a.mu.Lock()
		a.cfg.SaveDir = filepath.Clean(dir)
		a.mu.Unlock()
	}
	if req.PCName != nil {
		name := cleanShort(*req.PCName, 40)
		if name == "" {
			name = defaultPCName()
		}
		a.mu.Lock()
		a.cfg.PCName = name
		a.mu.Unlock()
		a.hub.send("pcname", map[string]string{"pcName": name}, false, true)
	}
	a.mu.Lock()
	if req.AutoCopy != nil {
		a.cfg.AutoCopy = *req.AutoCopy
	}
	if req.Notify != nil {
		a.cfg.Notify = *req.Notify
	}
	a.mu.Unlock()
	if req.SendTo != nil {
		if err := setSendTo(*req.SendTo); err != nil {
			jsonError(w, http.StatusInternalServerError, "보내기 메뉴를 바꾸지 못했어요: "+err.Error())
			return
		}
	}
	if req.AutoStart != nil {
		if err := setAutoStart(*req.AutoStart); err != nil {
			jsonError(w, http.StatusInternalServerError, "자동 실행 설정을 바꾸지 못했어요: "+err.Error())
			return
		}
	}
	if (req.SendTo != nil && *req.SendTo) || (req.AutoStart != nil && *req.AutoStart) {
		a.rememberLinkExe()
	}
	a.saveConfig()
	st := a.pcState()
	a.hub.send("settings", st["settings"], true, false)
	writeJSON(w, st)
}

func checkWritableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, partialPrefix+"check-*.part")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func (a *App) handlePCPickFolder(w http.ResponseWriter, r *http.Request) {
	dir, err := pickFolder(a.ownerWindow(), a.saveDir())
	switch {
	case errors.Is(err, errCanceled):
		writeJSON(w, map[string]any{"canceled": true})
		return
	case err != nil:
		jsonError(w, http.StatusNotImplemented, "폴더 선택 창을 열 수 없어요. 경로를 직접 적어 주세요.")
		return
	}
	writeJSON(w, map[string]any{"path": dir})
}

func (a *App) handlePCReset(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.cfg.Token = randomToken(12)
	tok := a.cfg.Token
	a.devices = map[string]*Device{}
	a.mu.Unlock()
	a.saveConfig()
	a.writeInstanceFile()
	a.hub.kickPhones(tok)
	a.hub.send("devices", a.deviceList(), true, false)
	log.Printf("연결을 초기화했어요 (새 QR)")
	writeJSON(w, a.pcState())
}

func (a *App) handlePCFirewall(w http.ResponseWriter, r *http.Request) {
	if err := fixFirewall(a.ownerWindow()); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handlePCCopy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, 512<<10, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "잘못된 요청이에요")
		return
	}
	if err := clipboard.WriteAll(req.Text); err != nil {
		jsonError(w, http.StatusInternalServerError, "복사하지 못했어요")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handlePCQuit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
	go func() {
		time.Sleep(300 * time.Millisecond)
		a.requestQuit()
	}()
}

func (a *App) ownerWindow() uintptr {
	if h := a.hooks(); h.owner != nil {
		return h.owner()
	}
	return 0
}

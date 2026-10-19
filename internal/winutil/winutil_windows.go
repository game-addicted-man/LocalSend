//go:build windows

package winutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const createNoWindow = 0x08000000

// ---------- 폴더 위치 ----------

func knownFolder(id *windows.KNOWNFOLDERID, fallback string) string {
	if p, err := windows.KnownFolderPath(id, 0); err == nil && p != "" {
		return p
	}
	return fallback
}

// ProgramsDir은 시작 메뉴의 프로그램 폴더입니다 (%APPDATA%\Microsoft\Windows\Start Menu\Programs).
func ProgramsDir() string {
	return knownFolder(windows.FOLDERID_Programs,
		filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`))
}

// DesktopDir은 바탕화면 폴더입니다 (OneDrive로 옮긴 경우도 따라갑니다).
func DesktopDir() string {
	home, _ := os.UserHomeDir()
	return knownFolder(windows.FOLDERID_Desktop, filepath.Join(home, "Desktop"))
}

// StartupDir은 로그인할 때 자동으로 실행되는 시작프로그램 폴더입니다.
func StartupDir() string {
	return knownFolder(windows.FOLDERID_Startup,
		filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Startup`))
}

// SendToDir은 탐색기 오른쪽 클릭 '보내기' 메뉴 폴더입니다.
func SendToDir() string {
	return knownFolder(windows.FOLDERID_SendTo, filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\SendTo`))
}

// InstallDir은 설치 위치입니다 (%LOCALAPPDATA%\Programs\LocalDrop, 관리자 권한 없이 쓸 수 있는 곳).
func InstallDir() string {
	base := knownFolder(windows.FOLDERID_UserProgramFiles, filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"))
	return filepath.Join(base, AppName)
}

// DataDir은 설정·기록 폴더(%APPDATA%\LocalDrop), CacheDir은 앱 창 캐시 폴더(%LOCALAPPDATA%\LocalDrop)입니다.
func DataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.Getenv("APPDATA")
	}
	return filepath.Join(base, AppName)
}

func CacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.Getenv("LOCALAPPDATA")
	}
	return filepath.Join(base, AppName)
}

// SamePath는 대소문자·끝의 \ 를 무시하고 같은 경로인지 봅니다.
func SamePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// IsWithin은 p가 dir 안(또는 dir 자체)에 있는지 봅니다.
func IsWithin(p, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(p))
	if err != nil {
		return false
	}
	rel = strings.ToLower(rel)
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, `..\`) && !filepath.IsAbs(rel))
}

// ---------- 바로가기 ----------

// Shortcut은 만들 바로가기(.lnk) 하나입니다.
type Shortcut struct {
	Link   string `json:"link"`
	Target string `json:"target"`
	Args   string `json:"args"`
	Desc   string `json:"desc"`
}

// MakeShortcuts는 바로가기를 만듭니다. Windows 바로가기 API로 직접 만들고,
// 그게 안 되는 드문 경우에만 PowerShell(WScript.Shell)로 다시 시도해요.
func MakeShortcuts(items ...Shortcut) error {
	var retry []Shortcut
	for _, it := range items {
		_ = os.MkdirAll(filepath.Dir(it.Link), 0o755)
		if err := makeShortcutNative(it); err != nil || !fileExists(it.Link) {
			retry = append(retry, it)
		}
	}
	return makeShortcutsPowerShell(retry...)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// makeShortcutsPowerShell은 예비 방법입니다. 경로에 한글·공백이 있어도 깨지지 않게 값은 JSON 환경 변수로 넘겨요.
// (Windows PowerShell 5.1의 ConvertFrom-Json은 배열을 한 덩어리로 내보내서, @( ) 대신 ( )로 감싸야 하나씩 돌아요)
func makeShortcutsPowerShell(items ...Shortcut) error {
	if len(items) == 0 {
		return nil
	}
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	script := `$ErrorActionPreference='Stop';$sh=New-Object -ComObject WScript.Shell;` +
		`foreach($i in ($env:LD_SHORTCUTS|ConvertFrom-Json)){` +
		`$s=$sh.CreateShortcut($i.link);$s.TargetPath=$i.target;$s.Arguments=$i.args;` +
		`$s.WorkingDirectory=(Split-Path -Parent $i.target);$s.IconLocation=$i.target+',0';` +
		`$s.Description=$i.desc;$s.Save()}`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "LD_SHORTCUTS="+string(data))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
	}
	for _, it := range items {
		if _, err := os.Stat(it.Link); err != nil {
			return fmt.Errorf("바로가기를 만들지 못했어요: %s", it.Link)
		}
	}
	return nil
}

// RemoveFile은 파일을 지우고, 원래 없었으면 성공으로 칩니다.
func RemoveFile(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ---------- 설치된 앱 목록 (설정 → 앱) ----------

// UninstallInfo는 Windows '설치된 앱' 목록에 보이는 정보입니다.
type UninstallInfo struct {
	Version   string
	ExePath   string
	Dir       string
	SizeBytes int64
	URL       string
}

// RegisterUninstall은 현재 사용자 기준으로 '설치된 앱' 목록에 등록합니다 (관리자 권한 불필요).
func RegisterUninstall(info UninstallInfo) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, UninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	quoted := `"` + info.ExePath + `"`
	strs := map[string]string{
		"DisplayName":          AppName,
		"DisplayVersion":       info.Version,
		"Publisher":            AppName,
		"DisplayIcon":          info.ExePath + ",0",
		"InstallLocation":      info.Dir,
		"UninstallString":      quoted + " --uninstall",
		"QuietUninstallString": quoted + " --uninstall --quiet",
		"URLInfoAbout":         info.URL,
		"InstallDate":          time.Now().Format("20060102"),
	}
	for name, v := range strs {
		if err := k.SetStringValue(name, v); err != nil {
			return err
		}
	}
	for name, v := range map[string]uint32{"NoModify": 1, "NoRepair": 1, "EstimatedSize": uint32(info.SizeBytes / 1024)} {
		if err := k.SetDWordValue(name, v); err != nil {
			return err
		}
	}
	return nil
}

// UnregisterUninstall은 '설치된 앱' 목록에서 뺍니다.
func UnregisterUninstall() error {
	err := registry.DeleteKey(registry.CURRENT_USER, UninstallKey)
	if err != nil && !errors.Is(err, registry.ErrNotExist) && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return err
	}
	return nil
}

// ---------- 실행 중인 LocalDrop 끄기 ----------

// QuitResult는 RequestQuit의 결과입니다.
type QuitResult int

const (
	NotRunning   QuitResult = iota // 켜져 있지 않았어요
	Quit                           // 요청대로 꺼졌어요
	StillRunning                   // 켜져 있는데 꺼지지 않았어요 (예전 버전이거나 응답 없음) → Terminate로 끄세요
)

// RequestQuit는 dataDir의 instance.json을 보고 켜져 있는 LocalDrop에 종료를 요청한 뒤,
// 그 프로세스가 끝날 때까지 최대 wait만큼 기다립니다.
func RequestQuit(dataDir string, wait time.Duration) (QuitResult, int) {
	b, err := os.ReadFile(filepath.Join(dataDir, InstanceFile))
	if err != nil {
		return NotRunning, 0
	}
	var inf struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
		PID   int    `json:"pid"`
	}
	if json.Unmarshal(b, &inf) != nil || inf.Port == 0 || inf.Token == "" {
		return NotRunning, 0
	}
	var h windows.Handle
	if inf.PID > 0 {
		h, _ = windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(inf.PID))
	}
	if h != 0 {
		defer windows.CloseHandle(h)
	}
	body, _ := json.Marshal(map[string]any{"token": inf.Token, "quit": true})
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/api/instance", inf.Port), "application/json", bytes.NewReader(body))
	if err != nil {
		return NotRunning, 0 // instance.json이 남아 있을 뿐 실제로는 꺼져 있어요
	}
	var ack struct {
		Quitting bool `json:"quitting"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ack)
	resp.Body.Close()
	if !ack.Quitting {
		// 1.0.0처럼 종료 요청을 모르는 예전 버전은 창만 앞으로 나오고 꺼지지 않아요.
		return StillRunning, inf.PID
	}
	if h == 0 {
		time.Sleep(1500 * time.Millisecond)
		return Quit, inf.PID
	}
	if ev, _ := windows.WaitForSingleObject(h, uint32(wait/time.Millisecond)); ev == windows.WAIT_OBJECT_0 {
		return Quit, inf.PID
	}
	return StillRunning, inf.PID // 끄겠다고 했지만 시간 안에 안 꺼졌어요
}

// Terminate는 응답하지 않는 LocalDrop 프로세스를 강제로 끕니다.
func Terminate(pid int) error {
	if pid <= 0 {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return err
	}
	_, _ = windows.WaitForSingleObject(h, 5000)
	return nil
}

// ---------- 지우기 ----------

// SafeAppDir은 지워도 되는 LocalDrop 전용 폴더인지 확인합니다:
// 절대 경로이고, 마지막 폴더 이름이 LocalDrop이어야 해요 (환경 변수가 비어 있는 등 이상한 경우를 막기 위해).
func SafeAppDir(d string) bool {
	return d != "" && filepath.IsAbs(d) && strings.EqualFold(filepath.Base(filepath.Clean(d)), AppName) &&
		!strings.ContainsAny(d, `"%&|<>^!`)
}

// CleanupLater는 이 프로그램이 끝난 뒤 폴더들을 지우고, 설치 폴더가 사라지면
// '설치된 앱' 목록에서도 뺍니다. 실행 중인 exe가 들어 있는 폴더는 바로 지울 수 없어서
// 숨은 cmd가 1초 간격으로 최대 20번 다시 시도해요. 안전하지 않은 경로는 돌려줍니다.
func CleanupLater(dirs []string, installDir string) (skipped []string, err error) {
	var ok []string
	for _, d := range dirs {
		if SafeAppDir(d) {
			ok = append(ok, filepath.Clean(d))
		} else {
			skipped = append(skipped, d)
		}
	}
	if len(ok) == 0 && installDir == "" {
		return skipped, nil
	}
	var body strings.Builder
	for _, d := range ok {
		fmt.Fprintf(&body, `(if exist "%[1]s" rmdir /s /q "%[1]s") & `, d)
	}
	body.WriteString(`ver >nul`)
	// 바깥 괄호로 for 전체를 묶어야 뒤의 & 명령이 반복마다가 아니라 한 번만 실행돼요.
	line := fmt.Sprintf(`cmd.exe /d /c (for /l %%n in (1,1,20) do @(ping -n 2 127.0.0.1 >nul & %s))`, body.String())
	if installDir != "" && SafeAppDir(installDir) {
		line += fmt.Sprintf(` & if not exist "%s" reg delete "HKCU\%s" /f >nul 2>&1`, filepath.Clean(installDir), UninstallKey)
	}
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line, HideWindow: true, CreationFlags: createNoWindow}
	cmd.Dir = os.TempDir() // 지울 폴더 안에서 실행하면 안 지워져요
	return skipped, cmd.Start()
}

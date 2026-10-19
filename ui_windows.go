//go:build windows

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/game-addicted-man/LocalSend/internal/winutil"
	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"github.com/ncruces/zenity"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	dwmapi  = windows.NewLazySystemDLL("dwmapi.dll")

	procShowWindow               = user32.NewProc("ShowWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	procGetDpiForSystem          = user32.NewProc("GetDpiForSystem")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procMessageBoxW              = user32.NewProc("MessageBoxW")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procShellExecuteW            = shell32.NewProc("ShellExecuteW")
	procShellExecuteExW          = shell32.NewProc("ShellExecuteExW")
	procDwmSetWindowAttribute    = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	swHide        = 0
	swShowNormal  = 1
	swShow        = 5
	swMinimize    = 6
	swRestore     = 9
	createNoWin   = 0x08000000
	smCxScreen    = 0
	smCyScreen    = 1
	mbIconError   = 0x10
	mbSetForegrnd = 0x10000
)

func runUI(app *App, pcURL string, forceBrowser bool) {
	if pathExists(app.uiMarkerPath()) {
		log.Printf("지난번에 앱 창을 여는 중 멈춰서 이번에는 브라우저로 엽니다")
		app.markUIWorking()
		forceBrowser = true
	}
	if !forceBrowser {
		ver, err := webviewloader.GetInstalledVersion()
		if err == nil && ver != "" {
			log.Printf("WebView2 %s 로 앱 창을 엽니다", ver)
			if runWebView(app, pcURL) {
				return
			}
			log.Printf("앱 창을 만들지 못해 브라우저로 엽니다")
		} else {
			log.Printf("WebView2 런타임이 없어 브라우저로 엽니다 (%v)", err)
		}
	}
	app.setBrowserUI(true)
	if *flagMinimized {
		<-app.quit // 자동 실행일 때는 브라우저를 띄우지 않고 조용히 기다려요 (exe를 다시 실행하면 열려요)
		return
	}
	if err := openBrowserUI(pcURL); err != nil {
		showErrorBox("브라우저를 열지 못했어요. 브라우저 주소창에 아래 주소를 넣어 주세요.\n\n" + pcURL)
	}
	<-app.quit
}

// openBrowserUI는 Edge의 앱 창 모드(주소창 없는 창)로 열고, 안 되면 기본 브라우저로 엽니다.
func openBrowserUI(u string) error {
	if err := shellExecute(0, "open", "msedge.exe", "--app="+u, swShowNormal); err == nil {
		return nil
	}
	return openURL(u)
}

func runWebView(app *App, pcURL string) bool {
	cacheBase, err := os.UserCacheDir()
	if err != nil {
		cacheBase = app.dataDir
	}
	_ = os.WriteFile(app.uiMarkerPath(), []byte(time.Now().Format(time.RFC3339)), 0o644)
	scale := systemScale()
	sw, sh := screenSize()
	width := clampDim(1120*scale, sw, 0.94)
	height := clampDim(780*scale, sh, 0.88)
	// 가운데 정렬은 화면 크기를 알고 창이 화면보다 작을 때만 (go-webview2가 부호 없는 정수로 계산해서
	// 창이 더 크면 위치가 넘쳐 화면 밖으로 나가요). 아니면 Windows가 위치를 정하게 둡니다.
	center := sw > 0 && sh > 0 && width < sw && height < sh

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  filepath.Join(cacheBase, appName, "WebView2"),
		WindowOptions: webview2.WindowOptions{
			Title:  "LocalDrop",
			Width:  uint(width),
			Height: uint(height),
			IconId: 1, // winres.json의 아이콘 "#1"
			Center: center,
		},
	})
	if w == nil {
		hideStrayWindow()
		app.markUIWorking()
		return false
	}
	hwnd := uintptr(w.Window())
	applyDarkTitleBar(hwnd)
	if *flagMinimized {
		procShowWindow.Call(hwnd, swMinimize)
	}
	w.SetSize(min(int(820*scale), width), min(int(560*scale), height), webview2.HintMin)

	app.setHooks(uiHooks{
		show:         func() { w.Dispatch(func() { bringToFront(hwnd) }) },
		isForeground: func() bool { fg, _, _ := procGetForegroundWindow.Call(); return fg == hwnd },
		owner:        func() uintptr { return hwnd },
	})
	go func() {
		<-app.quit
		w.Dispatch(func() { w.Destroy() })
	}()
	w.Navigate(pcURL)
	w.Run()
	app.setHooks(uiHooks{})
	app.requestQuit()
	return true
}

func clampDim(want float64, screen int, frac float64) int {
	if screen > 0 {
		if max := float64(screen) * frac; want > max {
			want = max
		}
	}
	if want < 400 {
		want = 400
	}
	return int(want)
}

func systemScale() float64 {
	if procGetDpiForSystem.Find() == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi >= 96 {
			return float64(dpi) / 96
		}
	}
	return 1
}

func screenSize() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(smCxScreen)
	h, _, _ := procGetSystemMetrics.Call(smCyScreen)
	return int(w), int(h)
}

// WebView2를 붙이지 못하면 빈 창이 남을 수 있어 숨깁니다.
func hideStrayWindow() {
	cls, _ := windows.UTF16PtrFromString("webview")
	title, _ := windows.UTF16PtrFromString("LocalDrop")
	if hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title))); hwnd != 0 {
		procShowWindow.Call(hwnd, swHide)
	}
}

func bringToFront(hwnd uintptr) {
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		procShowWindow.Call(hwnd, swRestore)
	} else {
		procShowWindow.Call(hwnd, swShow)
	}
	procSetForegroundWindow.Call(hwnd)
}

func allowForegroundFor(pid int) {
	if pid > 0 {
		procAllowSetForegroundWindow.Call(uintptr(pid))
	}
}

// 윈도우가 어두운 테마면 제목 표시줄도 어둡게 맞춥니다.
func applyDarkTitleBar(hwnd uintptr) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil || v != 0 || procDwmSetWindowAttribute.Find() != nil {
		return
	}
	on := int32(1)
	for _, attr := range []uintptr{20, 19} { // DWMWA_USE_IMMERSIVE_DARK_MODE (구버전 19)
		if r, _, _ := procDwmSetWindowAttribute.Call(hwnd, attr, uintptr(unsafe.Pointer(&on)), 4); r == 0 {
			return
		}
	}
}

func showErrorBox(msg string) {
	text, _ := windows.UTF16PtrFromString(msg)
	title, _ := windows.UTF16PtrFromString("LocalDrop")
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconError|mbSetForegrnd)
}

func platformDownloadsDir() string {
	p, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
	if err != nil {
		return ""
	}
	return p
}

func defaultUserName() string { return os.Getenv("USERNAME") }

// withCOM은 셸 기능(ShellExecute)이 쓰는 COM을 이 스레드에 준비한 뒤 f를 실행합니다.
// (Microsoft 문서: ShellExecute 전에 COINIT_APARTMENTTHREADED로 CoInitializeEx를 부르라고 권장)
func withCOM(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
	if err == nil || err == syscall.Errno(1) { // S_OK 또는 S_FALSE(이미 초기화됨) → 짝을 맞춰 해제
		defer windows.CoUninitialize()
	}
	f()
}

func shellExecute(owner uintptr, verb, file, params string, show int) (err error) {
	withCOM(func() { err = shellExecuteRaw(owner, verb, file, params, show) })
	return err
}

func shellExecuteRaw(owner uintptr, verb, file, params string, show int) error {
	v, _ := windows.UTF16PtrFromString(verb)
	f, _ := windows.UTF16PtrFromString(file)
	var p *uint16
	if params != "" {
		p, _ = windows.UTF16PtrFromString(params)
	}
	r, _, _ := procShellExecuteW.Call(owner, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(p)), 0, uintptr(show))
	if r <= 32 {
		return fmt.Errorf("ShellExecute 오류 %d", r)
	}
	return nil
}

func openPath(p string) error { return shellExecute(0, "open", p, "", swShowNormal) }

func openURL(u string) error { return shellExecute(0, "open", u, "", swShowNormal) }

func revealPath(p string) error {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: fmt.Sprintf(`explorer.exe /select,"%s"`, p)}
	return cmd.Start() // explorer는 성공해도 종료 코드 1을 돌려줘서 결과를 기다리지 않아요
}

func dialogsSupported() bool { return true }

func dialogOpts(owner uintptr, title string) []zenity.Option {
	opts := []zenity.Option{zenity.Title(title)}
	if owner != 0 {
		opts = append(opts, zenity.Attach(owner))
	}
	return opts
}

func pickFiles(owner uintptr) ([]string, error) {
	paths, err := zenity.SelectFileMultiple(dialogOpts(owner, "폰으로 보낼 파일 선택")...)
	if errors.Is(err, zenity.ErrCanceled) {
		return nil, errCanceled
	}
	return paths, err
}

func pickFolder(owner uintptr, start string) (string, error) {
	opts := append(dialogOpts(owner, "받은 파일을 저장할 폴더 선택"), zenity.Directory())
	if start != "" {
		opts = append(opts, zenity.Filename(start+`\`))
	}
	dir, err := zenity.SelectFile(opts...)
	if errors.Is(err, zenity.ErrCanceled) {
		return "", errCanceled
	}
	return dir, err
}

func firewallSupported() bool { return true }

// SHELLEXECUTEINFOW (shellapi.h). 64비트에서 112바이트, 32비트에서 60바이트.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

// 구조체 크기가 Windows SDK와 다르면 컴파일이 실패하도록 확인합니다 (64비트 112, 32비트 60바이트).
const seiSize = 60 + 52*(^uintptr(0)>>63)

var (
	_ [unsafe.Sizeof(shellExecuteInfo{}) - seiSize]struct{}
	_ [seiSize - unsafe.Sizeof(shellExecuteInfo{})]struct{}
)

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	errorCancelled        = syscall.Errno(1223)
)

// runElevated는 관리자 권한(UAC 확인)으로 프로그램을 실행하고 끝날 때까지 기다려 종료 코드를 돌려줍니다.
func runElevated(owner uintptr, file, params string) (code uint32, err error) {
	withCOM(func() {
		verb, _ := windows.UTF16PtrFromString("runas")
		f, _ := windows.UTF16PtrFromString(file)
		p, _ := windows.UTF16PtrFromString(params)
		info := shellExecuteInfo{fMask: seeMaskNoCloseProcess | seeMaskNoAsync, hwnd: owner,
			lpVerb: verb, lpFile: f, lpParameters: p, nShow: swHide}
		info.cbSize = uint32(unsafe.Sizeof(info))
		r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
		if r == 0 {
			err = callErr
			return
		}
		if info.hProcess == 0 {
			return // 프로세스 핸들을 못 받으면 결과는 모르지만 실행은 됐어요
		}
		defer windows.CloseHandle(info.hProcess)
		if ev, werr := windows.WaitForSingleObject(info.hProcess, 60*1000); werr != nil || ev != windows.WAIT_OBJECT_0 {
			err = errors.New("방화벽 설정이 끝나기를 기다리다 멈췄어요")
			return
		}
		err = windows.GetExitCodeProcess(info.hProcess, &code)
	})
	return code, err
}

// fixFirewall은 관리자 권한으로 LocalDrop을 막는 방화벽 규칙을 지우고 허용 규칙을 추가합니다.
// (처음 뜬 방화벽 창에서 '취소'를 누르면 Windows가 차단 규칙을 만들어 두기 때문)
func fixFirewall(owner uintptr) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// cmd /c "A & B"의 종료 코드는 마지막 명령(B: 허용 규칙 추가)의 결과예요.
	params := fmt.Sprintf(`/c netsh advfirewall firewall delete rule name=all program="%[1]s" & `+
		`netsh advfirewall firewall add rule name="LocalDrop" dir=in action=allow program="%[1]s" enable=yes profile=any`, exe)
	code, err := runElevated(owner, "cmd.exe", params)
	switch {
	case errors.Is(err, errorCancelled):
		return errors.New("관리자 권한을 허용하지 않아 방화벽 설정을 바꾸지 못했어요")
	case err != nil:
		return fmt.Errorf("방화벽 설정을 바꾸지 못했어요 (%v)", err)
	case code != 0:
		return fmt.Errorf("방화벽 규칙을 추가하지 못했어요 (오류 코드 %d)", code)
	}
	log.Printf("방화벽 허용 규칙을 추가했어요")
	return nil
}

func sendToSupported() bool { return true }

func sendToLinkPath() string { return filepath.Join(winutil.SendToDir(), winutil.SendToLink) }

func sendToEnabled() bool { return pathExists(sendToLinkPath()) }

func setSendTo(on bool) error {
	if !on {
		return winutil.RemoveFile(sendToLinkPath())
	}
	return makeShortcut(sendToLinkPath(), "", "LocalDrop으로 폰에 보내기")
}

func autoStartSupported() bool { return true }

func autoStartLinkPath() string { return filepath.Join(winutil.StartupDir(), winutil.AutoStartLink) }

func autoStartEnabled() bool { return pathExists(autoStartLinkPath()) }

func setAutoStart(on bool) error {
	if !on {
		return winutil.RemoveFile(autoStartLinkPath())
	}
	return makeShortcut(autoStartLinkPath(), "--minimized", "LocalDrop (폰 ↔ PC 전송)")
}

// makeShortcut은 지금 실행 중인 exe를 가리키는 바로가기(.lnk)를 만듭니다.
func makeShortcut(link, args, desc string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return winutil.MakeShortcuts(winutil.Shortcut{Link: link, Target: exe, Args: args, Desc: desc})
}

// LocalDrop — 같은 와이파이에서 폰 ↔ PC 파일·텍스트 전송
//
// PC에서 실행하면 작은 웹 서버가 열리고, 폰은 QR 코드를 찍어 브라우저로 접속합니다.
// 폰에는 앱을 설치할 필요가 없고 계정도 필요 없습니다.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/game-addicted-man/LocalSend/internal/version"
)

const (
	appName     = "LocalDrop"
	appVersion  = version.App
	defaultPort = 53390
)

var (
	flagHeadless  = flag.Bool("headless", false, "창이나 브라우저를 열지 않고 서버만 실행합니다 (테스트용)")
	flagBrowser   = flag.Bool("browser", false, "앱 창 대신 기본 브라우저로 PC 화면을 엽니다")
	flagPort      = flag.Int("port", 0, "사용할 포트 (기본 53390, 사용 중이면 다음 번호)")
	flagData      = flag.String("data", "", "설정·기록을 저장할 폴더 (기본: 사용자 설정 폴더)")
	flagSaveDir   = flag.String("save", "", "받은 파일을 저장할 폴더 (설정보다 우선)")
	flagMinimized = flag.Bool("minimized", false, "작업 표시줄에 최소화한 채로 시작합니다 (Windows 자동 실행용)")
	flagUninstall = flag.Bool("uninstall", false, "LocalDrop을 제거합니다 (설정 → 앱에서 '제거'를 누르면 이렇게 실행돼요)")
	flagQuiet     = flag.Bool("quiet", false, "제거할 때 묻지 않습니다")
)

func init() {
	// Windows 앱 창(WebView2)은 처음 만든 OS 스레드에서 메시지 루프를 돌려야 합니다.
	runtime.LockOSThread()
}

func main() {
	flag.Parse()
	sharePaths := flag.Args() // 탐색기 '보내기' 메뉴나 exe에 끌어다 놓은 파일들

	if *flagUninstall {
		runUninstall(*flagQuiet)
		return
	}
	// 설치 프로그램이 업데이트하면서 남긴 예전 exe(LocalDrop.exe.old, .old-시각)를 정리합니다.
	if exe, err := os.Executable(); err == nil {
		if olds, _ := filepath.Glob(exe + ".old*"); len(olds) > 0 {
			for _, o := range olds {
				_ = os.Remove(o)
			}
		}
	}

	dataDir := *flagData
	if dataDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = "."
		}
		dataDir = filepath.Join(base, appName)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fatalf("설정 폴더를 만들 수 없어요:\n%s\n\n%v", dataDir, err)
	}
	logFile := setupLogging(dataDir)
	if logFile != nil {
		defer logFile.Close()
	}
	log.Printf("%s %s 시작 (%s/%s), 설정 폴더: %s", appName, appVersion, runtime.GOOS, runtime.GOARCH, dataDir)

	// 이미 실행 중이면 그 창을 앞으로 가져오고(파일이 있으면 넘기고) 종료합니다.
	if forwardToRunningInstance(dataDir, sharePaths) {
		log.Printf("이미 실행 중인 LocalDrop에 넘기고 종료합니다")
		return
	}

	app, err := newApp(dataDir)
	if err != nil {
		fatalf("시작하지 못했어요:\n%v", err)
	}
	if *flagSaveDir != "" {
		app.cfg.SaveDir = *flagSaveDir
	}
	if err := os.MkdirAll(app.saveDir(), 0o755); err != nil {
		log.Printf("저장 폴더를 만들 수 없음: %v", err)
	}
	cleanupPartials(app.saveDir())

	port := app.cfg.Port
	if *flagPort != 0 {
		port = *flagPort
	}
	ln, err := listenFrom(port)
	if err != nil {
		fatalf("네트워크 포트를 열 수 없어요:\n%v", err)
	}
	app.port = ln.Addr().(*net.TCPAddr).Port
	if app.port != app.cfg.Port && *flagPort == 0 {
		// 다음에도 같은 주소로 접속할 수 있게 실제로 연 포트를 기억합니다.
		app.cfg.Port = app.port
		app.saveConfig()
	}

	srv := &http.Server{
		Handler:           app.routes(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("서버 오류: %v", err)
		}
	}()
	app.writeInstanceFile()
	app.refreshSendTo()

	if len(sharePaths) > 0 {
		app.sharePaths(sharePaths)
	}

	pcURL := fmt.Sprintf("http://127.0.0.1:%d/pc?a=%s", app.port, app.adminToken)
	for _, a := range app.addresses() {
		log.Printf("폰 접속 주소: %s (%s)", app.phoneURL(a.IP), a.Iface)
	}
	log.Printf("PC 화면: http://127.0.0.1:%d/pc", app.port)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		app.requestQuit()
	}()

	if *flagHeadless {
		fmt.Printf("LocalDrop %s 실행 중 (포트 %d)\n", appVersion, app.port)
		fmt.Printf("PC 화면: %s\n", pcURL)
		for _, a := range app.addresses() {
			fmt.Printf("폰 주소: %s\n", app.phoneURL(a.IP))
		}
		<-app.quit
	} else {
		runUI(app, pcURL, *flagBrowser) // 창을 닫거나 종료를 누를 때까지 돌아오지 않습니다.
	}

	log.Printf("종료 중…")
	app.hub.closeAll()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = srv.Shutdown(ctx)
	cancel()
	app.shutdown()
	log.Printf("종료했어요")
}

// listenFrom은 지정한 포트부터 차례로 시도하고, 모두 실패하면 임의의 빈 포트를 씁니다.
func listenFrom(port int) (net.Listener, error) {
	if port <= 0 || port > 65535 {
		port = defaultPort
	}
	var lastErr error
	for p := port; p < port+20 && p <= 65535; p++ {
		ln, err := net.Listen("tcp", ":"+strconv.Itoa(p))
		if err == nil {
			return ln, nil
		}
		lastErr = err
	}
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, err
	}
	return ln, nil
}

func setupLogging(dataDir string) *os.File {
	path := filepath.Join(dataDir, "LocalDrop.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 2<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	// 파일을 먼저 씁니다. Windows 창 모드에서는 stderr가 없어 쓰기가 실패할 수 있습니다.
	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.SetFlags(log.Ldate | log.Ltime)
	return f
}

func fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Print(msg)
	showErrorBox(msg)
	os.Exit(1)
}

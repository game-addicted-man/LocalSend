//go:build !windows

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/ncruces/zenity"
)

// Windows가 아닌 곳(맥·리눅스)에서는 기본 브라우저로 PC 화면을 엽니다.
func runUI(app *App, pcURL string, forceBrowser bool) {
	app.setBrowserUI(true)
	if err := openBrowserUI(pcURL); err != nil {
		log.Printf("브라우저를 열지 못했어요: %v", err)
	}
	fmt.Printf("LocalDrop 실행 중. PC 화면: %s\n종료하려면 Ctrl+C\n", pcURL)
	<-app.quit
}

func showErrorBox(msg string) { fmt.Fprintln(os.Stderr, msg) }

func platformDownloadsDir() string {
	if out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output(); err == nil {
		if p := string(out); len(p) > 1 {
			return filepath.Clean(p[:len(p)-1])
		}
	}
	return ""
}

func defaultUserName() string { return os.Getenv("USER") }

func opener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

func openPath(p string) error { return exec.Command(opener(), p).Start() }

func openURL(u string) error { return exec.Command(opener(), u).Start() }

func openBrowserUI(u string) error { return openURL(u) }

func revealPath(p string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", p).Start()
	}
	return exec.Command("xdg-open", filepath.Dir(p)).Start()
}

func dialogsSupported() bool { return zenity.IsAvailable() }

func pickFiles(owner uintptr) ([]string, error) {
	if !zenity.IsAvailable() {
		return nil, errUnsupported
	}
	paths, err := zenity.SelectFileMultiple(zenity.Title("폰으로 보낼 파일 선택"))
	if errors.Is(err, zenity.ErrCanceled) {
		return nil, errCanceled
	}
	return paths, err
}

func pickFolder(owner uintptr, start string) (string, error) {
	if !zenity.IsAvailable() {
		return "", errUnsupported
	}
	opts := []zenity.Option{zenity.Title("받은 파일을 저장할 폴더 선택"), zenity.Directory()}
	if start != "" {
		opts = append(opts, zenity.Filename(start+"/"))
	}
	dir, err := zenity.SelectFile(opts...)
	if errors.Is(err, zenity.ErrCanceled) {
		return "", errCanceled
	}
	return dir, err
}

func allowForegroundFor(pid int) {}

func firewallSupported() bool { return false }

func fixFirewall(owner uintptr) error {
	return errors.New("이 기능은 Windows에서만 쓸 수 있어요")
}

func sendToSupported() bool { return false }

func sendToEnabled() bool { return false }

func setSendTo(on bool) error { return errors.New("이 기능은 Windows에서만 쓸 수 있어요") }

func autoStartSupported() bool { return false }

func autoStartEnabled() bool { return false }

func setAutoStart(on bool) error {
	return errors.New("이 기능은 Windows에서만 쓸 수 있어요")
}

package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
)

var (
	errCanceled    = errors.New("canceled")
	errUnsupported = errors.New("unsupported")
)

// refreshSendTo는 exe를 다른 폴더로 옮긴 경우 '보내기' 메뉴·자동 실행 바로가기를 새 위치로 고칩니다.
func (a *App) refreshSendTo() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	a.mu.Lock()
	same := a.cfg.LinkExe == exe
	a.mu.Unlock()
	if same || !(sendToSupported() && sendToEnabled()) && !(autoStartSupported() && autoStartEnabled()) {
		return
	}
	a.rememberLinkExe()
	go func() {
		if sendToSupported() && sendToEnabled() {
			if err := setSendTo(true); err != nil {
				log.Printf("보내기 메뉴 갱신 실패: %v", err)
			}
		}
		if autoStartSupported() && autoStartEnabled() {
			if err := setAutoStart(true); err != nil {
				log.Printf("자동 실행 갱신 실패: %v", err)
			}
		}
	}()
}

// 앱 창(WebView2)을 여는 도중 프로그램이 멈추면 다음 실행 때 브라우저로 열기 위한 표시 파일
func (a *App) uiMarkerPath() string { return filepath.Join(a.dataDir, "window-starting") }

// markUIWorking은 PC 화면이 실제로 열렸을 때 표시 파일을 지웁니다.
func (a *App) markUIWorking() {
	_ = os.Remove(a.uiMarkerPath())
}

// rememberLinkExe는 바로가기를 만든 exe 위치를 기억해 둡니다.
func (a *App) rememberLinkExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	a.mu.Lock()
	a.cfg.LinkExe = exe
	a.mu.Unlock()
	a.saveConfig()
}

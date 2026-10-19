package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// LocalDrop은 한 번만 실행됩니다. 두 번째로 실행하면(예: 탐색기 '보내기' 메뉴)
// 이미 켜져 있는 쪽에 파일을 넘기고 창을 앞으로 가져온 뒤 바로 끝납니다.

type instanceInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func instancePath(dataDir string) string { return filepath.Join(dataDir, "instance.json") }

func (a *App) writeInstanceFile() {
	b, _ := json.Marshal(instanceInfo{Port: a.port, Token: a.instanceToken, PID: os.Getpid()})
	if err := os.WriteFile(instancePath(a.dataDir), b, 0o600); err != nil {
		log.Printf("instance.json 저장 실패: %v", err)
	}
}

func (a *App) removeInstanceFile() {
	p := instancePath(a.dataDir)
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var inf instanceInfo
	if json.Unmarshal(b, &inf) == nil && inf.PID == os.Getpid() {
		_ = os.Remove(p)
	}
}

func forwardToRunningInstance(dataDir string, paths []string) bool {
	b, err := os.ReadFile(instancePath(dataDir))
	if err != nil {
		return false
	}
	var inf instanceInfo
	if err := json.Unmarshal(b, &inf); err != nil || inf.Port == 0 || inf.Token == "" {
		return false
	}
	abs := make([]string, 0, len(paths))
	for _, p := range paths {
		if ap, err := filepath.Abs(p); err == nil {
			abs = append(abs, ap)
		}
	}
	allowForegroundFor(inf.PID) // 켜져 있는 쪽이 창을 맨 앞으로 가져올 수 있게 허락
	body, _ := json.Marshal(map[string]any{"token": inf.Token, "paths": abs})
	client := &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/api/instance", inf.Port), "application/json", bytes.NewReader(body))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (a *App) handleInstance(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackAddr(r.RemoteAddr) {
		jsonError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		Token string   `json:"token"`
		Paths []string `json:"paths"`
		Quit  bool     `json:"quit"` // 설치·제거 프로그램이 업데이트 전에 끄라고 할 때
	}
	if err := readJSON(r, 1<<20, &req); err != nil || !tokenEqual(req.Token, a.instanceToken) {
		jsonError(w, http.StatusForbidden, "forbidden")
		return
	}
	if req.Quit {
		writeJSON(w, map[string]bool{"ok": true, "quitting": true})
		log.Printf("설치·제거 프로그램의 요청으로 종료합니다")
		go func() {
			time.Sleep(200 * time.Millisecond)
			a.requestQuit()
		}()
		return
	}
	if len(req.Paths) > 0 {
		a.sharePaths(req.Paths)
	}
	writeJSON(w, map[string]bool{"ok": true})
	go a.showUI()
}

func (a *App) showUI() {
	if h := a.hooks(); h.show != nil {
		h.show()
		return
	}
	if a.isBrowserUI() {
		_ = openBrowserUI(fmt.Sprintf("http://127.0.0.1:%d/pc?a=%s", a.port, a.adminToken))
	}
}

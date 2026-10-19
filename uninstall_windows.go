//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/game-addicted-man/LocalSend/internal/winutil"
	"github.com/ncruces/zenity"
)

// runUninstall은 설치된 LocalDrop을 제거합니다. Windows 설정 → 앱 → LocalDrop → 제거를 누르면
// 'LocalDrop.exe --uninstall'로 실행돼요. 받은 파일(저장 폴더)은 지우지 않습니다.
func runUninstall(quiet bool) {
	title := zenity.Title("LocalDrop 제거")
	dataDir := winutil.DataDir()
	cacheDir := winutil.CacheDir()
	saveDir := savedSaveDir(dataDir)
	saveText := saveDir
	if saveText == "" {
		saveText = `다운로드\LocalDrop`
	}

	if !quiet {
		err := zenity.Question("LocalDrop을 제거할까요?\n\n설정과 주고받은 기록도 함께 지워요.\n받은 파일("+saveText+")은 그대로 남아요.",
			title, zenity.OKLabel("제거"), zenity.CancelLabel("취소"), zenity.Icon(zenity.QuestionIcon))
		if err != nil {
			return
		}
	}

	// 켜져 있으면 먼저 끕니다 (응답이 없거나 예전 버전이면 강제로).
	if res, pid := winutil.RequestQuit(dataDir, 5*time.Second); res == winutil.StillRunning {
		_ = winutil.Terminate(pid)
	}

	var problems []string
	for _, link := range []string{
		filepath.Join(winutil.ProgramsDir(), winutil.StartMenuLink),
		filepath.Join(winutil.DesktopDir(), winutil.DesktopLink),
		filepath.Join(winutil.StartupDir(), winutil.AutoStartLink),
		filepath.Join(winutil.SendToDir(), winutil.SendToLink),
	} {
		if err := winutil.RemoveFile(link); err != nil {
			problems = append(problems, link)
		}
	}

	// 표준 설치 폴더에서 실행 중일 때만 설치 폴더도 지웁니다 (설치 없이 쓰던 exe 폴더는 건드리지 않아요).
	installDir := ""
	if exe, err := os.Executable(); err == nil && winutil.SamePath(filepath.Dir(exe), winutil.InstallDir()) {
		installDir = filepath.Dir(exe)
	}
	dirs := []string{dataDir, cacheDir}
	if installDir != "" {
		dirs = append(dirs, installDir)
	}
	var later []string
	for _, d := range dirs {
		if saveDir != "" && winutil.IsWithin(saveDir, d) {
			problems = append(problems, d+" (받은 파일 저장 위치가 들어 있어서 남겨 뒀어요)")
			continue
		}
		if !winutil.SafeAppDir(d) {
			problems = append(problems, d)
			continue
		}
		if err := os.RemoveAll(d); err != nil || pathExists(d) {
			later = append(later, d) // 잠긴 파일(실행 중인 이 exe, 앱 창 캐시)은 끝난 뒤 지워요
		}
	}

	// 설치 폴더를 나중에 지우는 경우, '설치된 앱' 목록에서는 그 폴더가 사라진 뒤에 뺍니다.
	// (혹시 못 지워도 목록에 남아 있어야 다시 제거할 수 있으니까요)
	keyLater := installDir != "" && containsPath(later, installDir)
	if !keyLater {
		if err := winutil.UnregisterUninstall(); err != nil {
			problems = append(problems, "Windows '설치된 앱' 목록")
		}
	}

	// 마지막 안내를 먼저 보여 주고, 창을 닫은 뒤에 정리 도우미를 띄웁니다.
	// (안내 창이 떠 있는 동안에는 이 exe가 실행 중이라 설치 폴더를 지울 수 없어요)
	if !quiet {
		if len(problems) > 0 {
			_ = zenity.Warning("LocalDrop을 제거했지만 몇 가지는 지우지 못했어요:\n\n"+strings.Join(problems, "\n"), title)
		} else {
			_ = zenity.Info("LocalDrop을 제거했어요.\n\n받은 파일은 "+saveText+" 폴더에 그대로 있어요.", title)
		}
	}
	if len(later) > 0 {
		keyDir := ""
		if keyLater {
			keyDir = installDir
		}
		if _, err := winutil.CleanupLater(later, keyDir); err != nil && keyLater {
			_ = winutil.UnregisterUninstall() // 도우미를 못 띄우면 목록에서라도 빼요
		}
	}
}

func containsPath(list []string, p string) bool {
	for _, x := range list {
		if winutil.SamePath(x, p) {
			return true
		}
	}
	return false
}

// savedSaveDir는 설정 파일에 적힌 받은 파일 저장 위치를 읽습니다 (그 폴더는 지우지 않으려고).
func savedSaveDir(dataDir string) string {
	b, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return ""
	}
	var c struct {
		SaveDir string `json:"saveDir"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return ""
	}
	return c.SaveDir
}

//go:build windows

// LocalDrop 설치 프로그램 (LocalDrop-Setup.exe)
//
// 관리자 권한 없이 현재 사용자에게 설치합니다:
//   - %LOCALAPPDATA%\Programs\LocalDrop\LocalDrop.exe 에 복사
//   - 시작 메뉴와 바탕화면에 바로가기
//   - Windows 설정 → 앱 목록에 등록 (여기서 '제거'를 누르면 LocalDrop.exe --uninstall 실행)
//
// 이미 설치돼 있으면 새 버전으로 바꿉니다(설정과 기록은 그대로). 켜져 있는 LocalDrop은 먼저 끕니다.
//
// 조용히 설치: LocalDrop-Setup.exe /S   (또는 --quiet). 설치 후 실행하지 않으려면 --nolaunch 를 더하세요.
package main

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/game-addicted-man/LocalSend/internal/version"
	"github.com/game-addicted-man/LocalSend/internal/winutil"
	"github.com/ncruces/zenity"
)

//go:embed payload
var payload embed.FS

var title = zenity.Title("LocalDrop 설치")

func main() {
	quiet, noLaunch := false, false
	for _, a := range os.Args[1:] {
		switch strings.ToLower(strings.TrimLeft(a, "-/")) {
		case "s", "quiet", "silent", "verysilent":
			quiet = true
		case "nolaunch":
			noLaunch = true
		}
	}

	exeData, err := payload.ReadFile("payload/" + winutil.ExeName)
	if err != nil || len(exeData) < 1<<20 {
		fail(quiet, "설치 파일 안에 LocalDrop.exe가 없어요.\nbuild.bat으로 설치 파일을 다시 만들어 주세요.")
	}

	dir := winutil.InstallDir()
	target := filepath.Join(dir, winutil.ExeName)
	updating := fileExists(target)

	if !quiet {
		msg := fmt.Sprintf("LocalDrop %s을 설치할까요?\n\n"+
			"· 설치 위치: %s\n"+
			"· 시작 메뉴와 바탕화면에 바로가기를 만들어요.\n"+
			"· 관리자 권한은 필요 없어요.", version.App, dir)
		ok := "설치"
		if updating {
			msg = fmt.Sprintf("LocalDrop을 %s 버전으로 업데이트할까요?\n\n설정과 주고받은 기록은 그대로 남아요.\n켜져 있는 LocalDrop은 잠깐 꺼졌다가 다시 켤 수 있어요.", version.App)
			ok = "업데이트"
		}
		if zenity.Question(msg, title, zenity.OKLabel(ok), zenity.CancelLabel("취소"), zenity.Icon(zenity.QuestionIcon)) != nil {
			return
		}
	}

	var progress zenity.ProgressDialog
	openProgress := func(text string) {
		if quiet || progress != nil {
			return
		}
		if p, err := zenity.Progress(title, zenity.Pulsate(), zenity.NoCancel()); err == nil {
			progress = p
			_ = p.Text(text)
		}
	}
	closeProgress := func() {
		if progress != nil {
			_ = progress.Complete()
			_ = progress.Close()
			progress = nil
		}
	}

	// 켜져 있는 LocalDrop을 끕니다 (실행 중인 exe는 덮어쓸 수 없고, 새 버전으로 바로 열리게 하려고).
	openProgress("켜져 있는 LocalDrop을 끄는 중…")
	res, pid := winutil.RequestQuit(winutil.DataDir(), 5*time.Second)
	if res == winutil.StillRunning {
		closeProgress()
		if !quiet && zenity.Question("LocalDrop이 켜져 있어요. 끄고 계속 설치할까요?\n\n(보내는 중인 파일이 있으면 끝난 뒤에 진행하세요)",
			title, zenity.OKLabel("끄고 계속"), zenity.CancelLabel("취소"), zenity.Icon(zenity.WarningIcon)) != nil {
			return
		}
		_ = winutil.Terminate(pid)
	}
	wasRunning := res != winutil.NotRunning
	openProgress("설치하는 중…")

	restartNeeded, err := placeExe(exeData, dir, target)
	if err != nil {
		closeProgress()
		fail(quiet, "LocalDrop을 설치하지 못했어요.\n\n"+err.Error())
	}

	var warnings []string
	if err := winutil.MakeShortcuts(
		winutil.Shortcut{Link: filepath.Join(winutil.ProgramsDir(), winutil.StartMenuLink), Target: target, Desc: winutil.ShortcutDesc},
		winutil.Shortcut{Link: filepath.Join(winutil.DesktopDir(), winutil.DesktopLink), Target: target, Desc: winutil.ShortcutDesc},
	); err != nil {
		warnings = append(warnings, "시작 메뉴·바탕화면 바로가기를 만들지 못했어요. 설치 폴더의 LocalDrop.exe를 직접 실행해 주세요.")
	}
	if err := winutil.RegisterUninstall(winutil.UninstallInfo{
		Version: version.App, ExePath: target, Dir: dir, SizeBytes: int64(len(exeData)), URL: version.Repo,
	}); err != nil {
		warnings = append(warnings, "Windows '설치된 앱' 목록에 등록하지 못했어요. 지울 때는 설치 폴더를 직접 지우면 돼요.")
	}
	closeProgress()

	if quiet {
		if !noLaunch && wasRunning && !restartNeeded {
			launch(target, dir) // 업데이트 전에 켜져 있었으면 다시 켜 둡니다
		}
		return
	}

	msg := "설치를 마쳤어요.\n\n시작 메뉴나 바탕화면의 LocalDrop 아이콘으로 실행할 수 있어요."
	if len(warnings) > 0 {
		msg += "\n\n" + strings.Join(warnings, "\n")
	}
	if restartNeeded {
		msg += "\n\n예전 LocalDrop이 아직 켜져 있어요. 작업 표시줄에서 끈 다음 다시 실행하면 새 버전이 열려요."
	}
	if noLaunch || restartNeeded {
		_ = zenity.Info(msg, title)
		return
	}
	if zenity.Question(msg+"\n\n지금 실행할까요?", title, zenity.OKLabel("지금 실행"), zenity.CancelLabel("닫기"), zenity.Icon(zenity.InfoIcon)) == nil {
		launch(target, dir)
	}
}

// placeExe는 새 exe를 임시 이름으로 쓴 뒤 제자리로 옮깁니다.
// 예전 exe가 아직 실행 중이라 바꿀 수 없으면, 실행 중인 파일도 이름은 바꿀 수 있다는 점을 이용해
// 예전 것을 LocalDrop.exe.old-<시각>으로 비켜 두고 새 것을 넣습니다 (다음 실행 때 앱이 지워요).
func placeExe(data []byte, dir, target string) (restartNeeded bool, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	tmp := target + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return false, fmt.Errorf("파일을 쓸 수 없어요: %w", err)
	}
	for i := 0; i < 20; i++ {
		if err = os.Rename(tmp, target); err == nil {
			return false, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	// 예전 .old가 아직 실행 중일 수도 있어서 겹치지 않는 이름을 씁니다.
	old := fmt.Sprintf("%s.old-%d", target, time.Now().Unix())
	if err2 := os.Rename(target, old); err2 == nil {
		if err = os.Rename(tmp, target); err == nil {
			return true, nil
		}
		_ = os.Rename(old, target) // 되돌리기
	}
	_ = os.Remove(tmp)
	return false, errors.New("LocalDrop이 아직 실행 중이라 바꾸지 못했어요. LocalDrop을 끄고 설치 파일을 다시 실행해 주세요")
}

func launch(target, dir string) {
	cmd := exec.Command(target)
	cmd.Dir = dir
	_ = cmd.Start()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func fail(quiet bool, msg string) {
	if !quiet {
		_ = zenity.Error(msg, title)
	}
	os.Exit(1)
}

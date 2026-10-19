// Package winutil은 앱(LocalDrop.exe)과 설치 프로그램(LocalDrop-Setup.exe)이 함께 쓰는
// Windows 전용 기능입니다: 바로가기 만들기, 시작 메뉴·바탕화면 위치, '설치된 앱' 등록,
// 실행 중인 LocalDrop 끄기.
//
// Windows가 아닌 곳에서는 이 패키지를 쓰지 않습니다.
package winutil

// 앱 이름과 바로가기 파일 이름 (앱 설정 화면과 설치·제거가 같은 이름을 써야 해요)
const (
	AppName       = "LocalDrop"
	ExeName       = "LocalDrop.exe"
	StartMenuLink = "LocalDrop.lnk"
	DesktopLink   = "LocalDrop.lnk"
	AutoStartLink = "LocalDrop.lnk"
	SendToLink    = "LocalDrop (폰으로 보내기).lnk"
	UninstallKey  = `Software\Microsoft\Windows\CurrentVersion\Uninstall\LocalDrop`
	InstanceFile  = "instance.json"
	ShortcutDesc  = "같은 와이파이에서 폰 ↔ PC 파일 전송"
)

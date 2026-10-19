//go:build windows

package winutil

import (
	"fmt"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows의 바로가기 API(IShellLinkW + IPersistFile)로 .lnk를 직접 만듭니다.
// PowerShell을 띄우지 않아 빠르고, 백신이 의심할 일이 적고, 한글 경로·설명도 그대로 저장돼요.

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidShellLink  = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIShellLinkW  = windows.GUID{Data1: 0x000214F9, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile = windows.GUID{Data1: 0x0000010B, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const clsctxInprocServer = 0x1

// 가상 함수 표(vtable) 순서 — ShObjIdl_core.h의 IShellLinkW, ObjIdl.h의 IPersistFile
const (
	vtQueryInterface      = 0
	vtRelease             = 2
	slSetDescription      = 7
	slSetWorkingDirectory = 9
	slSetArguments        = 11
	slSetIconLocation     = 17
	slSetPath             = 20
	pfSave                = 6
)

// comCall은 COM 객체 obj의 idx번째 메서드를 부릅니다.
//
//go:uintptrescapes
func comCall(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func hrErr(what string, hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("%s 실패 (0x%08X)", what, uint32(hr))
	}
	return nil
}

// makeShortcutNative는 바로가기 하나를 만듭니다.
func makeShortcutNative(sc Shortcut) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if e := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); e == nil || e == syscall.Errno(1) {
		defer windows.CoUninitialize() // S_OK 또는 S_FALSE(이미 초기화됨)일 때만 짝을 맞춰 해제
	}

	link := new(unsafe.Pointer) // 힙에 둬서 호출 중에 주소가 바뀌지 않게
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(link)))
	if err := hrErr("CoCreateInstance(ShellLink)", hr); err != nil {
		return err
	}
	if *link == nil {
		return fmt.Errorf("ShellLink를 만들지 못했어요")
	}
	defer comCall(*link, vtRelease)

	setStr := func(name string, idx int, s string, extra ...uintptr) error {
		p, err := windows.UTF16PtrFromString(s)
		if err != nil {
			return err
		}
		hr := comCall(*link, idx, append([]uintptr{uintptr(unsafe.Pointer(p))}, extra...)...)
		runtime.KeepAlive(p)
		return hrErr(name, hr)
	}
	if err := setStr("SetPath", slSetPath, sc.Target); err != nil {
		return err
	}
	if err := setStr("SetArguments", slSetArguments, sc.Args); err != nil {
		return err
	}
	if err := setStr("SetWorkingDirectory", slSetWorkingDirectory, filepath.Dir(sc.Target)); err != nil {
		return err
	}
	if err := setStr("SetDescription", slSetDescription, sc.Desc); err != nil {
		return err
	}
	if err := setStr("SetIconLocation", slSetIconLocation, sc.Target, 0); err != nil {
		return err
	}

	persist := new(unsafe.Pointer)
	hr = comCall(*link, vtQueryInterface, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(persist)))
	if err := hrErr("QueryInterface(IPersistFile)", hr); err != nil {
		return err
	}
	if *persist == nil {
		return fmt.Errorf("IPersistFile을 얻지 못했어요")
	}
	defer comCall(*persist, vtRelease)

	path, err := windows.UTF16PtrFromString(sc.Link)
	if err != nil {
		return err
	}
	hr = comCall(*persist, pfSave, uintptr(unsafe.Pointer(path)), 1)
	runtime.KeepAlive(path)
	return hrErr("IPersistFile.Save", hr)
}

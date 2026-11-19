//go:build windows

package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// canRun says whether hom knows how to start this kind of installer on this OS (spec 1.6).
func canRun(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".msi", ".exe":
		return true
	}
	return false
}

// SHELLEXECUTEINFOW of shellapi.h. Go lays the fields out the way C does.
type shellExecInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	hIconOrMonitor uintptr
	hProcess       syscall.Handle
}

var (
	shell32            = syscall.NewLazyDLL("shell32.dll")
	ole32              = syscall.NewLazyDLL("ole32.dll")
	procShellExecuteEx = shell32.NewProc("ShellExecuteExW")
	procCoInitializeEx = ole32.NewProc("CoInitializeEx")
	procCoUninitialize = ole32.NewProc("CoUninitialize")
)

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	swShowNormal          = 1
)

// runProgram starts an installer the way Explorer does (so a "run as administrator" prompt
// appears when the installer wants one) and waits for it. It returns the exit code.
func runProgram(path, args string) (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := procCoInitializeEx.Call(0, 0x2|0x4); int32(hr) >= 0 {
		defer procCoUninitialize.Call()
	}

	file, params := path, args
	if strings.EqualFold(filepath.Ext(path), ".msi") {
		file = "msiexec.exe"
		params = strings.TrimSpace(`/i "` + path + `" ` + args)
	}
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return 0, err
	}
	pf, err := syscall.UTF16PtrFromString(file)
	if err != nil {
		return 0, err
	}
	var pp *uint16
	if params != "" {
		if pp, err = syscall.UTF16PtrFromString(params); err != nil {
			return 0, err
		}
	}
	sei := shellExecInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       pf,
		lpParameters: pp,
		nShow:        swShowNormal,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	r, _, callErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&sei)))
	if r == 0 {
		return 0, fmt.Errorf("%v", callErr)
	}
	var code uint32
	if sei.hProcess != 0 {
		syscall.WaitForSingleObject(sei.hProcess, syscall.INFINITE)
		syscall.GetExitCodeProcess(sei.hProcess, &code)
		syscall.CloseHandle(sei.hProcess)
	}
	return int(code), nil
}

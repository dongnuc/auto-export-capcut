//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

var (
	shell32 = syscall.NewLazyDLL("shell32.dll")
	ole32   = syscall.NewLazyDLL("ole32.dll")

	pSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	pCoInitializeEx       = ole32.NewProc("CoInitializeEx")
	pCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")
)

type browseInfo struct {
	owner       uintptr
	root        uintptr
	displayName *uint16
	title       *uint16
	flags       uint32
	callback    uintptr
	lParam      uintptr
	image       int32
}

// PickFolder shows the Windows folder picker. Returns "" if cancelled.
func PickFolder(title string) string {
	res := make(chan string, 1)
	go func() {
		lockThread()
		pCoInitializeEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED
		display := make([]uint16, 260)
		t, _ := syscall.UTF16PtrFromString(title)
		bi := browseInfo{displayName: &display[0], title: t, flags: 0x1 | 0x10 | 0x40}
		pidl, _, _ := pSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
		if pidl == 0 {
			res <- ""
			return
		}
		defer pCoTaskMemFree.Call(pidl)
		path := make([]uint16, 1024)
		pSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
		res <- syscall.UTF16ToString(path)
	}()
	return <-res
}

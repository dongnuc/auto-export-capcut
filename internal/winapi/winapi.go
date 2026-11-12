//go:build windows

// Package winapi wraps the few Win32 calls the tool needs: finding CapCut's
// windows, focusing them, sending keys and clicks, and a few system helpers.
package winapi

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pEnumWindows               = user32.NewProc("EnumWindows")
	pGetWindowTextW            = user32.NewProc("GetWindowTextW")
	pGetClassNameW             = user32.NewProc("GetClassNameW")
	pIsWindowVisible           = user32.NewProc("IsWindowVisible")
	pIsIconic                  = user32.NewProc("IsIconic")
	pGetWindowThreadProcessId  = user32.NewProc("GetWindowThreadProcessId")
	pGetWindowRect             = user32.NewProc("GetWindowRect")
	pGetClientRect             = user32.NewProc("GetClientRect")
	pClientToScreen            = user32.NewProc("ClientToScreen")
	pGetWindow                 = user32.NewProc("GetWindow")
	pShowWindow                = user32.NewProc("ShowWindow")
	pSetForegroundWindow       = user32.NewProc("SetForegroundWindow")
	pGetForegroundWindow       = user32.NewProc("GetForegroundWindow")
	pBringWindowToTop          = user32.NewProc("BringWindowToTop")
	pAttachThreadInput         = user32.NewProc("AttachThreadInput")
	pSendInput                 = user32.NewProc("SendInput")
	pMapVirtualKeyW            = user32.NewProc("MapVirtualKeyW")
	pSetCursorPos              = user32.NewProc("SetCursorPos")
	pGetCursorPos              = user32.NewProc("GetCursorPos")
	pPostMessageW              = user32.NewProc("PostMessageW")
	pSetProcessDpiAwarenessCtx = user32.NewProc("SetProcessDpiAwarenessContext")
	pRegisterHotKey            = user32.NewProc("RegisterHotKey")
	pGetMessageW               = user32.NewProc("GetMessageW")

	pGetCurrentThreadId      = kernel32.NewProc("GetCurrentThreadId")
	pSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
)

const (
	swRestore  = 9
	swMaximize = 3
	gwOwner    = 4
	wmClose    = 0x0010
	wmHotkey   = 0x0312
)

type Rect struct{ Left, Top, Right, Bottom int32 }

func (r Rect) W() int32 { return r.Right - r.Left }
func (r Rect) H() int32 { return r.Bottom - r.Top }

type Window struct {
	HWND    uintptr
	PID     uint32
	Title   string
	Class   string
	Rect    Rect
	Visible bool
	Iconic  bool
	Owned   bool
}

func (w Window) String() string {
	return fmt.Sprintf("%q [%s] pid=%d %dx%d visible=%v min=%v", w.Title, w.Class, w.PID, w.Rect.W(), w.Rect.H(), w.Visible, w.Iconic)
}

// EnableDPIAwareness makes coordinates physical pixels on scaled displays.
func EnableDPIAwareness() {
	if pSetProcessDpiAwarenessCtx.Find() == nil {
		pSetProcessDpiAwarenessCtx.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	}
}

// ProcessIDs returns the PIDs of every running process with this exe name.
func ProcessIDs(exe string) []uint32 {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snap)
	var pe syscall.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	var out []uint32
	for err = syscall.Process32First(snap, &pe); err == nil; err = syscall.Process32Next(snap, &pe) {
		if strings.EqualFold(syscall.UTF16ToString(pe.ExeFile[:]), exe) {
			out = append(out, pe.ProcessID)
		}
	}
	return out
}

func windowText(h uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func className(h uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := pGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func describe(h uintptr) Window {
	var pid uint32
	pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	var r Rect
	pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	vis, _, _ := pIsWindowVisible.Call(h)
	ico, _, _ := pIsIconic.Call(h)
	owner, _, _ := pGetWindow.Call(h, gwOwner)
	return Window{HWND: h, PID: pid, Title: windowText(h), Class: className(h), Rect: r,
		Visible: vis != 0, Iconic: ico != 0, Owned: owner != 0}
}

// Windows lists visible top-level windows belonging to the given PIDs.
func Windows(pids []uint32) []Window {
	set := map[uint32]bool{}
	for _, p := range pids {
		set[p] = true
	}
	var out []Window
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		var pid uint32
		pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
		if set[pid] {
			if w := describe(h); w.Visible {
				out = append(out, w)
			}
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)
	return out
}

// MainWindow picks the largest un-owned window (minimized windows count).
func MainWindow(ws []Window) (Window, bool) {
	var best Window
	var bestArea int64 = -1
	for _, w := range ws {
		if w.Owned || w.IsTool() {
			continue
		}
		area := int64(w.Rect.W()) * int64(w.Rect.H())
		if w.Iconic {
			area = 1 << 40 // a minimized main window is still the main window
		}
		if area > bestArea {
			best, bestArea = w, area
		}
	}
	return best, bestArea >= 0
}

func Restore(h uintptr)  { pShowWindow.Call(h, swRestore) }
func Maximize(h uintptr) { pShowWindow.Call(h, swMaximize) }

func Foreground() uintptr {
	h, _, _ := pGetForegroundWindow.Call()
	return h
}

// Focus brings h to the foreground, working around the foreground lock by
// attaching to the current foreground thread and tapping Alt.
func Focus(h uintptr) bool {
	if ico, _, _ := pIsIconic.Call(h); ico != 0 {
		Restore(h)
	}
	for attempt := 0; attempt < 5; attempt++ {
		if Foreground() == h {
			return true
		}
		fg := Foreground()
		fgThread, _, _ := pGetWindowThreadProcessId.Call(fg, 0)
		cur, _, _ := pGetCurrentThreadId.Call()
		if fgThread != 0 && fgThread != cur {
			pAttachThreadInput.Call(cur, fgThread, 1)
		}
		tapKey(vkMenu)
		pBringWindowToTop.Call(h)
		pSetForegroundWindow.Call(h)
		if fgThread != 0 && fgThread != cur {
			pAttachThreadInput.Call(cur, fgThread, 0)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return Foreground() == h
}

// ClientRectOnScreen returns the window's client area in screen coordinates.
func ClientRectOnScreen(h uintptr) Rect {
	var r Rect
	pGetClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
	pt := [2]int32{0, 0}
	pClientToScreen.Call(h, uintptr(unsafe.Pointer(&pt)))
	return Rect{pt[0], pt[1], pt[0] + r.W(), pt[1] + r.H()}
}

func CursorPos() (int32, int32) {
	var pt [2]int32
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return pt[0], pt[1]
}

// CloseWindow asks the window to close (like clicking X).
func CloseWindow(h uintptr) { pPostMessageW.Call(h, wmClose, 0, 0) }

// KeepAwake stops the PC from sleeping or turning the display off; pass
// false to restore normal behaviour.
func KeepAwake(on bool) {
	const esContinuous, esSystem, esDisplay = 0x80000000, 0x1, 0x2
	if on {
		pSetThreadExecutionState.Call(esContinuous | esSystem | esDisplay)
	} else {
		pSetThreadExecutionState.Call(esContinuous)
	}
}

// WatchHotkey registers a global hotkey (e.g. "Ctrl+Alt+Q") and calls fn every
// time it is pressed. It runs until the process exits.
func WatchHotkey(combo string, fn func()) error {
	k, err := ParseKey(combo)
	if err != nil {
		return err
	}
	var mods uintptr
	for _, m := range k.Mods {
		switch m {
		case vkControl:
			mods |= 0x2
		case vkShift:
			mods |= 0x4
		case vkMenu:
			mods |= 0x1
		case vkLWin:
			mods |= 0x8
		}
	}
	errc := make(chan error, 1)
	go func() {
		lockThread()
		ok, _, e := pRegisterHotKey.Call(0, 1, mods|0x4000 /*MOD_NOREPEAT*/, uintptr(k.VK))
		if ok == 0 {
			errc <- fmt.Errorf("RegisterHotKey %s: %v", combo, e)
			return
		}
		errc <- nil
		var msg [48]byte
		for {
			r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			if *(*uint32)(unsafe.Pointer(&msg[8])) == wmHotkey {
				fn()
			}
		}
	}()
	return <-errc
}

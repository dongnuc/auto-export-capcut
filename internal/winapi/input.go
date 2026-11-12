//go:build windows

package winapi

import (
	"fmt"
	"runtime"
	"strings"
	"time"
	"unsafe"
)

const (
	vkBack    = 0x08
	vkTab     = 0x09
	vkReturn  = 0x0D
	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkEscape  = 0x1B
	vkSpace   = 0x20
	vkLWin    = 0x5B
	vkDelete  = 0x2E

	inputMouse    = 0
	inputKeyboard = 1

	keyeventfExtended = 0x1
	keyeventfKeyUp    = 0x2

	mouseLeftDown = 0x2
	mouseLeftUp   = 0x4
)

func lockThread() { runtime.LockOSThread() }

// Key is a parsed shortcut such as Ctrl+E.
type Key struct {
	Mods []uint16
	VK   uint16
	Text string
}

var namedKeys = map[string]uint16{
	"enter": vkReturn, "return": vkReturn, "esc": vkEscape, "escape": vkEscape,
	"tab": vkTab, "space": vkSpace, "backspace": vkBack, "delete": vkDelete, "del": vkDelete,
	"left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28,
	"home": 0x24, "end": 0x23, "pageup": 0x21, "pagedown": 0x22,
}

var extendedVK = map[uint16]bool{0x25: true, 0x26: true, 0x27: true, 0x28: true,
	0x24: true, 0x23: true, 0x21: true, 0x22: true, vkDelete: true}

// ParseKey parses strings like "Ctrl+E", "Ctrl+Shift+K", "Enter", "F5".
func ParseKey(s string) (Key, error) {
	k := Key{Text: s}
	parts := strings.Split(strings.ReplaceAll(s, " ", ""), "+")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return k, fmt.Errorf("phím tắt không hợp lệ: %q", s)
	}
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "control":
			k.Mods = append(k.Mods, vkControl)
		case "shift":
			k.Mods = append(k.Mods, vkShift)
		case "alt":
			k.Mods = append(k.Mods, vkMenu)
		case "win":
			k.Mods = append(k.Mods, vkLWin)
		default:
			return k, fmt.Errorf("phím bổ trợ không hợp lệ %q trong %q", p, s)
		}
	}
	last := strings.ToLower(parts[len(parts)-1])
	switch {
	case namedKeys[last] != 0:
		k.VK = namedKeys[last]
	case len(last) == 1 && last[0] >= 'a' && last[0] <= 'z':
		k.VK = uint16(last[0]-'a') + 'A'
	case len(last) == 1 && last[0] >= '0' && last[0] <= '9':
		k.VK = uint16(last[0])
	case len(last) >= 2 && last[0] == 'f':
		var n int
		if _, err := fmt.Sscanf(last[1:], "%d", &n); err == nil && n >= 1 && n <= 24 {
			k.VK = uint16(0x70 + n - 1)
		}
	}
	if k.VK == 0 {
		return k, fmt.Errorf("phím không hợp lệ %q trong %q", parts[len(parts)-1], s)
	}
	return k, nil
}

// keyInput / mouseInput mirror the 40-byte INPUT struct on amd64.
type keyInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uintptr
	_     [8]byte
}

type mouseInput struct {
	typ       uint32
	_         uint32
	dx, dy    int32
	mouseData uint32
	flags     uint32
	time      uint32
	extra     uintptr
}

func keyEvent(vk uint16, up bool) keyInput {
	scan, _, _ := pMapVirtualKeyW.Call(uintptr(vk), 0)
	ev := keyInput{typ: inputKeyboard, vk: vk, scan: uint16(scan)}
	if up {
		ev.flags |= keyeventfKeyUp
	}
	if extendedVK[vk] {
		ev.flags |= keyeventfExtended
	}
	return ev
}

func sendKeys(evs []keyInput) error {
	if len(evs) == 0 {
		return nil
	}
	n, _, err := pSendInput.Call(uintptr(len(evs)), uintptr(unsafe.Pointer(&evs[0])), unsafe.Sizeof(evs[0]))
	if int(n) != len(evs) {
		return fmt.Errorf("SendInput gửi %d/%d sự kiện: %v", n, len(evs), err)
	}
	return nil
}

func tapKey(vk uint16) { _ = sendKeys([]keyInput{keyEvent(vk, false), keyEvent(vk, true)}) }

// Press sends a whole shortcut: modifiers down, key down/up, modifiers up.
// Small pauses help Qt apps register the modifiers.
func Press(k Key) error {
	for _, m := range k.Mods {
		if err := sendKeys([]keyInput{keyEvent(m, false)}); err != nil {
			return err
		}
		time.Sleep(30 * time.Millisecond)
	}
	err := sendKeys([]keyInput{keyEvent(k.VK, false)})
	time.Sleep(40 * time.Millisecond)
	_ = sendKeys([]keyInput{keyEvent(k.VK, true)})
	for i := len(k.Mods) - 1; i >= 0; i-- {
		time.Sleep(30 * time.Millisecond)
		_ = sendKeys([]keyInput{keyEvent(k.Mods[i], true)})
	}
	return err
}

// Click moves the mouse to (x, y) in screen pixels and left-clicks.
//
// The pointer first glides onto the target so web-based views (CapCut's home
// screen) see a hover before the press; down and up are separate events.
func Click(x, y int32, double bool) error {
	for i := int32(4); i >= 0; i-- {
		pSetCursorPos.Call(uintptr(x-i*12), uintptr(y-i*8))
		time.Sleep(40 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	// A few pixels of drift is normal (pointer ballistics, a nudge of the
	// mouse); put it back once, and only refuse when it is far off.
	if cx, cy := CursorPos(); cx != x || cy != y {
		pSetCursorPos.Call(uintptr(x), uintptr(y))
		time.Sleep(150 * time.Millisecond)
		if cx, cy = CursorPos(); abs32(cx-x) > 8 || abs32(cy-y) > 8 {
			return fmt.Errorf("con trỏ ở (%d,%d) thay vì (%d,%d) — chuột đang bị người dùng di chuyển hoặc lệch DPI", cx, cy, x, y)
		}
	}
	clicks := 1
	if double {
		clicks = 2
	}
	send := func(flags uint32) error {
		ev := []mouseInput{{typ: inputMouse, flags: flags}}
		n, _, err := pSendInput.Call(1, uintptr(unsafe.Pointer(&ev[0])), unsafe.Sizeof(ev[0]))
		if n != 1 {
			return fmt.Errorf("SendInput chuột bị từ chối (có thể CapCut chạy quyền Admin còn tool thì không): %v", err)
		}
		return nil
	}
	for i := 0; i < clicks; i++ {
		if err := send(mouseLeftDown); err != nil {
			return err
		}
		time.Sleep(70 * time.Millisecond)
		if err := send(mouseLeftUp); err != nil {
			return err
		}
		time.Sleep(110 * time.Millisecond)
	}
	return nil
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

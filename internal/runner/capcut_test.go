package runner

import (
	"testing"

	"capcut-auto-exporter/internal/winapi"
)

func win(h uintptr, title, class string, w, ht int32) winapi.Window {
	return winapi.Window{HWND: h, Title: title, Class: class, Visible: true,
		Rect: winapi.Rect{Left: 0, Top: 0, Right: w, Bottom: ht}}
}

const qt = "Qt622QWindowIcon"

// The situation from the failed run: the update popup was the only large
// window on screen while the real home/editor window was not visible yet. It
// must not be taken for the main window.
func TestUpdatePopupIsNotMainWindow(t *testing.T) {
	ws := []winapi.Window{
		win(9374550, "Cập nhật phiên bản", qt, 852, 568),
		win(4459988, "CapCut", "Qt622QWindowToolSaveBits", 586, 363),
	}
	if m, ok := mainOf(ws); ok {
		t.Fatalf("update popup taken for main window: %+v", m)
	}
	d := dialogsOf(ws)
	if len(d) != 1 || d[0].HWND != 9374550 || !isUpdatePopup(d[0].Title) {
		t.Fatalf("dialogs: %+v", d)
	}
}

// Once the real window is visible, it is the main window and the update
// popup is a dialog; neither the home nor the editor is ever a "popup".
func TestRealWindowsAreNeverPopups(t *testing.T) {
	ws := []winapi.Window{
		win(9374550, "Cập nhật phiên bản", qt, 852, 568),
		win(1117406, "CapCut", qt, 1936, 1048),  // home
		win(12258354, "CapCut", qt, 1936, 1048), // editor
		win(7, "CapCut", qt, 320, 164),          // small loading notice
	}
	if m, ok := mainOf(ws); !ok || (m.HWND != 1117406 && m.HWND != 12258354) {
		t.Fatalf("main: %+v %v", m, ok)
	}
	got := map[uintptr]bool{}
	for _, d := range dialogsOf(ws) {
		got[d.HWND] = true
	}
	if got[1117406] || got[12258354] {
		t.Fatalf("a real CapCut window was listed as a popup: %v", got)
	}
	if !got[9374550] || !got[7] {
		t.Fatalf("popups missing: %v", got)
	}
}

func TestMinimizedMainStillMain(t *testing.T) {
	w := win(5, "CapCut", qt, 160, 28)
	w.Iconic = true
	if _, ok := mainOf([]winapi.Window{w}); !ok {
		t.Fatal("minimized CapCut window not recognised")
	}
}

func TestIsUpdatePopup(t *testing.T) {
	for _, s := range []string{"Cập nhật phiên bản", "Version Update", "CapCut update", "Nâng cấp"} {
		if !isUpdatePopup(s) {
			t.Errorf("%q should be an update popup", s)
		}
	}
	for _, s := range []string{"Liên kết tệp phương tiện", "Xuất", "CapCut", "Export"} {
		if isUpdatePopup(s) {
			t.Errorf("%q should not be an update popup", s)
		}
	}
}

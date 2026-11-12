package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"capcut-auto-exporter/internal/logx"
	"capcut-auto-exporter/internal/winapi"
)

const capcutExe = "CapCut.exe"

// FindCapCut returns the CapCut launcher and the bundled ffmpeg.exe (if any).
func FindCapCut(configured string) (exe, ffmpeg string) {
	exe = configured
	appsDir := ""
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		appsDir = filepath.Join(la, "CapCut", "Apps")
	}
	if exe == "" && appsDir != "" {
		if _, err := os.Stat(filepath.Join(appsDir, capcutExe)); err == nil {
			exe = filepath.Join(appsDir, capcutExe)
		}
	}
	if appsDir != "" {
		dirs, _ := filepath.Glob(filepath.Join(appsDir, "*", "ffmpeg.exe"))
		sort.Strings(dirs)
		if len(dirs) > 0 {
			ffmpeg = dirs[len(dirs)-1]
		}
	}
	if ffmpeg == "" {
		if p, err := exec.LookPath("ffmpeg"); err == nil {
			ffmpeg = p
		}
	}
	return exe, ffmpeg
}

// App drives the running CapCut instance. Every action is logged.
type App struct {
	Exe string
	L   *logx.Logger // may be nil (UI helpers)
}

func (a *App) debugf(f string, args ...any) {
	if a.L != nil {
		a.L.Debugf(f, args...)
	}
}

func (a *App) windows() []winapi.Window {
	return winapi.Windows(winapi.ProcessIDs(capcutExe))
}

// Main returns CapCut's main window (home screen or editor), if one is open.
func (a *App) Main() (winapi.Window, bool) { return mainOf(a.windows()) }

// Running reports whether CapCut shows any window at all (a lone popup counts).
func (a *App) Running() bool {
	for _, w := range a.windows() {
		if !w.IsTool() {
			return true
		}
	}
	return false
}

// Has reports whether window h is currently visible.
func (a *App) Has(h uintptr) bool {
	for _, w := range a.windows() {
		if w.HWND == h {
			return true
		}
	}
	return false
}

// Editor returns the home/editor window that is not the given home window.
func (a *App) Editor(home uintptr) (winapi.Window, bool) {
	for _, w := range a.windows() {
		if isAppWindow(w) && w.HWND != home {
			return w, true
		}
	}
	return winapi.Window{}, false
}

// Dialogs lists CapCut's popups and dialogs (export dialog, notices, update
// window…): every visible window that is neither a tooltip helper nor the
// home/editor window.
func (a *App) Dialogs() []winapi.Window { return dialogsOf(a.windows()) }

const appTitle = "CapCut"

// isAppWindow reports whether w is CapCut's home screen or editor. Both are
// full-size windows titled exactly "CapCut"; popups never are (the small
// loading notice is also titled "CapCut" but is far smaller). Telling them
// apart by title and size, not "the biggest window", matters: a popup that
// shows up before the real window is visible must not be taken for it.
func isAppWindow(w winapi.Window) bool {
	if w.IsTool() || w.Owned || w.Title != appTitle {
		return false
	}
	return w.Iconic || (w.Rect.W() >= 800 && w.Rect.H() >= 500)
}

// mainOf picks the home/editor window among ws (largest wins).
func mainOf(ws []winapi.Window) (winapi.Window, bool) {
	var best winapi.Window
	var bestArea int64 = -1
	for _, w := range ws {
		if !isAppWindow(w) {
			continue
		}
		area := int64(w.Rect.W()) * int64(w.Rect.H())
		if w.Iconic {
			area = 1 << 40
		}
		if area > bestArea {
			best, bestArea = w, area
		}
	}
	return best, bestArea >= 0
}

func dialogsOf(ws []winapi.Window) []winapi.Window {
	var out []winapi.Window
	for _, w := range ws {
		if !w.IsTool() && !isAppWindow(w) {
			out = append(out, w)
		}
	}
	return out
}

// isUpdatePopup recognises CapCut's "new version" window by its title.
func isUpdatePopup(title string) bool {
	t := strings.ToLower(title)
	for _, k := range []string{"cập nhật", "cap nhat", "update", "upgrade", "nâng cấp"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

// DescribeWindows lists every CapCut window, for the log.
func (a *App) DescribeWindows() string {
	ws := a.windows()
	if len(ws) == 0 {
		return "(không có cửa sổ CapCut)"
	}
	main, _ := mainOf(ws)
	parts := make([]string, 0, len(ws))
	for _, w := range ws {
		tag := ""
		switch {
		case w.HWND == main.HWND:
			tag = "[CHÍNH] "
		case w.IsTool():
			tag = "[tooltip] "
		}
		parts = append(parts, fmt.Sprintf("%shwnd=%d %s", tag, w.HWND, w))
	}
	return strings.Join(parts, " | ")
}

// Launch starts CapCut, waits for its main window, moves it to the primary
// monitor (optional) and maximizes it.
func (a *App) Launch(wait time.Duration, toPrimary bool, cancelled func() bool) (winapi.Window, error) {
	if a.Exe == "" {
		return winapi.Window{}, fmt.Errorf("không tìm thấy CapCut.exe — hãy đặt \"capcut_exe\" trong config.json")
	}
	cmd := exec.Command(a.Exe)
	cmd.Dir = filepath.Dir(a.Exe)
	if err := cmd.Start(); err != nil {
		return winapi.Window{}, fmt.Errorf("mở CapCut (%s): %w", a.Exe, err)
	}
	a.debugf("đã chạy %s (pid launcher %d)", a.Exe, cmd.Process.Pid)
	go cmd.Wait()
	start := time.Now()
	for time.Since(start) < wait {
		if cancelled() {
			return winapi.Window{}, errStopped
		}
		if w, ok := a.Main(); ok && (w.Iconic || w.Rect.W() > 400) {
			a.debugf("cửa sổ CapCut xuất hiện sau %v: %s", time.Since(start).Round(100*time.Millisecond), w)
			a.Arrange(w.HWND, toPrimary)
			w, _ = a.Main()
			return w, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return winapi.Window{}, fmt.Errorf("CapCut không hiện cửa sổ sau %v. Cửa sổ: %s", wait, a.DescribeWindows())
}

// Arrange maximizes the window, on the primary monitor when asked.
func (a *App) Arrange(h uintptr, toPrimary bool) {
	if toPrimary {
		wa := winapi.MoveToPrimaryMaximized(h)
		a.debugf("đưa CapCut về màn hình chính (vùng làm việc %dx%d tại %d,%d) và phóng to", wa.W(), wa.H(), wa.Left, wa.Top)
	} else {
		winapi.Restore(h)
		winapi.Maximize(h)
	}
	r := winapi.ClientRectOnScreen(h)
	a.debugf("vùng client CapCut: %dx%d tại %d,%d", r.W(), r.H(), r.Left, r.Top)
}

// Quit closes every CapCut window and waits until none is left.
func (a *App) Quit(wait time.Duration, cancelled func() bool) error {
	deadline := time.Now().Add(wait)
	round := 0
	for time.Now().Before(deadline) {
		ws := a.windows()
		if len(ws) == 0 {
			a.debugf("CapCut đã đóng hết cửa sổ (sau %d lượt gửi WM_CLOSE)", round)
			sleepCancellable(3*time.Second, cancelled)
			return nil
		}
		round++
		for _, w := range ws {
			if !w.IsTool() {
				a.debugf("WM_CLOSE → %s", w)
				winapi.CloseWindow(w.HWND)
			}
		}
		sleepCancellable(2*time.Second, cancelled)
	}
	return fmt.Errorf("CapCut không đóng sau %v: %s", wait, a.DescribeWindows())
}

// PressTo focuses window h and sends a shortcut, logging the focus result.
func (a *App) PressTo(h uintptr, shortcut, why string) error {
	k, err := winapi.ParseKey(shortcut)
	if err != nil {
		return err
	}
	before := winapi.Foreground()
	focused := winapi.Focus(h)
	after := winapi.Foreground()
	a.debugf("phím %s (%s): cửa sổ đích=%d, foreground trước=%d sau=%d, focus ok=%v", shortcut, why, h, before, after, focused)
	if !focused {
		return fmt.Errorf("không đưa được cửa sổ CapCut (hwnd %d) lên trước để gửi %s (%s); cửa sổ đang ở trước: %d", h, shortcut, why, after)
	}
	time.Sleep(150 * time.Millisecond)
	if err := winapi.Press(k); err != nil {
		return fmt.Errorf("gửi %s (%s): %w", shortcut, why, err)
	}
	return nil
}

// ClickIn clicks a point given as a fraction of a window. client=true uses
// the client area (main window), false the whole window rectangle (dialogs).
func (a *App) ClickIn(h uintptr, fx, fy float64, client, double bool, why string) error {
	if !winapi.Focus(h) {
		return fmt.Errorf("không đưa được cửa sổ hwnd %d lên trước để click (%s)", h, why)
	}
	r := winapi.WindowRect(h)
	if client {
		r = winapi.ClientRectOnScreen(h)
	}
	x := r.Left + int32(fx*float64(r.W()))
	y := r.Top + int32(fy*float64(r.H()))
	a.debugf("click%s (%s) tại (%d,%d) = tỉ lệ (%.3f,%.3f) của vùng %dx%d tại %d,%d",
		map[bool]string{true: " đúp", false: ""}[double], why, x, y, fx, fy, r.W(), r.H(), r.Left, r.Top)
	if err := winapi.Click(x, y, double); err != nil {
		return fmt.Errorf("click %s: %w", why, err)
	}
	return nil
}

// ClickPoint clicks a screen point after bringing window h to the front.
func (a *App) ClickPoint(h uintptr, x, y int32, double bool, why string) error {
	if !winapi.Focus(h) {
		return fmt.Errorf("không đưa được cửa sổ hwnd %d lên trước để click (%s)", h, why)
	}
	a.debugf("click%s (%s) tại (%d,%d)", map[bool]string{true: " đúp", false: ""}[double], why, x, y)
	if err := winapi.Click(x, y, double); err != nil {
		return fmt.Errorf("click %s: %w", why, err)
	}
	return nil
}

// CursorFraction returns the mouse position as a fraction of CapCut's client
// area (used to calibrate where the first project card is).
func (a *App) CursorFraction() (float64, float64, error) {
	w, ok := a.Main()
	if !ok {
		return 0, 0, fmt.Errorf("hãy mở CapCut ở màn hình chính trước")
	}
	r := winapi.ClientRectOnScreen(w.HWND)
	x, y := winapi.CursorPos()
	if x < r.Left || x > r.Right || y < r.Top || y > r.Bottom || r.W() == 0 || r.H() == 0 {
		return 0, 0, fmt.Errorf("chuột (%d,%d) không nằm trong cửa sổ CapCut (%dx%d tại %d,%d)", x, y, r.W(), r.H(), r.Left, r.Top)
	}
	return float64(x-r.Left) / float64(r.W()), float64(y-r.Top) / float64(r.H()), nil
}

func sleepCancellable(d time.Duration, cancelled func() bool) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cancelled != nil && cancelled() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

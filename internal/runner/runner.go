// Package runner exports a queue of CapCut drafts one by one by driving the
// CapCut app itself: open project → handle popups → export shortcut →
// confirm in the export dialog → wait for the file → close the project.
//
// Facts measured on CapCut 6.9 (Windows) that the flow relies on:
//   - home screen and editor are different top-level windows (class
//     Qt622QWindowIcon, both titled "CapCut"); the home window is hidden, not
//     destroyed, while a project is open, and comes back after Ctrl+W
//   - the export dialog is its own window (title "Xuất" / "Export")
//   - missing media shows a "Liên kết tệp phương tiện" window on open
//   - WM_CLOSE closes those dialogs like their Cancel button
package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"capcut-auto-exporter/internal/config"
	"capcut-auto-exporter/internal/detect"
	"capcut-auto-exporter/internal/drafts"
	"capcut-auto-exporter/internal/logx"
	"capcut-auto-exporter/internal/winapi"
)

var errStopped = errors.New("đã dừng")

type State string

const (
	StatePending State = "Chờ"
	StateSkipped State = "Bỏ qua"
	StateOpening State = "Đang mở"
	StateExport  State = "Đang xuất"
	StateClosing State = "Đang đóng"
	StateDone    State = "Xong"
	StateWarn    State = "Xong (cảnh báo)"
	StateFailed  State = "Lỗi"
)

type Item struct {
	Draft  drafts.Draft
	State  State
	Detail string
	Output string
	Took   time.Duration
}

type Callbacks struct {
	Update func(index int, it Item)
	// AskOpen is used in manual mode: show the user which project to open and
	// block until they confirm (true) or stop (false).
	AskOpen func(d drafts.Draft) bool
}

type Runner struct {
	cfg     config.Config
	items   []Item
	cb      Callbacks
	app     *App
	ffmpeg  string
	stop    atomic.Bool
	backups bool
	L       *logx.Logger

	homeHWND     uintptr         // CapCut home window, known after a launch in auto mode
	exportDlg    uintptr         // export dialog of the export in progress: never close it
	missingMedia bool            // the current project reported missing media
	updTried     map[uintptr]int // update popups already handled (attempt counts)
}

// stepError is a failure with the step it happened in, for the log and UI.
type stepError struct {
	step string
	err  error
	shot string
}

func (e *stepError) Error() string {
	s := fmt.Sprintf("[%s] %v", e.step, e.err)
	if e.shot != "" {
		s += " (ảnh: " + filepath.Base(e.shot) + ")"
	}
	return s
}
func (e *stepError) Unwrap() error { return e.err }

func New(cfg config.Config, list []drafts.Draft, l *logx.Logger, cb Callbacks) *Runner {
	exe, ff := FindCapCut(cfg.CapCutExe)
	r := &Runner{cfg: cfg, cb: cb, ffmpeg: ff, L: l}
	r.app = &App{Exe: exe, L: l}
	for _, d := range list {
		r.items = append(r.items, Item{Draft: d, State: StatePending})
	}
	return r
}

func (r *Runner) Stop()         { r.stop.Store(true) }
func (r *Runner) stopped() bool { return r.stop.Load() }

// Stopped reports whether the queue was stopped before the end (by the user
// or because CapCut was left mid-export).
func (r *Runner) Stopped() bool { return r.stop.Load() }

func (r *Runner) set(i int, st State, detail string) {
	r.items[i].State, r.items[i].Detail = st, detail
	r.L.Debugf("trạng thái [%s] %s: %s", r.items[i].Draft.Name, st, detail)
	if r.cb.Update != nil {
		r.cb.Update(i, r.items[i])
	}
}

// fail builds a step error, with a screenshot of the moment it happened.
func (r *Runner) fail(step string, err error) error {
	if errors.Is(err, errStopped) {
		return errStopped
	}
	r.L.Debugf("cửa sổ lúc lỗi (%s): %s", step, r.app.DescribeWindows())
	return &stepError{step: step, err: err, shot: r.L.Shot("loi-" + step)}
}

func (r *Runner) shot(label string) {
	if r.cfg.DebugScreenshots {
		r.L.Shot(label)
	}
}

func (r *Runner) validate() error {
	c := r.cfg
	if c.OutputDir == "" {
		return fmt.Errorf("chưa chọn thư mục output")
	}
	if st, err := os.Stat(c.OutputDir); err != nil || !st.IsDir() {
		return fmt.Errorf("thư mục output không tồn tại: %q", c.OutputDir)
	}
	for name, k := range map[string]string{"phím xuất": c.ExportKey, "phím xác nhận": c.ConfirmKey, "phím đóng": c.CloseKey, "phím popup": c.DismissKey} {
		if _, err := winapi.ParseKey(k); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if c.OpenMode == config.OpenAuto {
		if r.app.Exe == "" {
			return fmt.Errorf("không tìm thấy CapCut.exe")
		}
		if c.CardX <= 0 || c.CardY <= 0 {
			return fmt.Errorf("chưa có vị trí thẻ project đầu tiên — bấm \"Mở CapCut\" rồi \"Hiệu chỉnh vị trí thẻ\"")
		}
	}
	return nil
}

// Run processes the whole queue. It blocks until done or stopped.
func (r *Runner) Run() {
	defer r.L.Recover("Run")
	if err := r.validate(); err != nil {
		r.L.Errorf("%v", err)
		return
	}
	winapi.KeepAwake(true)
	defer winapi.KeepAwake(false)

	c := r.cfg
	r.L.Infof("File log: %s", r.L.Path())
	r.L.Infof("CapCut: %s · ffmpeg: %s", orNone(r.app.Exe), orNone(r.ffmpeg))
	r.L.Infof("Phím: xuất=%s, xác nhận=%s, đóng=%s, popup=%s · chế độ mở: %s · output: %s",
		c.ExportKey, c.ConfirmKey, c.CloseKey, c.DismissKey, c.OpenMode, c.OutputDir)
	r.L.Debugf("cấu hình đầy đủ: %+v", c)
	wa := winapi.PrimaryWorkArea()
	r.L.Debugf("màn hình chính (vùng làm việc): %dx%d tại %d,%d", wa.W(), wa.H(), wa.Left, wa.Top)

	ok, warn, skip, fail := 0, 0, 0, 0
	for i := range r.items {
		if r.stopped() {
			break
		}
		start := time.Now()
		err := r.exportOne(i)
		r.items[i].Took = time.Since(start)
		switch {
		case err == nil:
			if r.items[i].State == StateWarn {
				warn++
			} else {
				ok++
			}
		case errors.Is(err, errSkip):
			skip++
		default:
			fail++
			msg := err.Error()
			if errors.Is(err, errStopped) {
				msg = "đã dừng"
			}
			r.set(i, StateFailed, msg)
			r.L.Errorf("%s: %s", r.items[i].Draft.Name, msg)
			r.recover()
		}
		r.L.Infof("-- %s: %s sau %s", r.items[i].Draft.Name, r.items[i].State, r.items[i].Took.Round(time.Second))
	}
	r.L.Infof("== Kết thúc: %d xong, %d cảnh báo, %d bỏ qua, %d lỗi / %d project ==", ok, warn, skip, fail, len(r.items))
	r.L.Infof("Log chi tiết: %s", r.L.Path())
}

var errSkip = errors.New("skip")

func (r *Runner) exportOne(i int) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("lỗi nghiêm trọng: %v", p)
			r.L.Errorf("panic khi xử lý %s: %v", r.items[i].Draft.Name, p)
		}
	}()
	d := r.items[i].Draft
	c := r.cfg
	r.missingMedia = false
	r.L.Infof("-- [%d/%d] %s (%s) · %s", i+1, len(r.items), d.Name, d.Duration().Round(time.Second), d.Folder)

	// 1. Existing output.
	if old := detect.ExistingExport(c.OutputDir, d.Name); old != "" {
		if c.ExistingPolicy == "backup" {
			bak := old + "." + time.Now().Format("20060102-150405") + ".bak"
			if err := os.Rename(old, bak); err != nil {
				return r.fail("kiem-tra-output", fmt.Errorf("không đổi tên được file cũ %s: %w", filepath.Base(old), err))
			}
			r.L.Infof("Đã đổi tên file cũ → %s", filepath.Base(bak))
		} else {
			r.set(i, StateSkipped, "đã có "+filepath.Base(old))
			r.L.Infof("Bỏ qua, output đã có %s", filepath.Base(old))
			return errSkip
		}
	}

	// 2. Open the project in the editor.
	r.set(i, StateOpening, "")
	editor, err := r.open(i)
	if err != nil {
		return err
	}

	// 3. Popups shown when the project opens (missing media, notices…).
	// 3. Let the project load. Popups that show up meanwhile (a small loading
	// notice, then "Liên kết tệp phương tiện" when media is missing) are only
	// recorded here — closing a loading notice could interrupt the load.
	if err := r.waitProjectLoad(i); err != nil {
		return err
	}
	// Then deal with whatever is still open, and export.
	if err := r.clearPopups("sau-khi-tai", true); err != nil {
		return err
	}
	sleepCancellable(time.Second, r.stopped)
	r.shot("editor-san-sang")

	// 4. Open the export dialog and start the export.
	r.set(i, StateExport, "mở hộp thoại xuất")
	before := detect.Take(c.OutputDir)
	r.L.Debugf("output trước khi xuất: %d file video", len(before))
	exportStart := time.Now()
	dlg, err := r.startExport(editor, d, before)
	if err != nil {
		return err
	}
	r.exportDlg = dlg.HWND
	abort := r.exportWatch(dlg, d, before)

	// 5. Wait until CapCut has written the file.
	var total time.Duration
	if c.MaxExportMin > 0 {
		total = time.Duration(c.MaxExportMin) * time.Minute
	}
	r.L.Infof("Đang chờ CapCut xuất vào %s — không giới hạn thời gian; chỉ dừng nếu tiến trình đứng yên %d phút", c.OutputDir, c.StallMin)
	lastUI, lastWin := time.Time{}, ""
	res := detect.Wait(detect.Options{
		Dir: c.OutputDir, Before: before, DraftName: d.Name,
		Expected: d.Duration(), Tolerance: time.Duration(c.DurationTolSec * float64(time.Second)),
		StableFor: time.Duration(c.StableSec) * time.Second, FFmpeg: r.ffmpeg,
		TotalLimit: total,
		Cancelled:  r.stopped,
		Abort:      abort,
		OnStatus: func(s detect.Status) {
			detail := s.Message
			if s.Path != "" {
				lock := "CapCut đang ghi"
				if !s.Locked {
					lock = fmt.Sprintf("ổn định %ds", int(s.Stable.Seconds()))
				}
				detail = fmt.Sprintf("%s · %.1f MB · %s", filepath.Base(s.Path), float64(s.Size)/1e6, lock)
			}
			r.set(i, StateExport, detail)
			if time.Since(lastUI) > 30*time.Second {
				lastUI = time.Now()
				r.L.Infof("   %s (đã %s)", detail, time.Since(exportStart).Round(time.Second))
			}
			// Any window change while exporting (progress, popups) goes to the log.
			if w := r.app.DescribeWindows(); w != lastWin {
				lastWin = w
				r.L.Debugf("cửa sổ khi đang xuất: %s", w)
			}
		},
	})

	state, note := StateDone, ""
	switch {
	case res.Err == detect.ErrCancelled:
		return errStopped
	case res.Err == detect.ErrDuration:
		state = StateWarn
		note = fmt.Sprintf("thời lượng %s ≠ project %s", res.Duration.Round(100*time.Millisecond), d.Duration().Round(100*time.Millisecond))
		r.L.Warnf("%s: %s", d.Name, note)
	case res.Err != nil:
		r.L.Debugf("popup đang mở khi lỗi: %s", r.describeDialogs())
		return r.fail("cho-file", res.Err)
	}
	r.items[i].Output = res.Path
	if r.missingMedia {
		state = StateWarn
		note = joinNonEmpty(note, "project thiếu media — kiểm tra video")
	}
	r.exportDlg = 0
	if res.HeldOpen {
		r.L.Debugf("CapCut vẫn giữ file mở (màn hình hoàn tất của hộp thoại xuất) — xác nhận xong bằng dung lượng + thời lượng")
	}
	r.L.Infof("[OK] Xuất xong: %s · %.1f MB · thời lượng %s · mất %s", filepath.Base(res.Path),
		fileMB(res.Path), res.Duration.Round(100*time.Millisecond), time.Since(exportStart).Round(time.Second))
	r.shot("xuat-xong")

	// 6. Close the project.
	r.set(i, StateClosing, "")
	if err := r.close(editor); err != nil {
		r.L.Warnf("Đóng project: %v", err)
	}
	r.set(i, state, joinNonEmpty(filepath.Base(res.Path), note))
	return nil
}

// open gets draft i into the CapCut editor and returns the editor window.
func (r *Runner) open(i int) (uintptr, error) {
	d := r.items[i].Draft
	c := r.cfg
	if c.OpenMode == config.OpenManual {
		r.L.Infof("Chờ bạn mở project %q trong CapCut…", d.Name)
		if r.cb.AskOpen == nil || !r.cb.AskOpen(d) {
			return 0, errStopped
		}
		w, ok := r.app.Main()
		if !ok {
			return 0, r.fail("mo-project", fmt.Errorf("không thấy cửa sổ CapCut"))
		}
		r.homeHWND = 0
		r.L.Infof("Dùng cửa sổ editor: hwnd=%d %s", w.HWND, w)
		r.L.Debugf("cửa sổ: %s", r.app.DescribeWindows())
		return w.HWND, nil
	}

	// Auto: CapCut must be closed to edit the draft list safely.
	if r.app.Running() {
		r.L.Infof("Đóng CapCut để sắp xếp lại danh sách project…")
		if err := r.app.Quit(60*time.Second, r.stopped); err != nil {
			return 0, r.fail("dong-capcut", err)
		}
	}
	if !r.backups {
		bak, err := drafts.Backup(c.DraftsDir)
		if err != nil {
			return 0, r.fail("sao-luu", fmt.Errorf("sao lưu root_meta_info.json: %w", err))
		}
		r.backups = true
		r.L.Infof("Đã sao lưu danh sách project → %s", bak)
	}
	if err := drafts.PromoteToTop(c.DraftsDir, d); err != nil {
		return 0, r.fail("dua-len-dau", err)
	}
	if ds, err := drafts.List(c.DraftsDir); err == nil && len(ds) > 0 {
		r.L.Debugf("project đầu danh sách sau khi sắp xếp: %q", ds[0].Name)
		if ds[0].Folder != d.Folder {
			return 0, r.fail("dua-len-dau", fmt.Errorf("project đầu danh sách là %q, không phải %q", ds[0].Name, d.Name))
		}
	}
	r.L.Infof("Đã đưa %q lên đầu danh sách, mở CapCut…", d.Name)
	home, err := r.app.Launch(time.Duration(c.LaunchWaitSec)*time.Second, c.MoveToPrimary, r.stopped)
	if err != nil {
		return 0, r.fail("mo-capcut", err)
	}
	r.homeHWND = home.HWND
	r.L.Infof("Màn hình chính CapCut: hwnd=%d %s", home.HWND, home)
	r.waitHomeStable(home.HWND)
	if r.stopped() {
		return 0, errStopped
	}
	r.shot("man-hinh-chinh")

	// Click the first card until the editor window replaces the home window.
	tries := c.OpenRetries
	if tries < 1 {
		tries = 1
	}
	for attempt := 1; attempt <= tries; attempt++ {
		double := attempt == tries && tries > 1 // last resort: double-click
		r.closeUpdatePopups("truoc-khi-click")
		if r.app.Has(home.HWND) {
			x, y, how := r.locateFirstCard(home.HWND)
			r.L.Infof("Click thẻ project đầu tiên tại (%d,%d) — %s (lần %d/%d%s)", x, y, how, attempt, tries, map[bool]string{true: ", click đúp", false: ""}[double])
			if err := r.app.ClickPoint(home.HWND, x, y, double, "thẻ project đầu tiên"); err != nil {
				return 0, r.fail("click-the", err)
			}
			r.L.Debugf("foreground sau click: %d (home=%d)", winapi.Foreground(), home.HWND)
			r.shot(fmt.Sprintf("sau-click-%d", attempt))
		}
		deadline := time.Now().Add(time.Duration(c.OpenWaitSec) * time.Second)
		for time.Now().Before(deadline) && !r.stopped() {
			r.closeUpdatePopups("cho-editor")
			if w, ok := r.app.Editor(home.HWND); ok {
				r.L.Infof("Editor đã mở: hwnd=%d %s", w.HWND, w)
				r.L.Debugf("cửa sổ: %s", r.app.DescribeWindows())
				r.app.Arrange(w.HWND, false)
				return w.HWND, nil
			}
			time.Sleep(400 * time.Millisecond)
		}
		if r.stopped() {
			return 0, errStopped
		}
		r.L.Warnf("Lần %d: editor chưa mở sau %ds. Cửa sổ: %s", attempt, c.OpenWaitSec, r.app.DescribeWindows())
		r.L.Shot(fmt.Sprintf("click-khong-mo-%d", attempt))
		if dl := r.app.Dialogs(); len(dl) > 0 {
			r.L.Warnf("Có hộp thoại đang che: %s — đóng lại", r.describeDialogs())
			r.closeDialogs("truoc-khi-click-lai")
		}
	}
	return 0, r.fail("mo-project", fmt.Errorf("click %d lần vào thẻ đầu tiên mà editor không mở — kiểm tra vị trí thẻ (Hiệu chỉnh) hoặc dùng chế độ Thủ công", tries))
}

func isMissingMedia(title string) bool {
	t := strings.ToLower(title)
	for _, k := range []string{"liên kết tệp", "lien ket tep", "link media", "missing", "relink", "phương tiện bị mất"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

func isExportDialog(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	return strings.HasPrefix(t, "xuất") || strings.HasPrefix(t, "export")
}

func (r *Runner) describeDialogs() string {
	dl := r.app.Dialogs()
	if len(dl) == 0 {
		return "(không có)"
	}
	parts := make([]string, len(dl))
	for k, w := range dl {
		parts[k] = fmt.Sprintf("hwnd=%d %s", w.HWND, w)
	}
	return strings.Join(parts, " | ")
}

// clearPopups closes every dialog over the editor. A missing-media popup
// fails the project unless the config says to continue.
func (r *Runner) clearPopups(phase string, checkMedia bool) error {
	r.closeUpdatePopups(phase)
	for round := 0; round < 5; round++ {
		dl := r.app.Dialogs()
		if len(dl) == 0 {
			if round == 0 {
				r.L.Debugf("không có popup (%s)", phase)
			}
			return nil
		}
		for _, w := range dl {
			r.L.Infof("Popup (%s): %q [%s] %dx%d", phase, w.Title, w.Class, w.Rect.W(), w.Rect.H())
			if checkMedia && isMissingMedia(w.Title) {
				r.L.Shot("thieu-media")
				if r.cfg.SkipMissingMedia {
					winapi.CloseWindow(w.HWND)
					return r.fail(phase, fmt.Errorf("project thiếu file phương tiện (popup %q) — bỏ qua theo tùy chọn \"Bỏ qua project thiếu media\"", w.Title))
				}
				r.L.Warnf("Project thiếu file phương tiện — bấm Hủy trên popup và vẫn xuất (phần media bị thiếu sẽ trống trong video)")
				r.missingMedia = true
				if r.cancelPopup(w) {
					continue
				}
			}
			r.L.Debugf("WM_CLOSE → popup hwnd=%d %q", w.HWND, w.Title)
			winapi.CloseWindow(w.HWND)
		}
		sleepCancellable(1200*time.Millisecond, r.stopped)
	}
	if dl := r.app.Dialogs(); len(dl) > 0 {
		return r.fail(phase, fmt.Errorf("không đóng được popup: %s", r.describeDialogs()))
	}
	return nil
}

func (r *Runner) closeDialogs(phase string) {
	for _, w := range r.app.Dialogs() {
		r.L.Debugf("WM_CLOSE (%s) → hwnd=%d %q", phase, w.HWND, w.Title)
		winapi.CloseWindow(w.HWND)
	}
	sleepCancellable(time.Second, r.stopped)
}

// startExport opens the export dialog and presses its Export button.
// It returns the export dialog as it looked before the export started.
func (r *Runner) startExport(editor uintptr, d drafts.Draft, before detect.Snapshot) (winapi.Window, error) {
	c := r.cfg
	var dlg winapi.Window
	for attempt := 1; attempt <= 2 && dlg.HWND == 0; attempt++ {
		if err := r.app.PressTo(editor, c.ExportKey, "mở hộp thoại xuất"); err != nil {
			return winapi.Window{}, r.fail("phim-xuat", err)
		}
		deadline := time.Now().Add(time.Duration(c.DialogWaitSec+5) * time.Second)
		for time.Now().Before(deadline) && !r.stopped() {
			for _, w := range r.app.Dialogs() {
				if isExportDialog(w.Title) {
					dlg = w
				}
			}
			if dlg.HWND != 0 {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
		if r.stopped() {
			return winapi.Window{}, errStopped
		}
		if dlg.HWND == 0 {
			r.L.Warnf("Lần %d: %s không mở hộp thoại xuất. Cửa sổ: %s", attempt, c.ExportKey, r.app.DescribeWindows())
			r.L.Shot(fmt.Sprintf("khong-co-hop-thoai-xuat-%d", attempt))
			if err := r.clearPopups("truoc-khi-xuat-lai", true); err != nil {
				return winapi.Window{}, err
			}
		}
	}
	if dlg.HWND == 0 {
		return winapi.Window{}, r.fail("phim-xuat", fmt.Errorf("phím %s không mở hộp thoại xuất (kiểm tra phím tắt xuất trong CapCut)", c.ExportKey))
	}
	r.L.Infof("Hộp thoại xuất: %q hwnd=%d %dx%d tại %d,%d", dlg.Title, dlg.HWND, dlg.Rect.W(), dlg.Rect.H(), dlg.Rect.Left, dlg.Rect.Top)
	sleepCancellable(time.Duration(c.DialogWaitSec)*time.Second, r.stopped)
	r.shot("hop-thoai-xuat")

	started := func() (bool, string) {
		if p := detect.FindNew(c.OutputDir, before, d.Name); p != "" {
			return true, "đã thấy file " + filepath.Base(p)
		}
		if !winapi.IsWindow(dlg.HWND) {
			return true, "hộp thoại xuất đã đóng"
		}
		// The dialog swaps its settings view for the "Đang xuất" progress view
		// (a different height) as soon as the export starts. Long exports may
		// not create the output file until much later.
		if now := winapi.WindowRect(dlg.HWND); abs(int(now.H())-int(dlg.Rect.H())) > 8 {
			return true, fmt.Sprintf("hộp thoại chuyển sang màn hình đang xuất (%dx%d → %dx%d)", dlg.Rect.W(), dlg.Rect.H(), now.W(), now.H())
		}
		for _, w := range r.app.Dialogs() {
			if w.HWND == dlg.HWND {
				return false, ""
			}
		}
		return true, "hộp thoại xuất không còn hiển thị"
	}
	waitStart := func(limit time.Duration) (bool, string) {
		end := time.Now().Add(limit)
		for time.Now().Before(end) && !r.stopped() {
			if ok, why := started(); ok {
				return true, why
			}
			time.Sleep(400 * time.Millisecond)
		}
		return false, ""
	}

	// a) the confirm key
	if err := r.app.PressTo(dlg.HWND, c.ConfirmKey, "bấm Xuất trong hộp thoại"); err != nil {
		return winapi.Window{}, r.fail("xac-nhan", err)
	}
	if ok, why := waitStart(8 * time.Second); ok {
		r.L.Infof("Đã bắt đầu xuất bằng phím %s (%s)", c.ConfirmKey, why)
		r.shot("dang-xuat")
		return dlg, nil
	}
	if r.stopped() {
		return winapi.Window{}, errStopped
	}
	// b) click the Export button
	r.L.Warnf("Phím %s không bắt đầu xuất — click nút Xuất trong hộp thoại", c.ConfirmKey)
	r.L.Shot("phim-xac-nhan-khong-chay")
	if err := r.app.ClickIn(dlg.HWND, c.ExportBtnX, c.ExportBtnY, false, false, "nút Xuất"); err != nil {
		return winapi.Window{}, r.fail("xac-nhan", err)
	}
	if ok, why := waitStart(10 * time.Second); ok {
		r.L.Infof("Đã bắt đầu xuất bằng click nút Xuất (%s)", why)
		r.shot("dang-xuat")
		return dlg, nil
	}
	if r.stopped() {
		return winapi.Window{}, errStopped
	}
	return winapi.Window{}, r.fail("xac-nhan", fmt.Errorf("hộp thoại xuất vẫn mở sau phím %s và click nút Xuất — chỉnh export_btn_x/y trong config.json. Popup: %s", c.ConfirmKey, r.describeDialogs()))
}

// close dismisses popups, closes the project and (in auto mode) quits CapCut.
func (r *Runner) close(editor uintptr) error {
	c := r.cfg
	if dl := r.app.Dialogs(); len(dl) > 0 {
		r.L.Debugf("popup sau khi xuất: %s", r.describeDialogs())
		r.closeDialogs("sau-khi-xuat")
	}
	closed := func() bool {
		w, ok := r.app.Main()
		if !ok {
			return false // between windows; check again
		}
		if r.homeHWND != 0 {
			return w.HWND == r.homeHWND
		}
		return w.HWND != editor
	}
	done := false
	for attempt := 1; attempt <= 2 && !done; attempt++ {
		if err := r.app.PressTo(editor, c.CloseKey, "đóng project"); err != nil {
			r.L.Warnf("%v", err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if closed() {
				w, _ := r.app.Main()
				r.L.Infof("Đã đóng project (về màn hình chính hwnd=%d)", w.HWND)
				done = true
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !done {
			r.L.Warnf("Lần %d: %s chưa đóng project. Cửa sổ: %s", attempt, c.CloseKey, r.app.DescribeWindows())
			r.L.Shot(fmt.Sprintf("chua-dong-project-%d", attempt))
			r.closeDialogs("dong-project-lai")
		}
	}
	if c.OpenMode == config.OpenAuto && c.RestartPerDraft {
		if err := r.app.Quit(60*time.Second, r.stopped); err != nil {
			return err
		}
	}
	if !done {
		return fmt.Errorf("%s không đóng được project", c.CloseKey)
	}
	return nil
}

// recover tries to get CapCut back to a clean state after a failure.
func (r *Runner) recover() {
	defer r.L.Recover("recover")
	if !r.app.Running() {
		return
	}
	r.L.Debugf("khôi phục sau lỗi. Cửa sổ: %s", r.app.DescribeWindows())
	if r.exportDlg != 0 && winapi.IsWindow(r.exportDlg) {
		// Closing the export dialog mid-export makes CapCut ask to stop the
		// export. Leave CapCut exactly as it is and stop the queue instead.
		r.L.Errorf("CapCut vẫn đang ở hộp thoại xuất — tool KHÔNG đóng để tránh hủy video đang xuất, và dừng hàng đợi. Kiểm tra CapCut rồi chạy lại các project còn lại.")
		r.L.Shot("dung-hang-doi-khi-dang-xuat")
		r.Stop()
		return
	}
	r.exportDlg = 0
	r.closeDialogs("khoi-phuc")
	if w, ok := r.app.Main(); ok && r.homeHWND != 0 && w.HWND != r.homeHWND {
		_ = r.app.PressTo(w.HWND, r.cfg.CloseKey, "đóng project sau lỗi")
		time.Sleep(3 * time.Second)
	}
	if r.cfg.OpenMode == config.OpenAuto {
		if err := r.app.Quit(60*time.Second, func() bool { return false }); err != nil {
			r.L.Warnf("%v", err)
		}
	}
}

func fileMB(p string) float64 {
	if st, err := os.Stat(p); err == nil {
		return float64(st.Size()) / 1e6
	}
	return 0
}

func orNone(s string) string {
	if s == "" {
		return "(không có)"
	}
	return s
}

func joinNonEmpty(a, b string) string {
	if b == "" {
		return a
	}
	return a + " · " + b
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// waitProjectLoad waits ProjectLoadSec after the editor opens, logging (and
// photographing once) every popup that appears, without touching any.
func (r *Runner) waitProjectLoad(i int) error {
	c := r.cfg
	wait := time.Duration(c.ProjectLoadSec) * time.Second
	r.set(i, StateOpening, fmt.Sprintf("chờ project tải (%ds)", c.ProjectLoadSec))
	r.L.Infof("Chờ %ds cho project tải xong rồi mới kiểm tra popup", c.ProjectLoadSec)
	open := map[uintptr]string{} // popups currently shown
	logged := map[uintptr]bool{}
	start := time.Now()
	for time.Since(start) < wait {
		if r.stopped() {
			return errStopped
		}
		r.closeUpdatePopups("khi-tai-project")
		for _, w := range r.app.Dialogs() {
			if isUpdatePopup(w.Title) {
				continue
			}
			if !logged[w.HWND] {
				logged[w.HWND] = true
				open[w.HWND] = w.Title
				r.L.Infof("Popup khi đang tải (sau %s): %q %dx%d — chờ hết thời gian tải rồi mới xử lý", time.Since(start).Round(100*time.Millisecond), w.Title, w.Rect.W(), w.Rect.H())
				r.L.Shot("popup-khi-tai")
			}
		}
		for h, title := range open {
			if !winapi.IsWindow(h) || !r.dialogVisible(h) {
				r.L.Debugf("popup %q tự đóng sau %s", title, time.Since(start).Round(100*time.Millisecond))
				delete(open, h)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

// cancelPopup presses Hủy on a popup (Escape = its Cancel button), falling
// back to WM_CLOSE, and reports whether it is gone.
func (r *Runner) cancelPopup(w winapi.Window) bool {
	if err := r.app.PressTo(w.HWND, r.cfg.DismissKey, "bấm Hủy trên popup "+w.Title); err != nil {
		r.L.Debugf("%v", err)
	}
	for k := 0; k < 6; k++ {
		time.Sleep(300 * time.Millisecond)
		if !winapi.IsWindow(w.HWND) || !r.dialogVisible(w.HWND) {
			r.L.Infof("Đã bấm Hủy trên popup %q", w.Title)
			return true
		}
	}
	r.L.Debugf("popup %q chưa đóng sau %s — gửi WM_CLOSE", w.Title, r.cfg.DismissKey)
	winapi.CloseWindow(w.HWND)
	time.Sleep(800 * time.Millisecond)
	gone := !winapi.IsWindow(w.HWND) || !r.dialogVisible(w.HWND)
	if gone {
		r.L.Infof("Đã đóng popup %q", w.Title)
	}
	return gone
}

// closeUpdatePopups closes CapCut's "new version available" window without
// updating. It can appear at any time (home screen, right after a project
// opens, mid-export) and covers the editor, so it is handled at every stage.
// It returns how many were closed this call.
func (r *Runner) closeUpdatePopups(phase string) int {
	if r.updTried == nil {
		r.updTried = map[uintptr]int{}
	}
	n := 0
	for _, w := range r.app.Dialogs() {
		if !isUpdatePopup(w.Title) {
			continue
		}
		tries := r.updTried[w.HWND]
		if tries == 0 {
			r.L.Infof("Popup cập nhật phiên bản (%s): %q %dx%d — đóng, không cập nhật", phase, w.Title, w.Rect.W(), w.Rect.H())
			r.L.Shot("popup-cap-nhat")
		} else if tries >= 8 {
			continue // refuses to close; the stage's own checks will report it
		} else {
			r.L.Debugf("popup cập nhật vẫn còn (lần %d), thử lại", tries)
		}
		r.updTried[w.HWND] = tries + 1
		if tries%2 == 0 {
			winapi.CloseWindow(w.HWND)
		} else if err := r.app.PressTo(w.HWND, r.cfg.DismissKey, "đóng popup cập nhật"); err != nil {
			r.L.Debugf("%v", err)
		}
		n++
	}
	if n > 0 {
		sleepCancellable(900*time.Millisecond, r.stopped)
	}
	return n
}

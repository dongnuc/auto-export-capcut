package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"capcut-auto-exporter/internal/config"
	"capcut-auto-exporter/internal/drafts"
	"capcut-auto-exporter/internal/logx"
	"capcut-auto-exporter/internal/runner"
	"capcut-auto-exporter/internal/winapi"
)

const (
	appVersion = "1.0.0"
	stopHotkey = "Ctrl+Alt+Q"
)

type page int

const (
	pageExport page = iota
	pageHistory
	pageSettings
	pageAbout
)

// row is one project in the list.
type row struct {
	draft drafts.Draft
	check widget.Bool
	item  runner.Item
	has   bool // item set by the runner

	editing            bool
	wantFocus          bool
	nameEd             widget.Editor
	saveBtn, cancelBtn widget.Clickable
	playBtn, moreBtn   widget.Clickable
}

type ui struct {
	win *app.Window
	th  *material.Theme
	cfg config.Config

	page    page
	navBtns [4]widget.Clickable
	helpBtn widget.Clickable

	// export page
	draftsEd, outputEd                          widget.Editor
	draftsBtn, outputBtn, reloadBtn, reloadBtn2 widget.Clickable
	openOutBtn                                  widget.Clickable
	exportKey, confirmKey, closeKey             *combo
	dialogWait, projectLoad, stall              *numField
	openMode                                    widget.Enum
	launchBtn, calibrateBtn                     widget.Clickable
	backupOld, skipMissing, shutdownWhenDone    widget.Bool
	startBtn, stopBtn, continueBtn              widget.Clickable
	cancelShutdownBtn                           widget.Clickable
	allBtn, noneBtn, filterBtn                  widget.Clickable
	filter                                      statusFilter
	searchEd                                    widget.Editor
	list                                        widget.List

	// log
	logEd                                       widget.Editor
	copyLogBtn, clearLogBtn, logDirBtn, logTogl widget.Clickable
	logCollapsed                                bool
	logLines                                    []string
	logDirty                                    bool

	mu              sync.Mutex
	rows            []*row
	running         bool
	run             *runner.Runner
	askName         string
	askCh           chan bool
	status          string
	shutdownPending bool

	pendingDrafts, pendingOutput string // set by folder-picker goroutines

	// pop-up menu and pointer tracking
	menu   menu
	ptrTag byte
	ptrPos f32.Point

	hist      histState
	set       settingsState
	aboutList widget.List
}

func newUI(w *app.Window) *ui {
	u := &ui{win: w, cfg: config.Load()}
	u.th = material.NewTheme()
	u.th.Shaper = text.NewShaper(text.WithCollection(loadFonts()))
	u.th.Palette.ContrastBg = cPrimary
	u.th.Palette.Fg = cText
	u.th.Palette.Bg = cSurface

	u.draftsEd.SingleLine, u.outputEd.SingleLine, u.searchEd.SingleLine = true, true, true
	u.logEd.ReadOnly = true
	u.list.Axis = layout.Vertical
	u.aboutList.Axis = layout.Vertical

	u.draftsEd.SetText(u.cfg.DraftsDir)
	u.outputEd.SetText(u.cfg.OutputDir)
	u.exportKey = newCombo(u.cfg.ExportKey, "Ctrl+E", "Ctrl+M", "Ctrl+Shift+E", "F5")
	u.confirmKey = newCombo(u.cfg.ConfirmKey, "Enter", "Space")
	u.closeKey = newCombo(u.cfg.CloseKey, "Ctrl+W", "Ctrl+F4", "Alt+F4")
	u.dialogWait = newNum(u.cfg.DialogWaitSec, 1, 120)
	u.projectLoad = newNum(u.cfg.ProjectLoadSec, 5, 600)
	u.stall = newNum(u.cfg.StallMin, 1, 240)
	u.openMode.Value = u.cfg.OpenMode
	u.backupOld.Value = u.cfg.ExistingPolicy == "backup"
	u.skipMissing.Value = u.cfg.SkipMissingMedia
	u.initSettings()

	if err := winapi.WatchHotkey(stopHotkey, func() { u.stop("phím " + stopHotkey) }); err != nil {
		u.log("[!] Không đăng ký được phím dừng: %v", err)
	} else {
		u.log("Phím dừng khẩn cấp: %s", stopHotkey)
	}
	exe, ff := runner.FindCapCut(u.cfg.CapCutExe)
	u.log("CapCut: %s · ffmpeg: %s", orDash(exe), orDash(ff))
	u.loadDrafts(false)
	return u
}

// loadFonts prefers Segoe UI (full Vietnamese coverage), with Go fonts as fallback.
func loadFonts() []font.FontFace {
	var faces []font.FontFace
	dir := os.Getenv("WINDIR")
	if dir == "" {
		dir = `C:\Windows`
	}
	for _, f := range []struct {
		file   string
		weight font.Weight
	}{
		{"segoeui.ttf", font.Normal},
		{"segoeuib.ttf", font.Bold},
		{"segoeuib.ttf", font.SemiBold}, // no semibold file ships with Windows: bold stands in
	} {
		data, err := os.ReadFile(dir + `\Fonts\` + f.file)
		if err != nil {
			continue
		}
		fs, err := opentype.ParseCollection(data)
		if err != nil {
			continue
		}
		for i := range fs {
			fs[i].Font.Typeface = "Segoe UI"
			fs[i].Font.Weight = f.weight
		}
		faces = append(faces, fs...)
	}
	return append(faces, gofont.Collection()...)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (u *ui) log(f string, a ...any) {
	line := time.Now().Format("15:04:05 ") + fmt.Sprintf(f, a...)
	u.mu.Lock()
	u.logLines = append(u.logLines, line)
	if len(u.logLines) > 2000 {
		u.logLines = u.logLines[len(u.logLines)-2000:]
	}
	u.logDirty = true
	u.mu.Unlock()
	u.win.Invalidate()
}

func (u *ui) isRunning() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.running
}

// loadDrafts reads the project list again from the folder in the path field.
// Rows that are still there keep their selection and run status. CapCut
// rewrites names and times when it opens a project, so this is also done after
// every run.
func (u *ui) loadDrafts(announce bool) {
	dir := strings.TrimSpace(u.draftsEd.Text())
	ds, err := drafts.List(dir)
	if err != nil {
		u.log("[LỖI] Không đọc được danh sách project từ %q: %v", dir, err)
		return
	}
	u.mu.Lock()
	old := map[string]*row{}
	for _, r := range u.rows {
		old[r.draft.Folder] = r
	}
	rows := make([]*row, 0, len(ds))
	for _, d := range ds {
		if r, ok := old[d.Folder]; ok {
			r.draft = d
			rows = append(rows, r)
		} else {
			rows = append(rows, &row{draft: d})
		}
	}
	u.rows = rows
	u.mu.Unlock()
	if announce {
		u.log("Đã tải lại %d project từ %s", len(ds), dir)
	}
}

// readCfg copies the export-page fields into the config and saves it.
func (u *ui) readCfg() config.Config {
	u.mu.Lock()
	c := u.cfg
	u.mu.Unlock()
	c.DraftsDir = strings.TrimSpace(u.draftsEd.Text())
	c.OutputDir = strings.TrimSpace(u.outputEd.Text())
	if t := u.exportKey.text(); t != "" {
		c.ExportKey = t
	}
	if t := u.confirmKey.text(); t != "" {
		c.ConfirmKey = t
	}
	if t := u.closeKey.text(); t != "" {
		c.CloseKey = t
	}
	c.DialogWaitSec = u.dialogWait.val(c.DialogWaitSec)
	c.ProjectLoadSec = u.projectLoad.val(c.ProjectLoadSec)
	c.StallMin = u.stall.val(c.StallMin)
	c.OpenMode = u.openMode.Value
	c.ExistingPolicy = "skip"
	if u.backupOld.Value {
		c.ExistingPolicy = "backup"
	}
	c.SkipMissingMedia = u.skipMissing.Value
	u.mu.Lock()
	u.cfg = c
	u.mu.Unlock()
	if err := config.Save(c); err != nil {
		u.log("[!] Lưu cài đặt: %v", err)
	}
	return c
}

// ─── Run control ──────────────────────────────────────────────────────────────

func (u *ui) start() {
	u.mu.Lock()
	var sel []*row
	for _, r := range u.rows {
		if r.check.Value {
			sel = append(sel, r)
		}
	}
	u.mu.Unlock()
	u.startRows(sel)
}

func (u *ui) startRows(sel []*row) {
	if u.isRunning() {
		return
	}
	cfg := u.readCfg()
	if len(sel) == 0 {
		u.log("Chưa chọn project nào.")
		return
	}
	u.mu.Lock()
	ds := make([]drafts.Draft, len(sel))
	for i, r := range sel {
		r.has = false
		ds[i] = r.draft
	}
	u.mu.Unlock()
	lg := logx.New(filepath.Join(config.Dir(), "logs"), func(s string) { u.log("%s", s) }, winapi.Screenshot)
	lg.Debugf("bắt đầu: %d project: [%s]", len(ds), strings.Join(names(ds), " | "))
	rn := runner.New(cfg, ds, lg, runner.Callbacks{
		Update: func(i int, it runner.Item) {
			u.mu.Lock()
			if i < len(sel) {
				sel[i].item, sel[i].has = it, true
				u.status = fmt.Sprintf("[%d/%d] %s — %s", i+1, len(sel), it.Draft.Name, it.State)
			}
			u.mu.Unlock()
			u.win.Invalidate()
		},
		AskOpen: u.askOpen,
	})
	shutdown := u.shutdownWhenDone.Value
	u.mu.Lock()
	u.running, u.run = true, rn
	u.mu.Unlock()
	u.log("== Bắt đầu xuất %d project ==", len(ds))
	go func() {
		rn.Run()
		lg.Close()
		u.mu.Lock()
		u.running, u.run, u.status = false, nil, "Đã xong"
		u.mu.Unlock()
		u.loadDrafts(false) // CapCut may have rewritten names/times while opening projects
		u.win.Invalidate()
		if shutdown {
			if rn.Stopped() {
				u.log("Không tắt máy vì hàng đợi đã bị dừng giữa chừng.")
			} else {
				u.scheduleShutdown()
			}
		}
	}()
}

func (u *ui) stop(why string) {
	u.mu.Lock()
	rn, ch := u.run, u.askCh
	u.askCh, u.askName = nil, ""
	u.mu.Unlock()
	if rn == nil {
		return
	}
	u.log("[DỪNG] Dừng (%s) — sẽ dừng sau bước hiện tại", why)
	rn.Stop()
	if ch != nil {
		ch <- false
	}
}

// askOpen is called from the runner goroutine in manual mode.
func (u *ui) askOpen(d drafts.Draft) bool {
	ch := make(chan bool, 1)
	u.mu.Lock()
	u.askName, u.askCh = d.Name, ch
	u.mu.Unlock()
	u.log(">> Hãy mở project %q trong CapCut rồi bấm \"Đã mở — tiếp tục\"", d.Name)
	u.win.Invalidate()
	return <-ch
}

func (u *ui) calibrate() {
	go func() {
		app := &runner.App{}
		if _, ok := app.Main(); !ok {
			u.log("Hãy mở CapCut ở màn hình chính (bấm \"Mở CapCut\") rồi hiệu chỉnh lại.")
			return
		}
		for s := 5; s > 0; s-- {
			u.log("Đưa chuột vào GIỮA thẻ project ĐẦU TIÊN trên màn hình chính CapCut… %d", s)
			time.Sleep(time.Second)
		}
		fx, fy, err := app.CursorFraction()
		if err != nil {
			u.log("[LỖI] Hiệu chỉnh: %v", err)
			return
		}
		u.mu.Lock()
		u.cfg.CardX, u.cfg.CardY = fx, fy
		err = config.Save(u.cfg)
		u.mu.Unlock()
		if err != nil {
			u.log("[!] Lưu cài đặt: %v", err)
		}
		u.log("[OK] Đã lưu vị trí thẻ project đầu tiên: %.3f, %.3f (tỉ lệ theo cửa sổ). Tool vẫn tự dò vị trí này; giá trị chỉ dùng khi không dò được.", fx, fy)
	}()
}

func (u *ui) launchCapCut() {
	go func() {
		exe, _ := runner.FindCapCut(u.cfg.CapCutExe)
		a := &runner.App{Exe: exe}
		if a.Running() {
			u.log("CapCut đang mở: %s", a.DescribeWindows())
			return
		}
		w, err := a.Launch(time.Duration(u.cfg.LaunchWaitSec)*time.Second, u.cfg.MoveToPrimary, func() bool { return false })
		if err != nil {
			u.log("[LỖI] %v", err)
			return
		}
		u.log("CapCut đã mở: %s", w)
	}()
}

// ─── Rename, shutdown ─────────────────────────────────────────────────────────

func (u *ui) renameRow(r *row) {
	if u.isRunning() {
		u.log("[!] Không đổi tên khi đang xuất.")
		return
	}
	if (&runner.App{}).Running() {
		u.log("[!] Hãy đóng CapCut trước khi đổi tên — nếu đang mở, CapCut sẽ ghi đè lại tên cũ.")
		return
	}
	dir := strings.TrimSpace(u.draftsEd.Text())
	old := r.draft.Name
	name, err := drafts.ValidateName(dir, r.draft, r.nameEd.Text())
	if err != nil {
		u.log("[LỖI] Đổi tên %q: %v", old, err)
		return
	}
	r.editing = false
	if name == old {
		return
	}
	bak, err := drafts.Backup(dir)
	if err != nil {
		u.log("[LỖI] Sao lưu danh sách project trước khi đổi tên: %v", err)
		return
	}
	oldFolder := filepath.Base(r.draft.Folder)
	if err := drafts.Rename(dir, r.draft, name); err != nil {
		u.log("[LỖI] Đổi tên %q: %v (bản sao lưu: %s)", old, err, filepath.Base(bak))
		return
	}
	u.log("Đã đổi tên project %q → %q (thư mục %q → %q, giống cách CapCut tự đổi tên)", old, name, oldFolder, name)
	u.loadDrafts(false)
}

func hiddenCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

// scheduleShutdown asks Windows to shut down in 60 s, cancellable.
func (u *ui) scheduleShutdown() {
	cmd := hiddenCmd("shutdown", "/s", "/t", "60", "/c", "CapCut Auto Exporter: xuất xong, máy sẽ tắt sau 60 giây.")
	if out, err := cmd.CombinedOutput(); err != nil {
		u.log("[LỖI] Lệnh tắt máy thất bại: %v %s", err, strings.TrimSpace(string(out)))
		return
	}
	u.mu.Lock()
	u.shutdownPending = true
	u.mu.Unlock()
	u.log("Máy sẽ TẮT sau 60 giây. Bấm \"Hủy tắt máy\" để hủy.")
	u.win.Invalidate()
}

func (u *ui) cancelShutdown() {
	out, err := hiddenCmd("shutdown", "/a").CombinedOutput()
	u.mu.Lock()
	u.shutdownPending = false
	u.mu.Unlock()
	if err != nil {
		u.log("[!] Hủy tắt máy: %v %s", err, strings.TrimSpace(string(out)))
		return
	}
	u.log("Đã hủy tắt máy.")
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func names(ds []drafts.Draft) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Name
	}
	return out
}

func openInExplorer(p string) {
	if p == "" {
		return
	}
	_ = exec.Command("explorer", p).Start()
}

func revealFile(p string) {
	if p == "" {
		return
	}
	_ = exec.Command("explorer", "/select,", p).Start()
}

// copyText puts text on the clipboard through the window system, which keeps
// Vietnamese (and any Unicode) intact — piping to clip.exe garbles it.
func (u *ui) copyText(gtx C, s string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
}

// ─── Event loop ───────────────────────────────────────────────────────────────

func (u *ui) loop() error {
	var ops op.Ops
	for {
		switch e := u.win.Event().(type) {
		case app.DestroyEvent:
			u.stop("đóng cửa sổ")
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			u.handle(gtx)
			u.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// handle processes the clicks of the controls that are always on screen.
func (u *ui) handle(gtx C) {
	for i := range u.navBtns {
		if u.navBtns[i].Clicked(gtx) {
			u.page = page(i)
			if u.page == pageHistory {
				u.refreshHistory()
			}
		}
	}
	if u.helpBtn.Clicked(gtx) {
		u.page = pageAbout
	}

	if u.draftsBtn.Clicked(gtx) {
		go func() {
			if p := winapi.PickFolder("Chọn thư mục chứa project CapCut (có root_meta_info.json)"); p != "" {
				u.mu.Lock()
				u.pendingDrafts = p
				u.mu.Unlock()
				u.win.Invalidate()
			}
		}()
	}
	if u.outputBtn.Clicked(gtx) {
		go func() {
			if p := winapi.PickFolder("Chọn thư mục output (trùng với thư mục xuất trong CapCut)"); p != "" {
				u.mu.Lock()
				u.pendingOutput = p
				u.mu.Unlock()
				u.win.Invalidate()
			}
		}()
	}
	u.mu.Lock()
	pd, po := u.pendingDrafts, u.pendingOutput
	u.pendingDrafts, u.pendingOutput = "", ""
	u.mu.Unlock()
	if pd != "" {
		u.draftsEd.SetText(pd)
		u.readCfg()
		u.loadDrafts(true)
	}
	if po != "" {
		u.outputEd.SetText(po)
		u.readCfg()
	}
	if u.reloadBtn.Clicked(gtx) || u.reloadBtn2.Clicked(gtx) {
		if u.isRunning() {
			u.log("[!] Đang xuất, hãy tải lại sau khi xong.")
		} else {
			u.readCfg()
			u.loadDrafts(true)
		}
	}
	if u.openOutBtn.Clicked(gtx) {
		openInExplorer(strings.TrimSpace(u.outputEd.Text()))
	}

	// presets of the key fields
	for _, c := range []*combo{u.exportKey, u.confirmKey, u.closeKey} {
		c := c
		if c.btn.Clicked(gtx) {
			items := make([]menuItem, len(c.presets))
			for i, p := range c.presets {
				p := p
				items[i] = menuItem{label: p, do: func() { c.ed.SetText(p); u.readCfg() }}
			}
			u.openMenu(150, items)
		}
	}
	for _, n := range []*numField{u.dialogWait, u.projectLoad, u.stall} {
		if n.step(gtx) {
			u.readCfg()
		}
	}

	if all, none := u.allBtn.Clicked(gtx), u.noneBtn.Clicked(gtx); all || none {
		u.mu.Lock()
		for _, r := range u.rows {
			r.check.Value = all
		}
		u.mu.Unlock()
	}
	if u.filterBtn.Clicked(gtx) {
		items := make([]menuItem, len(filterOptions))
		for i, f := range filterOptions {
			f := f
			items[i] = menuItem{label: f.label, do: func() { u.filter = f.kind }}
		}
		u.openMenu(190, items)
	}

	if u.startBtn.Clicked(gtx) && !u.isRunning() {
		u.start()
	}
	if u.stopBtn.Clicked(gtx) {
		u.stop("nút Dừng")
	}
	if u.cancelShutdownBtn.Clicked(gtx) {
		u.cancelShutdown()
	}
	if u.calibrateBtn.Clicked(gtx) {
		u.calibrate()
	}
	if u.launchBtn.Clicked(gtx) {
		u.launchCapCut()
	}
	if u.continueBtn.Clicked(gtx) {
		u.mu.Lock()
		ch := u.askCh
		u.askCh, u.askName = nil, ""
		u.mu.Unlock()
		if ch != nil {
			ch <- true
		}
	}

	if u.logDirBtn.Clicked(gtx) {
		dir := filepath.Join(config.Dir(), "logs")
		_ = os.MkdirAll(dir, 0o755)
		openInExplorer(dir)
	}
	if u.copyLogBtn.Clicked(gtx) {
		u.mu.Lock()
		txt := strings.Join(u.logLines, "\n")
		u.mu.Unlock()
		u.copyText(gtx, txt)
	}
	if u.clearLogBtn.Clicked(gtx) {
		u.mu.Lock()
		u.logLines, u.logDirty = nil, true
		u.mu.Unlock()
	}
	if u.logTogl.Clicked(gtx) {
		u.logCollapsed = !u.logCollapsed
	}

	if u.openMode.Update(gtx) || u.backupOld.Update(gtx) || u.skipMissing.Update(gtx) {
		u.readCfg()
	}
	u.handleSettings(gtx)
}

package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"capcut-auto-exporter/internal/config"
)

// ─── History ──────────────────────────────────────────────────────────────────

type histEntry struct {
	file, shots string
	when        time.Time
	projects    string
	hasSummary  bool
	ok, warn    int
	skip, fail  int
	logBtn      widget.Clickable
	shotsBtn    widget.Clickable
}

type histState struct {
	mu         sync.Mutex
	entries    []*histEntry
	loading    bool
	list       widget.List
	refreshBtn widget.Clickable
	dirBtn     widget.Clickable
}

var (
	reSummary  = regexp.MustCompile(`(\d+) xong, (\d+) cảnh báo, (\d+) bỏ qua, (\d+) lỗi / (\d+) project`)
	reSummary2 = regexp.MustCompile(`(\d+) xong, (\d+) lỗi / (\d+) project`) // older logs
	reProjects = regexp.MustCompile(`bắt đầu: \d+ project: \[(.*?)\]`)
)

func (u *ui) refreshHistory() {
	u.hist.mu.Lock()
	if u.hist.loading {
		u.hist.mu.Unlock()
		return
	}
	u.hist.loading = true
	u.hist.mu.Unlock()
	go func() {
		dir := filepath.Join(config.Dir(), "logs")
		files, _ := filepath.Glob(filepath.Join(dir, "run-*.log"))
		sort.Sort(sort.Reverse(sort.StringSlice(files)))
		if len(files) > 60 {
			files = files[:60]
		}
		var out []*histEntry
		for _, f := range files {
			out = append(out, parseLog(f))
		}
		u.hist.mu.Lock()
		u.hist.entries, u.hist.loading = out, false
		u.hist.mu.Unlock()
		u.win.Invalidate()
	}()
}

// parseLog reads only the start and end of a log: they hold the project list
// and the final summary line.
func parseLog(path string) *histEntry {
	e := &histEntry{file: path}
	base := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "run-"), ".log")
	if t, err := time.ParseInLocation("20060102-150405", base, time.Local); err == nil {
		e.when = t
	}
	shots := strings.TrimSuffix(path, ".log") + "-screens"
	if st, err := os.Stat(shots); err == nil && st.IsDir() {
		e.shots = shots
	}
	f, err := os.Open(path)
	if err != nil {
		return e
	}
	defer f.Close()
	st, _ := f.Stat()
	head := make([]byte, 8192)
	n, _ := f.Read(head)
	if m := reProjects.FindStringSubmatch(string(head[:n])); m != nil {
		// New logs separate names with " | "; older ones used plain spaces.
		e.projects = strings.ReplaceAll(m[1], " | ", ", ")
	}
	size := st.Size()
	off := size - 16384
	if off < 0 {
		off = 0
	}
	tail := make([]byte, size-off)
	n, _ = f.ReadAt(tail, off)
	text := string(tail[:n])
	if i := strings.LastIndex(text, "== Kết thúc"); i >= 0 {
		line := text[i:]
		if m := reSummary.FindStringSubmatch(line); m != nil {
			e.ok, _ = strconv.Atoi(m[1])
			e.warn, _ = strconv.Atoi(m[2])
			e.skip, _ = strconv.Atoi(m[3])
			e.fail, _ = strconv.Atoi(m[4])
			e.hasSummary = true
		} else if m := reSummary2.FindStringSubmatch(line); m != nil {
			e.ok, _ = strconv.Atoi(m[1])
			e.fail, _ = strconv.Atoi(m[2])
			e.hasSummary = true
		}
	}
	return e
}

func (u *ui) historyPage(gtx C) D {
	if u.hist.refreshBtn.Clicked(gtx) {
		u.refreshHistory()
	}
	if u.hist.dirBtn.Clicked(gtx) {
		dir := filepath.Join(config.Dir(), "logs")
		_ = os.MkdirAll(dir, 0o755)
		openInExplorer(dir)
	}
	u.hist.mu.Lock()
	entries := append([]*histEntry(nil), u.hist.entries...)
	loading := u.hist.loading
	u.hist.mu.Unlock()
	u.hist.list.Axis = layout.Vertical

	return card(gtx, 20, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				title := fmt.Sprintf("Các lần chạy gần đây (%d)", len(entries))
				if loading {
					title = "Đang đọc lịch sử…"
				}
				return u.cardTitle(gtx, "", title, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return u.button(gtx, &u.hist.refreshBtn, bopt{style: bSoft, icon: icRefresh, label: "Làm mới", padV: 7})
						}),
						gapW(8),
						layout.Rigid(func(gtx C) D {
							return u.button(gtx, &u.hist.dirBtn, bopt{style: bOutline, icon: icFolderOpn, label: "Thư mục log", padV: 7})
						}),
					)
				})
			}),
			gapH(14),
			layout.Flexed(1, func(gtx C) D {
				if len(entries) == 0 {
					return layout.Center.Layout(gtx, func(gtx C) D {
						return u.text(gtx, 13, cMuted, false, "Chưa có lần chạy nào. Mỗi lần bấm Bắt đầu xuất sẽ tạo một file log ở đây.")
					})
				}
				return material.List(u.th, &u.hist.list).Layout(gtx, len(entries), func(gtx C, i int) D {
					return u.historyRow(gtx, entries[i])
				})
			}),
		)
	})
}

func (u *ui) historyRow(gtx C, e *histEntry) D {
	if e.logBtn.Clicked(gtx) {
		_ = hiddenCmd("cmd", "/c", "start", "", e.file).Start()
	}
	if e.shotsBtn.Clicked(gtx) && e.shots != "" {
		openInExplorer(e.shots)
	}
	summary, fg, bg := "Chưa có kết quả", cMuted, cGraySoft
	var ic *widget.Icon
	if e.hasSummary {
		parts := []string{}
		if e.ok > 0 {
			parts = append(parts, fmt.Sprintf("%d xong", e.ok))
		}
		if e.warn > 0 {
			parts = append(parts, fmt.Sprintf("%d cảnh báo", e.warn))
		}
		if e.skip > 0 {
			parts = append(parts, fmt.Sprintf("%d bỏ qua", e.skip))
		}
		if e.fail > 0 {
			parts = append(parts, fmt.Sprintf("%d lỗi", e.fail))
		}
		summary = strings.Join(parts, " · ")
		switch {
		case e.fail > 0:
			fg, bg, ic = cRed, cRedSoft, icError
		case e.warn > 0:
			fg, bg, ic = cOrange, cOrangeSoft, icWarn
		default:
			fg, bg, ic = cGreen, cGreenSoft, icCheckCirc
		}
	}
	when := "—"
	if !e.when.IsZero() {
		when = e.when.Format("02/01/2006 15:04:05")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					u.cell(176, func(gtx C) D { return u.text1(gtx, 13.5, cText, true, when) }),
					layout.Flexed(1, func(gtx C) D {
						p := e.projects
						if p == "" {
							p = "—"
						}
						return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D { return u.text1(gtx, 13, cMuted, false, p) })
					}),
					layout.Rigid(func(gtx C) D { return u.pill(gtx, summary, fg, bg, ic) }),
					gapW(14),
					layout.Rigid(func(gtx C) D {
						return u.button(gtx, &e.logBtn, bopt{style: bSoft, icon: icList, label: "Mở log", padV: 7})
					}),
					gapW(8),
					layout.Rigid(func(gtx C) D {
						return u.button(gtx, &e.shotsBtn, bopt{style: bOutline, icon: icFolderOpn, label: "Ảnh chụp", padV: 7, disabled: e.shots == ""})
					}),
				)
			})
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, rgb(0xEEF2F8)) }),
	)
}

// ─── Settings ─────────────────────────────────────────────────────────────────

type settingsState struct {
	capcutExe                                       widget.Editor
	launchWait, homeWait, openWait, retries, stable *numField
	maxExport                                       *numField
	durTol                                          widget.Editor
	dismiss                                         *combo
	moveToPrimary, restartPerDraft, debugShots      widget.Bool
	saveBtn, resetBtn, calibBtn                     widget.Clickable
	list                                            widget.List
}

func (u *ui) initSettings() {
	s := &u.set
	s.capcutExe.SingleLine = true
	s.durTol.SingleLine = true
	s.durTol.Filter = "0123456789."
	s.list.Axis = layout.Vertical
	s.launchWait = newNum(0, 5, 300)
	s.homeWait = newNum(0, 3, 120)
	s.openWait = newNum(0, 5, 120)
	s.retries = newNum(0, 1, 10)
	s.stable = newNum(0, 2, 120)
	s.maxExport = newNum(0, 0, 1440)
	s.dismiss = newCombo("", "Escape", "Enter")
	u.setFrom(u.cfg)
}

func (u *ui) setFrom(c config.Config) {
	s := &u.set
	s.capcutExe.SetText(c.CapCutExe)
	s.launchWait.set(c.LaunchWaitSec)
	s.homeWait.set(c.HomeWaitSec)
	s.openWait.set(c.OpenWaitSec)
	s.retries.set(c.OpenRetries)
	s.stable.set(c.StableSec)
	s.maxExport.set(c.MaxExportMin)
	s.durTol.SetText(strconv.FormatFloat(c.DurationTolSec, 'f', -1, 64))
	s.dismiss.ed.SetText(c.DismissKey)
	s.moveToPrimary.Value = c.MoveToPrimary
	s.restartPerDraft.Value = c.RestartPerDraft
	s.debugShots.Value = c.DebugScreenshots
}

func (u *ui) saveSettings() {
	u.readCfg() // export-page fields first
	s := &u.set
	u.mu.Lock()
	c := u.cfg
	c.CapCutExe = strings.TrimSpace(s.capcutExe.Text())
	c.LaunchWaitSec = s.launchWait.val(c.LaunchWaitSec)
	c.HomeWaitSec = s.homeWait.val(c.HomeWaitSec)
	c.OpenWaitSec = s.openWait.val(c.OpenWaitSec)
	c.OpenRetries = s.retries.val(c.OpenRetries)
	c.StableSec = s.stable.val(c.StableSec)
	c.MaxExportMin = s.maxExport.val(c.MaxExportMin)
	if v, err := strconv.ParseFloat(strings.TrimSpace(s.durTol.Text()), 64); err == nil && v > 0 {
		c.DurationTolSec = v
	}
	if t := s.dismiss.text(); t != "" {
		c.DismissKey = t
	}
	c.MoveToPrimary = s.moveToPrimary.Value
	c.RestartPerDraft = s.restartPerDraft.Value
	c.DebugScreenshots = s.debugShots.Value
	u.cfg = c
	err := config.Save(c)
	u.mu.Unlock()
	if err != nil {
		u.log("[LỖI] Lưu cài đặt: %v", err)
		return
	}
	u.log("[OK] Đã lưu cài đặt (%s)", filepath.Join(config.Dir(), "config.json"))
}

func (u *ui) handleSettings(gtx C) {
	s := &u.set
	for _, n := range []*numField{s.launchWait, s.homeWait, s.openWait, s.retries, s.stable, s.maxExport} {
		n.step(gtx)
	}
	if s.dismiss.btn.Clicked(gtx) {
		items := []menuItem{}
		for _, p := range s.dismiss.presets {
			p := p
			items = append(items, menuItem{label: p, do: func() { s.dismiss.ed.SetText(p) }})
		}
		u.openMenu(150, items)
	}
	if s.saveBtn.Clicked(gtx) {
		u.saveSettings()
	}
	if s.resetBtn.Clicked(gtx) {
		d := config.Default()
		u.setFrom(d)
		u.log("Đã đặt các thông số nâng cao về mặc định (chưa lưu — bấm Lưu cài đặt).")
	}
	if s.calibBtn.Clicked(gtx) {
		u.calibrate()
	}
}

func (u *ui) settingsPage(gtx C) D {
	s := &u.set
	col := func(caption string, w layout.Widget) layout.FlexChild {
		return layout.Flexed(1, func(gtx C) D { return u.labeled(gtx, caption, w) })
	}
	section := func(title, hint string, body layout.Widget) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Bottom: 14}.Layout(gtx, func(gtx C) D {
				return card(gtx, 20, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.text(gtx, 16, cText, true, title) }),
						layout.Rigid(func(gtx C) D {
							if hint == "" {
								return D{}
							}
							return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D { return u.text(gtx, 12.5, cMuted, false, hint) })
						}),
						gapH(14),
						layout.Rigid(body),
					)
				})
			})
		}
	}
	blocks := []layout.Widget{
		section("CapCut", "Để trống để tool tự tìm CapCut.exe trong %LOCALAPPDATA%\\CapCut\\Apps.", func(gtx C) D {
			return u.input(gtx, &s.capcutExe, "Đường dẫn CapCut.exe (tùy chọn)", icMovie, nil)
		}),
		section("Thời gian chờ", "Tăng các giá trị này nếu máy chậm hoặc project nặng.", func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						col("CapCut khởi động (s)", func(gtx C) D { return u.numInput(gtx, s.launchWait) }), gapW(12),
						col("Màn hình chính ổn định (s)", func(gtx C) D { return u.numInput(gtx, s.homeWait) }), gapW(12),
						col("Chờ editor mở sau click (s)", func(gtx C) D { return u.numInput(gtx, s.openWait) }),
					)
				}),
				gapH(12),
				layout.Rigid(func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						col("Số lần click thẻ project", func(gtx C) D { return u.numInput(gtx, s.retries) }), gapW(12),
						col("File đứng yên mới coi là xong (s)", func(gtx C) D { return u.numInput(gtx, s.stable) }), gapW(12),
						col("Sai lệch thời lượng cho phép (s)", func(gtx C) D { return u.input(gtx, &s.durTol, "1.5", nil, nil) }),
					)
				}),
				gapH(12),
				layout.Rigid(func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						col("Giới hạn mỗi video (phút, 0 = không giới hạn)", func(gtx C) D { return u.numInput(gtx, s.maxExport) }), gapW(12),
						col("Phím đóng popup", func(gtx C) D { return u.comboInput(gtx, s.dismiss) }), gapW(12),
						layout.Flexed(1, func(gtx C) D { return D{} }),
					)
				}),
			)
		}),
		section("Hành vi", "", func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return u.checkbox(gtx, &s.moveToPrimary, "Đưa CapCut về màn hình chính và phóng to trước khi thao tác")
				}),
				layout.Rigid(func(gtx C) D {
					return u.checkbox(gtx, &s.restartPerDraft, "Thoát CapCut sau mỗi project (cần cho chế độ Tự động)")
				}),
				layout.Rigid(func(gtx C) D {
					return u.checkbox(gtx, &s.debugShots, "Chụp màn hình ở từng bước (giúp tìm lỗi, tốn dung lượng)")
				}),
			)
		}),
		section("Vị trí thẻ project", "Tool tự dò thẻ đầu tiên trên màn hình chính CapCut; vị trí hiệu chỉnh chỉ dùng khi không dò được.", func(gtx C) D {
			u.mu.Lock()
			x, y := u.cfg.CardX, u.cfg.CardY
			u.mu.Unlock()
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return u.button(gtx, &s.calibBtn, bopt{style: bOutline, icon: icSettings, label: "Hiệu chỉnh vị trí thẻ", padV: 8})
				}),
				gapW(14),
				layout.Rigid(func(gtx C) D {
					return u.text(gtx, 13, cMuted, false, fmt.Sprintf("Hiện tại: %.3f , %.3f (tỉ lệ theo cửa sổ)", x, y))
				}),
			)
		}),
		func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return u.button(gtx, &s.saveBtn, bopt{style: bPrimary, icon: icCheck, label: "Lưu cài đặt", padV: 12, padH: 22})
				}),
				gapW(10),
				layout.Rigid(func(gtx C) D {
					return u.button(gtx, &s.resetBtn, bopt{style: bOutline, icon: icRefresh, label: "Khôi phục mặc định", padV: 12, padH: 18})
				}),
			)
		},
	}
	return material.List(u.th, &s.list).Layout(gtx, len(blocks), func(gtx C, i int) D { return blocks[i](gtx) })
}

// ─── About ────────────────────────────────────────────────────────────────────

func (u *ui) aboutPage(gtx C) D {
	paragraph := func(s string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D { return u.text(gtx, 13.5, cText, false, s) })
		}
	}
	step := func(n, title, body string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D { return u.badge(gtx, n) }) }),
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return u.text(gtx, 14, cText, true, title) }),
							layout.Rigid(func(gtx C) D { return u.text(gtx, 13, cMuted, false, body) }),
						)
					}),
				)
			})
		}
	}
	section := func(title string, ws ...layout.Widget) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Bottom: 14}.Layout(gtx, func(gtx C) D {
				return card(gtx, 20, func(gtx C) D {
					children := []layout.FlexChild{
						layout.Rigid(func(gtx C) D { return u.text(gtx, 16, cText, true, title) }),
						gapH(12),
					}
					for _, w := range ws {
						children = append(children, layout.Rigid(w))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
				})
			})
		}
	}
	bullet := func(s string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: 10, Top: 1}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, icBulb, 18, cOrange) })
					}),
					layout.Flexed(1, func(gtx C) D { return u.text(gtx, 13, cText, false, s) }),
				)
			})
		}
	}
	blocks := []layout.Widget{
		section("CapCut Auto Exporter "+appVersion,
			paragraph("Xuất lần lượt nhiều project CapCut bằng chính CapCut. Tool chỉ điều khiển ứng dụng: mở project, bấm phím xuất, chờ file xuất xong rồi đóng project và sang project tiếp theo."),
			paragraph("Chất lượng video giống hệt khi bạn tự xuất trong CapCut (hiệu ứng, chuyển cảnh, chữ…)."),
		),
		section("Cách dùng nhanh",
			step("1", "Chọn thư mục", "Thư mục project của CapCut có sẵn mặc định. Thư mục output tự lấy từ CapCut (ô Theo CapCut); muốn đổi nơi xuất, chọn thư mục khác trong hộp thoại xuất của CapCut một lần."),
			step("2", "Chọn project", "Tích chọn các project cần xuất, hoặc bấm nút ▶ ở cuối dòng để xuất riêng một project."),
			step("3", "Bấm Bắt đầu xuất", "Tool tự mở CapCut, vào từng project, xuất và đóng. Trong lúc chạy đừng dùng chuột và bàn phím."),
			step("4", "Theo dõi", "Cột Trạng thái và Chi tiết cập nhật trực tiếp; mọi bước được ghi trong Nhật ký và trang Lịch sử."),
		),
		section("Lưu ý",
			bullet("Dừng khẩn cấp bất cứ lúc nào: "+stopHotkey+" (hoạt động cả khi CapCut đang ở phía trước)."),
			bullet("Đổi tên project: bấm … ở cuối dòng → Đổi tên. CapCut phải đang đóng. Tool đổi tên cả thư mục project giống như CapCut tự đổi tên."),
			bullet("Project thiếu media: tool bấm Hủy trên popup \"Liên kết tệp phương tiện\" rồi vẫn xuất; kết quả ghi là \"Xong (cảnh báo)\". Tích \"Bỏ qua project thiếu media\" nếu muốn bỏ qua."),
			bullet("Popup \"Cập nhật phiên bản\" của CapCut được tự đóng, không cập nhật."),
			bullet("\"Tắt máy khi xuất xong\" tắt máy sau 60 giây, có nút Hủy. Không tắt nếu hàng đợi bị dừng giữa chừng."),
		),
		section("Dữ liệu của tool",
			paragraph("Cài đặt: "+filepath.Join(config.Dir(), "config.json")),
			paragraph("Log và ảnh chụp: "+filepath.Join(config.Dir(), "logs")),
			func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return u.button(gtx, &u.logDirBtn, bopt{style: bOutline, icon: icFolderOpn, label: "Mở thư mục log", padV: 8})
					}),
				)
			},
		),
	}
	return material.List(u.th, &u.aboutList).Layout(gtx, len(blocks), func(gtx C, i int) D { return blocks[i](gtx) })
}

var _ = image.Pt
var _ color.NRGBA

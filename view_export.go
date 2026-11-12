package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"capcut-auto-exporter/internal/config"
	"capcut-auto-exporter/internal/runner"
)

// ─── Status ───────────────────────────────────────────────────────────────────

type statusFilter int

const (
	fAll statusFilter = iota
	fPending
	fRunning
	fDone
	fWarn
	fFailed
	fSkipped
)

var filterOptions = []struct {
	label string
	kind  statusFilter
}{
	{"Tất cả trạng thái", fAll},
	{"Chưa chạy", fPending},
	{"Đang chạy", fRunning},
	{"Đã xong", fDone},
	{"Xong có cảnh báo", fWarn},
	{"Lỗi", fFailed},
	{"Bỏ qua", fSkipped},
}

func (f statusFilter) label() string {
	for _, o := range filterOptions {
		if o.kind == f {
			return o.label
		}
	}
	return ""
}

func rowKind(r *row) statusFilter {
	if !r.has {
		return fPending
	}
	switch r.item.State {
	case runner.StateOpening, runner.StateExport, runner.StateClosing:
		return fRunning
	case runner.StateDone:
		return fDone
	case runner.StateWarn:
		return fWarn
	case runner.StateFailed:
		return fFailed
	case runner.StateSkipped:
		return fSkipped
	}
	return fPending
}

func (f statusFilter) matches(k statusFilter) bool {
	switch f {
	case fAll:
		return true
	case fDone:
		return k == fDone || k == fWarn
	}
	return f == k
}

func (u *ui) visibleRows() (rows []*row, total, selected int) {
	q := strings.ToLower(strings.TrimSpace(u.searchEd.Text()))
	u.mu.Lock()
	defer u.mu.Unlock()
	total = len(u.rows)
	for _, r := range u.rows {
		if r.check.Value {
			selected++
		}
		if q != "" && !strings.Contains(strings.ToLower(r.draft.Name), q) {
			continue
		}
		if !u.filter.matches(rowKind(r)) {
			continue
		}
		rows = append(rows, r)
	}
	return
}

// ─── Frame ────────────────────────────────────────────────────────────────────

func (u *ui) layout(gtx C) D {
	u.mu.Lock()
	if u.logDirty {
		u.logEd.SetText(strings.Join(u.logLines, "\n"))
		u.logEd.SetCaret(u.logEd.Len(), u.logEd.Len())
		u.logDirty = false
	}
	u.mu.Unlock()

	fillBg(gtx, cBg)
	dims := layout.Flex{}.Layout(gtx,
		layout.Rigid(u.sidebar),
		layout.Flexed(1, u.mainArea),
	)
	u.layoutMenu(gtx)
	u.trackPointer(gtx)
	return dims
}

var navItems = []struct {
	label string
	icon  *widget.Icon
}{
	{"Xuất dự án", icPlay},
	{"Lịch sử", icHistory},
	{"Cài đặt", icSettings},
	{"Giới thiệu", icInfo},
}

func (u *ui) sidebar(gtx C) D {
	w := gtx.Dp(224)
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	return layout.Background{}.Layout(gtx,
		func(gtx C) D {
			sz := gtx.Constraints.Min
			fillRect(gtx, image.Rectangle{Max: sz}, cSurface)
			fillRect(gtx, image.Rectangle{Min: image.Pt(sz.X-1, 0), Max: sz}, cBorder)
			return D{Size: sz}
		},
		func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16, Top: 24, Bottom: 18}.Layout(gtx, func(gtx C) D {
				children := []layout.FlexChild{
					layout.Rigid(func(gtx C) D { return u.logo(gtx) }),
					gapH(26),
				}
				for i := range navItems {
					i := i
					children = append(children,
						layout.Rigid(func(gtx C) D { return u.navItem(gtx, i) }),
						gapH(8))
				}
				children = append(children,
					layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, gtx.Constraints.Min.Y)} }),
					layout.Rigid(u.tipCard))
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			})
		})
}

func (u *ui) logo(gtx C) D {
	return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			s := gtx.Dp(58)
			gtx.Constraints.Min = image.Pt(s, s)
			gtx.Constraints.Max = gtx.Constraints.Min
			fillRR(gtx, image.Pt(s, s), 16, cPrimarySoft)
			return layout.Center.Layout(gtx, func(gtx C) D { return drawIcon(gtx, icMovie, 32, cPrimary) })
		}),
		gapH(10),
		layout.Rigid(func(gtx C) D {
			return layout.Center.Layout(gtx, func(gtx C) D {
				l := material.Label(u.th, 16, "CapCut\nAuto Exporter")
				l.Color, l.Alignment = cText, text.Middle
				l.Font.Weight = font.Bold
				return l.Layout(gtx)
			})
		}),
	)
}

func (u *ui) navItem(gtx C, i int) D {
	active := u.page == page(i)
	it := navItems[i]
	return u.navBtns[i].Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		bg, fg := color.NRGBA{}, cMuted
		if active {
			bg, fg = cPrimary, rgb(0xFFFFFF)
		} else if u.navBtns[i].Hovered() {
			bg, fg = cPrimarySoft, cPrimary
		}
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return panel(gtx, 12, bg, nil, insetHV(14, 12), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, it.icon, 20, fg) })
				}),
				layout.Rigid(func(gtx C) D { return u.text1(gtx, 14, fg, active, it.label) }),
			)
		})
	})
}

func (u *ui) tipCard(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return panel(gtx, 14, cPrimarySoft, nil, inset(14), func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return drawIcon(gtx, icRocket, 30, cPrimary) }),
			gapH(8),
			layout.Rigid(func(gtx C) D {
				return u.text(gtx, 12.5, cText, true, "Tự động xuất hàng loạt project CapCut một cách dễ dàng!")
			}),
			gapH(6),
			layout.Rigid(func(gtx C) D { return u.text(gtx, 11.5, cMuted, false, "Dừng khẩn cấp: "+stopHotkey) }),
		)
	})
}

var pageTitles = [4][2]string{
	{"CapCut Auto Exporter", "Xuất nhiều project CapCut cùng lúc, tiết kiệm thời gian và công sức"},
	{"Lịch sử xuất", "Các lần chạy trước đây, kèm file log và ảnh chụp từng bước"},
	{"Cài đặt", "Thời gian chờ, hành vi và các thông số nâng cao"},
	{"Giới thiệu", "Hướng dẫn sử dụng và thông tin về công cụ"},
}

func (u *ui) mainArea(gtx C) D {
	return layout.Inset{Left: 28, Right: 28, Top: 20, Bottom: 12}.Layout(gtx, func(gtx C) D {
		u.mu.Lock()
		pending := u.shutdownPending
		u.mu.Unlock()
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.header),
			gapH(10),
			layout.Rigid(func(gtx C) D {
				if !pending {
					return D{}
				}
				return layout.Inset{Bottom: 12}.Layout(gtx, u.shutdownBanner)
			}),
			layout.Flexed(1, func(gtx C) D {
				switch u.page {
				case pageHistory:
					return u.historyPage(gtx)
				case pageSettings:
					return u.settingsPage(gtx)
				case pageAbout:
					return u.aboutPage(gtx)
				}
				return u.exportPage(gtx)
			}),
			gapH(8),
			layout.Rigid(u.footer),
		)
	})
}

func (u *ui) header(gtx C) D {
	t := pageTitles[u.page]
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, icSpark, 38, cPrimary) })
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.text1(gtx, 26, cText, true, t[0]) }),
				layout.Rigid(func(gtx C) D { return u.text1(gtx, 13, cMuted, false, t[1]) }),
			)
		}),
		layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
		layout.Rigid(func(gtx C) D {
			return u.button(gtx, &u.helpBtn, bopt{style: bSoft, icon: icHelp, label: "Hướng dẫn sử dụng", radius: 20, padH: 16})
		}),
	)
}

func (u *ui) footer(gtx C) D {
	u.mu.Lock()
	running, status := u.running, u.status
	u.mu.Unlock()
	icon, col, msg := icCheckCirc, cGreen, "Sẵn sàng xuất project..."
	if running {
		icon, col, msg = icSchedule, cPrimary, "Đang xuất: "+status
	} else if status != "" {
		msg = status
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, icon, 22, col) })
		}),
		layout.Flexed(1, func(gtx C) D { return u.text1(gtx, 13, cText, false, msg) }),
		layout.Rigid(func(gtx C) D { return u.text1(gtx, 12, cMuted, false, "Phiên bản "+appVersion) }),
	)
}

func (u *ui) shutdownBanner(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return panel(gtx, 12, cOrangeSoft, nil, insetHV(16, 10), func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, icPower, 22, cOrange) })
			}),
			layout.Flexed(1, func(gtx C) D {
				return u.text(gtx, 13.5, cText, true, "Đã xuất xong — máy sẽ TẮT sau 60 giây.")
			}),
			layout.Rigid(func(gtx C) D {
				return u.button(gtx, &u.cancelShutdownBtn, bopt{style: bDanger, label: "Hủy tắt máy", padV: 7})
			}),
		)
	})
}

// ─── Export page ──────────────────────────────────────────────────────────────

func (u *ui) exportPage(gtx C) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(u.setupCard),
		gapH(10),
		layout.Rigid(u.askBanner),
		layout.Flexed(1, u.listCard),
		gapH(10),
		layout.Rigid(u.logCard),
	)
}

func (u *ui) askBanner(gtx C) D {
	u.mu.Lock()
	name := u.askName
	u.mu.Unlock()
	if name == "" {
		return D{}
	}
	return layout.Inset{Bottom: 14}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return panel(gtx, 12, cOrangeSoft, nil, insetHV(16, 12), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return u.text(gtx, 14, cText, true, fmt.Sprintf("Hãy mở project \"%s\" trong CapCut (vào màn hình chỉnh sửa), rồi bấm nút bên phải.", name))
				}),
				layout.Rigid(func(gtx C) D {
					return u.button(gtx, &u.continueBtn, bopt{style: bPrimary, label: "Đã mở — tiếp tục"})
				}),
			)
		})
	})
}

func (u *ui) pathRow(gtx C, ic *widget.Icon, label string, ed *widget.Editor, hint string, btns layout.Widget) D {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Dp(150)
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, ic, 20, cMuted) })
				}),
				layout.Rigid(func(gtx C) D { return u.text1(gtx, 13.5, cText, true, label) }),
			)
		}),
		layout.Flexed(1, func(gtx C) D { return u.input(gtx, ed, hint, nil, nil) }),
		gapW(10),
		layout.Rigid(btns),
	)
}

func (u *ui) setupCard(gtx C) D {
	running := u.isRunning()
	return card(gtx, 16, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return u.cardTitle(gtx, "1", "Thiết lập xuất project", nil) }),
			gapH(10),
			layout.Rigid(func(gtx C) D {
				return u.pathRow(gtx, icFolder, "Thư mục project", &u.draftsEd, "…/CapCut/User Data/Projects/com.lveditor.draft", func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return u.button(gtx, &u.draftsBtn, bopt{style: bOutline, icon: icFolderOpn, label: "Chọn…", minW: 96})
						}),
						gapW(8),
						layout.Rigid(func(gtx C) D {
							return u.button(gtx, &u.reloadBtn, bopt{style: bSoft, icon: icRefresh, label: "Tải lại", disabled: running})
						}),
					)
				})
			}),
			gapH(8),
			layout.Rigid(func(gtx C) D {
				return u.pathRow(gtx, icMovie, "Thư mục output", &u.outputEd, "Thư mục CapCut đang xuất video vào", func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return u.button(gtx, &u.outputBtn, bopt{style: bOutline, icon: icFolderOpn, label: "Chọn…", minW: 96})
						}),
						gapW(8),
						layout.Rigid(func(gtx C) D {
							return u.button(gtx, &u.openOutBtn, bopt{style: bSoft, icon: icOpenNew})
						}),
					)
				})
			}),
			gapH(10),
			layout.Rigid(func(gtx C) D {
				col := func(caption string, w layout.Widget) layout.FlexChild {
					return layout.Flexed(1, func(gtx C) D { return u.labeled(gtx, caption, w) })
				}
				return layout.Flex{}.Layout(gtx,
					col("Phím xuất", func(gtx C) D { return u.comboInput(gtx, u.exportKey) }), gapW(12),
					col("Xác nhận", func(gtx C) D { return u.comboInput(gtx, u.confirmKey) }), gapW(12),
					col("Đóng project", func(gtx C) D { return u.comboInput(gtx, u.closeKey) }), gapW(12),
					col("Chờ hộp thoại (s)", func(gtx C) D { return u.numInput(gtx, u.dialogWait) }), gapW(12),
					col("Chờ project tải (s)", func(gtx C) D { return u.numInput(gtx, u.projectLoad) }), gapW(12),
					col("Báo lỗi nếu đứng yên (phút)", func(gtx C) D { return u.numInput(gtx, u.stall) }),
				)
			}),
			gapH(10),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D { return u.optionsBlock(gtx, running) }),
					gapW(16),
					layout.Rigid(func(gtx C) D { return u.startButton(gtx, running) }),
				)
			}),
		)
	})
}

func (u *ui) optionsBlock(gtx C, running bool) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.text1(gtx, 13, cMuted, false, "Mở project:") }),
				gapW(12),
				layout.Rigid(func(gtx C) D {
					return u.radio(gtx, &u.openMode, config.OpenAuto, "Tự động (đưa lên đầu + click thẻ)")
				}),
				gapW(14),
				layout.Rigid(func(gtx C) D { return u.radio(gtx, &u.openMode, config.OpenManual, "Thủ công (tôi tự mở)") }),
				gapW(16),
				layout.Rigid(func(gtx C) D {
					return u.button(gtx, &u.launchBtn, bopt{style: bSoft, icon: icMovie, label: "Mở CapCut", padV: 8})
				}),
				gapW(8),
				layout.Rigid(func(gtx C) D {
					return u.button(gtx, &u.calibrateBtn, bopt{style: bOutline, icon: icSettings, label: "Hiệu chỉnh thẻ", padV: 8})
				}),
			)
		}),
		gapH(4),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.checkbox(gtx, &u.backupOld, "Đổi tên file cũ thay vì bỏ qua") }),
				gapW(14),
				layout.Rigid(func(gtx C) D { return u.checkbox(gtx, &u.skipMissing, "Bỏ qua project thiếu media") }),
				gapW(14),
				layout.Rigid(func(gtx C) D { return u.checkbox(gtx, &u.shutdownWhenDone, "Tắt máy khi xuất xong") }),
			)
		}),
	)
}

func (u *ui) startButton(gtx C, running bool) D {
	if running {
		return u.button(gtx, &u.stopBtn, bopt{style: bDanger, icon: icStop, label: "Dừng xuất", size: 16, padV: 15, padH: 24, radius: 14, minW: 180})
	}
	return u.button(gtx, &u.startBtn, bopt{style: bPrimary, icon: icPlay, label: "Bắt đầu xuất", size: 16, padV: 15, padH: 24, radius: 14, minW: 180})
}

// ─── Project list ─────────────────────────────────────────────────────────────

const (
	colCheck  = unit.Dp(44)
	colStatus = unit.Dp(124)
	colDur    = unit.Dp(84)
	colMod    = unit.Dp(146)
	colAct    = unit.Dp(96)
)

func (u *ui) listCard(gtx C) D {
	minH := gtx.Dp(230)
	if gtx.Constraints.Max.Y < minH {
		minH = gtx.Constraints.Max.Y
	}
	gtx.Constraints.Min.Y = minH
	rows, total, selected := u.visibleRows()
	running := u.isRunning()
	return card(gtx, 16, func(gtx C) D {
		gtx.Constraints.Min.Y = minH - gtx.Dp(32)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return u.cardTitle(gtx, "2", fmt.Sprintf("Danh sách project (%d) – Đã chọn %d", total, selected), nil)
			}),
			gapH(10),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return u.button(gtx, &u.allBtn, bopt{style: bPrimary, icon: icCheck, label: "Chọn tất cả", padV: 8})
					}),
					gapW(8),
					layout.Rigid(func(gtx C) D {
						return u.button(gtx, &u.noneBtn, bopt{style: bOutline, label: "Bỏ chọn", padV: 8})
					}),
					gapW(8),
					layout.Rigid(func(gtx C) D {
						return u.button(gtx, &u.filterBtn, bopt{style: bOutline, label: u.filter.label(), trail: icDropdown, padV: 8, minW: 150})
					}),
					layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(260), gtx.Dp(260)
						return u.input(gtx, &u.searchEd, "Tìm kiếm project...", icSearch, nil)
					}),
					gapW(8),
					layout.Rigid(func(gtx C) D {
						return u.button(gtx, &u.reloadBtn2, bopt{style: bSoft, icon: icRefresh, disabled: running})
					}),
				)
			}),
			gapH(10),
			layout.Rigid(u.tableHeader),
			layout.Flexed(1, func(gtx C) D {
				if len(rows) == 0 {
					return layout.Center.Layout(gtx, func(gtx C) D {
						msg := "Chưa có project. Chọn thư mục project của CapCut rồi bấm Tải lại."
						if total > 0 {
							msg = "Không có project nào khớp bộ lọc."
						}
						return u.text(gtx, 13, cMuted, false, msg)
					})
				}
				return material.List(u.th, &u.list).Layout(gtx, len(rows), func(gtx C, i int) D {
					return u.projectRow(gtx, rows[i], running)
				})
			}),
		)
	})
}

// cell is a fixed-width table column. Its content is laid out at its natural
// size (not stretched to the column) and the column keeps its width.
func (u *ui) cell(w unit.Dp, f layout.Widget) layout.FlexChild {
	return layout.Rigid(func(gtx C) D {
		px := gtx.Dp(w)
		gtx.Constraints.Min.X, gtx.Constraints.Max.X = 0, px
		dims := f(gtx)
		dims.Size.X = px
		return dims
	})
}

func (u *ui) tableHeader(gtx C) D {
	h := func(s string) layout.Widget {
		return func(gtx C) D { return u.text1(gtx, 12.5, cMuted, true, s) }
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return panel(gtx, 10, cSurfaceAlt, nil, layout.Inset{Top: 9, Bottom: 9}, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			u.cell(colCheck, func(gtx C) D { return D{} }),
			layout.Flexed(2.3, h("Tên project")),
			u.cell(colStatus, h("Trạng thái")),
			u.cell(colDur, h("Thời lượng")),
			u.cell(colMod, h("Sửa lần cuối")),
			layout.Flexed(3, h("Chi tiết")),
			u.cell(colAct, h("Hành động")),
		)
	})
}

func (u *ui) rowEvents(gtx C, r *row, running bool) {
	for {
		ev, ok := r.nameEd.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			u.renameRow(r)
		}
	}
	if r.saveBtn.Clicked(gtx) {
		u.renameRow(r)
	}
	if r.cancelBtn.Clicked(gtx) {
		r.editing = false
	}
	if r.playBtn.Clicked(gtx) && !running {
		u.startRows([]*row{r})
	}
	if r.moreBtn.Clicked(gtx) {
		items := []menuItem{}
		if !running {
			items = append(items, menuItem{label: "Đổi tên", icon: icEdit, do: func() {
				r.editing = true
				r.wantFocus = true
				r.nameEd.SingleLine, r.nameEd.Submit = true, true
				r.nameEd.SetText(r.draft.Name)
				r.nameEd.SetCaret(r.nameEd.Len(), 0)
			}})
		}
		items = append(items, menuItem{label: "Mở thư mục project", icon: icFolderOpn, do: func() { openInExplorer(r.draft.Folder) }})
		if r.item.Output != "" {
			items = append(items, menuItem{label: "Hiện video đã xuất", icon: icMovie, do: func() { revealFile(r.item.Output) }})
		}
		if !running {
			items = append(items,
				menuItem{label: "Chỉ chọn project này", icon: icCheck, do: func() {
					u.mu.Lock()
					for _, o := range u.rows {
						o.check.Value = o == r
					}
					u.mu.Unlock()
				}},
				menuItem{label: "Xóa trạng thái", icon: icClose, do: func() { u.mu.Lock(); r.has = false; u.mu.Unlock() }},
			)
		}
		u.openMenu(220, items)
	}
}

func (u *ui) projectRow(gtx C, r *row, running bool) D {
	u.rowEvents(gtx, r, running)
	if r.wantFocus {
		r.wantFocus = false
		gtx.Execute(key.FocusCmd{Tag: &r.nameEd})
	}
	bg := color.NRGBA{}
	if r.check.Value {
		bg = cRowSel
	}
	kind := rowKind(r)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return panel(gtx, 8, bg, nil, layout.Inset{Top: 6, Bottom: 6}, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					u.cell(colCheck, func(gtx C) D {
						c := material.CheckBox(u.th, &r.check, "")
						c.IconColor = cPrimary
						return layout.Inset{Left: 8}.Layout(gtx, c.Layout)
					}),
					layout.Flexed(2.3, func(gtx C) D { return u.nameCell(gtx, r) }),
					u.cell(colStatus, func(gtx C) D { return u.statusPill(gtx, kind) }),
					u.cell(colDur, func(gtx C) D { return u.text1(gtx, 13, cText, false, fmtDur(r.draft.Duration())) }),
					u.cell(colMod, func(gtx C) D { return u.text1(gtx, 13, cText, false, r.draft.Modified.Format("02/01/2006 15:04")) }),
					layout.Flexed(3, func(gtx C) D { return u.detailCell(gtx, r) }),
					u.cell(colAct, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return u.button(gtx, &r.playBtn, bopt{style: bOutline, icon: icPlay, radius: 20, padV: 7, disabled: running})
							}),
							gapW(6),
							layout.Rigid(func(gtx C) D {
								return u.button(gtx, &r.moreBtn, bopt{style: bGhost, icon: icMore, radius: 20, padV: 7})
							}),
						)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, rgb(0xEEF2F8)) }),
	)
}

func (u *ui) nameCell(gtx C, r *row) D {
	if !r.editing {
		return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D { return u.text1(gtx, 13.5, cText, true, r.draft.Name) })
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D { return u.input(gtx, &r.nameEd, "Tên mới", nil, nil) }),
		gapW(6),
		layout.Rigid(func(gtx C) D {
			return u.button(gtx, &r.saveBtn, bopt{style: bPrimary, icon: icCheck, radius: 10, padV: 8})
		}),
		gapW(4),
		layout.Rigid(func(gtx C) D {
			return u.button(gtx, &r.cancelBtn, bopt{style: bGhost, icon: icClose, radius: 10, padV: 8})
		}),
		gapW(8),
	)
}

func (u *ui) statusPill(gtx C, k statusFilter) D {
	switch k {
	case fRunning:
		return u.pill(gtx, "Đang chạy", cPrimary, cPrimarySoft, icSchedule)
	case fDone, fWarn:
		return u.pill(gtx, "Đã xong", cGreen, cGreenSoft, icCheckCirc)
	case fFailed:
		return u.pill(gtx, "Lỗi", cRed, cRedSoft, icError)
	case fSkipped:
		return u.pill(gtx, "Bỏ qua", cOrange, cOrangeSoft, nil)
	}
	return u.pill(gtx, "Chưa chạy", cMuted, cGraySoft, nil)
}

func (u *ui) detailCell(gtx C, r *row) D {
	if !r.has {
		return D{}
	}
	var (
		ic    *widget.Icon
		col   color.NRGBA
		title = string(r.item.State)
	)
	switch rowKind(r) {
	case fRunning:
		ic, col = icSchedule, cPrimary
	case fDone:
		ic, col = icCheckCirc, cGreen
	case fWarn:
		ic, col = icWarn, cOrange
	case fFailed:
		ic, col = icError, cRed
	default:
		ic, col = icInfo, cOrange
	}
	detail := r.item.Detail
	if r.item.Took > 0 && (r.item.State == runner.StateDone || r.item.State == runner.StateWarn) {
		detail += " · " + r.item.Took.Round(time.Second).String()
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, ic, 16, col) })
		}),
		layout.Rigid(func(gtx C) D { return u.text1(gtx, 12.5, col, true, title) }),
		gapW(8),
		layout.Flexed(1, func(gtx C) D { return u.text1(gtx, 12, cMuted, false, detail) }),
	)
}

func fmtDur(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// ─── Log ──────────────────────────────────────────────────────────────────────

func (u *ui) logCard(gtx C) D {
	return card(gtx, 16, func(gtx C) D {
		toggle := bopt{style: bGhost, icon: icUp, label: "Thu gọn", padV: 7}
		if u.logCollapsed {
			toggle.icon, toggle.label = icDown, "Mở rộng"
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return u.cardTitle(gtx, "3", "Nhật ký hoạt động", func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return u.button(gtx, &u.clearLogBtn, bopt{style: bOutline, icon: icDelete, label: "Xóa log", padV: 7})
						}),
						gapW(8),
						layout.Rigid(func(gtx C) D {
							return u.button(gtx, &u.copyLogBtn, bopt{style: bOutline, icon: icCopy, label: "Sao chép", padV: 7})
						}),
						gapW(8),
						layout.Rigid(func(gtx C) D {
							return u.button(gtx, &u.logDirBtn, bopt{style: bOutline, icon: icFolderOpn, label: "Thư mục log", padV: 7})
						}),
						gapW(8),
						layout.Rigid(func(gtx C) D { return u.button(gtx, &u.logTogl, toggle) }),
					)
				})
			}),
			layout.Rigid(func(gtx C) D {
				if u.logCollapsed {
					return D{}
				}
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return panel(gtx, 10, cSurfaceAlt, &cBorder, insetHV(14, 8), func(gtx C) D {
						h := gtx.Dp(96)
						gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = h, h
						return u.editorStyle(&u.logEd, "", 12.5).Layout(gtx)
					})
				})
			}),
		)
	})
}

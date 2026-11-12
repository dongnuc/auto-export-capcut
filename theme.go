package main

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// ─── Palette ──────────────────────────────────────────────────────────────────

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

var (
	cBg          = rgb(0xF2F5FB)
	cSurface     = rgb(0xFFFFFF)
	cSurfaceAlt  = rgb(0xF7F9FD)
	cBorder      = rgb(0xE2E8F4)
	cBorderInput = rgb(0xD3DCEE)
	cText        = rgb(0x1C2A4E)
	cMuted       = rgb(0x6C7A98)
	cFaint       = rgb(0x9AA5BE)
	cPrimary     = rgb(0x2F6BF2)
	cPrimaryDark = rgb(0x2358D4)
	cPrimarySoft = rgb(0xE9F0FF)
	cPrimaryHov  = rgb(0xDCE7FF)
	cGreen       = rgb(0x1E9D62)
	cGreenSoft   = rgb(0xE3F5EC)
	cOrange      = rgb(0xE5800B)
	cOrangeSoft  = rgb(0xFFF1DD)
	cRed         = rgb(0xD93C3C)
	cRedSoft     = rgb(0xFCE8E8)
	cGraySoft    = rgb(0xECEFF5)
	cRowSel      = rgb(0xF4F8FF)
)

// ─── Icons ────────────────────────────────────────────────────────────────────

func mustIcon(data []byte) *widget.Icon {
	i, err := widget.NewIcon(data)
	if err != nil {
		panic(err)
	}
	return i
}

var (
	icPlay      = mustIcon(icons.AVPlayArrow)
	icStop      = mustIcon(icons.AVStop)
	icHistory   = mustIcon(icons.ActionHistory)
	icSettings  = mustIcon(icons.ActionSettings)
	icInfo      = mustIcon(icons.ActionInfo)
	icHelp      = mustIcon(icons.ActionHelp)
	icFolder    = mustIcon(icons.FileFolder)
	icFolderOpn = mustIcon(icons.FileFolderOpen)
	icRefresh   = mustIcon(icons.NavigationRefresh)
	icSearch    = mustIcon(icons.ActionSearch)
	icCheck     = mustIcon(icons.NavigationCheck)
	icCheckCirc = mustIcon(icons.ActionCheckCircle)
	icWarn      = mustIcon(icons.AlertWarning)
	icError     = mustIcon(icons.AlertError)
	icMore      = mustIcon(icons.NavigationMoreHoriz)
	icEdit      = mustIcon(icons.ImageEdit)
	icDelete    = mustIcon(icons.ActionDelete)
	icCopy      = mustIcon(icons.ContentContentCopy)
	icUp        = mustIcon(icons.NavigationExpandLess)
	icDown      = mustIcon(icons.NavigationExpandMore)
	icDropdown  = mustIcon(icons.NavigationArrowDropDown)
	icSpark     = mustIcon(icons.ImageFlare)
	icBolt      = mustIcon(icons.ImageFlashOn)
	icClose     = mustIcon(icons.NavigationClose)
	icPower     = mustIcon(icons.ActionPowerSettingsNew)
	icMovie     = mustIcon(icons.AVMovie)
	icKeyboard  = mustIcon(icons.HardwareKeyboard)
	icRocket    = mustIcon(icons.ActionFlightTakeoff)
	icPlayCirc  = mustIcon(icons.AVPlayCircleOutline)
	icOpenNew   = mustIcon(icons.ActionOpenInNew)
	icSchedule  = mustIcon(icons.ActionSchedule)
	icBulb      = mustIcon(icons.ActionLightbulbOutline)
	icList      = mustIcon(icons.ActionViewList)
)

func drawIcon(gtx C, ic *widget.Icon, size unit.Dp, col color.NRGBA) D {
	s := gtx.Dp(size)
	gtx.Constraints.Min = image.Pt(s, s)
	gtx.Constraints.Max = gtx.Constraints.Min
	return ic.Layout(gtx, col)
}

// ─── Shapes ───────────────────────────────────────────────────────────────────

func fillRR(gtx C, size image.Point, radius unit.Dp, col color.NRGBA) {
	paint.FillShape(gtx.Ops, col, clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(radius)).Op(gtx.Ops))
}

func strokeRR(gtx C, size image.Point, radius unit.Dp, width unit.Dp, col color.NRGBA) {
	w := gtx.Dp(width)
	if w < 1 {
		w = 1
	}
	half := w / 2
	rect := image.Rectangle{Min: image.Pt(half, half), Max: size.Sub(image.Pt(half, half))}
	paint.FillShape(gtx.Ops, col, clip.Stroke{
		Path:  clip.UniformRRect(rect, gtx.Dp(radius)).Path(gtx.Ops),
		Width: float32(w),
	}.Op())
}

// panel draws content on a rounded background (with optional border).
func panel(gtx C, radius unit.Dp, bg color.NRGBA, border *color.NRGBA, pad layout.Inset, w layout.Widget) D {
	return layout.Background{}.Layout(gtx,
		func(gtx C) D {
			sz := gtx.Constraints.Min
			fillRR(gtx, sz, radius, bg)
			if border != nil {
				strokeRR(gtx, sz, radius, 1, *border)
			}
			return D{Size: sz}
		},
		func(gtx C) D { return pad.Layout(gtx, w) })
}

func inset(all unit.Dp) layout.Inset { return layout.UniformInset(all) }

func insetHV(h, v unit.Dp) layout.Inset {
	return layout.Inset{Left: h, Right: h, Top: v, Bottom: v}
}

// card is a white rounded surface with a soft shadow.
func card(gtx C, pad unit.Dp, w layout.Widget) D {
	return layout.Background{}.Layout(gtx,
		func(gtx C) D {
			sz := gtx.Constraints.Min
			for _, s := range []struct {
				dy int
				a  uint8
			}{{4, 6}, {2, 9}} {
				st := op.Offset(image.Pt(0, gtx.Dp(unit.Dp(s.dy)))).Push(gtx.Ops)
				fillRR(gtx, sz, 14, color.NRGBA{R: 0x1c, G: 0x2a, B: 0x4e, A: s.a})
				st.Pop()
			}
			fillRR(gtx, sz, 14, cSurface)
			strokeRR(gtx, sz, 14, 1, cBorder)
			return D{Size: sz}
		},
		func(gtx C) D { return inset(pad).Layout(gtx, w) })
}

func gapW(w unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Width: w}.Layout) }
func gapH(h unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Height: h}.Layout) }

func fillRect(gtx C, r image.Rectangle, col color.NRGBA) {
	paint.FillShape(gtx.Ops, col, clip.Rect(r).Op())
}

func fillBg(gtx C, col color.NRGBA) {
	paint.FillShape(gtx.Ops, col, clip.Rect{Max: gtx.Constraints.Max}.Op())
}

func hline(gtx C, col color.NRGBA) D {
	h := gtx.Dp(1)
	paint.FillShape(gtx.Ops, col, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, h)}.Op())
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

// ─── Text ─────────────────────────────────────────────────────────────────────

func (u *ui) text(gtx C, size float32, col color.NRGBA, bold bool, s string) D {
	l := material.Label(u.th, unit.Sp(size), s)
	l.Color = col
	if bold {
		l.Font.Weight = font.Bold
	}
	return l.Layout(gtx)
}

func (u *ui) text1(gtx C, size float32, col color.NRGBA, bold bool, s string) D {
	l := material.Label(u.th, unit.Sp(size), s)
	l.Color, l.MaxLines = col, 1
	if bold {
		l.Font.Weight = font.Bold
	}
	return l.Layout(gtx)
}

// ─── Buttons ──────────────────────────────────────────────────────────────────

type bstyle int

const (
	bPrimary bstyle = iota
	bOutline
	bSoft
	bDanger
	bGhost
)

type bopt struct {
	style    bstyle
	icon     *widget.Icon
	label    string
	trail    *widget.Icon // trailing icon, e.g. a drop-down arrow
	size     float32      // text size (default 13)
	padV     unit.Dp
	padH     unit.Dp
	minW     unit.Dp
	radius   unit.Dp
	disabled bool
}

func (u *ui) button(gtx C, c *widget.Clickable, o bopt) D {
	if o.size == 0 {
		o.size = 13
	}
	if o.padV == 0 {
		o.padV = 9
	}
	if o.padH == 0 {
		o.padH = 14
	}
	if o.radius == 0 {
		o.radius = 10
	}
	if o.label == "" && o.icon != nil {
		o.padH = o.padV
	}
	content := func(gtx C, fg color.NRGBA) D {
		isz := unit.Dp(o.size + 5)
		return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceSides}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				if o.icon == nil {
					return D{}
				}
				return drawIcon(gtx, o.icon, isz, fg)
			}),
			layout.Rigid(func(gtx C) D {
				if o.icon == nil || o.label == "" {
					return D{}
				}
				return layout.Spacer{Width: 6}.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				if o.label == "" {
					return D{}
				}
				l := material.Label(u.th, unit.Sp(o.size), o.label)
				l.Color, l.MaxLines = fg, 1
				l.Font.Weight = font.SemiBold
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				if o.trail == nil {
					return D{}
				}
				return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, o.trail, 20, fg) })
			}),
		)
	}
	colors := func(hover bool) (bg color.NRGBA, border *color.NRGBA, fg color.NRGBA) {
		switch o.style {
		case bPrimary:
			bg, fg = cPrimary, rgb(0xFFFFFF)
			if hover {
				bg = cPrimaryDark
			}
		case bOutline:
			bg, fg = cSurface, cPrimary
			b := rgb(0xBFD0F5)
			border = &b
			if hover {
				bg = rgb(0xF3F7FF)
			}
		case bSoft:
			bg, fg = cPrimarySoft, cPrimary
			if hover {
				bg = cPrimaryHov
			}
		case bDanger:
			bg, fg = cRed, rgb(0xFFFFFF)
			if hover {
				bg = rgb(0xBF2F2F)
			}
		default: // ghost
			bg, fg = color.NRGBA{}, cMuted
			if hover {
				bg = cGraySoft
			}
		}
		if o.disabled {
			bg, fg = cGraySoft, cFaint
			border = nil
			if o.style == bGhost {
				bg = color.NRGBA{}
			}
		}
		return
	}
	draw := func(gtx C, hover bool) D {
		bg, border, fg := colors(hover)
		if o.minW > 0 {
			gtx.Constraints.Min.X = gtx.Dp(o.minW)
		}
		return panel(gtx, o.radius, bg, border, insetHV(o.padH, o.padV), func(gtx C) D { return content(gtx, fg) })
	}
	if o.disabled {
		return draw(gtx, false)
	}
	return c.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		return draw(gtx, c.Hovered())
	})
}

// ─── Inputs ───────────────────────────────────────────────────────────────────

func (u *ui) editorStyle(ed *widget.Editor, hint string, size float32) material.EditorStyle {
	e := material.Editor(u.th, ed, hint)
	e.TextSize = unit.Sp(size)
	e.Color = cText
	e.HintColor = cFaint
	e.SelectionColor = withAlpha(cPrimary, 0x40)
	return e
}

// input is a rounded text field with an optional leading icon and trailing widget.
func (u *ui) input(gtx C, ed *widget.Editor, hint string, lead *widget.Icon, trail layout.Widget) D {
	border := cBorderInput
	if gtx.Focused(ed) {
		border = cPrimary
	}
	return panel(gtx, 10, cSurface, &border, layout.Inset{Left: 12, Right: 8, Top: 9, Bottom: 9}, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				if lead == nil {
					return D{}
				}
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, lead, 18, cFaint) })
			}),
			layout.Flexed(1, func(gtx C) D { return u.editorStyle(ed, hint, 13).Layout(gtx) }),
			layout.Rigid(func(gtx C) D {
				if trail == nil {
					return D{}
				}
				return trail(gtx)
			}),
		)
	})
}

// numField is a small integer field with up/down steppers.
type numField struct {
	ed       widget.Editor
	up, down widget.Clickable
	min, max int
}

func newNum(v, min, max int) *numField {
	n := &numField{min: min, max: max}
	n.ed.SingleLine = true
	n.ed.Filter = "0123456789"
	n.ed.SetText(strconv.Itoa(v))
	return n
}

func (n *numField) val(def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(n.ed.Text()))
	if err != nil || v < n.min {
		return def
	}
	if n.max > 0 && v > n.max {
		return n.max
	}
	return v
}

func (n *numField) set(v int) { n.ed.SetText(strconv.Itoa(v)) }

// step handles the stepper buttons; it returns true when the value changed.
func (n *numField) step(gtx C) bool {
	d := 0
	if n.up.Clicked(gtx) {
		d++
	}
	if n.down.Clicked(gtx) {
		d--
	}
	if d == 0 {
		return false
	}
	v := n.val(n.min) + d
	if v < n.min {
		v = n.min
	}
	if n.max > 0 && v > n.max {
		v = n.max
	}
	n.set(v)
	return true
}

func (u *ui) numInput(gtx C, n *numField) D {
	return u.input(gtx, &n.ed, "", nil, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return n.up.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					return drawIcon(gtx, icUp, 15, cMuted)
				})
			}),
			layout.Rigid(func(gtx C) D {
				return n.down.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					return drawIcon(gtx, icDown, 15, cMuted)
				})
			}),
		)
	})
}

// combo is a text field with a drop-down of presets.
type combo struct {
	ed      widget.Editor
	btn     widget.Clickable
	presets []string
}

func newCombo(val string, presets ...string) *combo {
	c := &combo{presets: presets}
	c.ed.SingleLine = true
	c.ed.SetText(val)
	return c
}

func (c *combo) text() string { return strings.TrimSpace(c.ed.Text()) }

func (u *ui) comboInput(gtx C, c *combo) D {
	return u.input(gtx, &c.ed, "", nil, func(gtx C) D {
		return c.btn.Layout(gtx, func(gtx C) D {
			pointer.CursorPointer.Add(gtx.Ops)
			return drawIcon(gtx, icDropdown, 22, cMuted)
		})
	})
}

// labeled puts a small caption above a control.
func (u *ui) labeled(gtx C, caption string, w layout.Widget) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.text1(gtx, 12, cMuted, false, caption) }),
		gapH(5),
		layout.Rigid(w),
	)
}

// check / radio with the app's colours.
func (u *ui) checkbox(gtx C, b *widget.Bool, label string) D {
	c := material.CheckBox(u.th, b, label)
	c.TextSize = unit.Sp(13)
	c.Color = cText
	c.IconColor = cPrimary
	return c.Layout(gtx)
}

func (u *ui) radio(gtx C, g *widget.Enum, key, label string) D {
	r := material.RadioButton(u.th, g, key, label)
	r.TextSize = unit.Sp(13)
	r.Color = cText
	r.IconColor = cPrimary
	return r.Layout(gtx)
}

// badge is the numbered blue circle used in card titles.
func (u *ui) badge(gtx C, n string) D {
	s := gtx.Dp(26)
	gtx.Constraints.Min = image.Pt(s, s)
	gtx.Constraints.Max = gtx.Constraints.Min
	fillRR(gtx, image.Pt(s, s), 13, cPrimary)
	return layout.Center.Layout(gtx, func(gtx C) D { return u.text(gtx, 13, rgb(0xFFFFFF), true, n) })
}

func (u *ui) cardTitle(gtx C, n, title string, right layout.Widget) D {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			if n == "" {
				return D{}
			}
			return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D { return u.badge(gtx, n) })
		}),
		layout.Rigid(func(gtx C) D { return u.text(gtx, 17, cText, true, title) }),
		layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
		layout.Rigid(func(gtx C) D {
			if right == nil {
				return D{}
			}
			return right(gtx)
		}),
	)
}

// pill is a small rounded status label.
func (u *ui) pill(gtx C, txt string, fg, bg color.NRGBA, ic *widget.Icon) D {
	return panel(gtx, 20, bg, nil, layout.Inset{Left: 10, Right: 12, Top: 4, Bottom: 4}, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				if ic == nil {
					return D{}
				}
				return layout.Inset{Right: 5}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, ic, 15, fg) })
			}),
			layout.Rigid(func(gtx C) D {
				l := material.Label(u.th, 12, txt)
				l.Color, l.MaxLines = fg, 1
				l.Font.Weight = font.SemiBold
				return l.Layout(gtx)
			}),
		)
	})
}

// ─── Pop-up menu ──────────────────────────────────────────────────────────────

type menuItem struct {
	label  string
	icon   *widget.Icon
	danger bool
	do     func()
}

type menu struct {
	open  bool
	at    f32.Point
	items []menuItem
	btns  []widget.Clickable
	scrim widget.Clickable
	minW  unit.Dp
}

// openMenu shows a menu at the last pointer press position.
func (u *ui) openMenu(minW unit.Dp, items []menuItem) {
	u.menu.open = true
	u.menu.at = u.ptrPos
	u.menu.items = items
	u.menu.minW = minW
	if len(u.menu.btns) < len(items) {
		u.menu.btns = append(u.menu.btns, make([]widget.Clickable, len(items)-len(u.menu.btns))...)
	}
	u.win.Invalidate()
}

func (u *ui) layoutMenu(gtx C) {
	m := &u.menu
	if !m.open {
		return
	}
	win := gtx.Constraints.Max
	// Click outside closes.
	if m.scrim.Clicked(gtx) {
		m.open = false
		return
	}
	m.scrim.Layout(gtx, func(gtx C) D { return D{Size: win} })

	var picked func()
	macro := op.Record(gtx.Ops)
	gtx2 := gtx
	gtx2.Constraints.Min = image.Point{}
	gtx2.Constraints.Max = image.Pt(gtx.Dp(320), win.Y)
	dims := panel(gtx2, 12, cSurface, &cBorder, inset(6), func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Dp(m.minW)
		children := make([]layout.FlexChild, 0, len(m.items))
		for i := range m.items {
			i := i
			it := m.items[i]
			children = append(children, layout.Rigid(func(gtx C) D {
				if m.btns[i].Clicked(gtx) {
					picked = it.do
					m.open = false
				}
				return m.btns[i].Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					bg := color.NRGBA{}
					if m.btns[i].Hovered() {
						bg = cPrimarySoft
					}
					fg := cText
					if it.danger {
						fg = cRed
					}
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return panel(gtx, 8, bg, nil, insetHV(12, 9), func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								if it.icon == nil {
									return D{}
								}
								return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D { return drawIcon(gtx, it.icon, 18, cMuted) })
							}),
							layout.Rigid(func(gtx C) D { return u.text1(gtx, 13, fg, false, it.label) }),
						)
					})
				})
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	call := macro.Stop()

	x, y := int(m.at.X), int(m.at.Y)+gtx.Dp(6)
	if x+dims.Size.X > win.X-gtx.Dp(8) {
		x = win.X - gtx.Dp(8) - dims.Size.X
	}
	if y+dims.Size.Y > win.Y-gtx.Dp(8) {
		y = int(m.at.Y) - dims.Size.Y - gtx.Dp(6)
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	st.Pop()
	if picked != nil {
		picked()
		u.win.Invalidate()
	}
}

// trackPointer remembers where the pointer was last pressed, in window
// coordinates, so menus can open next to what was clicked. It must be the
// last thing laid out: it sits on top and lets events pass through.
func (u *ui) trackPointer(gtx C) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &u.ptrTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			u.ptrPos = pe.Position
		}
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &u.ptrTag)
}

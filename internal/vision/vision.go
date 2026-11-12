// Package vision holds the few pixel measurements the tool makes on CapCut's
// windows. It never reads text: it only finds where blocks of pixels are and
// whether a region changed.
package vision

import (
	"hash/fnv"
	"image"
)

// Run is a vertical stretch of non-background pixels in one column.
type Run struct{ Top, Bottom int }

func (r Run) Height() int { return r.Bottom - r.Top + 1 }
func (r Run) Center() int { return (r.Top + r.Bottom) / 2 }

func lum(img *image.RGBA, x, y int) int {
	i := img.PixOffset(x, y)
	p := img.Pix[i : i+3 : i+3]
	return (int(p[0])*299 + int(p[1])*587 + int(p[2])*114) / 1000
}

// ColumnRuns lists stretches in column x, rows y0..y1, brighter than thr.
func ColumnRuns(img *image.RGBA, x, y0, y1, thr int) []Run {
	b := img.Bounds()
	if x < b.Min.X || x >= b.Max.X {
		return nil
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if y1 >= b.Max.Y {
		y1 = b.Max.Y - 1
	}
	var out []Run
	start := -1
	for y := y0; y <= y1; y++ {
		on := lum(img, x, y) > thr
		switch {
		case on && start < 0:
			start = y
		case !on && start >= 0:
			out = append(out, Run{start, y - 1})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, Run{start, y1})
	}
	return out
}

// HomeLayout describes CapCut's home screen (client area, physical pixels at
// scale 1.0 = 96 DPI). Measured on CapCut 6.9:
//   - the "Dự án" header row has right-aligned buttons; the "Thùng rác"
//     button's centre column is RightOffset px from the right edge
//   - the first project card's centre sits CardBelow px under that row and
//     CardLeft px from the left edge
//   - the row moves down ~125 px when the "Các công cụ khác" section loads,
//     which is why it is found, not assumed
type HomeLayout struct {
	RightOffset float64 // 208
	CardBelow   float64 // 85
	CardLeft    float64 // 294
	ScanFrom    float64 // fraction of height to start scanning (below the promo banner)
}

var CapCut69Home = HomeLayout{RightOffset: 208, CardBelow: 85, CardLeft: 294, ScanFrom: 0.31}

// FirstCard finds the centre of the first project card in a screenshot of
// the home window's client area. scale = DPI/96. ok=false if the header
// buttons were not found.
func (l HomeLayout) FirstCard(img *image.RGBA, scale float64) (x, y int, header Run, ok bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scanX := b.Min.X + w - int(l.RightOffset*scale)
	minH, maxH := int(14*scale), int(40*scale)
	for _, r := range ColumnRuns(img, scanX, b.Min.Y+int(l.ScanFrom*float64(h)), b.Max.Y-1, 28) {
		if r.Height() >= minH && r.Height() <= maxH {
			cx := b.Min.X + int(l.CardLeft*scale)
			cy := r.Center() + int(l.CardBelow*scale)
			if cy >= b.Max.Y {
				return 0, 0, r, false
			}
			return cx - b.Min.X, cy - b.Min.Y, r, true
		}
	}
	return 0, 0, Run{}, false
}

// Hash fingerprints a region so changes (a progress bar moving) can be seen.
func Hash(img *image.RGBA) uint64 {
	h := fnv.New64a()
	h.Write(img.Pix)
	return h.Sum64()
}

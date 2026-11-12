package runner

import (
	"fmt"
	"image"
	"time"

	"capcut-auto-exporter/internal/detect"
	"capcut-auto-exporter/internal/drafts"
	"capcut-auto-exporter/internal/vision"
	"capcut-auto-exporter/internal/winapi"
)

// exportWatch returns the Abort hook used while waiting for the output file.
// It never reads text; it watches the export dialog:
//   - the bottom strip of the "Đang xuất" view (progress bar + percentage)
//     must keep changing; unchanged for StallMin minutes = stalled
//   - the dialog disappearing with no output file = CapCut stopped the export
//
// The dialog is captured with PrintWindow, so other windows on top of it do
// not fool the stall check.
func (r *Runner) exportWatch(dlg winapi.Window, d drafts.Draft, before detect.Snapshot) func() error {
	c := r.cfg
	stall := time.Duration(c.StallMin) * time.Minute
	var (
		lastCheck  time.Time
		lastChange = time.Now()
		lastHash   uint64
		lastLog    = time.Now()
		goneSince  time.Time
		lastSize   int32
		promptSeen bool
	)
	return func() error {
		if time.Since(lastCheck) < 5*time.Second {
			return nil
		}
		lastCheck = time.Now()
		r.closeUpdatePopups("khi-xuat")

		if !winapi.IsWindow(dlg.HWND) || !r.dialogVisible(dlg.HWND) {
			if goneSince.IsZero() {
				goneSince = time.Now()
				r.L.Debugf("hộp thoại xuất đã đóng. Cửa sổ: %s", r.app.DescribeWindows())
			}
			if detect.FindNew(c.OutputDir, before, d.Name) == "" && time.Since(goneSince) > time.Minute {
				return fmt.Errorf("hộp thoại xuất đã đóng mà không có file trong %s — CapCut đã dừng hoặc hủy xuất", c.OutputDir)
			}
			return nil
		}

		rect := winapi.WindowRect(dlg.HWND)
		if rect.H() != lastSize {
			r.L.Debugf("hộp thoại xuất đổi kích thước: %dx%d", rect.W(), rect.H())
			lastSize = rect.H()
			lastChange = time.Now()
		}
		// A small extra CapCut window during an export is usually its "stop
		// exporting?" confirmation; log it loudly, never answer it.
		for _, w := range r.app.Dialogs() {
			if w.HWND != dlg.HWND && w.Rect.W() < 500 && !promptSeen {
				promptSeen = true
				r.L.Warnf("Có hộp thoại nhỏ xuất hiện khi đang xuất: %q %dx%d — tool không bấm gì vào nó", w.Title, w.Rect.W(), w.Rect.H())
				r.L.Shot("hop-thoai-nho-khi-xuat")
			}
		}

		img, err := winapi.CaptureWindow(dlg.HWND)
		if err != nil {
			r.L.Debugf("chụp hộp thoại xuất lỗi: %v", err)
			return nil
		}
		b := img.Bounds()
		strip := img.SubImage(image.Rect(b.Min.X, b.Max.Y-b.Dy()*14/100, b.Max.X, b.Max.Y)).(*image.RGBA)
		if h := vision.Hash(copyRGBA(strip)); h != lastHash {
			lastHash, lastChange = h, time.Now()
		}
		idle := time.Since(lastChange)
		if time.Since(lastLog) >= time.Minute {
			lastLog = time.Now()
			r.L.Debugf("tiến trình xuất: thanh tiến trình đổi lần cuối cách đây %s", idle.Round(time.Second))
		}
		if stall > 0 && idle > stall {
			r.L.Shot("tien-trinh-dung")
			return fmt.Errorf("tiến trình xuất không thay đổi trong %d phút — CapCut có thể bị treo", c.StallMin)
		}
		return nil
	}
}

func (r *Runner) dialogVisible(h uintptr) bool {
	for _, w := range r.app.Dialogs() {
		if w.HWND == h {
			return true
		}
	}
	return false
}

// copyRGBA makes a sub-image contiguous so its bytes can be hashed.
func copyRGBA(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+b.Dx()*4], src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):])
	}
	return dst
}

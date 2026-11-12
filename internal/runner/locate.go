package runner

import (
	"fmt"
	"time"

	"capcut-auto-exporter/internal/vision"
	"capcut-auto-exporter/internal/winapi"
)

// homeCard finds the first project card on the home window, in screen pixels.
func (r *Runner) homeCard(h uintptr) (x, y int32, hdr vision.Run, ok bool) {
	rect := winapi.ClientRectOnScreen(h)
	img, err := winapi.Capture(rect)
	if err != nil {
		r.L.Debugf("chụp màn hình chính để dò thẻ lỗi: %v", err)
		return 0, 0, vision.Run{}, false
	}
	cx, cy, hdr, ok := vision.CapCut69Home.FirstCard(img, winapi.DPIScale(h))
	return rect.Left + int32(cx), rect.Top + int32(cy), hdr, ok
}

// waitHomeStable waits until the home grid stops moving. The "Các công cụ
// khác" section loads from the network a few seconds after the window shows
// and pushes the project grid ~125 px down; clicking before that misses.
func (r *Runner) waitHomeStable(h uintptr) {
	c := r.cfg
	minWait := 4 * time.Second
	limit := time.Duration(c.HomeWaitSec) * time.Second
	if limit < 12*time.Second {
		limit = 12 * time.Second
	}
	start := time.Now()
	var lastY int32 = -1
	same := 0
	for time.Since(start) < limit && !r.stopped() {
		r.closeUpdatePopups("man-hinh-chinh")
		_, y, hdr, ok := r.homeCard(h)
		switch {
		case !ok:
			same = 0
		case y == lastY:
			same++
		default:
			if lastY >= 0 {
				r.L.Debugf("lưới project di chuyển: thẻ đầu y %d → %d (hàng tiêu đề %d..%d)", lastY, y, hdr.Top, hdr.Bottom)
			}
			same = 0
		}
		if ok {
			lastY = y
		}
		if time.Since(start) >= minWait && same >= 3 {
			r.L.Debugf("màn hình chính ổn định sau %v (thẻ đầu y=%d)", time.Since(start).Round(100*time.Millisecond), lastY)
			return
		}
		time.Sleep(time.Second)
	}
	if lastY < 0 {
		r.L.Warnf("Không dò được hàng project trên màn hình chính sau %v — sẽ dùng vị trí đã hiệu chỉnh", limit)
	} else {
		r.L.Debugf("hết %v chờ màn hình chính ổn định (thẻ đầu y=%d)", limit, lastY)
	}
}

// locateFirstCard returns where to click and how it was decided.
func (r *Runner) locateFirstCard(h uintptr) (int32, int32, string) {
	if x, y, hdr, ok := r.homeCard(h); ok {
		return x, y, fmt.Sprintf("dò tự động (hàng tiêu đề Dự án y=%d..%d)", hdr.Top, hdr.Bottom)
	}
	rect := winapi.ClientRectOnScreen(h)
	x := rect.Left + int32(r.cfg.CardX*float64(rect.W()))
	y := rect.Top + int32(r.cfg.CardY*float64(rect.H()))
	r.L.Shot("khong-do-duoc-the")
	return x, y, fmt.Sprintf("KHÔNG dò được, dùng vị trí hiệu chỉnh (%.3f,%.3f)", r.cfg.CardX, r.cfg.CardY)
}

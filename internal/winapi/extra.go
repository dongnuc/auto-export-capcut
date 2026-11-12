//go:build windows

package winapi

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")

	pSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	pSetWindowPos          = user32.NewProc("SetWindowPos")
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pGetDC                 = user32.NewProc("GetDC")
	pReleaseDC             = user32.NewProc("ReleaseDC")
	pIsWindow              = user32.NewProc("IsWindow")
	pGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	pPrintWindow           = user32.NewProc("PrintWindow")
	pCreateCompatibleDC    = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBmp   = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject          = gdi32.NewProc("SelectObject")
	pBitBlt                = gdi32.NewProc("BitBlt")
	pGetDIBits             = gdi32.NewProc("GetDIBits")
	pDeleteObject          = gdi32.NewProc("DeleteObject")
	pDeleteDC              = gdi32.NewProc("DeleteDC")
)

// IsTool reports Qt tooltip/popup helper windows, which are never the main
// window or a dialog.
func (w Window) IsTool() bool {
	return strings.Contains(w.Class, "Tool") || w.Rect.W() <= 0 || w.Rect.H() <= 0
}

// IsWindow reports whether the handle still refers to an existing window.
func IsWindow(h uintptr) bool {
	r, _, _ := pIsWindow.Call(h)
	return r != 0
}

// PrimaryWorkArea is the primary monitor minus the taskbar, in pixels.
func PrimaryWorkArea() Rect {
	var r Rect
	pSystemParametersInfoW.Call(0x0030 /*SPI_GETWORKAREA*/, 0, uintptr(unsafe.Pointer(&r)), 0)
	return r
}

// MoveToPrimaryMaximized restores the window, moves it onto the primary
// monitor and maximizes it there, so card positions are the same every run
// whatever monitor CapCut last opened on.
func MoveToPrimaryMaximized(h uintptr) Rect {
	Restore(h)
	time.Sleep(300 * time.Millisecond)
	wa := PrimaryWorkArea()
	w, hh := wa.W()*3/4, wa.H()*3/4
	const swpNoZOrder, swpShowWindow = 0x0004, 0x0040
	pSetWindowPos.Call(h, 0, uintptr(wa.Left+40), uintptr(wa.Top+40), uintptr(w), uintptr(hh), swpNoZOrder|swpShowWindow)
	time.Sleep(300 * time.Millisecond)
	Maximize(h)
	time.Sleep(600 * time.Millisecond)
	return wa
}

// WindowRect returns a window's outer rectangle on screen.
func WindowRect(h uintptr) Rect {
	var r Rect
	pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}

// Capture grabs a rectangle of the screen (screen pixels).
func Capture(r Rect) (*image.RGBA, error) {
	iw, ih := r.W(), r.H()
	if iw <= 0 || ih <= 0 {
		return nil, fmt.Errorf("vùng chụp không hợp lệ %dx%d", iw, ih)
	}
	screen, _, _ := pGetDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("GetDC thất bại")
	}
	defer pReleaseDC.Call(0, screen)
	mem, _, _ := pCreateCompatibleDC.Call(screen)
	defer pDeleteDC.Call(mem)
	bmp, _, _ := pCreateCompatibleBmp.Call(screen, uintptr(iw), uintptr(ih))
	if bmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap thất bại")
	}
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(mem, bmp)
	const srcCopy, captureBlt = 0x00CC0020, 0x40000000
	ok, _, err := pBitBlt.Call(mem, 0, 0, uintptr(iw), uintptr(ih), screen, uintptr(r.Left), uintptr(r.Top), srcCopy|captureBlt)
	pSelectObject.Call(mem, old)
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt: %v", err)
	}
	type bitmapInfoHeader struct {
		size                   uint32
		width, height          int32
		planes, bitCount       uint16
		compression, sizeImage uint32
		xPels, yPels           int32
		clrUsed, clrImportant  uint32
	}
	bi := bitmapInfoHeader{width: iw, height: -ih, planes: 1, bitCount: 32}
	bi.size = uint32(unsafe.Sizeof(bi))
	buf := make([]byte, int(iw)*int(ih)*4)
	n, _, _ := pGetDIBits.Call(mem, bmp, 0, uintptr(ih), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0)
	if n == 0 {
		return nil, fmt.Errorf("GetDIBits thất bại")
	}
	img := image.NewRGBA(image.Rect(0, 0, int(iw), int(ih)))
	for i := 0; i < len(buf); i += 4 { // BGRA → RGBA
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], 255
	}
	return img, nil
}

// VirtualScreen is the rectangle covering every monitor.
func VirtualScreen() Rect {
	const smX, smY, smW, smH = 76, 77, 78, 79
	x, _, _ := pGetSystemMetrics.Call(smX)
	y, _, _ := pGetSystemMetrics.Call(smY)
	w, _, _ := pGetSystemMetrics.Call(smW)
	h, _, _ := pGetSystemMetrics.Call(smH)
	return Rect{int32(x), int32(y), int32(x) + int32(w), int32(y) + int32(h)}
}

// Screenshot saves the whole virtual desktop (all monitors) as a PNG.
func Screenshot(path string) error {
	img, err := Capture(VirtualScreen())
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(f, img)
}

// DPIScale is the window's DPI divided by 96 (1.0 at 100 % scaling).
func DPIScale(h uintptr) float64 {
	if pGetDpiForWindow.Find() != nil {
		return 1
	}
	d, _, _ := pGetDpiForWindow.Call(h)
	if d == 0 {
		return 1
	}
	return float64(d) / 96
}

// CaptureWindow renders a window into an image with PrintWindow, so it works
// even when other windows cover it.
func CaptureWindow(h uintptr) (*image.RGBA, error) {
	r := WindowRect(h)
	iw, ih := r.W(), r.H()
	if iw <= 0 || ih <= 0 {
		return nil, fmt.Errorf("cửa sổ không có kích thước")
	}
	screen, _, _ := pGetDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("GetDC thất bại")
	}
	defer pReleaseDC.Call(0, screen)
	mem, _, _ := pCreateCompatibleDC.Call(screen)
	defer pDeleteDC.Call(mem)
	bmp, _, _ := pCreateCompatibleBmp.Call(screen, uintptr(iw), uintptr(ih))
	if bmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap thất bại")
	}
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(mem, bmp)
	ok, _, err := pPrintWindow.Call(h, mem, 2 /*PW_RENDERFULLCONTENT*/)
	pSelectObject.Call(mem, old)
	if ok == 0 {
		return nil, fmt.Errorf("PrintWindow: %v", err)
	}
	type bitmapInfoHeader struct {
		size                   uint32
		width, height          int32
		planes, bitCount       uint16
		compression, sizeImage uint32
		xPels, yPels           int32
		clrUsed, clrImportant  uint32
	}
	bi := bitmapInfoHeader{width: iw, height: -ih, planes: 1, bitCount: 32}
	bi.size = uint32(unsafe.Sizeof(bi))
	buf := make([]byte, int(iw)*int(ih)*4)
	if n, _, _ := pGetDIBits.Call(mem, bmp, 0, uintptr(ih), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0); n == 0 {
		return nil, fmt.Errorf("GetDIBits thất bại")
	}
	img := image.NewRGBA(image.Rect(0, 0, int(iw), int(ih)))
	for i := 0; i < len(buf); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], 255
	}
	return img, nil
}

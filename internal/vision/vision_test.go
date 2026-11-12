package vision

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"testing"
)

func load(t *testing.T, p string) *image.RGBA {
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, image.Point{}, draw.Src)
	return img
}

// Both home layouts captured on CapCut 6.9 (1920x1032 client, 100 % DPI).
// The first card is "Chay_anh" (no tools section) / "Table Tennis" (tools).
func TestFirstCardBothLayouts(t *testing.T) {
	cases := []struct {
		file string
		y0   int // card thumbnail spans y0..y0+98
	}{
		{"testdata/home_no_tools.png", 408},
		{"testdata/home_tools.png", 533},
	}
	for _, c := range cases {
		x, y, hdr, ok := CapCut69Home.FirstCard(load(t, c.file), 1)
		if !ok {
			t.Fatalf("%s: not found", c.file)
		}
		if x < 250 || x > 340 || y < c.y0+20 || y > c.y0+78 {
			t.Errorf("%s: card at (%d,%d), header %+v; want inside x 245..343 y %d..%d", c.file, x, y, hdr, c.y0, c.y0+98)
		}
	}
}

func TestHashChanges(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 4, 4))
	b := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if Hash(a) != Hash(b) {
		t.Fatal("equal images differ")
	}
	b.Pix[5] = 9
	if Hash(a) == Hash(b) {
		t.Fatal("different images equal")
	}
}

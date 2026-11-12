package detect

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestNameMatches(t *testing.T) {
	cases := []struct {
		stem, name string
		want       bool
	}{
		{"Boxing", "Boxing", true},
		{"boxing (1)", "Boxing", true},
		{"Bóng đá", "Bóng đá", true},
		{"MM-Oto", "MM-Oto", true},
		{"MM-Oto_2", "MM-Oto", true},
		{"MM-BC", "MM-Oto", false},
		{"a b", "a  b", true},
		{"x", "", false},
	}
	for _, c := range cases {
		if got := NameMatches(c.stem, c.name); got != c.want {
			t.Errorf("NameMatches(%q,%q)=%v want %v", c.stem, c.name, got, c.want)
		}
	}
}

func TestFindNewAndExisting(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Old.mp4"), []byte("x"), 0o644)
	before := Take(dir)
	if FindNew(dir, before, "Boxing") != "" {
		t.Fatal("nothing new yet")
	}
	os.WriteFile(filepath.Join(dir, "Boxing.mp4"), []byte("data"), 0o644)
	if got := FindNew(dir, before, "Boxing"); filepath.Base(got) != "Boxing.mp4" {
		t.Fatalf("FindNew=%q", got)
	}
	if got := ExistingExport(dir, "boxing"); filepath.Base(got) != "Boxing.mp4" {
		t.Fatalf("ExistingExport=%q", got)
	}
	// A single unrelated new video is still picked up (CapCut renamed it).
	dir2 := t.TempDir()
	b2 := Take(dir2)
	os.WriteFile(filepath.Join(dir2, "renamed.mp4"), []byte("d"), 0o644)
	if FindNew(dir2, b2, "Boxing") == "" {
		t.Fatal("single new file should match")
	}
}

func TestUnlocked(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.mp4")
	os.WriteFile(p, []byte("x"), 0o644)
	if !Unlocked(p) {
		t.Fatal("should be unlocked")
	}
	f, _ := os.OpenFile(p, os.O_WRONLY, 0)
	if Unlocked(p) {
		t.Fatal("should be locked while open for writing")
	}
	f.Close()
	_ = syscall.Getpid
}

// Simulates CapCut: file appears, grows while held open, then is closed.
func TestWaitWritingFile(t *testing.T) {
	dir := t.TempDir()
	before := Take(dir)
	go func() {
		time.Sleep(300 * time.Millisecond)
		f, _ := os.Create(filepath.Join(dir, "Clip.mp4"))
		for i := 0; i < 5; i++ {
			f.Write(make([]byte, 1000))
			time.Sleep(200 * time.Millisecond)
		}
		f.Close()
	}()
	start := time.Now()
	res := Wait(Options{Dir: dir, Before: before, DraftName: "Clip", StableFor: 500 * time.Millisecond,
		Poll: 100 * time.Millisecond, StartLimit: 5 * time.Second, TotalLimit: 10 * time.Second})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if time.Since(start) < 1300*time.Millisecond {
		t.Fatalf("finished too early (%v) — file was still being written", time.Since(start))
	}
	if st, _ := os.Stat(res.Path); st.Size() != 5000 {
		t.Fatalf("size %d", st.Size())
	}
}

func TestWaitNoFile(t *testing.T) {
	dir := t.TempDir()
	res := Wait(Options{Dir: dir, Before: Take(dir), DraftName: "X", Poll: 50 * time.Millisecond, StartLimit: 300 * time.Millisecond})
	if res.Err != ErrNoFile {
		t.Fatalf("want ErrNoFile, got %v", res.Err)
	}
}

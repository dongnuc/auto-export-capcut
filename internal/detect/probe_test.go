package detect

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProbeDuration(t *testing.T) {
	ff, vid := os.Getenv("PROBE_FFMPEG"), os.Getenv("PROBE_VIDEO")
	if ff == "" || vid == "" {
		t.Skip("set PROBE_FFMPEG and PROBE_VIDEO")
	}
	d, ok := ProbeDuration(ff, vid)
	if !ok || d.Seconds() < 2.9 || d.Seconds() > 3.1 {
		t.Fatalf("duration %v ok=%v", d, ok)
	}
}

// A finished file that CapCut keeps open (export "done" view) still counts
// as finished once its duration matches.
func TestWaitHeldOpenFinished(t *testing.T) {
	ff, vid := os.Getenv("PROBE_FFMPEG"), os.Getenv("PROBE_VIDEO")
	if ff == "" || vid == "" {
		t.Skip("set PROBE_FFMPEG and PROBE_VIDEO")
	}
	dir := t.TempDir()
	before := Take(dir)
	data, _ := os.ReadFile(vid)
	p := filepath.Join(dir, "Clip.mp4")
	os.WriteFile(p, data, 0o644)
	f, _ := os.OpenFile(p, os.O_RDWR, 0) // held open like CapCut does
	defer f.Close()
	res := Wait(Options{Dir: dir, Before: before, DraftName: "Clip", Expected: 3 * time.Second,
		Tolerance: time.Second, StableFor: 300 * time.Millisecond, FFmpeg: ff,
		Poll: 100 * time.Millisecond, StartLimit: 5 * time.Second, TotalLimit: 20 * time.Second})
	if res.Err != nil || !res.HeldOpen {
		t.Fatalf("err=%v heldOpen=%v", res.Err, res.HeldOpen)
	}
}

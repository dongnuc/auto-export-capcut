// Package detect decides when CapCut has finished writing an exported video.
//
// A file only counts as finished when all of these hold:
//  1. it is a new (or rewritten) video in the output folder whose name matches the draft
//  2. it can be opened exclusively (CapCut keeps it open while encoding)
//  3. its size has not changed for StableFor
//  4. (if ffmpeg is available) its duration is within Tolerance of the draft duration
package detect

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var videoExt = map[string]bool{".mp4": true, ".mov": true, ".mkv": true, ".avi": true, ".webm": true}

type fileState struct {
	size int64
	mod  time.Time
}

// Snapshot records the videos already present in dir before an export.
type Snapshot map[string]fileState

func Take(dir string) Snapshot {
	s := Snapshot{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !videoExt[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		if info, err := e.Info(); err == nil {
			s[strings.ToLower(e.Name())] = fileState{info.Size(), info.ModTime()}
		}
	}
	return s
}

// Normalize makes names comparable: lower case, no characters Windows forbids,
// collapsed spaces.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

var copySuffix = regexp.MustCompile(`\s*[\(\[]\d+[\)\]]$|[_-]\d+$`)

// NameMatches reports whether a file stem looks like the export of draftName.
func NameMatches(stem, draftName string) bool {
	a, b := Normalize(stem), Normalize(draftName)
	if b == "" {
		return false
	}
	return a == b || Normalize(copySuffix.ReplaceAllString(stem, "")) == b
}

// ExpectedPath is where CapCut writes the export by default.
func ExpectedPath(outDir, draftName string) string {
	return filepath.Join(outDir, draftName+".mp4")
}

// ExistingExport returns a video in outDir that already matches draftName.
func ExistingExport(outDir, draftName string) string {
	entries, _ := os.ReadDir(outDir)
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || !videoExt[ext] {
			continue
		}
		if NameMatches(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), draftName) {
			return filepath.Join(outDir, e.Name())
		}
	}
	return ""
}

// FindNew returns the video that appeared or changed in dir since before.
// A name match is preferred; otherwise, if exactly one video is new, it is used.
func FindNew(dir string, before Snapshot, draftName string) string {
	entries, _ := os.ReadDir(dir)
	var changed []string
	for _, e := range entries {
		if e.IsDir() || !videoExt[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		old, existed := before[strings.ToLower(e.Name())]
		if existed && old.size == info.Size() && old.mod.Equal(info.ModTime()) {
			continue
		}
		if NameMatches(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), draftName) {
			return filepath.Join(dir, e.Name())
		}
		changed = append(changed, filepath.Join(dir, e.Name()))
	}
	if len(changed) == 1 {
		return changed[0]
	}
	return ""
}

// Unlocked reports whether no other process has the file open.
func Unlocked(path string) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, 0 /* no sharing */, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return false
	}
	syscall.CloseHandle(h)
	return true
}

var durRe = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)

// ProbeDuration reads a media duration by running `ffmpeg -i`. ok is false if
// ffmpeg is missing or the duration could not be read.
func ProbeDuration(ffmpeg, path string) (time.Duration, bool) {
	if ffmpeg == "" {
		return 0, false
	}
	cmd := exec.Command(ffmpeg, "-hide_banner", "-i", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, _ := cmd.CombinedOutput() // exits non-zero without an output file; that's fine
	m := durRe.FindSubmatch(out)
	if m == nil {
		return 0, false
	}
	h, _ := strconv.Atoi(string(m[1]))
	mi, _ := strconv.Atoi(string(m[2]))
	s, _ := strconv.ParseFloat(string(m[3]), 64)
	return time.Duration((float64(h*3600+mi*60) + s) * float64(time.Second)), true
}

// Status is a progress report from Wait.
type Status struct {
	Path    string
	Size    int64
	Locked  bool
	Stable  time.Duration
	Message string
}

type Options struct {
	Dir        string
	Before     Snapshot
	DraftName  string
	Expected   time.Duration // draft duration; 0 = skip duration check
	Tolerance  time.Duration
	StableFor  time.Duration
	FFmpeg     string
	Poll       time.Duration
	StartLimit time.Duration // give up if no file appears in this time
	TotalLimit time.Duration // give up if not finished in this time
	OnStatus   func(Status)
	Cancelled  func() bool
	// Abort is polled every round; a non-nil error ends the wait with it
	// (used for "CapCut stopped exporting" and "progress stalled").
	Abort func() error
}

type Result struct {
	Path     string
	Duration time.Duration
	HeldOpen bool // CapCut still had the file open when it was judged finished
	Err      error
}

// lockedGrace is how long a file must stay the same size before it counts as
// finished when its duration cannot confirm it.
func lockedGrace(stable time.Duration) time.Duration {
	if g := 5 * stable; g > 30*time.Second {
		return g
	}
	return 30 * time.Second
}

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrNoFile    Error = "không thấy file xuất xuất hiện (có thể bị popup Pro/hộp thoại chặn, hoặc sai thư mục output)"
	ErrTimeout   Error = "quá thời gian chờ xuất xong"
	ErrCancelled Error = "đã dừng"
	ErrDuration  Error = "thời lượng file không khớp với project"
)

// Wait blocks until the export is finished, fails, or is cancelled.
func Wait(o Options) Result {
	if o.Poll == 0 {
		o.Poll = time.Second
	}
	start := time.Now()
	var path string
	var lastSize int64 = -1
	var stableSince time.Time
	durChecks := 0
	var lastProbe time.Time
	for {
		if o.Cancelled != nil && o.Cancelled() {
			return Result{Path: path, Err: ErrCancelled}
		}
		if o.Abort != nil {
			if err := o.Abort(); err != nil {
				return Result{Path: path, Err: err}
			}
		}
		elapsed := time.Since(start)
		if path == "" {
			path = FindNew(o.Dir, o.Before, o.DraftName)
			if path == "" {
				if o.StartLimit > 0 && elapsed > o.StartLimit {
					return Result{Err: ErrNoFile}
				}
				report(o, Status{Message: "đang chờ file xuất hiện…"})
				time.Sleep(o.Poll)
				continue
			}
		}
		if o.TotalLimit > 0 && elapsed > o.TotalLimit {
			return Result{Path: path, Err: ErrTimeout}
		}
		info, err := os.Stat(path)
		if err != nil { // CapCut may rename a temp file at the end
			path, lastSize = "", -1
			time.Sleep(o.Poll)
			continue
		}
		if info.Size() != lastSize {
			lastSize, stableSince = info.Size(), time.Now()
		}
		// CapCut keeps the file open after a finished export (its "done" view
		// previews the video), so a lock alone does not mean "still writing".
		locked := !Unlocked(path)
		stable := time.Since(stableSince)
		report(o, Status{Path: path, Size: info.Size(), Locked: locked, Stable: stable, Message: "đang ghi file…"})
		if info.Size() > 0 && stable >= o.StableFor && time.Since(lastProbe) >= 3*time.Second {
			lastProbe = time.Now()
			d, ok := ProbeDuration(o.FFmpeg, path)
			diff := d - o.Expected
			if diff < 0 {
				diff = -diff
			}
			switch {
			case ok && o.Expected > 0 && diff <= o.Tolerance:
				// A readable file of the right length is finished, held open or not.
				return Result{Path: path, Duration: d, HeldOpen: locked}
			case ok && o.Expected <= 0 && !locked:
				return Result{Path: path, Duration: d}
			case !ok && o.FFmpeg == "":
				// No way to read the duration: rely on the size and the lock.
				if !locked || stable >= lockedGrace(o.StableFor) {
					return Result{Path: path, HeldOpen: locked}
				}
			case ok && o.Expected > 0:
				// Wrong length: it may still be finalising; give it a few rounds.
				durChecks++
				if durChecks >= 5 && stable >= lockedGrace(o.StableFor) {
					return Result{Path: path, Duration: d, HeldOpen: locked, Err: ErrDuration}
				}
			}
			// !ok with ffmpeg present = moov atom not written yet → keep waiting.
		}
		time.Sleep(o.Poll)
	}
}

func report(o Options, s Status) {
	if o.OnStatus != nil {
		o.OnStatus(s)
	}
}

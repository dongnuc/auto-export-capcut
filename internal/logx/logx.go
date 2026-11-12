// Package logx writes the run log: every line goes to a file under
// %APPDATA%\capcut-auto-exporter\logs (DEBUG included), and INFO and above
// are also forwarded to the UI. Screenshots taken for diagnosis are stored in
// a folder next to the log file.
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sync"
	"time"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

func (l Level) tag() string {
	switch l {
	case Debug:
		return "DEBUG"
	case Info:
		return "INFO "
	case Warn:
		return "WARN "
	}
	return "ERROR"
}

type Logger struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	shotDir string
	shotN   int
	ui      func(string)
	shooter func(path string) error
}

// New opens a log file in dir named after the current time.
func New(dir string, ui func(string), shooter func(string) error) *Logger {
	l := &Logger{ui: ui, shooter: shooter}
	stamp := time.Now().Format("20060102-150405")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		l.path = filepath.Join(dir, "run-"+stamp+".log")
		l.shotDir = filepath.Join(dir, "run-"+stamp+"-screens")
		if f, err := os.Create(l.path); err == nil {
			l.file = f
		}
	}
	if l.file == nil && ui != nil {
		ui("[!] Không tạo được file log trong " + dir)
	}
	return l
}

func (l *Logger) Path() string    { return l.path }
func (l *Logger) ShotDir() string { return l.shotDir }

func (l *Logger) write(lv Level, msg string) {
	now := time.Now()
	l.mu.Lock()
	if l.file != nil {
		fmt.Fprintf(l.file, "%s %s %s\n", now.Format("2006-01-02 15:04:05.000"), lv.tag(), msg)
	}
	l.mu.Unlock()
	if lv >= Info && l.ui != nil {
		prefix := ""
		switch lv {
		case Warn:
			prefix = "[!] "
		case Error:
			prefix = "[LỖI] "
		}
		l.ui(prefix + msg)
	}
}

func (l *Logger) Debugf(f string, a ...any) { l.write(Debug, fmt.Sprintf(f, a...)) }
func (l *Logger) Infof(f string, a ...any)  { l.write(Info, fmt.Sprintf(f, a...)) }
func (l *Logger) Warnf(f string, a ...any)  { l.write(Warn, fmt.Sprintf(f, a...)) }
func (l *Logger) Errorf(f string, a ...any) { l.write(Error, fmt.Sprintf(f, a...)) }

var unsafeName = regexp.MustCompile(`[^\p{L}\p{N}_-]+`)

// Shot saves a numbered screenshot and logs where it went.
func (l *Logger) Shot(label string) string {
	if l.shooter == nil || l.shotDir == "" {
		return ""
	}
	if err := os.MkdirAll(l.shotDir, 0o755); err != nil {
		l.Debugf("không tạo được thư mục ảnh: %v", err)
		return ""
	}
	l.mu.Lock()
	l.shotN++
	n := l.shotN
	l.mu.Unlock()
	p := filepath.Join(l.shotDir, fmt.Sprintf("%03d-%s.png", n, unsafeName.ReplaceAllString(label, "_")))
	if err := l.shooter(p); err != nil {
		l.Debugf("chụp màn hình %q lỗi: %v", label, err)
		return ""
	}
	l.Debugf("ảnh chụp: %s", p)
	return p
}

// Recover logs a panic with its stack instead of crashing the app.
func (l *Logger) Recover(where string) {
	if r := recover(); r != nil {
		l.Errorf("lỗi nghiêm trọng trong %s: %v", where, r)
		l.Debugf("stack:\n%s", debug.Stack())
	}
}

func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

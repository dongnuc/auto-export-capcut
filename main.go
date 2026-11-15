// CapCut Auto Exporter — exports selected CapCut projects one after another by
// driving the CapCut app itself (export shortcut, wait for the file, Ctrl+W).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"gioui.org/app"
	"gioui.org/unit"

	"capcut-auto-exporter/internal/config"
	"capcut-auto-exporter/internal/drafts"
	"capcut-auto-exporter/internal/runner"
	"capcut-auto-exporter/internal/winapi"
)

func main() {
	list := flag.Bool("list", false, "in danh sách project rồi thoát")
	rename := flag.String("rename", "", "đổi tên project: \"tên cũ=>tên mới\" (CapCut phải đang đóng)")
	export := flag.String("export", "", "xuất các project (tên, cách nhau bởi dấu phẩy) không cần giao diện, dùng cài đặt đã lưu")
	flag.Parse()
	winapi.EnableDPIAwareness()

	if *list {
		cfg := config.Load()
		ds, err := drafts.List(cfg.DraftsDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		exe, ff := runner.FindCapCut(cfg.CapCutExe)
		fmt.Printf("CapCut: %s\nffmpeg: %s\n", exe, ff)
		for _, d := range ds {
			fmt.Printf("%-30s %8s  %s  %s\n", d.Name, d.Duration().Round(time.Second), d.Modified.Format("2006-01-02 15:04"), d.Folder)
		}
		return
	}
	if *rename != "" {
		os.Exit(renameCLI(*rename))
	}
	if *export != "" {
		os.Exit(runCLI(*export))
	}

	// A fatal Go error (not a recoverable panic) used to end the process with
	// its message sent to a stderr nobody sees. Keep it in a file next to the logs.
	if err := os.MkdirAll(filepath.Join(config.Dir(), "logs"), 0o755); err == nil {
		if f, err := os.OpenFile(filepath.Join(config.Dir(), "logs", "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			fmt.Fprintf(f, "\n=== %s: tool started ===\n", time.Now().Format("2006-01-02 15:04:05"))
			_ = debug.SetCrashOutput(f, debug.CrashOptions{})
		}
	}

	go func() {
		w := new(app.Window)
		w.Option(app.Title("CapCut Auto Exporter"), app.Size(unit.Dp(1360), unit.Dp(900)), app.MinSize(unit.Dp(1120), unit.Dp(760)))
		u := newUI(w)
		if err := u.loop(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"capcut-auto-exporter/internal/config"
	"capcut-auto-exporter/internal/drafts"
	"capcut-auto-exporter/internal/logx"
	"capcut-auto-exporter/internal/runner"
	"capcut-auto-exporter/internal/winapi"
)

// runCLI exports the named drafts with the saved settings, logging to the
// console and to the usual log file. Returns the process exit code.
func runCLI(namesArg string) int {
	cfg := config.Load()
	all, err := drafts.List(cfg.DraftsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var sel []drafts.Draft
	for _, n := range strings.Split(namesArg, ",") {
		n = strings.TrimSpace(n)
		found := false
		for _, d := range all {
			if strings.EqualFold(d.Name, n) {
				sel, found = append(sel, d), true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "không tìm thấy project %q\n", n)
			return 1
		}
	}
	lg := logx.New(filepath.Join(config.Dir(), "logs"),
		func(s string) { fmt.Println(time.Now().Format("15:04:05 ") + s) }, winapi.Screenshot)
	defer lg.Close()
	failed := 0
	rn := runner.New(cfg, sel, lg, runner.Callbacks{
		Update: func(_ int, it runner.Item) {
			if it.State == runner.StateFailed {
				failed++
			}
		},
		AskOpen: func(d drafts.Draft) bool {
			fmt.Printf("Mở project %q trong CapCut rồi nhấn Enter… ", d.Name)
			var s string
			fmt.Scanln(&s)
			return true
		},
	})
	rn.Run()
	if failed > 0 {
		return 2
	}
	return 0
}

// renameCLI renames one project: "old=>new".
func renameCLI(arg string) int {
	parts := strings.SplitN(arg, "=>", 2)
	if len(parts) != 2 {
		fmt.Fprintln(os.Stderr, `cú pháp: -rename "tên cũ=>tên mới"`)
		return 1
	}
	cfg := config.Load()
	if (&runner.App{}).Running() {
		fmt.Fprintln(os.Stderr, "hãy đóng CapCut trước khi đổi tên")
		return 1
	}
	all, err := drafts.List(cfg.DraftsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, d := range all {
		if strings.EqualFold(d.Name, strings.TrimSpace(parts[0])) {
			name, err := drafts.ValidateName(cfg.DraftsDir, d, parts[1])
			if err == nil {
				err = drafts.Rename(cfg.DraftsDir, d, name)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			fmt.Printf("đã đổi %q → %q\n", d.Name, name)
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "không tìm thấy project %q\n", parts[0])
	return 1
}

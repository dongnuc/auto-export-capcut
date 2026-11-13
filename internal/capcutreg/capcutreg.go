//go:build windows

// Package capcutreg reads CapCut's own export preferences. CapCut keeps the
// folder it last exported to (the "Xuất sang" folder of the export dialog) in
// the current user's registry, not in the project folders:
//
//	HKEY_CURRENT_USER\Software\Bytedance\CapCut\Modules\Export
//	    ExportPath = E:/folder/where/videos/go     (REG_SZ, forward slashes)
//	    Resolution = 1080P
//	    codec      = HEVC
//
// It is one value for the whole app: every project's export dialog starts from
// it, so it also tells where the next export will be written.
package capcutreg

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const bs = string(rune(92)) // backslash

// ExportKey is the registry key (under HKEY_CURRENT_USER) holding the settings.
var ExportKey = strings.Join([]string{"Software", "Bytedance", "CapCut", "Modules", "Export"}, bs)

// Export is what CapCut remembers about the last export.
type Export struct {
	Path       string // normalised folder, e.g. E:\folder\where\videos\go
	Resolution string
	Codec      string
}

// ReadExport reads CapCut's export preferences.
func ReadExport() (Export, error) { return ReadExportAt(ExportKey) }

// ReadExportAt reads the same values from another key (used by tests).
func ReadExportAt(subkey string) (Export, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE)
	if err != nil {
		return Export{}, fmt.Errorf("CapCut chưa lưu thư mục xuất (chưa xuất video lần nào?): %w", err)
	}
	defer k.Close()
	p, _, err := k.GetStringValue("ExportPath")
	if err != nil || strings.TrimSpace(p) == "" {
		return Export{}, fmt.Errorf("không đọc được ExportPath của CapCut trong registry")
	}
	e := Export{Path: Normalize(p)}
	e.Resolution, _, _ = k.GetStringValue("Resolution")
	e.Codec, _, _ = k.GetStringValue("codec")
	return e, nil
}

// Normalize turns CapCut's forward-slash path into a clean Windows path.
func Normalize(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(p))
}

// SamePath compares two folders the way Windows does (case-insensitive).
func SamePath(a, b string) bool {
	return strings.EqualFold(Normalize(a), Normalize(b))
}

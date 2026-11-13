// Package config persists the tool's settings in %APPDATA%\capcut-auto-exporter.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Open modes for getting a project into the CapCut editor.
const (
	OpenAuto   = "auto"   // move draft to top of the home list, restart CapCut, click first card
	OpenManual = "manual" // the user opens the project; the tool waits for "Continue"
)

type Config struct {
	DraftsDir string `json:"drafts_dir"` // folder holding root_meta_info.json
	OutputDir string `json:"output_dir"` // folder CapCut exports into
	// OutputAuto takes the output folder from CapCut's own saved export folder
	// (registry), so it never has to be configured here. OutputDir is then only
	// a cache of the last value read.
	OutputAuto bool   `json:"output_auto"`
	CapCutExe  string `json:"capcut_exe"` // empty = auto-detect

	ExportKey  string `json:"export_key"`  // opens the export dialog
	ConfirmKey string `json:"confirm_key"` // starts the export inside the dialog
	CloseKey   string `json:"close_key"`   // closes the project back to home
	DismissKey string `json:"dismiss_key"` // closes popups

	OpenMode string `json:"open_mode"`
	// First project card position on the home screen, as a fraction of the
	// window client area (set with the calibrate button).
	CardX float64 `json:"card_x"`
	CardY float64 `json:"card_y"`

	LaunchWaitSec   int     `json:"launch_wait_sec"`   // wait for home screen after launch
	OpenWaitSec     int     `json:"open_wait_sec"`     // wait for editor after clicking a card
	DialogWaitSec   int     `json:"dialog_wait_sec"`   // wait between export key and confirm key
	StartTimeoutSec int     `json:"start_timeout_sec"` // no output file after this = fail (e.g. Pro popup)
	TimeoutFactor   float64 `json:"timeout_factor"`    // export timeout = duration*factor + TimeoutExtraSec
	TimeoutExtraSec int     `json:"timeout_extra_sec"` //
	StableSec       int     `json:"stable_sec"`        // file size must stay unchanged this long
	DurationTolSec  float64 `json:"duration_tol_sec"`  // allowed |file duration - draft duration|
	RestartPerDraft bool    `json:"restart_per_draft"` // quit CapCut after each project (needed for auto mode)
	ExistingPolicy  string  `json:"existing_policy"`   // "skip" | "backup"

	HomeWaitSec      int     `json:"home_wait_sec"`      // let the home grid load before clicking
	EditorLoadSec    int     `json:"editor_load_sec"`    // let the editor load media before exporting
	OpenRetries      int     `json:"open_retries"`       // clicks on the card before giving up
	MoveToPrimary    bool    `json:"move_to_primary"`    // move CapCut to the primary monitor before clicking
	ExportBtnX       float64 `json:"export_btn_x"`       // export button inside the export dialog,
	ExportBtnY       float64 `json:"export_btn_y"`       // as a fraction of the dialog window
	SkipMissingMedia bool    `json:"skip_missing_media"` // skip projects whose media is missing (default: close the popup and export)
	ProjectLoadSec   int     `json:"project_load_sec"`   // wait after the editor opens before checking popups and exporting
	DebugScreenshots bool    `json:"debug_screenshots"`  // screenshot every step (not only errors)
	StallMin         int     `json:"stall_min"`          // fail if the export progress does not move for this long
	MaxExportMin     int     `json:"max_export_min"`     // hard limit per export, 0 = none
}

func Default() Config {
	c := Config{
		ExportKey:  "Ctrl+E",
		ConfirmKey: "Enter",
		CloseKey:   "Ctrl+W",
		DismissKey: "Escape",
		OpenMode:   OpenAuto,
		OutputAuto: true,
		// Centre of the first card under "Dự án" on CapCut 6.9's home screen,
		// measured on a maximized 1920x1080 window. The card grid has a fixed
		// pixel size, so other resolutions should use the calibrate button.
		CardX:           0.153,
		CardY:           0.564,
		LaunchWaitSec:   25,
		OpenWaitSec:     15,
		DialogWaitSec:   3,
		StartTimeoutSec: 90,
		TimeoutFactor:   3,
		TimeoutExtraSec: 180,
		StableSec:       6,
		DurationTolSec:  1.5,
		RestartPerDraft: true,
		ExistingPolicy:  "skip",

		HomeWaitSec:      8,
		EditorLoadSec:    8,
		OpenRetries:      3,
		MoveToPrimary:    true,
		ExportBtnX:       0.815, // "Xuất" button, measured on CapCut 6.9's export dialog
		ExportBtnY:       0.958,
		ProjectLoadSec:   30,
		DebugScreenshots: true,
		StallMin:         10,
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		c.DraftsDir = filepath.Join(la, "CapCut", "User Data", "Projects", "com.lveditor.draft")
	}
	return c
}

func Dir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "capcut-auto-exporter")
}

func path() string { return filepath.Join(Dir(), "config.json") }

// Load returns saved settings, filling missing fields with defaults.
func Load() Config {
	c := Default()
	data, err := os.ReadFile(path())
	if err == nil {
		_ = json.Unmarshal(data, &c)
	}
	d := Default()
	if c.CardX <= 0 || c.CardY <= 0 {
		c.CardX, c.CardY = d.CardX, d.CardY
	}
	// Settings added in later versions: a config saved by an older build has
	// them as zero values, so fill them from the defaults once.
	if c.HomeWaitSec <= 0 && c.EditorLoadSec <= 0 && c.OpenRetries <= 0 {
		c.HomeWaitSec, c.EditorLoadSec, c.OpenRetries = d.HomeWaitSec, d.EditorLoadSec, d.OpenRetries
		c.MoveToPrimary, c.DebugScreenshots = d.MoveToPrimary, d.DebugScreenshots
	}
	if c.ExportBtnX <= 0 || c.ExportBtnY <= 0 {
		c.ExportBtnX, c.ExportBtnY = d.ExportBtnX, d.ExportBtnY
	}
	if c.StallMin <= 0 {
		c.StallMin = d.StallMin
	}
	if c.ProjectLoadSec <= 0 {
		c.ProjectLoadSec = d.ProjectLoadSec
	}
	if c.ExportKey == "" {
		c.ExportKey = d.ExportKey
	}
	if c.ConfirmKey == "" {
		c.ConfirmKey = d.ConfirmKey
	}
	if c.CloseKey == "" {
		c.CloseKey = d.CloseKey
	}
	if c.DismissKey == "" {
		c.DismissKey = d.DismissKey
	}
	if c.OpenMode == "" {
		c.OpenMode = d.OpenMode
	}
	if c.ExistingPolicy == "" {
		c.ExistingPolicy = d.ExistingPolicy
	}
	return c
}

func Save(c Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), data, 0o644)
}

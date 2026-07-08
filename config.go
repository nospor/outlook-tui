package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	ClientID        string   `json:"client_id"`
	TenantID        string   `json:"tenant_id"`        // defaults to "common"
	RefreshTimeMin  int      `json:"refresh_time_min"` // defaults to 5
	Layout          int      `json:"layout"`           // 1 = side-by-side (default), 2 = folders above messages
	UseSQLite       int      `json:"use_sqlite"`       // 0 = disabled (default), 1 = cache messages in ~/.cache/outlook-tui/db.db
	ExcludedFolders []string `json:"excluded_folders"`
	ScrollLines     int      `json:"scroll_lines"` // defaults to 1
	ImageViewer     string   `json:"image_viewer"`
	AttachmentDir   string   `json:"attachment_dir"`
	TerminalBell    int      `json:"terminal_bell"`   // 0 = disabled, 1 = enabled (default)
	Theme           string   `json:"theme"`           // "catppuccin" (default) or "teams"
	BrowserCommand  string   `json:"browser_command"` // defaults to "xdg-open"
}

func GetConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "outlook-tui")
	err = os.MkdirAll(dir, 0700)
	return dir, err
}

func LoadConfig() (Config, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return Config{}, err
	}

	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		defaultAttachmentDir := ""
		if home, errHome := os.UserHomeDir(); errHome == nil {
			defaultAttachmentDir = filepath.Join(home, "Downloads")
		} else {
			defaultAttachmentDir = "."
		}
		cfg := Config{
			ClientID:       "",
			TenantID:       "common",
			RefreshTimeMin: 5,
			Layout:         1,
			UseSQLite:      0,
			ScrollLines:    1,
			AttachmentDir:  defaultAttachmentDir,
			TerminalBell:   1,
			Theme:          "catppuccin",
			BrowserCommand: "xdg-open",
		}
		_ = SaveConfig(cfg)
		return cfg, nil
	}

	cfg := Config{
		TenantID:       "common",
		RefreshTimeMin: 5,
		Layout:         1,
		UseSQLite:      0,
		ScrollLines:    1,
		TerminalBell:   1,
		Theme:          "catppuccin",
		BrowserCommand: "xdg-open",
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	if cfg.TenantID == "" {
		cfg.TenantID = "common"
	}

	if cfg.BrowserCommand == "" {
		cfg.BrowserCommand = "xdg-open"
	}

	if cfg.RefreshTimeMin <= 0 {
		cfg.RefreshTimeMin = 5
		_ = SaveConfig(cfg)
	}

	if cfg.Layout != 1 && cfg.Layout != 2 {
		cfg.Layout = 1
		_ = SaveConfig(cfg)
	}

	if cfg.ScrollLines <= 0 {
		cfg.ScrollLines = 1
		_ = SaveConfig(cfg)
	}

	if cfg.AttachmentDir == "" {
		if home, errHome := os.UserHomeDir(); errHome == nil {
			cfg.AttachmentDir = filepath.Join(home, "Downloads")
		} else {
			cfg.AttachmentDir = "."
		}
		_ = SaveConfig(cfg)
	}

	if cfg.TerminalBell != 0 && cfg.TerminalBell != 1 {
		cfg.TerminalBell = 1
		_ = SaveConfig(cfg)
	}

	if cfg.Theme == "" {
		cfg.Theme = "catppuccin"
	} else {
		cfg.Theme = strings.ToLower(cfg.Theme)
		if cfg.Theme != "catppuccin" && cfg.Theme != "teams" {
			cfg.Theme = "catppuccin"
		}
	}

	if !strings.Contains(string(data), "use_sqlite") || !strings.Contains(string(data), "excluded_folders") || !strings.Contains(string(data), "scroll_lines") || !strings.Contains(string(data), "image_viewer") || !strings.Contains(string(data), "attachment_dir") || !strings.Contains(string(data), "terminal_bell") || !strings.Contains(string(data), "theme") || !strings.Contains(string(data), "browser_command") {
		_ = SaveConfig(cfg)
	}

	return cfg, nil
}

func SaveConfig(cfg Config) error {
	if cfg.RefreshTimeMin <= 0 {
		cfg.RefreshTimeMin = 5
	}
	if cfg.ScrollLines <= 0 {
		cfg.ScrollLines = 1
	}
	if cfg.BrowserCommand == "" {
		cfg.BrowserCommand = "xdg-open"
	}
	dir, err := GetConfigDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// FilepickerSettings holds the filepicker sorting and directory persistence.
type FilepickerSettings struct {
	SortBy           string `json:"sort_by"`
	SortOrder        string `json:"sort_order"`
	CurrentDirectory string `json:"current_directory,omitempty"`
}

// LoadFilepickerSettings reads the filepicker sorting settings from filepicker_settings.json.
// Returns default values if the file does not exist or cannot be parsed.
func LoadFilepickerSettings() (string, string, string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	if abs, err := filepath.Abs(homeDir); err == nil {
		homeDir = abs
	}

	dir, err := GetConfigDir()
	if err != nil {
		return "Name", "asc", homeDir
	}
	data, err := os.ReadFile(filepath.Join(dir, "filepicker_settings.json"))
	if err != nil {
		return "Name", "asc", homeDir
	}
	var settings FilepickerSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return "Name", "asc", homeDir
	}
	if settings.SortBy == "" {
		settings.SortBy = "Name"
	}
	if settings.SortOrder == "" {
		settings.SortOrder = "asc"
	}
	if settings.CurrentDirectory == "" {
		settings.CurrentDirectory = homeDir
	} else {
		if abs, err := filepath.Abs(settings.CurrentDirectory); err == nil {
			settings.CurrentDirectory = abs
		}
	}
	return settings.SortBy, settings.SortOrder, settings.CurrentDirectory
}

// SaveFilepickerSettings writes the current filepicker settings to filepicker_settings.json.
func SaveFilepickerSettings(sortBy string, sortOrder string, currentDirectory string) error {
	dir, err := GetConfigDir()
	if err != nil {
		return err
	}
	settings := FilepickerSettings{
		SortBy:           sortBy,
		SortOrder:        sortOrder,
		CurrentDirectory: currentDirectory,
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal filepicker settings: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "filepicker_settings.json"), data, 0600)
}

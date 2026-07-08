package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	// Set HOME to a temporary directory to avoid touching the user's actual config
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	// Case 1: Config file does not exist
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if cfg.ClientID != "" {
		t.Errorf("expected empty ClientID, got %q", cfg.ClientID)
	}
	if cfg.TenantID != "common" {
		t.Errorf("expected TenantID 'common', got %q", cfg.TenantID)
	}
	if cfg.RefreshTimeMin != 5 {
		t.Errorf("expected default RefreshTimeMin to be 5, got %d", cfg.RefreshTimeMin)
	}
	if cfg.UseSQLite != 0 {
		t.Errorf("expected default UseSQLite to be 0, got %d", cfg.UseSQLite)
	}
	if cfg.ScrollLines != 1 {
		t.Errorf("expected default ScrollLines to be 1, got %d", cfg.ScrollLines)
	}
	if cfg.TerminalBell != 1 {
		t.Errorf("expected default TerminalBell to be 1, got %d", cfg.TerminalBell)
	}
	if cfg.ImageViewer != "" {
		t.Errorf("expected default ImageViewer to be empty, got %q", cfg.ImageViewer)
	}
	if cfg.Theme != "catppuccin" {
		t.Errorf("expected default Theme to be 'catppuccin', got %q", cfg.Theme)
	}
	if cfg.BrowserCommand != "xdg-open" {
		t.Errorf("expected default BrowserCommand to be 'xdg-open', got %q", cfg.BrowserCommand)
	}
	expectedDefaultDir := filepath.Join(tempDir, "Downloads")
	if cfg.AttachmentDir != expectedDefaultDir {
		t.Errorf("expected default AttachmentDir to be %q, got %q", expectedDefaultDir, cfg.AttachmentDir)
	}

	// Case 2: Config file exists but is missing refresh_time_min
	configDir := filepath.Join(tempDir, ".config", "outlook-tui")
	err = os.MkdirAll(configDir, 0700)
	if err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.json")
	partialJSON := `{"client_id": "test-client-id", "tenant_id": "test-tenant-id"}`
	err = os.WriteFile(configPath, []byte(partialJSON), 0600)
	if err != nil {
		t.Fatalf("failed to write partial config file: %v", err)
	}

	// Load should populate default RefreshTimeMin and write it back to disk
	cfg, err = LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if cfg.ClientID != "test-client-id" {
		t.Errorf("expected ClientID 'test-client-id', got %q", cfg.ClientID)
	}
	if cfg.TenantID != "test-tenant-id" {
		t.Errorf("expected TenantID 'test-tenant-id', got %q", cfg.TenantID)
	}
	if cfg.RefreshTimeMin != 5 {
		t.Errorf("expected populated RefreshTimeMin to be 5, got %d", cfg.RefreshTimeMin)
	}
	if cfg.ScrollLines != 1 {
		t.Errorf("expected populated ScrollLines to be 1, got %d", cfg.ScrollLines)
	}
	if cfg.TerminalBell != 1 {
		t.Errorf("expected populated TerminalBell to be 1, got %d", cfg.TerminalBell)
	}
	if cfg.Theme != "catppuccin" {
		t.Errorf("expected populated Theme to be 'catppuccin', got %q", cfg.Theme)
	}
	if cfg.BrowserCommand != "xdg-open" {
		t.Errorf("expected populated BrowserCommand to be 'xdg-open', got %q", cfg.BrowserCommand)
	}
	if cfg.AttachmentDir != expectedDefaultDir {
		t.Errorf("expected populated AttachmentDir to be %q, got %q", expectedDefaultDir, cfg.AttachmentDir)
	}

	// Verify it was written back to disk
	savedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read back config file: %v", err)
	}

	var savedCfg Config
	if err := json.Unmarshal(savedData, &savedCfg); err != nil {
		t.Fatalf("failed to unmarshal saved config: %v", err)
	}

	if savedCfg.RefreshTimeMin != 5 {
		t.Errorf("expected saved config to have RefreshTimeMin 5, got %d", savedCfg.RefreshTimeMin)
	}

	if savedCfg.UseSQLite != 0 {
		t.Errorf("expected saved config to have UseSQLite 0, got %d", savedCfg.UseSQLite)
	}

	if savedCfg.ScrollLines != 1 {
		t.Errorf("expected saved config to have ScrollLines 1, got %d", savedCfg.ScrollLines)
	}

	if savedCfg.TerminalBell != 1 {
		t.Errorf("expected saved config to have TerminalBell 1, got %d", savedCfg.TerminalBell)
	}

	if savedCfg.ImageViewer != "" {
		t.Errorf("expected saved config to have ImageViewer '', got %q", savedCfg.ImageViewer)
	}

	if savedCfg.AttachmentDir != expectedDefaultDir {
		t.Errorf("expected saved config to have AttachmentDir %q, got %q", expectedDefaultDir, savedCfg.AttachmentDir)
	}

	if savedCfg.Theme != "catppuccin" {
		t.Errorf("expected saved config to have Theme 'catppuccin', got %q", savedCfg.Theme)
	}

	if savedCfg.BrowserCommand != "xdg-open" {
		t.Errorf("expected saved config to have BrowserCommand 'xdg-open', got %q", savedCfg.BrowserCommand)
	}

	// Case 3: Config file exists and has custom non-zero values
	cfg.RefreshTimeMin = 10
	cfg.UseSQLite = 1
	cfg.ExcludedFolders = []string{"Junk Email", "RSS Feeds"}
	cfg.ScrollLines = 5
	cfg.ImageViewer = "sxiv"
	cfg.AttachmentDir = "/custom/download/dir"
	cfg.TerminalBell = 0
	cfg.Theme = "teams"
	cfg.BrowserCommand = "google-chrome"
	err = SaveConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error saving config: %v", err)
	}

	cfg, err = LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if cfg.RefreshTimeMin != 10 {
		t.Errorf("expected custom RefreshTimeMin to be 10, got %d", cfg.RefreshTimeMin)
	}
	if cfg.UseSQLite != 1 {
		t.Errorf("expected custom UseSQLite to be 1, got %d", cfg.UseSQLite)
	}
	if len(cfg.ExcludedFolders) != 2 || cfg.ExcludedFolders[0] != "Junk Email" || cfg.ExcludedFolders[1] != "RSS Feeds" {
		t.Errorf("expected custom ExcludedFolders to be [\"Junk Email\", \"RSS Feeds\"], got %v", cfg.ExcludedFolders)
	}
	if cfg.ScrollLines != 5 {
		t.Errorf("expected custom ScrollLines to be 5, got %d", cfg.ScrollLines)
	}
	if cfg.ImageViewer != "sxiv" {
		t.Errorf("expected custom ImageViewer to be 'sxiv', got %q", cfg.ImageViewer)
	}
	if cfg.AttachmentDir != "/custom/download/dir" {
		t.Errorf("expected custom AttachmentDir to be '/custom/download/dir', got %q", cfg.AttachmentDir)
	}
	if cfg.TerminalBell != 0 {
		t.Errorf("expected custom TerminalBell to be 0, got %d", cfg.TerminalBell)
	}
	if cfg.Theme != "teams" {
		t.Errorf("expected custom Theme to be 'teams', got %q", cfg.Theme)
	}
	if cfg.BrowserCommand != "google-chrome" {
		t.Errorf("expected custom BrowserCommand to be 'google-chrome', got %q", cfg.BrowserCommand)
	}
}

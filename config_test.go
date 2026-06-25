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

	// Case 3: Config file exists and has custom non-zero values
	cfg.RefreshTimeMin = 10
	cfg.UseSQLite = 1
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
}

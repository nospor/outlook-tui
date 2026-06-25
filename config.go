package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	ClientID       string `json:"client_id"`
	TenantID       string `json:"tenant_id"`        // defaults to "common"
	RefreshTimeMin int    `json:"refresh_time_min"` // defaults to 5
	Layout         int    `json:"layout"`           // 1 = side-by-side (default), 2 = folders above messages
	UseSQLite      int    `json:"use_sqlite"`       // 0 = disabled (default), 1 = cache messages in ~/.cache/outlook-tui/db.db
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
		return Config{
			ClientID:       "",
			TenantID:       "common",
			RefreshTimeMin: 5,
			Layout:         1,
			UseSQLite:      0,
		}, nil
	}
	
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	
	if cfg.TenantID == "" {
		cfg.TenantID = "common"
	}
	
	if cfg.RefreshTimeMin <= 0 {
		cfg.RefreshTimeMin = 5
		_ = SaveConfig(cfg)
	}

	if cfg.Layout != 1 && cfg.Layout != 2 {
		cfg.Layout = 1
		_ = SaveConfig(cfg)
	}

	if !strings.Contains(string(data), "use_sqlite") {
		_ = SaveConfig(cfg)
	}

	return cfg, nil
}

func SaveConfig(cfg Config) error {
	if cfg.RefreshTimeMin <= 0 {
		cfg.RefreshTimeMin = 5
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

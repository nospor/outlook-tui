package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	ClientID string `json:"client_id"`
	TenantID string `json:"tenant_id"` // defaults to "common"
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
			ClientID: "",
			TenantID: "common",
		}, nil
	}
	
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func SaveConfig(cfg Config) error {
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

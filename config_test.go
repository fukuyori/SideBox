package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	cfg, err := loadOrCreateConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CityCode != "130010" || cfg.RefreshMinutes != 15 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config was not created: %v", err)
	}
}

func TestConfigNormalization(t *testing.T) {
	cfg := (appConfig{CityCode: "016010", RefreshMinutes: 1, Opacity: 2}).normalized()
	if cfg.CityCode != "016010" {
		t.Fatalf("city code was changed: %q", cfg.CityCode)
	}
	if cfg.RefreshMinutes != 15 || cfg.Opacity != 0.94 {
		t.Fatalf("invalid values were not normalized: %+v", cfg)
	}
}

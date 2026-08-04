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
	if cfg.RefreshMinutes != 15 || cfg.WindowWidth != 760 || cfg.WindowHeight != 425 || cfg.Opacity != 0.94 {
		t.Fatalf("invalid values were not normalized: %+v", cfg)
	}
}

func TestWithWindowBoundsPreservesOtherSettings(t *testing.T) {
	cfg := appConfig{
		CityCode:         "140010",
		RefreshMinutes:   30,
		AlwaysOnTop:      false,
		StartWithWindows: true,
		Opacity:          0.8,
	}
	got := withWindowBounds(cfg, -120, 240, 988, 553)
	if got.WindowX != -120 || got.WindowY != 240 || got.WindowWidth != 988 || got.WindowHeight != 553 {
		t.Fatalf("window bounds = (%d, %d, %d, %d), want (-120, 240, 988, 553)", got.WindowX, got.WindowY, got.WindowWidth, got.WindowHeight)
	}
	if got.CityCode != cfg.CityCode || got.RefreshMinutes != cfg.RefreshMinutes ||
		got.AlwaysOnTop != cfg.AlwaysOnTop || got.StartWithWindows != cfg.StartWithWindows ||
		got.Opacity != cfg.Opacity {
		t.Fatalf("other settings changed: got %+v, want %+v", got, cfg)
	}
}

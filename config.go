package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultWindowWidth  = 760
	defaultWindowHeight = 425
)

type appConfig struct {
	CityCode         string  `json:"city_code"`
	RefreshMinutes   int     `json:"refresh_minutes"`
	AlwaysOnTop      bool    `json:"always_on_top"`
	StartWithWindows bool    `json:"start_with_windows"`
	Opacity          float64 `json:"opacity"`
	WindowX          int32   `json:"window_x"`
	WindowY          int32   `json:"window_y"`
	WindowWidth      int32   `json:"window_width"`
	WindowHeight     int32   `json:"window_height"`
}

func defaultConfig() appConfig {
	return appConfig{
		CityCode:         "130010",
		RefreshMinutes:   15,
		AlwaysOnTop:      true,
		StartWithWindows: false,
		Opacity:          0.94,
		WindowX:          32,
		WindowY:          32,
		WindowWidth:      defaultWindowWidth,
		WindowHeight:     defaultWindowHeight,
	}
}

func (c appConfig) normalized() appConfig {
	defaults := defaultConfig()
	if c.CityCode == "" {
		c.CityCode = defaults.CityCode
	}
	if c.RefreshMinutes < 5 {
		c.RefreshMinutes = defaults.RefreshMinutes
	}
	if c.WindowWidth <= 0 {
		c.WindowWidth = defaults.WindowWidth
	}
	if c.WindowHeight <= 0 {
		c.WindowHeight = defaults.WindowHeight
	}
	if c.Opacity < 0.35 || c.Opacity > 1 {
		c.Opacity = defaults.Opacity
	}
	return c
}

func withWindowBounds(cfg appConfig, x, y, width, height int32) appConfig {
	cfg.WindowX = x
	cfg.WindowY = y
	cfg.WindowWidth = width
	cfg.WindowHeight = height
	return cfg
}

func configFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sidebox", "config.json"), nil
}

func loadOrCreateConfig(path string) (appConfig, error) {
	cfg, err := loadConfig(path)
	if err == nil {
		return cfg.normalized(), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return appConfig{}, err
	}
	cfg = defaultConfig()
	if err := writeConfig(path, cfg); err != nil {
		return appConfig{}, err
	}
	return cfg, nil
}

func loadConfig(path string) (appConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return appConfig{}, err
	}
	var cfg appConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return appConfig{}, fmt.Errorf("設定ファイルのJSONが不正です: %w", err)
	}
	return cfg.normalized(), nil
}

func writeConfig(path string, cfg appConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

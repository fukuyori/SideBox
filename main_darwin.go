//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ServiceManagement
#include <stdlib.h>
#include "sidebox_darwin.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

type macWeatherPayload struct {
	Error       string                    `json:"error,omitempty"`
	Location    string                    `json:"location,omitempty"`
	Description string                    `json:"description,omitempty"`
	PublishedAt string                    `json:"published_at,omitempty"`
	Humidity    *int                      `json:"humidity,omitempty"`
	Daily       []macDailyForecastPayload `json:"daily,omitempty"`
}

type macDailyForecastPayload struct {
	DateLabel                string   `json:"date_label"`
	Description              string   `json:"description"`
	Wind                     string   `json:"wind,omitempty"`
	TemperatureMax           *float64 `json:"temperature_max,omitempty"`
	TemperatureMin           *float64 `json:"temperature_min,omitempty"`
	PrecipitationProbability *int     `json:"precipitation_probability,omitempty"`
}

type macConfigPayload struct {
	Error          string  `json:"error,omitempty"`
	RefreshMinutes int     `json:"refresh_minutes"`
	AlwaysOnTop    bool    `json:"always_on_top"`
	StartAtLogin   bool    `json:"start_at_login"`
	Opacity        float64 `json:"opacity"`
	WindowX        int32   `json:"window_x"`
	WindowY        int32   `json:"window_y"`
	WindowWidth    int32   `json:"window_width"`
	WindowHeight   int32   `json:"window_height"`
}

var (
	macStateMu       sync.RWMutex
	macCurrentConfig appConfig
	macConfigPath    string
	macClient        = newWeatherClient()
)

func main() {
	runtime.LockOSThread()

	path, err := configFilePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "設定ファイルの場所を取得できません:", err)
		return
	}
	cfg, err := loadOrCreateConfig(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "設定ファイルを読み込めません:", err)
		return
	}
	macConfigPath = path
	macCurrentConfig = cfg

	configJSON := macCString(macConfigPayloadFromConfig(cfg, nil))
	configPath := C.CString(path)
	version := C.CString(appVersion)
	defer C.free(unsafe.Pointer(configJSON))
	defer C.free(unsafe.Pointer(configPath))
	defer C.free(unsafe.Pointer(version))

	C.SideboxRun(configJSON, configPath, version)
}

func macConfigSnapshot() appConfig {
	macStateMu.RLock()
	defer macStateMu.RUnlock()
	return macCurrentConfig
}

func macConfigPayloadFromConfig(cfg appConfig, err error) macConfigPayload {
	payload := macConfigPayload{
		RefreshMinutes: cfg.RefreshMinutes,
		AlwaysOnTop:    cfg.AlwaysOnTop,
		StartAtLogin:   cfg.StartAtLogin,
		Opacity:        cfg.Opacity,
		WindowX:        cfg.WindowX,
		WindowY:        cfg.WindowY,
		WindowWidth:    cfg.WindowWidth,
		WindowHeight:   cfg.WindowHeight,
	}
	if err != nil {
		payload.Error = err.Error()
	}
	return payload
}

func macCString(value any) *C.char {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(`{"error":"JSONの生成に失敗しました"}`)
	}
	return C.CString(string(data))
}

//export SideboxFetchWeatherJSON
func SideboxFetchWeatherJSON() *C.char {
	report, err := macClient.fetch(context.Background(), macConfigSnapshot())
	if err != nil {
		return macCString(macWeatherPayload{Error: err.Error()})
	}
	payload := macWeatherPayload{
		Location:    report.Location,
		Description: report.Description,
		Humidity:    report.Humidity,
	}
	if !report.PublishedAt.IsZero() {
		payload.PublishedAt = report.PublishedAt.Format(time.RFC3339)
	}
	for _, daily := range report.Daily {
		payload.Daily = append(payload.Daily, macDailyForecastPayload{
			DateLabel:                daily.DateLabel,
			Description:              daily.Description,
			Wind:                     daily.Wind,
			TemperatureMax:           daily.TemperatureMax,
			TemperatureMin:           daily.TemperatureMin,
			PrecipitationProbability: daily.PrecipitationProbability,
		})
	}
	return macCString(payload)
}

//export SideboxReloadConfigJSON
func SideboxReloadConfigJSON() *C.char {
	cfg, err := loadConfig(macConfigPath)
	if err != nil {
		return macCString(macConfigPayloadFromConfig(macConfigSnapshot(), err))
	}
	macStateMu.Lock()
	macCurrentConfig = cfg
	macStateMu.Unlock()
	return macCString(macConfigPayloadFromConfig(cfg, nil))
}

//export SideboxSaveWindowFrame
func SideboxSaveWindowFrame(x, y, width, height C.int) {
	macStateMu.Lock()
	cfg := withWindowBounds(macCurrentConfig, int32(x), int32(y), int32(width), int32(height))
	if err := writeConfig(macConfigPath, cfg); err == nil {
		macCurrentConfig = cfg
	}
	macStateMu.Unlock()
}

//export SideboxSetStartAtLogin
func SideboxSetStartAtLogin(enabled C.int) *C.char {
	macStateMu.Lock()
	defer macStateMu.Unlock()

	cfg := macCurrentConfig
	cfg.StartAtLogin = enabled != 0
	if err := writeConfig(macConfigPath, cfg); err != nil {
		return macCString(macConfigPayloadFromConfig(macCurrentConfig, err))
	}
	macCurrentConfig = cfg
	return macCString(macConfigPayloadFromConfig(cfg, nil))
}

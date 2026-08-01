//go:build windows

package main

import "testing"

func TestContextMenuCommandAt(t *testing.T) {
	tests := []struct {
		name string
		x, y int32
		want uint16
	}{
		{"更新", 250, 60, menuRefresh},
		{"再読込", 250, 100, menuReload},
		{"設定を開く", 250, 140, menuOpen},
		{"終了", 250, 180, menuExit},
		{"メニュー外", 100, 100, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contextMenuCommandAt(tt.x, tt.y); got != tt.want {
				t.Fatalf("contextMenuCommandAt(%d, %d) = %d, want %d", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

func TestWeatherIconForDescription(t *testing.T) {
	tests := []struct {
		description string
		want        weatherIcon
	}{
		{"快晴", iconSun},
		{"晴れ時々曇り", iconPartlyCloudy},
		{"曇り", iconCloud},
		{"雨", iconRain},
		{"雪", iconSnow},
		{"雷雨", iconThunder},
		{"霧", iconFog},
	}
	for _, tt := range tests {
		if got := weatherIconForDescription(tt.description); got != tt.want {
			t.Errorf("weatherIconForDescription(%q) = %d, want %d", tt.description, got, tt.want)
		}
	}
}

//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSingleInstanceMutex(t *testing.T) {
	name := fmt.Sprintf(`Local\Sidebox.Test.%d.%d`, os.Getpid(), time.Now().UnixNano())
	firstHandle, alreadyRunning, err := acquireSingleInstanceMutex(name)
	if err != nil {
		t.Fatalf("1回目のミューテックス作成に失敗しました: %v", err)
	}
	if alreadyRunning {
		t.Fatal("1回目の起動が二重起動と判定されました")
	}
	defer procCloseHandle.Call(firstHandle)

	secondHandle, alreadyRunning, err := acquireSingleInstanceMutex(name)
	if err != nil {
		t.Fatalf("2回目のミューテックス作成に失敗しました: %v", err)
	}
	if !alreadyRunning {
		if secondHandle != 0 {
			procCloseHandle.Call(secondHandle)
		}
		t.Fatal("2回目の起動が二重起動と判定されませんでした")
	}
	if secondHandle != 0 {
		procCloseHandle.Call(secondHandle)
		t.Fatalf("二重起動判定時のハンドル = %d, want 0", secondHandle)
	}
}

func TestIsResumePowerEvent(t *testing.T) {
	tests := []struct {
		name  string
		event uintptr
		want  bool
	}{
		{"ユーザー操作からの復帰", pbtApmResumeSuspend, true},
		{"自動復帰", pbtApmResumeAutomatic, true},
		{"その他の電源イベント", 0x0004, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isResumePowerEvent(tt.event); got != tt.want {
				t.Fatalf("isResumePowerEvent(%#x) = %t, want %t", tt.event, got, tt.want)
			}
		})
	}
}

func TestContextMenuCommandAt(t *testing.T) {
	tests := []struct {
		name string
		x, y int32
		want uint16
	}{
		{"更新", 250, 60, menuRefresh},
		{"再読込", 250, 100, menuReload},
		{"設定を開く", 250, 140, menuOpen},
		{"自動起動", 250, 180, menuStartup},
		{"終了", 250, 220, menuExit},
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

func TestResizeHitTest(t *testing.T) {
	bounds := rect{Left: 100, Top: 100, Right: 860, Bottom: 525}
	tests := []struct {
		name string
		x, y int32
		want uintptr
	}{
		{"左上", 100, 100, htTopLeft},
		{"右上", 859, 100, htTopRight},
		{"左下", 100, 524, htBottomLeft},
		{"右下", 859, 524, htBottomRight},
		{"左辺", 101, 300, htLeft},
		{"右辺", 859, 300, htRight},
		{"上辺", 400, 101, htTop},
		{"下辺", 400, 524, htBottom},
		{"中央", 400, 300, htClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resizeHitTest(bounds, tt.x, tt.y); got != tt.want {
				t.Fatalf("resizeHitTest(%d, %d) = %d, want %d", tt.x, tt.y, got, tt.want)
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

func TestFormatHumidity(t *testing.T) {
	humidity := 63
	if got := formatHumidity(&humidity); got != "63%" {
		t.Fatalf("formatHumidity(63) = %q, want 63%%", got)
	}
	if got := formatHumidity(nil); got != "--" {
		t.Fatalf("formatHumidity(nil) = %q, want --", got)
	}
}

func TestFitTextWithEllipsis(t *testing.T) {
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		t.Fatal("テキスト計測用DCを作成できませんでした")
	}
	defer procDeleteDC.Call(hdc)
	font := createFont(17, 400, "Yu Gothic UI")
	if font == 0 {
		t.Fatal("テキスト計測用フォントを作成できませんでした")
	}
	defer procDeleteObject.Call(font)

	value := "くもり 夕方 一時 雨 所により 昼過ぎ から 夜のはじめ 頃 雷を伴う"
	bounds := rect{0, 0, 170, 39}
	got := fitTextWithEllipsis(hdc, value, bounds, font)
	if got == value || !strings.HasSuffix(got, "…") {
		t.Fatalf("fitTextWithEllipsis() = %q, want ellipsis", got)
	}
	if !textFits(hdc, got, bounds, font) {
		t.Fatalf("省略後の文字列が表示領域に収まりません: %q", got)
	}

	shortValue := "晴れ"
	if got := fitTextWithEllipsis(hdc, shortValue, bounds, font); got != shortValue {
		t.Fatalf("短い予報 = %q, want %q", got, shortValue)
	}

	todayValue := "くもり 夕方 一時 雨 所により 昼過ぎ から 夜のはじめ"
	wideBounds := rect{0, 0, 688, 24}
	if got := fitSingleLineTextWithEllipsis(hdc, todayValue, wideBounds, font); got != todayValue {
		t.Fatalf("横長欄の当日予報 = %q, want %q", got, todayValue)
	}
	narrowBounds := rect{0, 0, 120, 24}
	got = fitSingleLineTextWithEllipsis(hdc, todayValue, narrowBounds, font)
	if got == todayValue || !strings.HasSuffix(got, "…") {
		t.Fatalf("狭い横長欄の当日予報 = %q, want ellipsis", got)
	}
}

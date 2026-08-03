//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	windowWidth  = 760
	windowHeight = 425

	csHRedraw   = 0x0002
	csVRedraw   = 0x0001
	csDblClks   = 0x0008
	wsPopup     = 0x80000000
	wsExLayered = 0x00080000
	wsExToolWin = 0x00000080

	swShow            = 5
	swpNoMove         = 0x0002
	swpNoSize         = 0x0001
	swpNoActivate     = 0x0010
	lwaAlpha          = 0x00000002
	wmCreate          = 0x0001
	wmDestroy         = 0x0002
	wmClose           = 0x0010
	wmPaint           = 0x000F
	wmEraseBkgnd      = 0x0014
	wmContextMenu     = 0x007B
	wmNCRButtonDown   = 0x00A4
	wmNCRButtonUp     = 0x00A5
	wmCommand         = 0x0111
	wmTimer           = 0x0113
	wmLButtonDown     = 0x0201
	wmRButtonDown     = 0x0204
	wmRButtonUp       = 0x0205
	wmNCLButtonDown   = 0x00A1
	wmAppWeatherReady = 0x8001
	htCaption         = 2

	menuRefresh = 1001
	menuReload  = 1002
	menuOpen    = 1003
	menuStartup = 1004
	menuExit    = 1005

	dtLeft       = 0x0000
	dtCenter     = 0x0001
	dtRight      = 0x0002
	dtVCenter    = 0x0004
	dtSingleLine = 0x0020
	dtNoPrefix   = 0x0800
	transparent  = 1
	srccopy      = 0x00CC0020
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type msg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}
type paintStruct struct {
	Hdc         uintptr
	Erase       int32
	Paint       rect
	Restore     int32
	IncUpdate   int32
	RGBReserved [32]byte
}
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassEx        = user32.NewProc("RegisterClassExW")
	procCreateWindowEx         = user32.NewProc("CreateWindowExW")
	procDefWindowProc          = user32.NewProc("DefWindowProcW")
	procShowWindow             = user32.NewProc("ShowWindow")
	procUpdateWindow           = user32.NewProc("UpdateWindow")
	procGetMessage             = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessage        = user32.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procBeginPaint             = user32.NewProc("BeginPaint")
	procEndPaint               = user32.NewProc("EndPaint")
	procGetClientRect          = user32.NewProc("GetClientRect")
	procFillRect               = user32.NewProc("FillRect")
	procInvalidateRect         = user32.NewProc("InvalidateRect")
	procSetTimer               = user32.NewProc("SetTimer")
	procKillTimer              = user32.NewProc("KillTimer")
	procPostMessage            = user32.NewProc("PostMessageW")
	procSendMessage            = user32.NewProc("SendMessageW")
	procReleaseCapture         = user32.NewProc("ReleaseCapture")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procSetWindowPos           = user32.NewProc("SetWindowPos")
	procSetLayeredWindowAttrs  = user32.NewProc("SetLayeredWindowAttributes")
	procSetWindowRgn           = user32.NewProc("SetWindowRgn")
	procGetWindowRect          = user32.NewProc("GetWindowRect")
	procLoadCursor             = user32.NewProc("LoadCursorW")
	procMessageBox             = user32.NewProc("MessageBoxW")
	procSetProcessDPIAwareCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetModuleHandle        = kernel32.NewProc("GetModuleHandleW")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procCreateFont             = gdi32.NewProc("CreateFontW")
	procCreatePen              = gdi32.NewProc("CreatePen")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procMoveToEx               = gdi32.NewProc("MoveToEx")
	procLineTo                 = gdi32.NewProc("LineTo")
	procEllipse                = gdi32.NewProc("Ellipse")
	procRoundRect              = gdi32.NewProc("RoundRect")
	procDrawText               = user32.NewProc("DrawTextW")
	procCreateRoundRectRgn     = gdi32.NewProc("CreateRoundRectRgn")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
)

var (
	configMu                                      sync.RWMutex
	currentCfg                                    appConfig
	configPath                                    string
	weatherMu                                     sync.RWMutex
	currentWeather                                weatherReport
	weatherError                                  string
	weatherBusy                                   atomic.Bool
	client                                        = newWeatherClient()
	backgroundBrush                               uintptr
	fontClock, fontDate, fontWeather, fontDetails uintptr
	contextMenuVisible                            bool
)

func main() {
	runtime.LockOSThread()
	path, err := configFilePath()
	if err != nil {
		showMessage("Sidebox", "設定ファイルの場所を取得できません: "+err.Error())
		return
	}
	configPath = path
	cfg, err := loadOrCreateConfig(path)
	if err != nil {
		showMessage("Sidebox", err.Error())
		return
	}
	currentCfg = cfg
	if err := syncStartupRegistration(cfg.StartWithWindows); err != nil {
		showMessage("Sidebox - 自動起動設定", err.Error())
	}

	procSetProcessDPIAwareCtx.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	instance, _, _ := procGetModuleHandle.Call(0)
	className := utf16Ptr("SideboxWidgetClass")
	cursor, _, _ := procLoadCursor.Call(0, 32512)
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		Style:     csHRedraw | csVRedraw | csDblClks,
		WndProc:   syscall.NewCallback(windowProc),
		Instance:  instance,
		Cursor:    cursor,
		ClassName: className,
	}
	if atom, _, callErr := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		showMessage("Sidebox", "ウィンドウクラスを登録できません: "+callErr.Error())
		return
	}

	hwnd, _, callErr := procCreateWindowEx.Call(
		wsExLayered|wsExToolWin,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr(appName+" "+appVersion))),
		wsPopup,
		uintptr(cfg.WindowX), uintptr(cfg.WindowY), windowWidth, windowHeight,
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		showMessage("Sidebox", "ウィンドウを作成できません: "+callErr.Error())
		return
	}
	applyWindowOptions(hwnd, cfg)
	region, _, _ := procCreateRoundRectRgn.Call(0, 0, windowWidth+1, windowHeight+1, 24, 24)
	procSetWindowRgn.Call(hwnd, region, 1)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	refreshWeather(hwnd)

	var message msg
	for {
		result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCreate:
		createDrawingResources()
		procSetTimer.Call(hwnd, 1, 1000, 0)
		cfg := configSnapshot()
		procSetTimer.Call(hwnd, 2, uintptr(cfg.RefreshMinutes*60*1000), 0)
		return 0
	case wmTimer:
		if wParam == 2 {
			refreshWeather(hwnd)
		}
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmAppWeatherReady:
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmPaint:
		paintWindow(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmLButtonDown:
		x := int32(int16(lParam & 0xffff))
		y := int32(int16((lParam >> 16) & 0xffff))
		if handleContextMenuClick(hwnd, x, y) {
			return 0
		}
		if x >= windowWidth-48 && y <= 48 {
			procDestroyWindow.Call(hwnd)
			return 0
		}
		procReleaseCapture.Call()
		procSendMessage.Call(hwnd, wmNCLButtonDown, htCaption, 0)
		return 0
	case wmRButtonDown, wmRButtonUp, wmNCRButtonDown, wmNCRButtonUp, wmContextMenu:
		showContextMenu(hwnd)
		return 0
	case wmCommand:
		handleCommand(hwnd, uint16(wParam&0xffff))
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		saveWindowPosition(hwnd)
		procKillTimer.Call(hwnd, 1)
		procKillTimer.Call(hwnd, 2)
		deleteDrawingResources()
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func refreshWeather(hwnd uintptr) {
	if !weatherBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer weatherBusy.Store(false)
		report, err := client.fetch(context.Background(), configSnapshot())
		weatherMu.Lock()
		if err != nil {
			weatherError = err.Error()
		} else {
			currentWeather = report
			weatherError = ""
		}
		weatherMu.Unlock()
		procPostMessage.Call(hwnd, wmAppWeatherReady, 0, 0)
	}()
}

func paintWindow(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var bounds rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	bitmap, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(bounds.Right), uintptr(bounds.Bottom))
	oldBitmap, _, _ := procSelectObject.Call(memDC, bitmap)
	defer func() {
		procSelectObject.Call(memDC, oldBitmap)
		procDeleteObject.Call(bitmap)
		procDeleteDC.Call(memDC)
	}()
	procFillRect.Call(memDC, uintptr(unsafe.Pointer(&bounds)), backgroundBrush)
	procSetBkMode.Call(memDC, transparent)

	now := time.Now()
	drawText(memDC, now.Format("15:04:05"), rect{24, 15, bounds.Right - 24, 99}, fontClock, rgb(245, 247, 250), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	drawText(memDC, "×", rect{bounds.Right - 50, 7, bounds.Right - 15, 42}, fontWeather, rgb(150, 159, 174), dtRight|dtVCenter|dtSingleLine|dtNoPrefix)
	date := fmt.Sprintf("%s（%s）", now.Format("2006年01月02日"), japaneseWeekday(now.Weekday()))
	drawText(memDC, date, rect{28, 97, bounds.Right - 28, 129}, fontDate, rgb(174, 183, 198), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)

	line := rect{28, 137, bounds.Right - 28, 138}
	divider, _, _ := procCreateSolidBrush.Call(rgb(66, 75, 91))
	procFillRect.Call(memDC, uintptr(unsafe.Pointer(&line)), divider)
	procDeleteObject.Call(divider)

	weatherMu.RLock()
	report, weatherErr := currentWeather, weatherError
	weatherMu.RUnlock()
	if weatherErr != "" {
		drawText(memDC, "天気を取得できません", rect{28, 146, bounds.Right - 28, 181}, fontWeather, rgb(255, 166, 158), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
		drawText(memDC, shorten(weatherErr, 55), rect{28, 181, bounds.Right - 28, 215}, fontDetails, rgb(174, 183, 198), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	} else if report.Location == "" {
		drawText(memDC, "天気情報を取得中…", rect{28, 151, bounds.Right - 28, 205}, fontWeather, rgb(210, 216, 226), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	} else {
		drawText(memDC, report.Location+"  "+report.Description, rect{28, 143, bounds.Right - 160, 176}, fontWeather, rgb(235, 239, 245), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
		drawText(memDC, "気象庁予報", rect{bounds.Right - 165, 143, bounds.Right - 28, 176}, fontDetails, rgb(135, 145, 162), dtRight|dtVCenter|dtSingleLine|dtNoPrefix)
		if len(report.Daily) > 0 {
			today := report.Daily[0]
			todayDetails := fmt.Sprintf("最高 %s   最低 %s   降水 %s", formatTemperature(today.TemperatureMax), formatTemperature(today.TemperatureMin), formatRainChance(today.PrecipitationProbability))
			drawText(memDC, todayDetails, rect{28, 174, bounds.Right - 28, 201}, fontDetails, rgb(218, 222, 230), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
			drawText(memDC, "風 "+shorten(today.Wind, 70), rect{28, 199, bounds.Right - 28, 226}, fontDetails, rgb(174, 183, 198), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
		}
		drawWeeklyForecast(memDC, bounds, report.Daily)
	}
	if contextMenuVisible {
		drawContextMenu(memDC)
	}
	procBitBlt.Call(hdc, 0, 0, uintptr(bounds.Right), uintptr(bounds.Bottom), memDC, 0, 0, srccopy)
}

func drawWeeklyForecast(hdc uintptr, bounds rect, forecasts []dailyForecast) {
	line := rect{28, 233, bounds.Right - 28, 234}
	divider, _, _ := procCreateSolidBrush.Call(rgb(66, 75, 91))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&line)), divider)
	procDeleteObject.Call(divider)

	drawText(hdc, "3日間の天気予報", rect{28, 239, 220, 266}, fontWeather, rgb(235, 239, 245), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	drawText(hdc, appName+" "+appVersion, rect{bounds.Right - 180, 239, bounds.Right - 28, 266}, fontDetails, rgb(112, 122, 139), dtRight|dtVCenter|dtSingleLine|dtNoPrefix)

	if len(forecasts) == 0 {
		drawText(hdc, "予報を取得できません", rect{28, 272, bounds.Right - 28, 310}, fontDetails, rgb(174, 183, 198), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
		return
	}
	const cardsTop = int32(266)
	count := min(len(forecasts), 3)
	cardWidth := int32(220)
	cardsLeft := (bounds.Right - int32(count)*cardWidth) / 2
	for index, forecast := range forecasts {
		if index >= count {
			break
		}
		left := cardsLeft + int32(index)*cardWidth
		right := left + cardWidth
		if index > 0 {
			separator := rect{left, cardsTop + 5, left + 1, bounds.Bottom - 18}
			separatorBrush, _, _ := procCreateSolidBrush.Call(rgb(53, 61, 75))
			procFillRect.Call(hdc, uintptr(unsafe.Pointer(&separator)), separatorBrush)
			procDeleteObject.Call(separatorBrush)
		}

		dateLabel := fmt.Sprintf("%s  %d/%d（%s）", forecast.DateLabel, forecast.Date.Month(), forecast.Date.Day(), japaneseWeekday(forecast.Date.Weekday()))
		drawText(hdc, dateLabel, rect{left + 3, cardsTop, right - 3, cardsTop + 25}, fontDetails, rgb(205, 211, 221), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
		drawWeatherIcon(hdc, (left+right)/2, cardsTop+47, weatherIconForDescription(forecast.Description))
		drawText(hdc, forecast.Description, rect{left + 3, cardsTop + 68, right - 3, cardsTop + 91}, fontDetails, rgb(205, 211, 221), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
		temperatures := fmt.Sprintf("最高 %s  最低 %s", formatTemperature(forecast.TemperatureMax), formatTemperature(forecast.TemperatureMin))
		drawText(hdc, temperatures, rect{left + 3, cardsTop + 91, right - 3, cardsTop + 116}, fontDetails, rgb(235, 192, 153), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
		drawText(hdc, "降水 "+formatRainChance(forecast.PrecipitationProbability), rect{left + 3, cardsTop + 116, right - 3, cardsTop + 141}, fontDetails, rgb(103, 200, 255), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	}
}

func formatTemperature(value *float64) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.0f°C", *value)
}

func formatRainChance(value *int) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%d%%", *value)
}

type weatherIcon int

const (
	iconSun weatherIcon = iota
	iconPartlyCloudy
	iconCloud
	iconRain
	iconSnow
	iconThunder
	iconFog
)

func weatherIconForDescription(description string) weatherIcon {
	switch {
	case strings.Contains(description, "雷"):
		return iconThunder
	case strings.Contains(description, "雪"):
		return iconSnow
	case strings.Contains(description, "雨"):
		return iconRain
	case strings.Contains(description, "霧"):
		return iconFog
	case strings.Contains(description, "晴") && strings.Contains(description, "曇"):
		return iconPartlyCloudy
	case strings.Contains(description, "晴"):
		return iconSun
	case strings.Contains(description, "曇"):
		return iconCloud
	default:
		return iconCloud
	}
}

func drawWeatherIcon(hdc uintptr, centerX, centerY int32, icon weatherIcon) {
	switch icon {
	case iconSun:
		drawSun(hdc, centerX, centerY)
	case iconPartlyCloudy:
		drawSun(hdc, centerX-9, centerY-6)
		drawCloud(hdc, centerX+5, centerY+3)
	case iconCloud:
		drawCloud(hdc, centerX, centerY)
	case iconRain:
		drawCloud(hdc, centerX, centerY-5)
		drawRain(hdc, centerX, centerY+8)
	case iconSnow:
		drawCloud(hdc, centerX, centerY-5)
		drawSnow(hdc, centerX, centerY+10)
	case iconThunder:
		drawCloud(hdc, centerX, centerY-5)
		drawThunder(hdc, centerX, centerY+7)
	case iconFog:
		drawFog(hdc, centerX, centerY)
	}
}

func drawSun(hdc uintptr, x, y int32) {
	color := rgb(255, 199, 79)
	pen, _, _ := procCreatePen.Call(0, 2, color)
	brush, _, _ := procCreateSolidBrush.Call(color)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	procEllipse.Call(hdc, uintptr(x-7), uintptr(y-7), uintptr(x+8), uintptr(y+8))
	for _, ray := range [][4]int32{{0, -16, 0, -11}, {0, 11, 0, 16}, {-16, 0, -11, 0}, {11, 0, 16, 0}, {-12, -12, -8, -8}, {8, 8, 12, 12}, {-12, 12, -8, 8}, {8, -8, 12, -12}} {
		procMoveToEx.Call(hdc, uintptr(x+ray[0]), uintptr(y+ray[1]), 0)
		procLineTo.Call(hdc, uintptr(x+ray[2]), uintptr(y+ray[3]))
	}
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func drawCloud(hdc uintptr, x, y int32) {
	color := rgb(205, 214, 226)
	pen, _, _ := procCreatePen.Call(0, 1, color)
	brush, _, _ := procCreateSolidBrush.Call(color)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	procEllipse.Call(hdc, uintptr(x-18), uintptr(y-5), uintptr(x-1), uintptr(y+10))
	procEllipse.Call(hdc, uintptr(x-10), uintptr(y-12), uintptr(x+10), uintptr(y+10))
	procEllipse.Call(hdc, uintptr(x+2), uintptr(y-6), uintptr(x+19), uintptr(y+10))
	procRoundRect.Call(hdc, uintptr(x-18), uintptr(y), uintptr(x+19), uintptr(y+11), 6, 6)
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func drawRain(hdc uintptr, x, y int32) {
	pen, _, _ := procCreatePen.Call(0, 2, rgb(79, 174, 255))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	for _, offset := range []int32{-11, 0, 11} {
		procMoveToEx.Call(hdc, uintptr(x+offset+2), uintptr(y), 0)
		procLineTo.Call(hdc, uintptr(x+offset-2), uintptr(y+8))
	}
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)
}

func drawSnow(hdc uintptr, x, y int32) {
	pen, _, _ := procCreatePen.Call(0, 1, rgb(155, 220, 255))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	for _, offset := range []int32{-10, 0, 10} {
		procMoveToEx.Call(hdc, uintptr(x+offset-3), uintptr(y), 0)
		procLineTo.Call(hdc, uintptr(x+offset+3), uintptr(y+6))
		procMoveToEx.Call(hdc, uintptr(x+offset+3), uintptr(y), 0)
		procLineTo.Call(hdc, uintptr(x+offset-3), uintptr(y+6))
	}
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)
}

func drawThunder(hdc uintptr, x, y int32) {
	pen, _, _ := procCreatePen.Call(0, 3, rgb(255, 199, 79))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procMoveToEx.Call(hdc, uintptr(x+3), uintptr(y), 0)
	procLineTo.Call(hdc, uintptr(x-3), uintptr(y+7))
	procLineTo.Call(hdc, uintptr(x+2), uintptr(y+7))
	procLineTo.Call(hdc, uintptr(x-5), uintptr(y+15))
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)
}

func drawFog(hdc uintptr, x, y int32) {
	pen, _, _ := procCreatePen.Call(0, 2, rgb(173, 184, 199))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	for _, offset := range []int32{-8, 0, 8} {
		procMoveToEx.Call(hdc, uintptr(x-18), uintptr(y+offset), 0)
		procLineTo.Call(hdc, uintptr(x+18), uintptr(y+offset))
	}
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)
}

func createDrawingResources() {
	backgroundBrush, _, _ = procCreateSolidBrush.Call(rgb(27, 32, 42))
	fontClock = createFont(72, 400, "Consolas")
	fontDate = createFont(20, 400, "Yu Gothic UI")
	fontWeather = createFont(22, 600, "Yu Gothic UI")
	fontDetails = createFont(17, 400, "Yu Gothic UI")
}

func deleteDrawingResources() {
	for _, object := range []uintptr{backgroundBrush, fontClock, fontDate, fontWeather, fontDetails} {
		if object != 0 {
			procDeleteObject.Call(object)
		}
	}
}

func createFont(height, weight int32, face string) uintptr {
	font, _, _ := procCreateFont.Call(
		uintptr(-height), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(utf16Ptr(face))),
	)
	return font
}

func drawText(hdc uintptr, text string, bounds rect, font uintptr, color uintptr, format uint32) {
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	procSetTextColor.Call(hdc, color)
	chars := syscall.StringToUTF16(text)
	procDrawText.Call(hdc, uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1), uintptr(unsafe.Pointer(&bounds)), uintptr(format))
	procSelectObject.Call(hdc, oldFont)
}

func showContextMenu(hwnd uintptr) {
	contextMenuVisible = true
	procInvalidateRect.Call(hwnd, 0, 0)
}

func drawContextMenu(hdc uintptr) {
	outer := rect{232, 50, 526, 241}
	borderBrush, _, _ := procCreateSolidBrush.Call(rgb(88, 98, 115))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&outer)), borderBrush)
	procDeleteObject.Call(borderBrush)

	inner := rect{233, 51, 525, 240}
	menuBrush, _, _ := procCreateSolidBrush.Call(rgb(43, 49, 61))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&inner)), menuBrush)
	procDeleteObject.Call(menuBrush)

	cfg := configSnapshot()
	startupLabel := "ログイン時に自動起動: オフ"
	if cfg.StartWithWindows {
		startupLabel = "ログイン時に自動起動: オン"
	}
	items := []string{"今すぐ更新", "設定を再読込", "設定ファイルを開く", startupLabel, "終了"}
	for index, label := range items {
		top := int32(53 + index*37)
		drawText(hdc, label, rect{250, top, 513, top + 36}, fontDetails, rgb(235, 239, 245), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	}
}

func handleContextMenuClick(hwnd uintptr, x, y int32) bool {
	if !contextMenuVisible {
		return false
	}
	contextMenuVisible = false
	procInvalidateRect.Call(hwnd, 0, 0)
	if command := contextMenuCommandAt(x, y); command != 0 {
		handleCommand(hwnd, command)
	}
	return true
}

func contextMenuCommandAt(x, y int32) uint16 {
	if x < 233 || x >= 525 || y < 53 || y >= 238 {
		return 0
	}
	commands := [...]uint16{menuRefresh, menuReload, menuOpen, menuStartup, menuExit}
	return commands[(y-53)/37]
}

func handleCommand(hwnd uintptr, command uint16) {
	switch command {
	case menuRefresh:
		refreshWeather(hwnd)
	case menuReload:
		cfg, err := loadConfig(configPath)
		if err != nil {
			showMessage("Sidebox - 設定エラー", err.Error())
			return
		}
		if err := syncStartupRegistration(cfg.StartWithWindows); err != nil {
			showMessage("Sidebox - 自動起動設定", err.Error())
			return
		}
		configMu.Lock()
		currentCfg = cfg
		configMu.Unlock()
		applyWindowOptions(hwnd, cfg)
		procKillTimer.Call(hwnd, 2)
		procSetTimer.Call(hwnd, 2, uintptr(cfg.RefreshMinutes*60*1000), 0)
		refreshWeather(hwnd)
	case menuOpen:
		openConfigFile()
	case menuStartup:
		cfg := configSnapshot()
		cfg.StartWithWindows = !cfg.StartWithWindows
		if err := syncStartupRegistration(cfg.StartWithWindows); err != nil {
			showMessage("Sidebox - 自動起動設定", err.Error())
			return
		}
		if err := writeConfig(configPath, cfg); err != nil {
			_ = syncStartupRegistration(!cfg.StartWithWindows)
			showMessage("Sidebox - 設定エラー", err.Error())
			return
		}
		configMu.Lock()
		currentCfg = cfg
		configMu.Unlock()
		procInvalidateRect.Call(hwnd, 0, 0)
	case menuExit:
		procDestroyWindow.Call(hwnd)
	}
}

func openConfigFile() {
	command := exec.Command("notepad.exe", configPath)
	if err := command.Start(); err != nil {
		showMessage("Sidebox", "設定ファイルを開けません: "+err.Error())
	} else {
		_ = command.Process.Release()
	}
}

func applyWindowOptions(hwnd uintptr, cfg appConfig) {
	insertAfter := ^uintptr(0) // HWND_TOPMOST
	if !cfg.AlwaysOnTop {
		insertAfter = ^uintptr(1) // HWND_NOTOPMOST
	}
	procSetWindowPos.Call(hwnd, insertAfter, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	alpha := byte(cfg.Opacity*255 + 0.5)
	procSetLayeredWindowAttrs.Call(hwnd, 0, uintptr(alpha), lwaAlpha)
}

func saveWindowPosition(hwnd uintptr) {
	var bounds rect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		return
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		cfg = configSnapshot()
	}
	cfg.WindowX, cfg.WindowY = bounds.Left, bounds.Top
	_ = writeConfig(configPath, cfg)
}

func configSnapshot() appConfig {
	configMu.RLock()
	defer configMu.RUnlock()
	return currentCfg
}

func japaneseWeekday(day time.Weekday) string {
	return [...]string{"日", "月", "火", "水", "木", "金", "土"}[day]
}

func shorten(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes-1]) + "…"
}

func rgb(red, green, blue byte) uintptr {
	return uintptr(red) | uintptr(green)<<8 | uintptr(blue)<<16
}

func utf16Ptr(value string) *uint16 {
	ptr, _ := syscall.UTF16PtrFromString(value)
	return ptr
}

func showMessage(title, text string) {
	procMessageBox.Call(0, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), 0x10)
}

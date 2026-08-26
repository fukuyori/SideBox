//go:build windows

package main

import (
	"context"
	"errors"
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

	csHRedraw    = 0x0002
	csVRedraw    = 0x0001
	csDblClks    = 0x0008
	wsPopup      = 0x80000000
	wsThickFrame = 0x00040000
	wsExLayered  = 0x00080000
	wsExToolWin  = 0x00000080

	swShow                = 5
	swpNoMove             = 0x0002
	swpNoSize             = 0x0001
	swpNoActivate         = 0x0010
	lwaAlpha              = 0x00000002
	mmText                = 1
	mmAnisotropic         = 8
	wmCreate              = 0x0001
	wmDestroy             = 0x0002
	wmSize                = 0x0005
	wmClose               = 0x0010
	wmPaint               = 0x000F
	wmEraseBkgnd          = 0x0014
	wmNCCalcSize          = 0x0083
	wmNCHitTest           = 0x0084
	wmContextMenu         = 0x007B
	wmNCRButtonDown       = 0x00A4
	wmNCRButtonUp         = 0x00A5
	wmCommand             = 0x0111
	wmTimer               = 0x0113
	wmLButtonDown         = 0x0201
	wmRButtonDown         = 0x0204
	wmRButtonUp           = 0x0205
	wmPowerBroadcast      = 0x0218
	wmExitSizeMove        = 0x0232
	wmNCLButtonDown       = 0x00A1
	wmAppWeatherReady     = 0x8001
	pbtApmResumeSuspend   = 0x0007
	pbtApmResumeAutomatic = 0x0012
	htCaption             = 2
	htClient              = 1
	htLeft                = 10
	htRight               = 11
	htTop                 = 12
	htTopLeft             = 13
	htTopRight            = 14
	htBottom              = 15
	htBottomLeft          = 16
	htBottomRight         = 17
	resizeBorderWidth     = 8
	errorAlreadyExists    = 183

	singleInstanceMutexName = `Local\Sidebox.SingleInstance`
	weatherRetryDelayMS     = 5_000
	weatherRetryMaxAttempts = 12

	timerClock        = 1
	timerWeather      = 2
	timerRetryWeather = 3

	menuRefresh = 1001
	menuReload  = 1002
	menuOpen    = 1003
	menuStartup = 1004
	menuExit    = 1005

	dtLeft       = 0x0000
	dtCenter     = 0x0001
	dtRight      = 0x0002
	dtVCenter    = 0x0004
	dtWordBreak  = 0x0010
	dtSingleLine = 0x0020
	dtCalcRect   = 0x0400
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
	procCreateMutex            = kernel32.NewProc("CreateMutexW")
	procCloseHandle            = kernel32.NewProc("CloseHandle")
	procFreeConsole            = kernel32.NewProc("FreeConsole")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procCreateFont             = gdi32.NewProc("CreateFontW")
	procCreatePen              = gdi32.NewProc("CreatePen")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procSetMapMode             = gdi32.NewProc("SetMapMode")
	procSetWindowExtEx         = gdi32.NewProc("SetWindowExtEx")
	procSetViewportExtEx       = gdi32.NewProc("SetViewportExtEx")
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
	configMu               sync.RWMutex
	currentCfg             appConfig
	configPath             string
	weatherMu              sync.RWMutex
	currentWeather         weatherReport
	weatherError           string
	weatherBusy            atomic.Bool
	client                 = newWeatherClient()
	backgroundBrush        uintptr
	fontClock              uintptr
	fontDate               uintptr
	fontWeather            uintptr
	fontDetails            uintptr
	fontPrimaryLabel       uintptr
	fontSecondaryLabel     uintptr
	fontPrimaryDescription uintptr
	fontMetricLabel        uintptr
	fontPrimaryValue       uintptr
	fontSecondaryValue     uintptr
	fontSmall              uintptr
	fontVersion            uintptr
	contextMenuVisible     bool
	weatherRetryAttempts   int
)

func main() {
	// Keep the widget free of a console window even when it was built without
	// the windowsgui linker flag (for example, with `go run .`).
	procFreeConsole.Call()
	runtime.LockOSThread()
	mutexHandle, alreadyRunning, err := acquireSingleInstanceMutex(singleInstanceMutexName)
	if err != nil {
		showMessage("Sidebox", err.Error())
		return
	}
	if alreadyRunning {
		return
	}
	defer procCloseHandle.Call(mutexHandle)

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
		wsPopup|wsThickFrame,
		uintptr(cfg.WindowX), uintptr(cfg.WindowY), uintptr(cfg.WindowWidth), uintptr(cfg.WindowHeight),
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		showMessage("Sidebox", "ウィンドウを作成できません: "+callErr.Error())
		return
	}
	applyWindowOptions(hwnd, cfg)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	startWeatherRetries(hwnd)

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

func acquireSingleInstanceMutex(name string) (handle uintptr, alreadyRunning bool, err error) {
	handle, _, callErr := procCreateMutex.Call(
		0,
		0,
		uintptr(unsafe.Pointer(utf16Ptr(name))),
	)
	if handle == 0 {
		return 0, false, fmt.Errorf("二重起動チェックを初期化できません: %w", callErr)
	}
	if errors.Is(callErr, syscall.Errno(errorAlreadyExists)) {
		procCloseHandle.Call(handle)
		return 0, true, nil
	}
	return handle, false, nil
}

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmNCCalcSize:
		return 0
	case wmNCHitTest:
		var bounds rect
		if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds))); ok != 0 {
			x := int32(int16(lParam & 0xffff))
			y := int32(int16((lParam >> 16) & 0xffff))
			return resizeHitTest(bounds, x, y)
		}
		return htClient
	case wmCreate:
		createDrawingResources()
		procSetTimer.Call(hwnd, timerClock, 1000, 0)
		cfg := configSnapshot()
		procSetTimer.Call(hwnd, timerWeather, uintptr(cfg.RefreshMinutes*60*1000), 0)
		return 0
	case wmSize:
		width := int32(uint16(lParam & 0xffff))
		height := int32(uint16((lParam >> 16) & 0xffff))
		if width > 0 && height > 0 {
			updateWindowRegion(hwnd, width, height)
		}
		return 0
	case wmTimer:
		switch wParam {
		case timerWeather:
			refreshWeather(hwnd)
		case timerRetryWeather:
			if refreshWeather(hwnd) {
				weatherRetryAttempts++
				if weatherRetryAttempts >= weatherRetryMaxAttempts {
					procKillTimer.Call(hwnd, timerRetryWeather)
				}
			}
		}
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmAppWeatherReady:
		if wParam != 0 {
			procKillTimer.Call(hwnd, timerRetryWeather)
			weatherRetryAttempts = 0
		}
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmPowerBroadcast:
		if isResumePowerEvent(wParam) {
			startWeatherRetries(hwnd)
			return 1
		}
	case wmPaint:
		paintWindow(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmLButtonDown:
		x := int32(int16(lParam & 0xffff))
		y := int32(int16((lParam >> 16) & 0xffff))
		x, y = logicalClientPoint(hwnd, x, y)
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
	case wmExitSizeMove:
		saveWindowPosition(hwnd)
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		saveWindowPosition(hwnd)
		procKillTimer.Call(hwnd, timerClock)
		procKillTimer.Call(hwnd, timerWeather)
		procKillTimer.Call(hwnd, timerRetryWeather)
		deleteDrawingResources()
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func isResumePowerEvent(event uintptr) bool {
	return event == pbtApmResumeSuspend || event == pbtApmResumeAutomatic
}

func startWeatherRetries(hwnd uintptr) {
	weatherRetryAttempts = 0
	procKillTimer.Call(hwnd, timerRetryWeather)
	if refreshWeather(hwnd) {
		weatherRetryAttempts++
	}
	procSetTimer.Call(hwnd, timerRetryWeather, weatherRetryDelayMS, 0)
}

func refreshWeather(hwnd uintptr) bool {
	if !weatherBusy.CompareAndSwap(false, true) {
		return false
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
		var succeeded uintptr
		if err == nil {
			succeeded = 1
		}
		procPostMessage.Call(hwnd, wmAppWeatherReady, succeeded, 0)
	}()
	return true
}

func paintWindow(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var physicalBounds rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&physicalBounds)))
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	bitmap, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(physicalBounds.Right), uintptr(physicalBounds.Bottom))
	oldBitmap, _, _ := procSelectObject.Call(memDC, bitmap)
	defer func() {
		procSelectObject.Call(memDC, oldBitmap)
		procDeleteObject.Call(bitmap)
		procDeleteDC.Call(memDC)
	}()
	procSetMapMode.Call(memDC, mmAnisotropic)
	procSetWindowExtEx.Call(memDC, windowWidth, windowHeight, 0)
	procSetViewportExtEx.Call(memDC, uintptr(physicalBounds.Right), uintptr(physicalBounds.Bottom), 0)
	bounds := rect{0, 0, windowWidth, windowHeight}
	procFillRect.Call(memDC, uintptr(unsafe.Pointer(&bounds)), backgroundBrush)
	procSetBkMode.Call(memDC, transparent)
	drawRoundedPanel(memDC, rect{1, 1, bounds.Right - 1, bounds.Bottom - 1}, 22, rgb(14, 19, 28), rgb(45, 49, 57), 1)

	now := time.Now()
	date := fmt.Sprintf("%d月%d日 %s曜日", now.Month(), now.Day(), japaneseWeekday(now.Weekday()))
	drawText(memDC, appName+" "+appVersion, rect{34, 7, 254, 22}, fontVersion, rgb(122, 122, 122), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	drawText(memDC, now.Format("15:04:05"), rect{30, 24, 300, 92}, fontClock, rgb(255, 255, 255), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	drawText(memDC, date, rect{34, 90, 294, 117}, fontDate, rgb(173, 173, 173), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	drawText(memDC, "×", rect{bounds.Right - 42, 8, bounds.Right - 10, 38}, fontWeather, rgb(184, 184, 184), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)

	weatherMu.RLock()
	report, weatherErr := currentWeather, weatherError
	weatherMu.RUnlock()
	const weatherX = int32(350)
	if report.Location != "" {
		drawText(memDC, report.Location, rect{weatherX, 32, bounds.Right - 52, 60}, fontWeather, rgb(255, 255, 255), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	}
	drawText(memDC, "気象庁  3日間予報", rect{weatherX, 65, bounds.Right - 52, 86}, fontDetails, rgb(133, 133, 133), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	if weatherBusy.Load() && len(report.Daily) > 0 {
		drawText(memDC, "更新中…", rect{weatherX, 88, bounds.Right - 52, 107}, fontSmall, rgb(115, 189, 255), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	}
	if weatherErr != "" {
		errorTitle := "天気を取得できません"
		if weatherRetryAttempts > 0 && weatherRetryAttempts < weatherRetryMaxAttempts {
			errorTitle = fmt.Sprintf("天気を取得できません（再試行中 %d/%d）", weatherRetryAttempts, weatherRetryMaxAttempts)
		}
		status := errorTitle + ": " + weatherErr
		statusBounds := rect{weatherX, 88, bounds.Right - 52, 119}
		status = fitTextWithEllipsis(memDC, status, statusBounds, fontDetails)
		drawText(memDC, status, statusBounds, fontDetails, rgb(255, 140, 140), dtLeft|dtWordBreak|dtNoPrefix)
	} else if report.Location == "" {
		drawText(memDC, "天気を取得しています…", rect{weatherX, 88, bounds.Right - 52, 119}, fontDetails, rgb(184, 184, 184), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	}
	if len(report.Daily) > 0 {
		drawForecastCards(memDC, bounds, report)
	}
	if contextMenuVisible {
		drawContextMenu(memDC)
	}
	procSetMapMode.Call(memDC, mmText)
	procBitBlt.Call(hdc, 0, 0, uintptr(physicalBounds.Right), uintptr(physicalBounds.Bottom), memDC, 0, 0, srccopy)
}

func drawForecastCards(hdc uintptr, bounds rect, report weatherReport) {
	areas := forecastCardRects(bounds)
	count := min(len(report.Daily), len(areas))
	for index := 0; index < count; index++ {
		var humidity *int
		if index == 0 {
			humidity = report.Humidity
		}
		drawForecastCard(hdc, report.Daily[index], areas[index], index == 0, humidity)
	}
}

func forecastCardRects(bounds rect) [3]rect {
	const (
		left     = int32(24)
		gap      = int32(12)
		cardsTop = int32(127)
	)
	contentWidth := bounds.Right - left*2
	usableWidth := contentWidth - gap*2
	todayWidth := usableWidth * 42 / 100
	futureWidth := (usableWidth - todayWidth) / 2
	cardHeight := max(int32(235), bounds.Bottom-cardsTop-24)
	return [3]rect{
		{left, cardsTop, left + todayWidth, cardsTop + cardHeight},
		{left + todayWidth + gap, cardsTop, left + todayWidth + gap + futureWidth, cardsTop + cardHeight},
		{left + todayWidth + gap*2 + futureWidth, cardsTop, bounds.Right - left, cardsTop + cardHeight},
	}
}

func drawForecastCard(hdc uintptr, forecast dailyForecast, bounds rect, primary bool, humidity *int) {
	fillColor := rgb(23, 29, 41)
	borderColor := rgb(42, 47, 58)
	borderWidth := int32(1)
	labelFont := fontSecondaryLabel
	descriptionFont := fontDetails
	valueFont := fontSecondaryValue
	labelColor := rgb(191, 191, 191)
	if primary {
		labelFont = fontPrimaryLabel
		descriptionFont = fontPrimaryDescription
		valueFont = fontPrimaryValue
		labelColor = rgb(156, 209, 255)
	}
	drawRoundedPanel(hdc, bounds, 16, fillColor, borderColor, borderWidth)

	drawText(hdc, forecast.DateLabel, rect{bounds.Left + 18, bounds.Top + 14, bounds.Right - 18, bounds.Top + 38}, labelFont, labelColor, dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	drawWeatherIcon(hdc, (bounds.Left+bounds.Right)/2, bounds.Top+71, weatherIconForDescription(forecast.Description))
	descriptionBounds := rect{bounds.Left + 18, bounds.Top + 101, bounds.Right - 18, bounds.Top + 166}
	description := fitTextWithEllipsis(hdc, forecast.Description, descriptionBounds, descriptionFont)
	drawText(hdc, description, descriptionBounds, descriptionFont, rgb(237, 237, 237), dtLeft|dtWordBreak|dtNoPrefix)

	divider := rect{bounds.Left + 16, bounds.Top + 174, bounds.Right - 16, bounds.Top + 175}
	dividerBrush, _, _ := procCreateSolidBrush.Call(rgb(47, 56, 68))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&divider)), dividerBrush)
	procDeleteObject.Call(dividerBrush)

	metricCount := int32(3)
	if primary {
		metricCount = 4
	}
	metricsLeft := bounds.Left + 10
	metricWidth := (bounds.Right - bounds.Left - 20) / metricCount
	metricTop := bounds.Top + 185
	drawForecastMetric(hdc, "最高", formatDegree(forecast.TemperatureMax), rect{metricsLeft, metricTop, metricsLeft + metricWidth, metricTop + 48}, rgb(255, 133, 97), valueFont)
	drawForecastMetric(hdc, "最低", formatDegree(forecast.TemperatureMin), rect{metricsLeft + metricWidth, metricTop, metricsLeft + metricWidth*2, metricTop + 48}, rgb(102, 184, 255), valueFont)
	drawForecastMetric(hdc, "降水", formatPercent(forecast.PrecipitationProbability), rect{metricsLeft + metricWidth*2, metricTop, metricsLeft + metricWidth*3, metricTop + 48}, rgb(87, 204, 255), valueFont)
	if primary {
		drawForecastMetric(hdc, "湿度", formatPercent(humidity), rect{metricsLeft + metricWidth*3, metricTop, bounds.Right - 10, metricTop + 48}, rgb(110, 224, 194), valueFont)
	}

	if forecast.Wind != "" {
		windBounds := rect{bounds.Left + 18, bounds.Bottom - 32, bounds.Right - 18, bounds.Bottom - 14}
		wind := fitSingleLineTextWithEllipsis(hdc, "風  "+forecast.Wind, windBounds, fontSmall)
		drawText(hdc, wind, windBounds, fontSmall, rgb(122, 122, 122), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	}
}

func drawForecastMetric(hdc uintptr, label, value string, bounds rect, color, valueFont uintptr) {
	drawText(hdc, label, rect{bounds.Left, bounds.Top, bounds.Right, bounds.Top + 18}, fontMetricLabel, rgb(143, 143, 143), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	drawText(hdc, value, rect{bounds.Left, bounds.Top + 18, bounds.Right, bounds.Top + 48}, valueFont, color, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
}

func drawRoundedPanel(hdc uintptr, bounds rect, radius int32, fillColor, borderColor uintptr, borderWidth int32) {
	pen, _, _ := procCreatePen.Call(0, uintptr(borderWidth), borderColor)
	brush, _, _ := procCreateSolidBrush.Call(fillColor)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	procRoundRect.Call(hdc, uintptr(bounds.Left), uintptr(bounds.Top), uintptr(bounds.Right), uintptr(bounds.Bottom), uintptr(radius*2), uintptr(radius*2))
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func fitTextWithEllipsis(hdc uintptr, value string, bounds rect, font uintptr) string {
	if textFits(hdc, value, bounds, font) {
		return value
	}
	runes := []rune(value)
	low, high := 0, len(runes)
	for low < high {
		middle := (low + high + 1) / 2
		candidate := string(runes[:middle]) + "…"
		if textFits(hdc, candidate, bounds, font) {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return string(runes[:low]) + "…"
}

func fitSingleLineTextWithEllipsis(hdc uintptr, value string, bounds rect, font uintptr) string {
	if singleLineTextFits(hdc, value, bounds, font) {
		return value
	}
	runes := []rune(value)
	low, high := 0, len(runes)
	for low < high {
		middle := (low + high + 1) / 2
		candidate := string(runes[:middle]) + "…"
		if singleLineTextFits(hdc, candidate, bounds, font) {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return string(runes[:low]) + "…"
}

func singleLineTextFits(hdc uintptr, value string, bounds rect, font uintptr) bool {
	width := bounds.Right - bounds.Left
	if width <= 0 {
		return false
	}
	measurement := rect{}
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	chars := syscall.StringToUTF16(value)
	procDrawText.Call(
		hdc,
		uintptr(unsafe.Pointer(&chars[0])),
		uintptr(len(chars)-1),
		uintptr(unsafe.Pointer(&measurement)),
		dtCalcRect|dtSingleLine|dtNoPrefix,
	)
	procSelectObject.Call(hdc, oldFont)
	return measurement.Right-measurement.Left <= width
}

func textFits(hdc uintptr, value string, bounds rect, font uintptr) bool {
	width := bounds.Right - bounds.Left
	height := bounds.Bottom - bounds.Top
	if width <= 0 || height <= 0 {
		return false
	}
	measurement := rect{0, 0, width, 0}
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	chars := syscall.StringToUTF16(value)
	measuredHeight, _, _ := procDrawText.Call(
		hdc,
		uintptr(unsafe.Pointer(&chars[0])),
		uintptr(len(chars)-1),
		uintptr(unsafe.Pointer(&measurement)),
		dtCalcRect|dtWordBreak|dtNoPrefix,
	)
	procSelectObject.Call(hdc, oldFont)
	return int32(measuredHeight) <= height
}

func formatTemperature(value *float64) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.0f°C", *value)
}

func formatDegree(value *float64) string {
	if value == nil {
		return "--°"
	}
	return fmt.Sprintf("%.0f°", *value)
}

func formatPercent(value *int) string {
	if value == nil {
		return "--%"
	}
	return fmt.Sprintf("%d%%", *value)
}

func formatRainChance(value *int) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%d%%", *value)
}

func formatHumidity(value *int) string {
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
	backgroundBrush, _, _ = procCreateSolidBrush.Call(rgb(14, 19, 28))
	fontClock = createFont(50, 300, "Consolas")
	fontDate = createFont(17, 500, "Yu Gothic UI")
	fontWeather = createFont(21, 600, "Yu Gothic UI")
	fontDetails = createFont(14, 500, "Yu Gothic UI")
	fontPrimaryLabel = createFont(17, 600, "Yu Gothic UI")
	fontSecondaryLabel = createFont(15, 600, "Yu Gothic UI")
	fontPrimaryDescription = createFont(16, 500, "Yu Gothic UI")
	fontMetricLabel = createFont(11, 500, "Yu Gothic UI")
	fontPrimaryValue = createFont(22, 600, "Yu Gothic UI")
	fontSecondaryValue = createFont(20, 600, "Yu Gothic UI")
	fontSmall = createFont(11, 400, "Yu Gothic UI")
	fontVersion = createFont(10, 500, "Yu Gothic UI")
}

func deleteDrawingResources() {
	for _, object := range []uintptr{
		backgroundBrush,
		fontClock,
		fontDate,
		fontWeather,
		fontDetails,
		fontPrimaryLabel,
		fontSecondaryLabel,
		fontPrimaryDescription,
		fontMetricLabel,
		fontPrimaryValue,
		fontSecondaryValue,
		fontSmall,
		fontVersion,
	} {
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
		procKillTimer.Call(hwnd, timerWeather)
		procSetTimer.Call(hwnd, timerWeather, uintptr(cfg.RefreshMinutes*60*1000), 0)
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
	procSetWindowPos.Call(hwnd, insertAfter, 0, 0, uintptr(cfg.WindowWidth), uintptr(cfg.WindowHeight), swpNoMove|swpNoActivate)
	alpha := byte(cfg.Opacity*255 + 0.5)
	procSetLayeredWindowAttrs.Call(hwnd, 0, uintptr(alpha), lwaAlpha)
	updateWindowRegion(hwnd, cfg.WindowWidth, cfg.WindowHeight)
	procInvalidateRect.Call(hwnd, 0, 0)
}

func updateWindowRegion(hwnd uintptr, width, height int32) {
	region, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(width+1), uintptr(height+1), 24, 24)
	procSetWindowRgn.Call(hwnd, region, 1)
}

func logicalClientPoint(hwnd uintptr, x, y int32) (int32, int32) {
	var bounds rect
	if ok, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds))); ok == 0 || bounds.Right <= 0 || bounds.Bottom <= 0 {
		return x, y
	}
	return int32(int64(x) * int64(windowWidth) / int64(bounds.Right)), int32(int64(y) * int64(windowHeight) / int64(bounds.Bottom))
}

func resizeHitTest(bounds rect, x, y int32) uintptr {
	left := x >= bounds.Left && x < bounds.Left+resizeBorderWidth
	right := x < bounds.Right && x >= bounds.Right-resizeBorderWidth
	top := y >= bounds.Top && y < bounds.Top+resizeBorderWidth
	bottom := y < bounds.Bottom && y >= bounds.Bottom-resizeBorderWidth
	switch {
	case top && left:
		return htTopLeft
	case top && right:
		return htTopRight
	case bottom && left:
		return htBottomLeft
	case bottom && right:
		return htBottomRight
	case left:
		return htLeft
	case right:
		return htRight
	case top:
		return htTop
	case bottom:
		return htBottom
	default:
		return htClient
	}
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
	cfg = withWindowBounds(cfg, bounds.Left, bounds.Top, bounds.Right-bounds.Left, bounds.Bottom-bounds.Top)
	if err := writeConfig(configPath, cfg); err != nil {
		return
	}
	configMu.Lock()
	currentCfg.WindowX, currentCfg.WindowY = cfg.WindowX, cfg.WindowY
	currentCfg.WindowWidth, currentCfg.WindowHeight = cfg.WindowWidth, cfg.WindowHeight
	configMu.Unlock()
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

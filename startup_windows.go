//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	hkeyCurrentUser   = 0x80000001
	keySetValue       = 0x0002
	regSZ             = 1
	errorFileNotFound = 2
	startupKeyPath    = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupValueName  = "Sidebox"
)

var (
	advapi32           = syscall.NewLazyDLL("advapi32.dll")
	procRegCreateKeyEx = advapi32.NewProc("RegCreateKeyExW")
	procRegOpenKeyEx   = advapi32.NewProc("RegOpenKeyExW")
	procRegSetValueEx  = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValue = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey    = advapi32.NewProc("RegCloseKey")
)

func syncStartupRegistration(enabled bool) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("実行ファイルの場所を取得できません: %w", err)
	}
	return setStartupRegistration(enabled, executable)
}

func setStartupRegistration(enabled bool, executable string) error {
	var key uintptr
	keyPath := uintptr(unsafe.Pointer(utf16Ptr(startupKeyPath)))
	var status uintptr
	if enabled {
		var disposition uint32
		status, _, _ = procRegCreateKeyEx.Call(
			hkeyCurrentUser, keyPath, 0, 0, 0, keySetValue, 0,
			uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&disposition)),
		)
	} else {
		status, _, _ = procRegOpenKeyEx.Call(
			hkeyCurrentUser, keyPath, 0, keySetValue,
			uintptr(unsafe.Pointer(&key)),
		)
	}
	if status != 0 {
		if !enabled && status == errorFileNotFound {
			return nil
		}
		return fmt.Errorf("自動起動設定を開けません (Windows error %d)", status)
	}
	defer procRegCloseKey.Call(key)

	valueName := utf16Ptr(startupValueName)
	if !enabled {
		status, _, _ = procRegDeleteValue.Call(key, uintptr(unsafe.Pointer(valueName)))
		if status == 0 || status == errorFileNotFound {
			return nil
		}
		return fmt.Errorf("自動起動設定を解除できません (Windows error %d)", status)
	}

	command := syscall.StringToUTF16(startupCommand(executable))
	status, _, _ = procRegSetValueEx.Call(
		key,
		uintptr(unsafe.Pointer(valueName)),
		0,
		regSZ,
		uintptr(unsafe.Pointer(&command[0])),
		uintptr(len(command)*2),
	)
	if status != 0 {
		return fmt.Errorf("自動起動設定を保存できません (Windows error %d)", status)
	}
	return nil
}

func startupCommand(executable string) string {
	return `"` + executable + `"`
}

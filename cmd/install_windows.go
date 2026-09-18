//go:build windows

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func addDirToWindowsUserPath(targetDir string) (bool, error) {
	cleanTarget, err := filepath.Abs(targetDir)
	if err != nil {
		cleanTarget = filepath.Clean(targetDir)
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()

	val, valType, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		val, valType, err = k.GetStringValue("PATH")
	}
	if err != nil && err != registry.ErrNotExist {
		return false, err
	}

	// Check if already present in User PATH
	for _, p := range strings.Split(val, ";") {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		cleanP, err := filepath.Abs(trimmed)
		if err != nil {
			cleanP = filepath.Clean(trimmed)
		}
		if strings.EqualFold(cleanP, cleanTarget) {
			return false, nil
		}
	}

	newPath := val
	if newPath != "" && !strings.HasSuffix(newPath, ";") {
		newPath += ";"
	}
	newPath += cleanTarget

	if valType == 0 {
		valType = registry.EXPAND_SZ
	}

	if valType == registry.EXPAND_SZ {
		if err := k.SetExpandStringValue("Path", newPath); err != nil {
			return false, err
		}
	} else {
		if err := k.SetStringValue("Path", newPath); err != nil {
			return false, err
		}
	}

	// Update PATH in current process
	currPath := os.Getenv("PATH")
	if currPath != "" && !strings.HasSuffix(currPath, ";") {
		currPath += ";"
	}
	_ = os.Setenv("PATH", currPath+cleanTarget)

	// Broadcast WM_SETTINGCHANGE so shells and Explorer pick up the change
	notifyEnvironmentChange()

	return true, nil
}

func notifyEnvironmentChange() {
	const (
		HWND_BROADCAST   = 0xffff
		WM_SETTINGCHANGE = 0x001a
		SMTO_ABORTIFHUNG = 0x0002
	)

	moduser32 := windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeout := moduser32.NewProc("SendMessageTimeoutW")

	envPtr, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}

	var result uintptr
	_, _, _ = procSendMessageTimeout.Call(
		HWND_BROADCAST,
		WM_SETTINGCHANGE,
		0,
		uintptr(unsafe.Pointer(envPtr)),
		SMTO_ABORTIFHUNG,
		5000,
		uintptr(unsafe.Pointer(&result)),
	)
}

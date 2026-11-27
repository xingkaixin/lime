//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"lime/locales"
)

const cliSupported = true

var (
	user32DLL           = windows.NewLazySystemDLL("user32.dll")
	sendMessageTimeoutW = user32DLL.NewProc("SendMessageTimeoutW")
)

func installCLIPlatform() (string, error) {
	resDir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return "", err
	}
	binDir := filepath.Join(resDir, "bin")
	launcher := filepath.Join(binDir, "lime.cmd")
	if info, err := os.Stat(launcher); err != nil || info.IsDir() {
		return "", fmt.Errorf("CLI launcher not found at %s", launcher)
	}
	if err := addWindowsUserPath(binDir); err != nil {
		return "", err
	}
	return locales.DialogCLIInstalledWinDetail(binDir), nil
}

func addWindowsUserPath(dir string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	current, _, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	next, changed := appendWindowsPath(current, dir)
	if !changed {
		return nil
	}
	if err := key.SetExpandStringValue("Path", next); err != nil {
		return err
	}
	broadcastEnvironmentChange()
	return nil
}

func broadcastEnvironmentChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	_, _, _ = sendMessageTimeoutW.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung,
		5000,
		uintptr(unsafe.Pointer(&result)),
	)
}

//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"

	"lime/locales"
)

const cliSupported = true

const darwinCLITarget = "/usr/local/bin/lime"

func installCLIPlatform() (string, error) {
	resDir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return "", err
	}
	src := filepath.Join(resDir, "bin", "lime")
	if info, err := os.Stat(src); err != nil || info.IsDir() {
		return "", fmt.Errorf("CLI launcher not found at %s", src)
	}
	if err := installCLIDarwin(src, darwinCLITarget, runElevatedSymlink); err != nil {
		return "", err
	}
	return locales.DialogCLIInstalledMacDetail(darwinCLITarget), nil
}

func installCLIDarwin(src, dst string, elevate func(src, dst string) error) error {
	if info, err := os.Lstat(dst); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s already exists and is not a symbolic link", dst)
		}
		if target, err := os.Readlink(dst); err == nil && target == src {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) && !os.IsPermission(err) {
		return err
	}

	if err := symlinkDirect(src, dst); err == nil {
		return nil
	} else if !os.IsPermission(err) {
		return err
	}
	return elevate(src, dst)
}

func symlinkDirect(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Symlink(src, dst)
}

func runElevatedSymlink(src, dst string) error {
	script := `on run argv
set src to item 1 of argv
set dst to item 2 of argv
set dir to item 3 of argv
do shell script "mkdir -p " & quoted form of dir & " && ln -sfn " & quoted form of src & " " & quoted form of dst with administrator privileges
end run`
	out, err := exec.Command("osascript", "-e", script, "--", src, dst, filepath.Dir(dst)).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "(-128)") || strings.Contains(strings.ToLower(msg), "user canceled") {
			return errCLICanceled
		}
		if msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

//go:build darwin

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/egoist/mygo"

	"lime/locales"
)

const cliSupported = true

const darwinCLITarget = "/usr/local/bin/lime"

func launchCLI(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	wd, _ := os.Getwd()
	return launchCLIDarwin(exe, wd, args, darwinBundleRunning, openDarwinBundle, startDetachedDarwin)
}

func launchCLIDarwin(
	exe, wd string,
	args []string,
	isRunning func(bundle string) bool,
	openBundle func(bundle, wd string, args []string) error,
	startDetached func(exe, wd string, args []string) error,
) error {
	if bundle, ok := darwinAppBundle(exe); ok {
		// Use LaunchServices for bare invocations and cold starts so the app
		// runs under launchd with no controlling TTY; warm invocations with
		// args forward via a detached process to avoid `open -n` Dock flicker.
		if len(args) == 0 || !isRunning(bundle) {
			return openBundle(bundle, wd, args)
		}
	}
	return startDetached(exe, wd, args)
}

func darwinAppBundle(exe string) (string, bool) {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	macosDir := filepath.Dir(exe)
	if filepath.Base(macosDir) != "MacOS" {
		return "", false
	}
	contentsDir := filepath.Dir(macosDir)
	if filepath.Base(contentsDir) != "Contents" {
		return "", false
	}
	if info, err := os.Stat(filepath.Join(contentsDir, "Info.plist")); err != nil || info.IsDir() {
		return "", false
	}
	bundle := filepath.Dir(contentsDir)
	if !strings.HasSuffix(bundle, ".app") {
		return "", false
	}
	return bundle, true
}

func darwinBundleRunning(bundle string) bool {
	out, err := exec.Command("/usr/bin/lsappinfo", "find", "bundlepath="+bundle).Output()
	return err == nil && len(bytes.TrimSpace(out)) > 0
}

func openDarwinBundle(bundle, wd string, args []string) error {
	openArgs := []string{"-a", bundle}
	if len(args) > 0 {
		openArgs = append(openArgs, "--args")
		if wd != "" {
			openArgs = append(openArgs, "--cwd", wd)
		}
		openArgs = append(openArgs, args...)
	}
	cmd := exec.Command("/usr/bin/open", openArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

func startDetachedDarwin(exe, wd string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = wd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

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

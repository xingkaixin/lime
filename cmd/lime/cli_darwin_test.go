//go:build darwin

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallCLIDarwin(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Lime.app", "Contents", "Resources", "bin", "lime")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "bin", "lime")

	elevated := false
	elevate := func(_, _ string) error {
		elevated = true
		return nil
	}

	// Fresh install creates symlink directly without elevation.
	if err := installCLIDarwin(src, dst, elevate); err != nil {
		t.Fatalf("fresh install: %v", err)
	}
	if elevated {
		t.Fatal("expected direct install without elevation")
	}
	if target, err := os.Readlink(dst); err != nil || target != src {
		t.Fatalf("symlink target = %q (err=%v), want %q", target, err, src)
	}

	// Re-installing the same target is a no-op.
	if err := installCLIDarwin(src, dst, elevate); err != nil {
		t.Fatalf("idempotent install: %v", err)
	}

	// Updating an existing symlink pointing elsewhere replaces it.
	other := filepath.Join(dir, "old-lime")
	if err := os.Remove(dst); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, dst); err != nil {
		t.Fatal(err)
	}
	if err := installCLIDarwin(src, dst, elevate); err != nil {
		t.Fatalf("replace stale symlink: %v", err)
	}
	if target, err := os.Readlink(dst); err != nil || target != src {
		t.Fatalf("replaced target = %q, want %q", target, src)
	}

	// Refuses to overwrite a non-symlink file.
	regular := filepath.Join(dir, "bin", "regular")
	if err := os.WriteFile(regular, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installCLIDarwin(src, regular, elevate); err == nil {
		t.Fatal("expected error when target is a regular file")
	}

	// Falls back to elevation when destination directory is read-only.
	roDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(roDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })
	roDst := filepath.Join(roDir, "lime")
	if err := installCLIDarwin(src, roDst, func(gotSrc, gotDst string) error {
		elevated = true
		if gotSrc != src || gotDst != roDst {
			t.Fatalf("elevate args = (%q, %q), want (%q, %q)", gotSrc, gotDst, src, roDst)
		}
		return errCLICanceled
	}); !errors.Is(err, errCLICanceled) {
		t.Fatalf("elevated cancel: err = %v, want errCLICanceled", err)
	}
	if !elevated {
		t.Fatal("expected elevate fallback on read-only dir")
	}
}

func TestDarwinLauncherSymlinkAndArgs(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "My Apps", "Lime.app", "Contents")
	binDir := filepath.Join(app, "Resources", "bin")
	macosDir := filepath.Join(app, "MacOS")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		t.Fatal(err)
	}

	launcherSrc, err := os.ReadFile(filepath.Join("..", "..", "resources", "darwin", "bin", "lime"))
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(binDir, "lime")
	if err := os.WriteFile(launcher, launcherSrc, 0o755); err != nil {
		t.Fatal(err)
	}

	outFile := filepath.Join(dir, "args.txt")
	fakeExe := filepath.Join(macosDir, "Lime")
	fakeScript := "#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" > \"" + outFile + "\"\n"
	if err := os.WriteFile(fakeExe, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "lime")
	if err := os.Symlink(launcher, link); err != nil {
		t.Fatal(err)
	}

	workDir := filepath.Join(dir, "work dir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(link, "-n", "file with space.txt")
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("symlink launch failed: %v\n%s", err, out)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(outFile); err == nil && len(b) > 0 {
			got = string(b)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("launcher forwarded %q, want 4 lines", got)
	}
	gotWork, err := filepath.EvalSymlinks(lines[0])
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", lines[0], err)
	}
	wantWork, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", workDir, err)
	}
	if gotWork != wantWork || lines[1] != "--cli" || lines[2] != "-n" || lines[3] != "file with space.txt" {
		t.Fatalf("launcher forwarded wd=%q args=%q, want wd=%q args=%q", gotWork, lines[1:], wantWork, []string{"--cli", "-n", "file with space.txt"})
	}
}

func TestLaunchCLIDarwinRouting(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Lime.app")
	contents := filepath.Join(bundle, "Contents")
	exe := filepath.Join(contents, "MacOS", "Lime")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	realBundle, err := filepath.EvalSymlinks(bundle)
	if err != nil {
		t.Fatal(err)
	}

	var openedBundle, openedWD string
	var openedArgs []string
	var detachedExe, detachedWD string
	var detachedArgs []string
	reset := func() {
		openedBundle, openedWD, openedArgs = "", "", nil
		detachedExe, detachedWD, detachedArgs = "", "", nil
	}
	openFn := func(b, wd string, args []string) error {
		openedBundle, openedWD, openedArgs = b, wd, append([]string(nil), args...)
		return nil
	}
	detachFn := func(e, wd string, args []string) error {
		detachedExe, detachedWD, detachedArgs = e, wd, append([]string(nil), args...)
		return nil
	}

	// Cold start with args launches via LaunchServices.
	if err := launchCLIDarwin(exe, "/work", []string{"."}, func(string) bool { return false }, openFn, detachFn); err != nil {
		t.Fatalf("cold start: %v", err)
	}
	if openedBundle != realBundle || openedWD != "/work" || len(openedArgs) != 1 || openedArgs[0] != "." || detachedExe != "" {
		t.Fatalf("cold start got open=(%q,%q,%q) detach=%q", openedBundle, openedWD, openedArgs, detachedExe)
	}

	// Bare invocation uses LaunchServices even when already running.
	reset()
	if err := launchCLIDarwin(exe, "/work", nil, func(string) bool { return true }, openFn, detachFn); err != nil {
		t.Fatalf("bare warm start: %v", err)
	}
	if openedBundle != realBundle || len(openedArgs) != 0 || detachedExe != "" {
		t.Fatalf("bare warm start got open=(%q,%q) detach=%q", openedBundle, openedArgs, detachedExe)
	}

	// Warm start with args forwards via detached process to avoid `open -n` Dock flicker.
	reset()
	if err := launchCLIDarwin(exe, "/work", []string{"-n", "."}, func(string) bool { return true }, openFn, detachFn); err != nil {
		t.Fatalf("warm start with args: %v", err)
	}
	if openedBundle != "" || detachedExe != exe || detachedWD != "/work" || len(detachedArgs) != 2 || detachedArgs[0] != "-n" || detachedArgs[1] != "." {
		t.Fatalf("warm start got open=%q detach=(%q,%q,%q)", openedBundle, detachedExe, detachedWD, detachedArgs)
	}

	// Standalone binary outside an .app bundle always launches detached.
	reset()
	standalone := filepath.Join(dir, "lime-dev")
	if err := os.WriteFile(standalone, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := launchCLIDarwin(standalone, "/work", []string{"."}, func(string) bool { return false }, openFn, detachFn); err != nil {
		t.Fatalf("standalone: %v", err)
	}
	if openedBundle != "" || detachedExe != standalone {
		t.Fatalf("standalone got open=%q detach=%q", openedBundle, detachedExe)
	}
}

func TestStartDetachedDarwinDetachesSession(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "proc.txt")
	exe := filepath.Join(dir, "fake-lime")
	script := "#!/bin/sh\n" +
		"ps -o pid=,pgid=,tty= -p $$ > \"" + outFile + "\"\n" +
		"sleep 2\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := startDetachedDarwin(exe, dir, nil); err != nil {
		t.Fatalf("startDetachedDarwin: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("startDetachedDarwin blocked for %v, expected immediate return", elapsed)
	}

	deadline := time.Now().Add(2 * time.Second)
	var fields []string
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(outFile); err == nil && len(strings.Fields(string(b))) == 3 {
			fields = strings.Fields(string(b))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(fields) != 3 {
		t.Fatalf("child did not write process info")
	}
	pid, pgid, tty := fields[0], fields[1], fields[2]
	if pid != pgid {
		t.Fatalf("child not session leader: pid=%s pgid=%s", pid, pgid)
	}
	if tty != "??" {
		t.Fatalf("child still attached to controlling tty: %s", tty)
	}
}

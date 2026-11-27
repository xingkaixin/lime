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
	if len(lines) != 3 {
		t.Fatalf("launcher forwarded %q, want 3 lines", got)
	}
	gotWork, err := filepath.EvalSymlinks(lines[0])
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", lines[0], err)
	}
	wantWork, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", workDir, err)
	}
	if gotWork != wantWork || lines[1] != "-n" || lines[2] != "file with space.txt" {
		t.Fatalf("launcher forwarded wd=%q args=%q, want wd=%q args=%q", gotWork, lines[1:], wantWork, []string{"-n", "file with space.txt"})
	}
}

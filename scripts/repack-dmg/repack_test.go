package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBinaryPlistWithBytes(t *testing.T) {
	raw := []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02}
	data := binaryPlist(map[string]any{
		"backgroundType":       2,
		"backgroundImageAlias": raw,
		"iconSize":             128.0,
		"arrangeBy":            "none",
		"labelOnBottom":        true,
	})
	if !bytes.HasPrefix(data, []byte("bplist00")) {
		t.Fatal("missing bplist00 header")
	}
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("plutil not available")
	}
	p := filepath.Join(t.TempDir(), "icvp.plist")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("plutil", "-convert", "xml1", "-o", "-", p).CombinedOutput()
	if err != nil {
		t.Fatalf("plutil failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"<key>backgroundType</key>\n\t<integer>2</integer>",
		"<key>backgroundImageAlias</key>\n\t<data>",
		"<real>128</real>",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("plist output missing %q:\n%s", want, out)
		}
	}
}

func TestCarbonAliasEncoding(t *testing.T) {
	created := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	alias := encodeCarbonAlias(aliasTarget{
		VolumeName:     "Lime",
		VolumeCreated:  created,
		FolderName:     ".background",
		FolderCNID:     42,
		FileName:       "background.tiff",
		FileCNID:       43,
		FileCreated:    created,
		POSIXRelPath:   "/.background/background.tiff",
		MountPointPath: "/Volumes/Lime",
	})

	if len(alias) <= aliasHeaderSize {
		t.Fatalf("alias too short: %d", len(alias))
	}
	be := binary.BigEndian
	if got := int(be.Uint16(alias[4:6])); got != len(alias) {
		t.Fatalf("recsize = %d, want %d", got, len(alias))
	}
	if got := be.Uint16(alias[6:8]); got != aliasVersion2 {
		t.Fatalf("version = %d, want %d", got, aliasVersion2)
	}
	if got := string(alias[11 : 11+alias[10]]); got != "Lime" {
		t.Fatalf("volume name = %q, want Lime", got)
	}
	if got := be.Uint32(alias[46:50]); got != 42 {
		t.Fatalf("folder CNID = %d, want 42", got)
	}
	if got := string(alias[51 : 51+alias[50]]); got != "background.tiff" {
		t.Fatalf("filename = %q, want background.tiff", got)
	}
	if got := be.Uint32(alias[114:118]); got != 43 {
		t.Fatalf("file CNID = %d, want 43", got)
	}

	// Walk TLV tags to verify framing and terminal -1 marker.
	off := aliasHeaderSize
	tags := map[uint16][]byte{}
	for off+4 <= len(alias) {
		tag := be.Uint16(alias[off : off+2])
		n := int(be.Uint16(alias[off+2 : off+4]))
		off += 4
		if tag == tagEnd {
			if n != 0 || off != len(alias) {
				t.Fatalf("unexpected end tag: len=%d off=%d total=%d", n, off, len(alias))
			}
			break
		}
		if off+n > len(alias) {
			t.Fatalf("tag %d overflows buffer", tag)
		}
		tags[tag] = alias[off : off+n]
		off += n
		if n%2 == 1 {
			off++
		}
	}
	if got := string(tags[tagPOSIXPath]); got != "/.background/background.tiff" {
		t.Fatalf("TAG_POSIX_PATH = %q", got)
	}
	if got := string(tags[tagVolumeMountPoint]); got != "/Volumes/Lime" {
		t.Fatalf("TAG_VOLUME_MOUNT_POINT = %q", got)
	}
	if got := string(tags[tagCarbonPath]); got != "Lime:.background:background.tiff" {
		t.Fatalf("TAG_CARBON_PATH = %q", got)
	}
}

func TestDSStoreWithBackgroundAlias(t *testing.T) {
	alias := encodeCarbonAlias(aliasTarget{
		VolumeName:     "Lime",
		FolderName:     ".background",
		FolderCNID:     17,
		FileName:       "background.tiff",
		FileCNID:       18,
		POSIXRelPath:   "/.background/background.tiff",
		MountPointPath: "/Volumes/Lime",
	})
	raw, err := dsStore(dmgRecords(dmgLayout{
		AppName:         "Lime.app",
		WindowX:         200,
		WindowY:         400,
		WindowWidth:     660,
		WindowHeight:    400,
		IconSize:        128,
		AppX:            180,
		AppY:            190,
		AppsX:           480,
		AppsY:           185,
		BackgroundAlias: alias,
	}))
	if err != nil {
		t.Fatalf("dsStore failed: %v", err)
	}
	if !bytes.Equal(raw[4:8], []byte("Bud1")) {
		t.Fatal("missing Bud1 magic")
	}
	if !bytes.Contains(raw, []byte("backgroundImageAlias")) {
		t.Fatal("DS_Store missing backgroundImageAlias key")
	}
}

func TestUpdateChecksums(t *testing.T) {
	dir := t.TempDir()
	dmg := filepath.Join(dir, "lime-0.1.0-arm64.dmg")
	if err := os.WriteFile(dmg, []byte("new-dmg-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	sums := filepath.Join(dir, "SHA256SUMS")
	initial := strings.Repeat("0", 64) + "  lime-0.1.0-arm64.dmg\n" +
		strings.Repeat("a", 64) + "  install.sh\n"
	if err := os.WriteFile(sums, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateChecksums(sums, []string{dmg}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(sums)
	if err != nil {
		t.Fatal(err)
	}
	wantHash, err := fileSHA256(dmg)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("a", 64) + "  install.sh\n" + wantHash + "  lime-0.1.0-arm64.dmg\n"
	if string(got) != want {
		t.Fatalf("SHA256SUMS =\n%s\nwant:\n%s", got, want)
	}
}

func TestRepackDMGMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("DMG repacking requires macOS")
	}
	dir := t.TempDir()

	// Create a minimal .app bundle.
	app := filepath.Join(dir, "Lime.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte("<plist version=\"1.0\"><dict/></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "MacOS", "Lime"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a 2x Retina PNG (1320x800 for a 660x400 window).
	bg := filepath.Join(dir, "background.png")
	img := image.NewRGBA(image.Rect(0, 0, 1320, 800))
	for y := range 800 {
		for x := range 1320 {
			img.SetRGBA(x, y, color.RGBA{R: 24, G: 28, B: 34, A: 255})
		}
	}
	f, err := os.Create(bg)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	outDMG := filepath.Join(dir, "lime-test.dmg")
	err = repackDMG(repackConfig{
		AppPath:      app,
		OutPath:      outDMG,
		BgPath:       bg,
		VolName:      "Lime",
		WindowWidth:  660,
		WindowHeight: 400,
		IconSize:     128,
		AppX:         180,
		AppY:         190,
		AppsX:        480,
		AppsY:        185,
	})
	if err != nil {
		t.Fatalf("repackDMG failed: %v", err)
	}

	// Mount read-only and verify contents.
	mnt := filepath.Join(dir, "verify-mnt")
	if err := os.Mkdir(mnt, 0o755); err != nil {
		t.Fatal(err)
	}
	disk, err := hdiutilAttach("-readonly", "-noverify", "-noautoopen", "-nobrowse", "-mountpoint", mnt, outDMG)
	if err != nil {
		t.Fatal(err)
	}
	defer exec.Command("hdiutil", "detach", "-force", disk).Run()

	if !fileExists(filepath.Join(mnt, "Lime.app", "Contents", "Info.plist")) {
		t.Fatal("mounted DMG missing Lime.app")
	}
	if target, err := os.Readlink(filepath.Join(mnt, "Applications")); err != nil || target != "/Applications" {
		t.Fatalf("Applications symlink = %q, %v", target, err)
	}
	if !fileExists(filepath.Join(mnt, ".background", "background.tiff")) {
		t.Fatal("mounted DMG missing Retina .background/background.tiff")
	}
	dsData, err := os.ReadFile(filepath.Join(mnt, ".DS_Store"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(dsData, []byte("backgroundImageAlias")) {
		t.Fatal("mounted .DS_Store missing backgroundImageAlias")
	}
}

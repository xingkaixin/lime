package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	finderHasCustomIcon = 0x0400
	finderIsInvisible   = 0x4000
	bundleIcon          = "icon.icns"
)

var (
	dmgFilesystems = []string{"HFS+", "APFS"}
	checksumLine   = regexp.MustCompile(`^([0-9a-f]{64}) [ *](.+)$`)
)

func main() {
	appPath := flag.String("app", "", "path to signed .app bundle")
	outPath := flag.String("out", "", "path to output .dmg")
	bgPath := flag.String("bg", "", "path to custom DMG background (.png or .tiff)")
	bg2xPath := flag.String("bg2x", "", "optional path to @2x Retina background PNG")
	volName := flag.String("volname", "Lime", "DMG volume name")
	winW := flag.Int("width", 660, "Finder window width in points")
	winH := flag.Int("height", 400, "Finder window height in points")
	iconSize := flag.Int("icon-size", 128, "Finder icon size in points")
	appPos := flag.String("app-pos", "180,190", "app icon x,y position")
	appsPos := flag.String("apps-pos", "480,185", "Applications link x,y position")
	updateSums := flag.String("update-checksums", "", "update SHA256SUMS file for the positional file paths")
	flag.Parse()

	if *updateSums != "" {
		if err := updateChecksums(*updateSums, flag.Args()); err != nil {
			fatalf("%v", err)
		}
		return
	}

	if *appPath == "" || *outPath == "" || *bgPath == "" {
		fatalf("usage: repack-dmg -app <App.app> -out <out.dmg> -bg <background.png> [-volname Lime]")
	}
	appX, appY, err := parsePos(*appPos)
	if err != nil {
		fatalf("-app-pos: %v", err)
	}
	appsX, appsY, err := parsePos(*appsPos)
	if err != nil {
		fatalf("-apps-pos: %v", err)
	}

	cfg := repackConfig{
		AppPath:      *appPath,
		OutPath:      *outPath,
		BgPath:       *bgPath,
		Bg2xPath:     *bg2xPath,
		VolName:      *volName,
		WindowWidth:  *winW,
		WindowHeight: *winH,
		IconSize:     *iconSize,
		AppX:         appX,
		AppY:         appY,
		AppsX:        appsX,
		AppsY:        appsY,
	}
	if err := repackDMG(cfg); err != nil {
		fatalf("%v", err)
	}
}

type repackConfig struct {
	AppPath      string
	OutPath      string
	BgPath       string
	Bg2xPath     string
	VolName      string
	WindowWidth  int
	WindowHeight int
	IconSize     int
	AppX, AppY   int
	AppsX, AppsY int
}

func repackDMG(cfg repackConfig) error {
	if runtime.GOOS != "darwin" {
		return errors.New("repacking DMGs requires macOS")
	}
	work, err := os.MkdirTemp("", "lime-repack-dmg-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	bgAsset, err := prepareBackground(work, cfg.BgPath, cfg.Bg2xPath, cfg.WindowWidth, cfg.WindowHeight)
	if err != nil {
		return fmt.Errorf("prepare background: %w", err)
	}
	bgName := filepath.Base(bgAsset)

	src := filepath.Join(work, "src")
	if err := os.MkdirAll(filepath.Join(src, ".background"), 0o755); err != nil {
		return err
	}
	appName := filepath.Base(cfg.AppPath)
	// ditto --noclone preserves code signatures, notarization tickets, and block counts.
	if err := runCmd("ditto", "--noclone", cfg.AppPath, filepath.Join(src, appName)); err != nil {
		return err
	}
	if err := os.Symlink("/Applications", filepath.Join(src, "Applications")); err != nil {
		return err
	}
	if err := copyFile(bgAsset, filepath.Join(src, ".background", bgName), 0o644); err != nil {
		return err
	}
	iconPath := filepath.Join(cfg.AppPath, "Contents", "Resources", bundleIcon)
	hasIcon := fileExists(iconPath)
	if hasIcon {
		if err := copyFile(iconPath, filepath.Join(src, ".VolumeIcon.icns"), 0o644); err != nil {
			return err
		}
	}

	// Reserve space for .DS_Store before creating the writable image.
	if err := writeDSStore(filepath.Join(src, ".DS_Store"), dmgLayout{
		AppName:      appName,
		WindowX:      200,
		WindowY:      400,
		WindowWidth:  cfg.WindowWidth,
		WindowHeight: cfg.WindowHeight,
		IconSize:     cfg.IconSize,
		AppX:         cfg.AppX,
		AppY:         cfg.AppY,
		AppsX:        cfg.AppsX,
		AppsY:        cfg.AppsY,
	}); err != nil {
		return err
	}

	rw := filepath.Join(work, "rw.dmg")
	if err := createDMG(cfg.VolName, src, rw); err != nil {
		return err
	}

	mnt := filepath.Join(work, "mnt")
	if err := os.Mkdir(mnt, 0o755); err != nil {
		return err
	}
	disk, err := hdiutilAttach("-readwrite", "-noverify", "-noautoopen", "-nobrowse", "-mountpoint", mnt, rw)
	if err != nil {
		return err
	}
	attached := true
	defer func() {
		if attached {
			_ = exec.Command("hdiutil", "detach", "-force", disk).Run()
		}
	}()

	// Stat files on the mounted volume so Carbon Alias CNIDs match the DMG filesystem.
	volMeta, err := statMeta(mnt)
	if err != nil {
		return err
	}
	bgDir := filepath.Join(mnt, ".background")
	bgDirMeta, err := statMeta(bgDir)
	if err != nil {
		return err
	}
	bgFileMeta, err := statMeta(filepath.Join(bgDir, bgName))
	if err != nil {
		return err
	}

	alias := encodeCarbonAlias(aliasTarget{
		VolumeName:     cfg.VolName,
		VolumeCreated:  volMeta.created,
		FolderName:     ".background",
		FolderCNID:     bgDirMeta.ino,
		FileName:       bgName,
		FileCNID:       bgFileMeta.ino,
		FileCreated:    bgFileMeta.created,
		POSIXRelPath:   "/.background/" + bgName,
		MountPointPath: "/Volumes/" + cfg.VolName,
	})

	if err := writeDSStore(filepath.Join(mnt, ".DS_Store"), dmgLayout{
		AppName:         appName,
		WindowX:         200,
		WindowY:         400,
		WindowWidth:     cfg.WindowWidth,
		WindowHeight:    cfg.WindowHeight,
		IconSize:        cfg.IconSize,
		AppX:            cfg.AppX,
		AppY:            cfg.AppY,
		AppsX:           cfg.AppsX,
		AppsY:           cfg.AppsY,
		BackgroundAlias: alias,
	}); err != nil {
		return err
	}

	if err := setFinderFlags(bgDir, finderIsInvisible); err != nil {
		return err
	}
	if hasIcon {
		if err := setFinderFlags(mnt, finderHasCustomIcon); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(filepath.Join(mnt, ".fseventsd"))
	_ = os.RemoveAll(filepath.Join(mnt, ".Trashes"))

	if err := hdiutilRetrying("detach", disk); err != nil {
		return err
	}
	attached = false

	if err := os.MkdirAll(filepath.Dir(cfg.OutPath), 0o755); err != nil {
		return err
	}
	return convertDMG(rw, cfg.OutPath)
}

// prepareBackground produces a multi-resolution Retina .tiff when a @2x image is
// present (or when bgPath itself is 2x the window size); otherwise it uses bgPath.
func prepareBackground(work, bgPath, bg2xPath string, winW, winH int) (string, error) {
	ext := strings.ToLower(filepath.Ext(bgPath))
	if ext == ".tiff" || ext == ".tif" {
		out := filepath.Join(work, "background.tiff")
		return out, copyFile(bgPath, out, 0o644)
	}

	if bg2xPath == "" {
		stem := strings.TrimSuffix(bgPath, filepath.Ext(bgPath))
		candidate := stem + "@2x" + filepath.Ext(bgPath)
		if fileExists(candidate) {
			bg2xPath = candidate
		}
	}

	if bg2xPath != "" {
		return buildHiDPITiff(work, bgPath, bg2xPath)
	}

	if w, h, err := imageDimensions(bgPath); err == nil && w == winW*2 && h == winH*2 {
		oneX := filepath.Join(work, "bg-1x.png")
		if err := copyFile(bgPath, oneX, 0o644); err != nil {
			return "", err
		}
		if err := runCmd("sips", "-z", strconv.Itoa(winH), strconv.Itoa(winW), "-s", "dpiHeight", "72", "-s", "dpiWidth", "72", oneX); err != nil {
			return "", err
		}
		return buildHiDPITiff(work, oneX, bgPath)
	}

	out := filepath.Join(work, "background"+ext)
	return out, copyFile(bgPath, out, 0o644)
}

func buildHiDPITiff(work, oneXSrc, twoXSrc string) (string, error) {
	oneX := filepath.Join(work, "hidpi-1x.png")
	twoX := filepath.Join(work, "hidpi-2x.png")
	if err := copyFile(oneXSrc, oneX, 0o644); err != nil {
		return "", err
	}
	if err := copyFile(twoXSrc, twoX, 0o644); err != nil {
		return "", err
	}
	if err := runCmd("sips", "-s", "dpiHeight", "72", "-s", "dpiWidth", "72", oneX); err != nil {
		return "", err
	}
	if err := runCmd("sips", "-s", "dpiHeight", "144", "-s", "dpiWidth", "144", twoX); err != nil {
		return "", err
	}
	out := filepath.Join(work, "background.tiff")
	if err := runCmd("tiffutil", "-cathidpicheck", oneX, twoX, "-out", out); err != nil {
		return "", err
	}
	return out, nil
}

func imageDimensions(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func createDMG(volume, src, out string) error {
	var errs []error
	for _, fs := range dmgFilesystems {
		args := []string{"create", "-ov", "-volname", volume, "-srcfolder", src, "-fs", fs}
		if fs == "HFS+" {
			args = append(args, "-fsargs", "-c c=64,a=16,e=16")
		}
		args = append(args, "-format", "UDRW", out)
		err := hdiutilRetrying(args...)
		if err == nil {
			return nil
		}
		errs = append(errs, fmt.Errorf("on %s: %w", fs, err))
		if strings.Contains(err.Error(), "Resource busy") {
			break
		}
	}
	return errors.Join(errs...)
}

func convertDMG(rw, dmg string) error {
	if exec.Command("diskutil", "image", "create", "from", "-h").Run() == nil {
		return runCmd("diskutil", "image", "create", "from", "--format", "ULMO", rw, dmg)
	}
	return runCmd("hdiutil", "convert", rw, "-format", "ULMO", "-ov", "-o", dmg)
}

func hdiutilAttach(args ...string) (string, error) {
	cmd := exec.Command("hdiutil", append([]string{"attach"}, args...)...)
	var errOut strings.Builder
	cmd.Stderr = &errOut
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("hdiutil attach: %v\n%s", err, strings.TrimSpace(errOut.String()))
	}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 && strings.HasPrefix(f[0], "/dev/disk") {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("hdiutil attach: no device found in output:\n%s", strings.TrimSpace(string(out)))
}

func hdiutilRetrying(args ...string) error {
	var first, err error
	for i := range 5 {
		if err = runCmd("hdiutil", args...); err == nil {
			return nil
		}
		if first == nil {
			first = err
		}
		if !strings.Contains(err.Error(), "Resource busy") {
			break
		}
		if i < 4 {
			time.Sleep(time.Duration(2<<i) * time.Second)
		}
	}
	if err != first {
		return fmt.Errorf("%w\nfirst attempt: %v", err, first)
	}
	return err
}

func setFinderFlags(path string, flags uint16) error {
	var info [32]byte
	info[8], info[9] = byte(flags>>8), byte(flags)
	return runCmd("/usr/bin/xattr", "-wx", "com.apple.FinderInfo", hex.EncodeToString(info[:]), path)
}

func updateChecksums(sumsPath string, files []string) error {
	sums := map[string]string{}
	if data, err := os.ReadFile(sumsPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if m := checksumLine.FindStringSubmatch(strings.TrimSuffix(line, "\r")); m != nil {
				sums[m[2]] = m[1]
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, path := range files {
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		sums[filepath.Base(path)] = sum
	}
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(sums)) {
		fmt.Fprintf(&b, "%s  %s\n", sums[name], name)
	}
	return os.WriteFile(sumsPath, []byte(b.String()), 0o644)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func parsePos(s string) (int, int, error) {
	xs, ys, ok := strings.Cut(s, ",")
	if !ok {
		return 0, 0, fmt.Errorf("expected x,y, got %q", s)
	}
	x, err := strconv.Atoi(strings.TrimSpace(xs))
	if err != nil {
		return 0, 0, err
	}
	y, err := strconv.Atoi(strings.TrimSpace(ys))
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "repack-dmg: "+format+"\n", args...)
	os.Exit(1)
}

package main

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

var errCLICanceled = errors.New("canceled")

type openArg struct {
	path      string
	line, col int
}

type cliRequest struct {
	newWindow bool
	wd        string
	args      []openArg
}

func realCLI(caption func(locales.Messages) string) func(locales.Messages) string {
	if !cliSupported {
		return nil
	}
	return caption
}

func parseCLIArgs(args []string) []openArg {
	return parseCLIRequest(args, "").args
}

func parseCLIRequest(args []string, wd string) cliRequest {
	req := cliRequest{wd: wd}
	literal := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !literal {
			switch {
			case arg == "--":
				literal = true
				continue
			case arg == "-n" || arg == "--new":
				req.newWindow = true
				continue
			case arg == "--cwd" && i+1 < len(args):
				i++
				req.wd = args[i]
				continue
			case strings.HasPrefix(arg, "--cwd="):
				req.wd = strings.TrimPrefix(arg, "--cwd=")
				continue
			case strings.HasPrefix(arg, "-"):
				continue
			}
		}
		req.args = append(req.args, parseOpenArgIn(arg, req.wd))
	}
	return req
}

func parseOpenArg(arg string) openArg {
	return parseOpenArgIn(arg, "")
}

func parseOpenArgIn(arg, wd string) openArg {
	if _, err := os.Stat(resolveCLIPath(arg, wd)); err == nil {
		return openArg{path: arg}
	}
	b1, n1, ok := splitTrailingInt(arg)
	if !ok {
		return openArg{path: arg}
	}
	if b2, n2, ok := splitTrailingInt(b1); ok {
		if _, err := os.Stat(resolveCLIPath(b1, wd)); err == nil {
			return openArg{path: b1, line: n1}
		}
		return openArg{path: b2, line: n2, col: n1}
	}
	return openArg{path: b1, line: n1}
}

func resolveCLIPath(p, wd string) string {
	return resolveCLIPathOS(p, wd, runtime.GOOS == "windows")
}

func resolveCLIPathOS(p, wd string, win bool) string {
	if win {
		if isWindowsDriveRootRel(p) {
			if vol := windowsDriveVolume(wd); vol != "" {
				cleaned := path.Clean(strings.ReplaceAll(p, `\`, "/"))
				return vol + strings.ReplaceAll(cleaned, "/", `\`)
			}
		} else if vol := windowsDriveVolume(p); vol != "" && !isWindowsDriveAbs(p) {
			if strings.EqualFold(vol, windowsDriveVolume(wd)) {
				base := strings.ReplaceAll(wd[2:], `\`, "/")
				rel := strings.ReplaceAll(p[2:], `\`, "/")
				cleaned := path.Clean(base + "/" + rel)
				return vol + strings.ReplaceAll(cleaned, "/", `\`)
			}
			if abs, err := filepath.Abs(p); err == nil {
				return abs
			}
			return p
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if wd != "" {
		return filepath.Clean(filepath.Join(wd, p))
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func isWindowsDriveAbs(p string) bool {
	return len(p) >= 3 && windowsDriveVolume(p) != "" && (p[2] == '\\' || p[2] == '/')
}

func isWindowsDriveRootRel(p string) bool {
	if len(p) == 0 || (p[0] != '\\' && p[0] != '/') {
		return false
	}
	return len(p) == 1 || (p[1] != '\\' && p[1] != '/')
}

func windowsDriveVolume(wd string) string {
	if len(wd) >= 2 && wd[1] == ':' && ((wd[0] >= 'A' && wd[0] <= 'Z') || (wd[0] >= 'a' && wd[0] <= 'z')) {
		return wd[:2]
	}
	return ""
}

func splitTrailingInt(s string) (string, int, bool) {
	i := strings.LastIndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return "", 0, false
	}
	tail := s[i+1:]
	for j := range len(tail) {
		if tail[j] < '0' || tail[j] > '9' {
			return "", 0, false
		}
	}
	n, err := strconv.Atoi(tail)
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return s[:i], n, true
}

func (a *app) openCLI(args []string) {
	a.openCLIIn(parseCLIArgs(args), "")
}

func (a *app) openCLIIn(args []openArg, wd string) {
	for _, arg := range args {
		p := resolveCLIPath(arg.path, wd)
		if d := a.docAt(p); d != nil {
			a.show(d)
			goToPos(d, arg.line, arg.col)
			continue
		}
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			d := a.add(p, "")
			goToPos(d, arg.line, arg.col)
			continue
		}
		a.open([]string{p})
		if arg.line > 0 {
			if d := a.docAt(p); d != nil {
				goToPos(d, arg.line, arg.col)
			}
		}
	}
}

func (l *lime) openCLILaunch(args []string, wd string, hadSession bool) {
	req := parseCLIRequest(args, wd)
	target := l.last
	if req.newWindow && hadSession {
		target = l.newWindow()
		l.last = target
	}
	if len(req.args) > 0 {
		target.openCLIIn(req.args, req.wd)
	}
}

func (l *lime) openCLISecondInstance(args []string, wd string) {
	req := parseCLIRequest(args, wd)
	if req.newWindow {
		if len(l.wins) == 0 {
			l.restoreSession()
			if l.ready {
				for _, a := range l.wins {
					if a.docCount() == 0 {
						a.newFile()
					}
					a.makeWindow(a.bounds)
				}
			}
		}
		var bounds mygo.Rectangle
		if l.last != nil && l.last.win != nil {
			r := l.last.win.Bounds()
			bounds = mygo.Rectangle{X: r.X + 24, Y: r.Y + 24, Width: r.Width, Height: r.Height}
		}
		b := l.newWindow()
		l.last = b
		if l.ready {
			b.makeWindow(bounds)
			b.win.Update(func() {
				if len(req.args) > 0 {
					b.openCLIIn(req.args, req.wd)
				}
				if b.docCount() == 0 {
					b.newFile()
				}
			})
			mygo.App.Focus()
			b.win.Focus()
		} else {
			if len(req.args) > 0 {
				b.openCLIIn(req.args, req.wd)
			}
			if b.docCount() == 0 {
				b.newFile()
			}
		}
		return
	}
	if len(l.wins) == 0 {
		if l.ready {
			l.reopen()
		} else {
			l.restoreSession()
			if len(l.wins) == 0 {
				l.newWindow()
			}
		}
	}
	a := l.last
	if a != nil && len(req.args) > 0 {
		if a.win == nil {
			a.openCLIIn(req.args, req.wd)
		} else {
			a.win.Update(func() { a.openCLIIn(req.args, req.wd) })
		}
	}
	if l.ready {
		mygo.App.Focus()
		if a != nil && a.win != nil {
			a.win.Focus()
		}
	}
}

func goToPos(d *doc, line, col int) {
	if d == nil || d.kind != textDoc || line <= 0 {
		return
	}
	buf := d.st.Buffer
	l := min(line, buf.LineCount()) - 1
	byteCol := 0
	if col > 1 {
		s := buf.Line(l)
		rem := col - 1
		for i := range s {
			if rem == 0 {
				byteCol = i
				rem = -1
				break
			}
			rem--
		}
		if rem >= 0 {
			byteCol = len(s)
		}
	}
	d.st.GoTo(editor.Pos{Line: l, Col: byteCol})
}

func (a *app) installCLI() {
	if !cliSupported {
		return
	}
	win := a.win
	go func() {
		detail, err := installCLIPlatform()
		if errors.Is(err, errCLICanceled) {
			return
		}
		if err != nil {
			mygo.Dialog.Error(locales.DialogUnableToInstallCLI(), err.Error())
			return
		}
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  win,
			Type:    mygo.MessageInfo,
			Message: locales.DialogCLIInstalled(),
			Detail:  detail,
		})
	}()
}

// appendWindowsPath adds dir to a semicolon-delimited Windows PATH string if
// an equivalent entry is not already present.
func appendWindowsPath(current, dir string) (string, bool) {
	norm := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.Trim(s, `"`)
		s = strings.ReplaceAll(s, "/", `\`)
		// Keep drive roots like "C:\" intact while trimming trailing slashes elsewhere.
		for len(s) > 3 && strings.HasSuffix(s, `\`) {
			s = s[:len(s)-1]
		}
		return strings.ToLower(s)
	}
	want := norm(dir)
	if want == "" {
		return current, false
	}
	var kept []string
	for part := range strings.SplitSeq(current, ";") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if norm(p) == want {
			return current, false
		}
		kept = append(kept, p)
	}
	kept = append(kept, dir)
	return strings.Join(kept, ";"), true
}

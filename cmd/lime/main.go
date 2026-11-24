// Lime is a plain text editor in the manner of Sublime Text, without
// packages: tabs, groups, Goto Anything, the command palette, a side bar
// of folders, syntax highlighting, word wrap, several carets and column
// selection, on Sublime Text's keys.
package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// makeWindow opens the window of a, at r unless r is empty, the first
// window where it was last.
func (a *app) makeWindow(r mygo.Rectangle) {
	o := mygo.WindowOptions{Title: "Lime", Width: 1100, Height: 760, MinWidth: 480, MinHeight: 320, Content: ui.View(a.view)}
	switch {
	case a == a.l.wins[0]:
		o.StateKey = "main"
	case r.Width > 0:
		o.X, o.Y, o.Width, o.Height = r.X, r.Y, r.Width, r.Height
	}
	if mac {
		o.Vibrancy, o.TitleBarStyle = mygo.VibrancySidebar, mygo.TitleBarHidden
	}
	a.win = mygo.NewWindow(o)
	a.win.OnFocus(func() {
		a.l.last, a.l.menuState = a, menuStateKey{}
		a.syncMenu()
		a.reloadChanged()
		a.readGit()
		a.readMarks()
		if !watchDirs {
			a.proj.clearDirs()
		}
	})
	a.win.OnClose(a.closing)
	a.win.OnClosed(func() { a.l.closed(a) })
}

func main() {
	initUpdater()
	l := newLime(dataDir())
	onIconFontLoaded = l.redraw
	l.restoreSession()
	if len(l.wins) == 0 {
		l.newWindow()
	}
	l.themeSource = themeSource(l.set.Appearance)
	mygo.Theme.SetSource(l.themeSource)
	mygo.App.SetLocale(l.set.locale())
	l.last.openCLI(os.Args[1:])
	for _, a := range l.wins {
		if a.docCount() == 0 {
			a.newFile()
		}
	}
	// Finder, the Dock and open(1) pass files here, not in os.Args.
	mygo.App.OnOpenFile(func(path string) {
		if len(l.wins) == 0 {
			l.reopen()
		}
		a := l.last
		if a.win == nil {
			// The files the app is launched with come before its windows.
			a.open([]string{path})
			return
		}
		a.win.Update(func() { a.open([]string{path}) })
	})
	mygo.App.WhenReady(func() {
		l.menu = l.newMenu()
		mygo.App.SetMenu(l.menu)
		// The menu's labels are said once, in the language shown as it is
		// made.
		mygo.App.OnLocaleChanged(func(string) { l.refreshMenu() })
		for _, a := range l.wins {
			a.makeWindow(a.bounds)
		}
		mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) { l.quit(e) })
		if mac {
			// A listener, though it does nothing, keeps the app running.
			mygo.App.OnWindowAllClosed(func() {})
			mygo.App.OnActivate(func(bool) {
				if len(l.wins) == 0 {
					l.reopen()
				}
			})
		}
		if err := l.setErr; err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToReadSettings(), err.Error())
		}
		go func() {
			for range time.Tick(3 * time.Second) {
				mygo.RunOnMain(l.tick)
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

type openArg struct {
	path      string
	line, col int
}

func parseCLIArgs(args []string) []openArg {
	var out []openArg
	literal := false
	for _, arg := range args {
		if !literal {
			if arg == "--" {
				literal = true
				continue
			}
			if strings.HasPrefix(arg, "-") {
				continue
			}
		}
		out = append(out, parseOpenArg(arg))
	}
	return out
}

func parseOpenArg(arg string) openArg {
	if _, err := os.Stat(arg); err == nil {
		return openArg{path: arg}
	}
	b1, n1, ok := splitTrailingInt(arg)
	if !ok {
		return openArg{path: arg}
	}
	if b2, n2, ok := splitTrailingInt(b1); ok {
		if _, err := os.Stat(b1); err == nil {
			return openArg{path: b1, line: n1}
		}
		return openArg{path: b2, line: n2, col: n1}
	}
	return openArg{path: b1, line: n1}
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
	for _, arg := range parseCLIArgs(args) {
		p, _ := filepath.Abs(arg.path)
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

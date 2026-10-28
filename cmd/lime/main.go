// Lime is a plain text editor in the manner of Sublime Text, without
// packages: tabs, groups, Goto Anything, the command palette, a side bar
// of folders, syntax highlighting, word wrap, several carets and column
// selection, on Sublime Text's keys.
package main

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

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
		a.l.last, a.l.menuState = a, ""
		a.syncMenu()
		a.reloadChanged()
		a.readGit()
		if !watchDirs {
			a.proj.dirs = nil
		}
	})
	a.win.OnClose(a.closing)
	a.win.OnClosed(func() { a.l.closed(a) })
}

func main() {
	l := newLime(dataDir())
	l.restoreSession()
	if len(l.wins) == 0 {
		l.newWindow()
	}
	l.themeSource = themeSource(l.set.Appearance)
	mygo.Theme.SetSource(l.themeSource)
	mygo.App.SetLocale(l.set.locale())
	var paths []string
	for _, arg := range os.Args[1:] {
		if !strings.HasPrefix(arg, "-") {
			paths = append(paths, arg)
		}
	}
	l.last.open(paths)
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
				mygo.RunOnMain(l.autosave)
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

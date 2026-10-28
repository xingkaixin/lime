package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"

	"lime/locales"
)

// recentList is what Open Recent lists, newest first, kept in the session
// as Sublime Text keeps it: the files opened, and the folders and
// .sublime-project files.
type recentList struct {
	Files   []string `json:"files,omitempty"`
	Folders []string `json:"folders,omitempty"`
}

// Open Recent lists 8 files and 8 folders, as Sublime Text's. Quick Switch
// Project lists more of the folders.
const (
	recentShown = 8
	recentKept  = 30
)

// addRecent puts the files and the folders or projects first in Open
// Recent, the last first, and records the files with the system: the Dock
// menu on macOS, the jump list on Windows. The system gives back what it
// lists as a file to open, which would add a folder to the window last
// focused, not switch to it, and open a project's file as text.
func (l *lime) addRecent(files, folders []string) {
	changed := false
	for _, p := range files {
		changed = pushRecent(&l.recent.Files, p, recentShown) || changed
	}
	for _, p := range folders {
		changed = pushRecent(&l.recent.Folders, p, recentKept) || changed
	}
	if !changed {
		return
	}
	l.refreshMenu()
	if len(files) > 0 && mygo.App.IsReady() {
		go func() {
			for _, p := range files {
				mygo.App.AddRecentDocument(p)
			}
		}()
	}
}

// pushRecent puts p first in list, keeping n, and reports whether that
// changed it.
func pushRecent(list *[]string, p string, n int) bool {
	if len(*list) > 0 && (*list)[0] == p {
		return false
	}
	*list = slices.Insert(slices.DeleteFunc(*list, func(x string) bool { return x == p }), 0, p)
	*list = (*list)[:min(len(*list), n)]
	return true
}

func (l *lime) clearRecent() {
	l.recent = recentList{}
	l.refreshMenu()
	if mygo.App.IsReady() {
		go mygo.App.ClearRecentDocuments()
	}
}

// recentMenu is File > Open Recent, as Sublime Text's.
func (l *lime) recentMenu() *mygo.MenuItem {
	items := []*mygo.MenuItem{l.menuItem(locales.MenuReopenClosedFile(), "reopen_last_file"), mygo.Separator()}
	for _, p := range l.recent.Files {
		items = append(items, l.recentItem(tildePath(p), func(a *app) { a.open([]string{p}) }))
	}
	if len(l.recent.Files) > 0 {
		items = append(items, mygo.Separator())
	}
	folders := l.recent.Folders[:min(len(l.recent.Folders), recentShown)]
	for _, p := range folders {
		items = append(items, l.recentItem(tildePath(p), func(a *app) { a.switchProject(p) }))
	}
	if len(folders) > 0 {
		items = append(items, mygo.Separator())
	}
	items = append(items, l.recentItem(locales.MenuClearItems(), func(a *app) { a.l.clearRecent() }))
	return &mygo.MenuItem{Label: locales.MenuOpenRecent(), Submenu: items}
}

// recentItem runs fn in the focused window, as menuItem runs a command.
func (l *lime) recentItem(label string, fn func(a *app)) *mygo.MenuItem {
	return &mygo.MenuItem{Label: label, Click: func(_ *mygo.MenuItem, w *mygo.Window) {
		if len(l.wins) == 0 {
			l.reopen()
		}
		if a := l.at(w); a != nil {
			a.win.Update(func() { fn(a) })
		}
	}}
}

// showWorkspaces is Sublime Text's Quick Switch Project: the folders and
// projects opened lately.
func (a *app) showWorkspaces() {
	var items []item
	for _, p := range a.l.recent.Folders {
		name := strings.TrimSuffix(filepath.Base(p), ".sublime-project")
		items = append(items, item{label: name, detail: tildePath(p), pick: func() { a.switchProject(p) }})
	}
	a.openPanel(&panel{list: func(q string) []item { return ranked(items, byName(q)) }})
}

// switchProject goes to the folder or .sublime-project file at p: to the
// window that has it, or else opens it here when this window has no
// folders and nothing unsaved, or in a new window. Sublime Text switches
// this window's project instead, keeping its tabs in a workspace file,
// which Lime has not.
func (a *app) switchProject(p string) {
	for _, w := range a.l.wins {
		if w.proj.path == p || slices.Contains(w.proj.folders, p) {
			a.l.addRecent(nil, []string{p})
			if w.win.IsMinimized() {
				w.win.Restore()
			}
			w.win.Focus()
			return
		}
	}
	fi, err := os.Stat(p)
	if err != nil {
		go mygo.Dialog.Error(locales.DialogUnableToOpen(filepath.Base(p)), err.Error())
		return
	}
	var pf projectFile
	if !fi.IsDir() {
		if pf, err = readProject(p); err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToOpenProject(), err.Error())
			return
		}
	}
	b := a
	if len(a.proj.folders) > 0 || a.proj.path != "" || slices.ContainsFunc(a.allDocs(), (*doc).dirty) {
		b = a.l.newWindow()
		b.newFile()
		r := a.win.Bounds()
		b.makeWindow(mygo.Rectangle{X: r.X + 24, Y: r.Y + 24, Width: r.Width, Height: r.Height})
	}
	if fi.IsDir() {
		b.addFolder(p)
		a.l.addRecent(nil, []string{p})
		return
	}
	b.useProject(p, pf)
}

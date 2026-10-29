package main

import (
	"errors"
	"io/fs"
	"os"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// The context menus, as Sublime Text's: of a tab, and in the side bar of
// a file, of a folder, and of the side bar around them.

// tabMenu is the menu of the tab of d in g.
func (a *app) tabMenu(m *ui.Menu, g *group, d *doc) {
	all := func(*doc) bool { return true }
	if m.Item(locales.MenuCloseTab()).Chosen() {
		a.closeDoc(d)
	}
	i := slices.Index(g.docs, d)
	if m.Item(locales.MenuCloseTabsToLeft()).Disabled(i == 0).Chosen() {
		a.closeTabs(g.docs[:i], all)
	}
	if m.Item(locales.MenuCloseTabsToRight()).Disabled(i == len(g.docs)-1).Chosen() {
		a.closeTabs(g.docs[i+1:], all)
	}
	m.Separator()
	if m.Item(locales.MenuCloseUnmodifiedTabs()).Chosen() {
		a.closeTabs(g.docs, all)
	}
	if m.Item(locales.MenuCloseDeletedTabs()).Chosen() {
		a.closeTabs(g.docs, (*doc).deleted)
	}
	m.Separator()
	if m.Item(locales.MenuNewFile()).Chosen() {
		a.focusGroup(slices.Index(a.groups, g))
		a.newFile()
	}
	if m.Item(locales.MenuOpenFile()).Chosen() {
		a.focusGroup(slices.Index(a.groups, g))
		a.promptOpen()
	}
	if d.markdown() {
		m.Separator()
		if m.Item(locales.MenuOpenPreview()).Chosen() {
			a.openPreview(d, false)
		}
		if m.Item(locales.MenuOpenPreviewToTheSide()).Chosen() {
			a.openPreview(d, true)
		}
	}
	m.Separator()
	if m.Item(locales.MenuFileHistory()).Disabled(!a.tracked(d.path)).Chosen() {
		a.fileHistory(d.path)
	}
}

// closeTabs closes the tabs of docs without changes that match.
func (a *app) closeTabs(docs []*doc, match func(*doc) bool) {
	for _, d := range slices.Clone(docs) {
		if !d.dirty() && match(d) {
			a.remove(d)
		}
	}
}

// deleted reports whether d's file is no longer on disk.
func (d *doc) deleted() bool {
	if d.path == "" {
		return false
	}
	_, err := os.Stat(d.path)
	return errors.Is(err, fs.ErrNotExist)
}

func (a *app) fileMenu(c *ui.Context, m *ui.Menu, path string) {
	if m.Item(locales.MenuRename()).Chosen() {
		a.promptRename(path)
	}
	if m.Item(locales.MenuDeleteFile()).Chosen() {
		a.trash(path, false)
	}
	if m.Item(locales.MenuRevealInFinder()).Chosen() {
		mygo.Shell.ShowItemInFolder(path)
	}
	if m.Item(locales.MenuCopyPath()).Chosen() {
		c.Services().WriteClipboard(path)
	}
	m.Separator()
	if m.Item(locales.MenuFileHistory()).Disabled(!a.tracked(path)).Chosen() {
		a.fileHistory(path)
	}
}

func (a *app) folderMenu(c *ui.Context, m *ui.Menu, dir string, root bool) {
	if m.Item(locales.MenuNewFile()).Chosen() {
		a.startCreate(dir, false)
	}
	if m.Item(locales.MenuRename()).Chosen() {
		a.promptRename(dir)
	}
	if m.Item(locales.MenuOpenFolder()).Chosen() {
		mygo.Shell.OpenPath(dir)
	}
	if m.Item(locales.MenuCopyPath()).Chosen() {
		c.Services().WriteClipboard(dir)
	}
	m.Separator()
	if m.Item(locales.MenuNewFolder()).Chosen() {
		a.startCreate(dir, true)
	}
	if m.Item(locales.MenuDeleteFolder()).Chosen() {
		a.trash(dir, true)
	}
	m.Separator()
	if m.Item(locales.MenuFindInFolder()).Chosen() {
		a.showFindInFiles(dir)
	}
	if !root {
		return
	}
	m.Separator()
	if m.Item(locales.MenuRemoveFolder()).Chosen() {
		a.proj.folders = slices.DeleteFunc(a.proj.folders, func(f string) bool { return f == dir })
		a.indexFolders()
	}
}

// projectMenu makes new files and folders in the first folder, as VS Code's
// explorer does in its blank space.
func (a *app) projectMenu(m *ui.Menu) {
	if len(a.proj.folders) > 0 {
		dir := a.proj.folders[0]
		if m.Item(locales.MenuNewFile()).Chosen() {
			a.startCreate(dir, false)
		}
		if m.Item(locales.MenuNewFolder()).Chosen() {
			a.startCreate(dir, true)
		}
		m.Separator()
	}
	if m.Item(locales.MenuAddFolderToProject()).Chosen() {
		a.promptAddFolder()
	}
	if m.Item(locales.MenuRefreshFolders()).Chosen() {
		a.refreshFolders()
	}
}

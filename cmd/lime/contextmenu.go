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

func (a *app) tabMenu(c *ui.Context, m *ui.Menu, g *group, d *doc) {
	all := func(*doc) bool { return true }
	if m.Item(locales.MenuCloseTab()).Chosen() {
		a.closeDoc(d)
	}
	if m.Item(locales.MenuCloseOtherTabs()).Disabled(len(g.docs) <= 1).Chosen() {
		a.closeOtherTabs(g, d)
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
	if m.Item(locales.MenuCopyPath()).Disabled(d.path == "").Chosen() {
		c.Services().WriteClipboard(d.path)
	}
	if m.Item(locales.MenuRevealInSideBar()).Disabled(d.path == "" || !a.proj.holds(d.path)).Chosen() {
		a.revealInSideBar(d.path)
	}
	if m.Item(locales.MenuRevealInFinder()).Disabled(d.path == "").Chosen() {
		mygo.Shell.ShowItemInFolder(d.path)
	}
	m.Separator()
	if m.Item(locales.MenuFileHistory()).Disabled(!a.tracked(d.path)).Chosen() {
		a.fileHistory(d.path)
	}
	a.permalinkMenu(m, d)
}

func (a *app) closeTabs(docs []*doc, match func(*doc) bool) {
	for _, d := range slices.Clone(docs) {
		if !d.dirty() && match(d) {
			a.remove(d)
		}
	}
}

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
	if terminalSupported && m.Item(locales.MenuOpenInTerminal()).Chosen() {
		a.openTerminalInDir(dir)
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

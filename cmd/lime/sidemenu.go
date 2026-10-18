package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// The context menus, as Sublime Text's: of a tab, and in the side bar of
// a file, of a folder, and of the side bar around them.

// tabMenu is the menu of the tab of d in g.
func (a *app) tabMenu(m *ui.Menu, g *group, d *doc) {
	if m.Item(locales.MenuCloseTab()).Chosen() {
		a.closeDoc(d)
	}
	m.Separator()
	if m.Item(locales.MenuCloseUnmodifiedTabs()).Chosen() {
		a.closeTabs(g, func(*doc) bool { return true })
	}
	if m.Item(locales.MenuCloseDeletedTabs()).Chosen() {
		a.closeTabs(g, (*doc).deleted)
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
}

// closeTabs closes the tabs of g without changes that match.
func (a *app) closeTabs(g *group, match func(*doc) bool) {
	for _, d := range slices.Clone(g.docs) {
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
}

func (a *app) folderMenu(c *ui.Context, m *ui.Menu, dir string, root bool) {
	if m.Item(locales.MenuNewFile()).Chosen() {
		a.add("", "").dir = dir
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
		a.promptNewFolder(dir)
	}
	if m.Item(locales.MenuDeleteFolder()).Chosen() {
		a.trash(dir, true)
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

func (a *app) projectMenu(m *ui.Menu) {
	if m.Item(locales.MenuAddFolderToProject()).Chosen() {
		a.promptAddFolder()
	}
	if m.Item(locales.MenuRefreshFolders()).Chosen() {
		a.refreshFolders()
	}
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// moved returns p once from is renamed to.
func moved(p, from, to string) string {
	if within(p, from) {
		return to + p[len(from):]
	}
	return p
}

func (a *app) promptRename(path string) {
	a.openPanel(&panel{prompt: locales.PromptNewName(), query: filepath.Base(path), submit: func(name string) {
		if name == "" || name == filepath.Base(path) {
			return
		}
		to := filepath.Join(filepath.Dir(path), name)
		if err := renameNew(path, to); err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToRename(filepath.Base(path)), err.Error())
			return
		}
		a.renamed(path, to)
	}})
}

// renameNew renames from to to, but not over another file as os.Rename
// does. A to that is from differs from it by case only, on a file system
// that ignores case.
func renameNew(from, to string) error {
	if ti, err := os.Lstat(to); err == nil {
		if fi, err := os.Lstat(from); err != nil || !os.SameFile(ti, fi) {
			return &os.PathError{Op: "rename", Path: to, Err: fs.ErrExist}
		}
	}
	return os.Rename(from, to)
}

// renamed has the tabs of the files in from, its folders shown open and
// the project's folders follow it to to.
func (a *app) renamed(from, to string) {
	for _, d := range a.allDocs() {
		if p := moved(d.path, from, to); p != d.path {
			d.path = p
			d.st.Buffer.SetLanguage(editor.LanguageFor(p))
		}
		d.dir = moved(d.dir, from, to)
	}
	open := make(map[string]*bool, len(a.proj.open))
	for dir, o := range a.proj.open {
		open[moved(dir, from, to)] = o
	}
	a.proj.open = open
	for i, f := range a.proj.folders {
		a.proj.folders[i] = moved(f, from, to)
	}
	a.proj.jump = true
	a.refreshFolders()
}

func (a *app) promptNewFolder(dir string) {
	a.openPanel(&panel{prompt: locales.PromptFolderName(), submit: func(name string) {
		if name == "" {
			return
		}
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToCreate(name), err.Error())
			return
		}
		*a.proj.isOpen(dir, true) = true
		a.refreshFolders()
	}})
}

// trash moves path to the Trash, with the tabs of its files. It asks first
// for a folder, or when a tab has changes.
func (a *app) trash(path string, folder bool) {
	var docs []*doc
	for _, d := range a.allDocs() {
		if d.path != "" && within(d.path, path) {
			docs = append(docs, d)
		}
	}
	del := func() {
		if err := mygo.Shell.TrashItem(path); err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToDelete(filepath.Base(path)), err.Error())
			return
		}
		for _, d := range docs {
			a.take(d)
		}
		release(docs...)
		a.proj.folders = slices.DeleteFunc(a.proj.folders, func(f string) bool { return within(f, path) })
		a.refreshFolders()
	}
	var detail []string
	if folder {
		detail = append(detail, locales.DialogDeleteFolder())
	}
	if slices.ContainsFunc(docs, (*doc).dirty) {
		detail = append(detail, locales.DialogUnsavedLost())
	}
	if detail == nil {
		del()
		return
	}
	go func() {
		res, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  a.win,
			Type:    mygo.MessageWarning,
			Message: locales.DialogDelete(filepath.Base(path)),
			Detail:  strings.Join(detail, " "),
			Buttons: []string{locales.ButtonDelete(), locales.ButtonCancel()},
		})
		if res.Button == 0 {
			a.win.Update(del)
		}
	}()
}

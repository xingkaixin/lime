package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// The side bar's context menus, as Sublime Text's: of a file, of a folder,
// and of the side bar around them.

func (a *app) fileMenu(c *ui.Context, m *ui.Menu, path string) {
	if m.Item("Rename…").Chosen() {
		a.promptRename(path)
	}
	if m.Item("Delete File").Chosen() {
		a.trash(path, false)
	}
	if m.Item("Reveal in Finder").Chosen() {
		mygo.Shell.ShowItemInFolder(path)
	}
	if m.Item("Copy Path").Chosen() {
		c.Services().WriteClipboard(path)
	}
}

func (a *app) folderMenu(c *ui.Context, m *ui.Menu, dir string, root bool) {
	if m.Item("New File").Chosen() {
		a.add("", "").dir = dir
	}
	if m.Item("Rename…").Chosen() {
		a.promptRename(dir)
	}
	if m.Item("Open Folder…").Chosen() {
		mygo.Shell.OpenPath(dir)
	}
	if m.Item("Copy Path").Chosen() {
		c.Services().WriteClipboard(dir)
	}
	m.Separator()
	if m.Item("New Folder…").Chosen() {
		a.promptNewFolder(dir)
	}
	if m.Item("Delete Folder").Chosen() {
		a.trash(dir, true)
	}
	if !root {
		return
	}
	m.Separator()
	if m.Item("Remove Folder from Project").Chosen() {
		a.proj.folders = slices.DeleteFunc(a.proj.folders, func(f string) bool { return f == dir })
		a.indexFolders()
	}
}

func (a *app) projectMenu(m *ui.Menu) {
	if m.Item("Add Folder to Project…").Chosen() {
		a.promptAddFolder()
	}
	if m.Item("Refresh Folders").Chosen() {
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
	a.openPanel(&panel{prompt: "New Name:", query: filepath.Base(path), submit: func(name string) {
		if name == "" || name == filepath.Base(path) {
			return
		}
		to := filepath.Join(filepath.Dir(path), name)
		if err := renameNew(path, to); err != nil {
			go mygo.Dialog.Error("Unable to rename "+filepath.Base(path), err.Error())
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
	a.openPanel(&panel{prompt: "Folder Name:", submit: func(name string) {
		if name == "" {
			return
		}
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			go mygo.Dialog.Error("Unable to create "+name, err.Error())
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
			go mygo.Dialog.Error("Unable to delete "+filepath.Base(path), err.Error())
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
		detail = append(detail, "The folder and what it holds move to the Trash.")
	}
	if slices.ContainsFunc(docs, (*doc).dirty) {
		detail = append(detail, "Unsaved changes are lost.")
	}
	if detail == nil {
		del()
		return
	}
	go func() {
		res, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  a.win,
			Type:    mygo.MessageWarning,
			Message: "Delete " + filepath.Base(path) + "?",
			Detail:  strings.Join(detail, " "),
			Buttons: []string{"Delete", "Cancel"},
		})
		if res.Button == 0 {
			a.win.Update(del)
		}
	}()
}

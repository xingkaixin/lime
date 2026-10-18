package main

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The side bar's context menus, as Sublime Text's: of a file, of a folder,
// and of the side bar around them.

func (a *app) fileMenu(c *ui.Context, m *ui.Menu, path string) {
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
	if m.Item("Open Folder…").Chosen() {
		mygo.Shell.OpenPath(dir)
	}
	if m.Item("Copy Path").Chosen() {
		c.Services().WriteClipboard(dir)
	}
	m.Separator()
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

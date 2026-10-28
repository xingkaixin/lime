package main

import (
	"path/filepath"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// comparison is what a diff tab shows: the text of a tab, which it edits,
// against an older one.
type comparison struct {
	diff    editor.DiffState
	src     *doc // the tab whose text is the new one
	old     *doc // the tab whose text is the old one, when it is a tab's
	oldName string
	oldPath string // "" when the old text is no file's
	unsaved bool   // compares src with its text on disk
}

func (cmp *comparison) title() string {
	if cmp.unsaved {
		return locales.TabUnsavedChanges(cmp.src.name())
	}
	return cmp.oldName + " ↔ " + cmp.src.name()
}

// showCompare lists what src can be compared with: a file the system's
// dialog picks, the other tabs and the project's files.
func (a *app) showCompare(src *doc) {
	a.indexFolders()
	browse := item{label: locales.PanelBrowseFiles(), detail: locales.PanelBrowseFilesDetail(), pick: func() { a.promptCompare(src) }}
	a.openPanel(&panel{placeholder: locales.PanelCompareWith(src.name()), list: func(q string) []item {
		var items []item
		seen := map[string]bool{src.path: true}
		for _, d := range a.mru {
			if d == src || d.kind != textDoc {
				continue
			}
			detail := ""
			if d.path != "" {
				seen[d.path] = true
				detail = a.proj.rel(d.path)
			}
			items = append(items, item{label: d.name(), detail: detail, hint: locales.PanelOpenTab(), pick: func() { a.compareDocs(src, d) }})
		}
		for _, p := range a.proj.files {
			if !seen[p] {
				items = append(items, item{label: filepath.Base(p), detail: a.proj.rel(p), pick: func() { a.compareFile(src, p) }})
			}
		}
		items = ranked(items, byName(q))
		// Enter takes the best match of a query, not the dialog.
		if q != "" {
			return append(items, browse)
		}
		return append([]item{browse}, items...)
	}})
}

func (a *app) promptCompare(src *doc) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win})
		if err != nil || len(paths) == 0 {
			return
		}
		a.win.Update(func() { a.compareFile(src, paths[0]) })
	}()
}

// compareFile compares src with the file at path, as its tab has it when
// it is open.
func (a *app) compareFile(src *doc, path string) {
	if d := a.docAt(path); d != nil {
		a.compareDocs(src, d)
		return
	}
	fallback := a.set.FallbackEncoding
	go func() {
		raw, err := readText(path)
		text, _ := openText(raw, fallback)
		a.win.Update(func() {
			if err != nil {
				go mygo.Dialog.Error(locales.DialogUnableToOpen(filepath.Base(path)), err.Error())
				return
			}
			text, _ := splitLines(text)
			old := editor.NewBuffer(text)
			old.SetLanguage(editor.LanguageFor(path))
			a.compare(&comparison{src: src, oldName: filepath.Base(path), oldPath: path, diff: editor.DiffState{Old: old}})
		})
	}()
}

func (a *app) compareDocs(src, old *doc) {
	a.compare(&comparison{src: src, old: old, oldName: old.name(), oldPath: old.path, diff: editor.DiffState{Old: old.st.Buffer}})
}

func (a *app) compareClipboard(src *doc) {
	old := editor.NewBuffer(mygo.Clipboard.ReadText())
	old.SetLanguage(src.st.Buffer.Language())
	a.compare(&comparison{src: src, oldName: locales.CompareClipboard(), diff: editor.DiffState{Old: old}})
}

// showUnsavedChanges compares src with its text on disk, which saving
// makes the same. Only reverting acts on a change: keeping one would make
// it the text on disk's, which only saving does.
func (a *app) showUnsavedChanges(src *doc) {
	old := editor.NewBuffer(src.saved.String())
	old.SetLanguage(src.st.Buffer.Language())
	a.compare(&comparison{src: src, unsaved: true, oldName: src.name(), oldPath: src.path,
		diff: editor.DiffState{Old: old, Layout: editor.DiffUnified, HunkActions: true}})
}

// diskChanged has the tabs of d's unsaved changes compare it with its
// text on disk as saving or reloading left it.
func (l *lime) diskChanged(d *doc) {
	for _, a := range l.wins {
		for _, x := range a.allDocs() {
			if x.cmp != nil && x.cmp.unsaved && x.cmp.src == d {
				x.cmp.diff.Old.Reset(d.saved.String())
			}
		}
	}
}

// compare opens a diff tab of cmp, or tells that its texts are the same.
func (a *app) compare(cmp *comparison) {
	if sameText(cmp.diff.Old.Snapshot(), cmp.src.st.Buffer.Snapshot()) {
		a.notify(locales.StatusNoDifferences())
		return
	}
	cmp.diff.New, cmp.diff.Editable = cmp.src.st.Buffer, true
	d := newDoc("", "")
	d.kind, d.cmp = diffDoc, cmp
	g := a.group()
	g.docs = append(g.docs, d)
	a.show(d)
}

// diffView shows the diff tab d in place of the editor of the group at
// gi, under the names of its texts.
func (a *app) diffView(c *ui.Context, gi int, d *doc) {
	cmp := d.cmp
	df := &cmp.diff
	df.Theme, df.Bindings = a.scheme.editorTheme(), a.l.keys.editor
	df.Wrap, df.TabSize = a.set.WordWrap, a.set.TabSize
	df.Font.Family, df.Font.Size = a.set.ContentFont, a.set.FontSize
	df.RenderFindBar = func(c *ui.Context, bar ui.Element, f *editor.FindBar) { a.findBar(c, bar, f, d) }
	if cmp.unsaved {
		df.RenderHunkActions = a.revertAction
	}
	col := ui.Column(c.Key(key(d))).Grow(1).FillWidth().MinHeight(0)
	// The diff's editors are its own, out of FocusBind's reach: a click in
	// one gives its group the focus here.
	if col.FocusWithin() && a.current() != d {
		a.show(d)
	}
	col.Children(func() {
		a.diffHeader(c, cmp)
		editor.DiffView(c, df).Grow(1).FillWidth().MinHeight(0)
	})
}

// diffHeader names the texts over their sides, or the old one before the
// new one over a unified diff.
func (a *app) diffHeader(c *ui.Context, cmp *comparison) {
	ch := a.scheme.chrome()
	oldName, newName := cmp.oldName, cmp.src.name()
	oldPath, newPath := cmp.oldPath, cmp.src.path
	if cmp.unsaved {
		oldName, newName = locales.CompareOnDisk(oldName), locales.CompareEdited(newName)
		oldPath, newPath = "", ""
	}
	side := func(name, path string, editable bool) {
		ui.Text(c, name).FontSize(12).TextColor(ch.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		if path != "" {
			ui.Text(c, a.proj.rel(path)).FontSize(11).TextColor(ch.muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		if editable {
			ui.Box(c).Padding(1, 6).Radius(4).Shrink(0).Background(a.scheme.accent.Alpha(0.15)).Children(func() {
				ui.Text(c, locales.CompareEditable()).FontSize(10).TextColor(a.scheme.accent)
			})
		}
	}
	pane := func(fill func()) ui.Element {
		return ui.Row(c).Padding(0, 12).Gap(8).AlignItems(ui.Center).MinWidth(0).Children(fill)
	}
	ui.Row(c).FillWidth().Height(30).Shrink(0).AlignItems(ui.Stretch).Background(ch.bg).
		BorderWidth(0, 0, 1, 0).BorderColor(ch.line).Children(func() {
		if cmp.diff.Layout == editor.DiffUnified {
			pane(func() {
				side(oldName, oldPath, false)
				ui.Text(c, "→").FontSize(12).TextColor(ch.muted).Shrink(0)
				side(newName, newPath, true)
			}).Grow(1)
			return
		}
		// The halves line up with the sides, which the diff splits evenly.
		pane(func() { side(oldName, oldPath, false) }).Grow(1).Basis(0)
		ui.Box(c).Width(1).Background(ch.line)
		pane(func() { side(newName, newPath, true) }).Grow(1).Basis(0)
	})
}

// revertAction is the one action on an unsaved change, built as the
// diff's own are.
func (a *app) revertAction(c *ui.Context, bar ui.Element, h *editor.DiffHunkActions) {
	t := c.Theme()
	bar.Children(func() {
		// The editor's lines are 1.5 times its font high.
		btn := ui.ButtonBase(c).Padding(0, t.Space(2)).Height(a.set.FontSize*1.5-4).Radius(t.Radius-2).
			Background(t.Surface).Border(1, t.Border).Label(locales.CompareRevert())
		btn.Children(func() { ui.Text(c, locales.CompareRevert()).FontSize(max(11, t.FontSize-2)) })
		if btn.Clicked() {
			h.Revert()
		}
	})
}

// diffCounts shows how many changes cmp has, and how many lines they
// add and delete.
func (a *app) diffCounts(c *ui.Context, cmp *comparison) {
	n := len(cmp.diff.Hunks())
	text := locales.StatusDifferences(n)
	if cmp.unsaved {
		text = locales.StatusChanges(n)
	}
	added, deleted := cmp.diff.Stats()
	ui.Text(c, text).FontSize(12).TextColor(a.shell(c).muted)
	ui.Text(c, "+"+strconv.Itoa(added)).FontSize(12).TextColor(a.scheme.tokens[editor.TokenInserted].Color)
	ui.Text(c, "−"+strconv.Itoa(deleted)).FontSize(12).TextColor(a.scheme.danger())
}

// diffLayoutButton shows how cmp shows, which its menu changes.
func (a *app) diffLayoutButton(c *ui.Context, cmp *comparison) {
	name := func(l editor.DiffLayout) string {
		if l == editor.DiffUnified {
			return locales.CompareUnified()
		}
		return locales.CompareSideBySide()
	}
	df := &cmp.diff
	a.statusMenu(c, name(df.Layout), locales.StatusDiffLayout(name(df.Layout)), func(m *ui.Menu) {
		for _, l := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
			if m.Item(name(l)).Checked(l == df.Layout).Chosen() {
				df.Layout = l
			}
		}
	})
}

package main

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// section is a group of the Changes tab's files, as Zed's git panel
// groups them by staging.
type section uint8

const (
	conflicted section = iota
	staged
	unstaged
)

// changes is the side bar's Changes tab: the work tree's changes, read
// for root, by section.
type changes struct {
	root  string
	files [3][]change
	gen   int // counts the readings, for the last to win
	list  ui.ListState
	cur   int // the row chosen, -1 for none
	rows  []changeRow
}

// changeKey names a row of the Changes tab: a section's header, of no
// path, or a file of it.
type changeKey struct {
	sec  section
	path string
}

type changeRow struct {
	key  changeKey
	file *change // nil for a header
}

// readStatus reads the work tree's changes by section, as git status
// tells them. It blocks, so call it off the main thread.
func readStatus(root string) [3][]change {
	out, _ := runGit(root, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	var files [3][]change
	f := strings.Split(out, "\x00")
	for i := 0; i < len(f); i++ {
		line := f[i]
		if len(line) < 3 {
			continue
		}
		switch line[0] {
		case '?':
			files[unstaged] = append(files[unstaged], change{status: '?', path: line[2:]})
		case 'u':
			files[conflicted] = append(files[conflicted], change{status: 'U', path: statusField(line, 10)})
		case '1', '2':
			ch := change{path: statusField(line, 8)}
			if line[0] == '2' && i+1 < len(f) {
				i++
				ch.path, ch.oldPath = statusField(line, 9), f[i]
			}
			if x := line[2]; x != '.' {
				in := ch
				in.status = statusKind(x)
				files[staged] = append(files[staged], in)
			}
			if y := line[3]; y != '.' {
				files[unstaged] = append(files[unstaged], change{status: statusKind(y), path: ch.path})
			}
		}
	}
	return files
}

// statusField is field n of a line of git status, from 0: the last, a
// path, may hold spaces.
func statusField(line string, n int) string {
	f := strings.SplitN(line, " ", n+1)
	if len(f) <= n {
		return ""
	}
	return f[n]
}

// statusKind is the letter the tab shows: a copy shows as a rename, and
// a file whose type changed as modified.
func statusKind(c byte) byte {
	switch c {
	case 'C':
		return 'R'
	case 'T':
		return 'M'
	}
	return c
}

// readChanges reads the Changes tab again, keeping what shows until it is
// read.
func (a *app) readChanges() {
	root := a.git.root
	w := &a.changes
	if root != w.root {
		*w = changes{root: root, gen: w.gen, cur: -1}
	}
	if root == "" {
		return
	}
	w.gen++
	gen := w.gen
	a.background(func() func() {
		files := readStatus(root)
		return func() {
			if w.gen == gen {
				w.files = files
			}
		}
	})
}

// readWorktree reads the work tree's changes again: the Changes tab's,
// while it shows, and the tabs of their diffs.
func (a *app) readWorktree() {
	if a.side == gitPanel {
		a.readChanges()
	}
	for _, d := range a.allDocs() {
		if cv := d.commit; cv != nil && cv.worktree() && cv.root == a.git.root {
			a.loadCommit(cv)
		}
	}
}

// layRows lays the Changes tab out: each section with files under its
// header.
func (w *changes) layRows() {
	w.rows = w.rows[:0]
	for _, sec := range []section{conflicted, staged, unstaged} {
		files := w.files[sec]
		if len(files) == 0 {
			continue
		}
		w.rows = append(w.rows, changeRow{key: changeKey{sec: sec}})
		for i := range files {
			w.rows = append(w.rows, changeRow{key: changeKey{sec, files[i].path}, file: &files[i]})
		}
	}
}

// showChange shows the change of r: the diffs of its section, at its
// file, or a file in conflict itself.
func (a *app) showChange(r changeRow) {
	if r.file == nil {
		return
	}
	switch r.key.sec {
	case conflicted:
		a.open([]string{filepath.Join(a.changes.root, filepath.FromSlash(r.file.path))})
	case staged:
		a.showCommit(commit{hash: stagedChanges}).at = r.file.path
	case unstaged:
		a.showCommit(commit{hash: unstagedChanges}).at = r.file.path
	}
}

// The side bar's git panel shows the tab at gitTab.
const (
	changesTab = iota
	historyTab
)

// gitTabs switches the git panel between its Changes and History, as
// Zed's does.
func (a *app) gitTabs(c *ui.Context) {
	ch := a.shell(c)
	labels := []string{locales.GitChanges(), locales.GitHistory()}
	tabs := ui.TabsBase(c.Key("gitTabs"), &a.gitTab, len(labels))
	tabs.List.Margin(0, 8, 8, 8).Gap(2).Padding(2).Radius(7).Background(ch.bg).Border(1, ch.line).Children(func() {
		for i, label := range labels {
			t := tabs.Tab(i).Grow(1).Basis(0).Height(24).Radius(5).Center().Label(label).Transition(colorFade)
			fg := ch.muted
			switch {
			case i == a.gitTab:
				fg = ch.text
				bg := ch.panel
				if a.scheme.dark {
					bg = ch.panel.Mix(ch.text, 0.14)
				}
				t.Background(bg).Shadow(0, 1, 2, 0, black.Alpha(0.15))
			case t.Hovered():
				t.Background(ch.hover)
			}
			t.Children(func() { ui.Text(c, label).FontSize(12).TextColor(fg) })
		}
	})
}

// changesView lists the work tree's changes by section, each file of
// which shows its diff.
func (a *app) changesView(c *ui.Context) {
	w := &a.changes
	w.layRows()
	if len(w.rows) == 0 {
		ui.Column(c).Grow(1).FillWidth().Padding(16).Center().Children(func() {
			ui.Text(c, locales.ChangesNone()).FontSize(12).TextColor(a.shell(c).muted).TextAlign(ui.Center)
		})
		return
	}
	w.list.Selected = &w.cur
	w.list.Header = func(i int) bool { return w.rows[i].file == nil }
	colors := a.scheme.editorTheme()
	list := ui.List(c.Key("changes"), &w.list, len(w.rows)).ItemKey(func(i int) any { return w.rows[i].key }).
		Rows(func(r ui.ListRow) {
			a.changeRow(c, w.rows[r.Index], r.Selected(), colors.Added, colors.Deleted, colors.Modified)
		})
	list.Grow(1).FillWidth().Label(locales.GitChanges())
	if list.Changed() && w.cur >= 0 && w.cur < len(w.rows) {
		a.showChange(w.rows[w.cur])
	}
}

// changeRow is a section's header, or a file with its name, its folder
// and the letter of its change, tinted as the editor's gutter tints it.
func (a *app) changeRow(c *ui.Context, r changeRow, selected bool, added, deleted, modified ui.Color) {
	th := c.Theme()
	ch := a.shell(c)
	if r.file == nil {
		label := [...]string{locales.ChangesConflicts(), locales.ChangesStaged(), locales.ChangesUnstaged()}[r.key.sec]
		ui.Row(c).FillWidth().Height(th.Space(7)).Padding(0, th.Space(3)).AlignItems(ui.Center).Background(ch.bg).Children(func() {
			ui.Text(c, label).FontSize(11).Bold().TextColor(ch.muted).SingleLine()
		})
		return
	}
	text, muted := ch.text, ch.muted
	if selected {
		text, muted = th.AccentText, th.AccentText.Alpha(0.8)
	}
	tint := modified
	switch r.file.status {
	case 'A', '?':
		tint = added
	case 'D', 'U':
		tint = deleted
	}
	if selected {
		tint = th.AccentText
	}
	p := r.file.path
	ui.Row(c).FillWidth().Height(th.Space(7)).Gap(treeGap).Padding(0, th.Space(3)).AlignItems(ui.Center).Children(func() {
		ic := fileIcon(p, a.scheme.dark)
		if selected {
			ic.view(c, treeIcon, text)
		} else {
			ic.view(c, treeIcon, ic.tint(muted))
		}
		ui.Text(c, path.Base(p)).FontSize(13).TextColor(text).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(180)
		if dir := path.Dir(p); dir != "." {
			ui.Text(c, dir).FontSize(12).TextColor(muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		ui.Box(c).Grow(1)
		ui.Text(c, string(r.file.status)).Font(a.codeFont()).FontSize(12).TextColor(tint).Shrink(0)
	})
}

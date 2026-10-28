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
	sel   ui.Selection[changeKey]
	cur   int // the row chosen last, -1 for none
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

// stageFiles stages files, or unstages them, as their paths name them
// literally.
func (a *app) stageFiles(files []change, unstage bool) {
	if len(files) == 0 {
		return
	}
	var paths strings.Builder
	for _, f := range files {
		paths.WriteString(f.path + "\x00")
		if f.oldPath != "" {
			paths.WriteString(f.oldPath + "\x00")
		}
	}
	// reset, unlike restore --staged, unstages before the first commit.
	cmd := []string{"--literal-pathspecs", "add", "--pathspec-from-file=-", "--pathspec-file-nul"}
	if unstage {
		cmd = []string{"--literal-pathspecs", "reset", "-q", "--pathspec-from-file=-", "--pathspec-file-nul"}
	}
	a.gitRunWith(paths.String(), nil, cmd...)
}

// chosen returns the files of sec chosen. A file in conflict is staged
// alone, as it is resolved.
func (w *changes) chosen(sec section) []change {
	var files []change
	for _, f := range w.files[sec] {
		if sec != conflicted && w.sel.Has(changeKey{sec, f.path}) {
			files = append(files, f)
		}
	}
	return files
}

// stageChosen stages the files chosen, or unstages them as the row
// chosen last is staged, as Space does in Zed's git panel.
func (a *app) stageChosen() {
	w := &a.changes
	if w.cur < 0 || w.cur >= len(w.rows) {
		return
	}
	if w.rows[w.cur].key.sec == staged {
		a.stageFiles(w.chosen(staged), true)
		return
	}
	a.stageFiles(w.chosen(unstaged), false)
}

// changeMenu is the menu of the rows chosen.
func (a *app) changeMenu(m *ui.Menu) {
	w := &a.changes
	toStage, toUnstage := w.chosen(unstaged), w.chosen(staged)
	if m.Item(locales.ChangesStage()).Disabled(len(toStage) == 0).Chosen() {
		a.stageFiles(toStage, false)
	}
	if m.Item(locales.ChangesUnstage()).Disabled(len(toUnstage) == 0).Chosen() {
		a.stageFiles(toUnstage, true)
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
	list := ui.List(c.Key("changes"), &w.list, len(w.rows)).ItemKey(func(i int) any { return w.rows[i].key }).Selection(&w.sel).
		Rows(func(r ui.ListRow) {
			a.changeRow(c, w.rows[r.Index], r.Selected(), colors.Added, colors.Deleted, colors.Modified)
		})
	list.Grow(1).FillWidth().Label(locales.GitChanges())
	if list.Changed() && w.cur >= 0 && w.cur < len(w.rows) {
		a.showChange(w.rows[w.cur])
	}
	if w.list.Shortcut(c, 0, ui.KeySpace) {
		a.stageChosen()
	}
}

// stageBox is the check box of a file of sec, or of all of them: checked
// in Staged, where a click unstages, and unchecked elsewhere, where it
// stages.
func (a *app) stageBox(c *ui.Context, sec section, files []change, all bool) {
	on := sec == staged
	var label string
	switch {
	case all && on:
		label = locales.ChangesUnstageAll()
	case all:
		label = locales.ChangesStageAll()
	case on:
		label = locales.ChangesUnstage()
	default:
		label = locales.ChangesStage()
	}
	ui.Checkbox(c, &on, "").Shrink(0).Label(label).Tooltip(label).OnClick(func() { a.stageFiles(files, sec == staged) })
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
			ui.Box(c).Grow(1)
			if r.key.sec != conflicted {
				a.stageBox(c, r.key.sec, a.changes.files[r.key.sec], true)
			}
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
	ui.Row(c).FillWidth().Height(th.Space(7)).Gap(treeGap).Padding(0, th.Space(3)).AlignItems(ui.Center).ContextMenu(a.changeMenu).Children(func() {
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
		a.stageBox(c, r.key.sec, []change{*r.file}, false)
	})
}

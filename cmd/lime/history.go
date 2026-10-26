package main

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// maxCommits is how many commits the history lists.
const maxCommits = 300

// uncommitted stands in the history for the work tree's changes.
const uncommitted = ""

type commit struct{ hash, short, subject, author, date string }

// change is a file a commit changes: added, modified, deleted or renamed
// (A, M, D, R), with its path before a rename.
type change struct {
	status        byte
	path, oldPath string // relative to the work tree's top, with slashes
}

// history is the side bar's list of the repository's commits, the work
// tree's changes first, read for root.
type history struct {
	root    string
	commits []commit
	changes []change            // the work tree's
	files   map[string][]change // the files of the commits shown open
	open    map[string]bool
	gen     int // counts the readings, for the last to win
}

func readCommits(root string) []commit {
	out, _ := runGit(root, "log", "-n"+strconv.Itoa(maxCommits), "--format=%H%x1f%h%x1f%s%x1f%an%x1f%as%x1e")
	var cs []commit
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.Split(strings.TrimSpace(rec), "\x1f")
		if len(f) == 5 {
			cs = append(cs, commit{f[0], f[1], f[2], f[3], f[4]})
		}
	}
	return cs
}

// readChanges reads the files hash changes against its first parent, or
// the work tree's changes against HEAD, staged or not, and the untracked
// files.
func readChanges(root, hash string) []change {
	if hash == uncommitted {
		out, _ := runGit(root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
		var cs []change
		f := strings.Split(out, "\x00")
		for i := 0; i < len(f); i++ {
			if len(f[i]) < 4 {
				continue
			}
			x, y, path := f[i][0], f[i][1], f[i][3:]
			ch := change{status: y, path: path}
			switch {
			case x == '?':
				ch.status = 'A'
			case x == 'R' || x == 'C':
				i++
				ch.status, ch.oldPath = 'R', f[i]
			case y == ' ':
				ch.status = x
			}
			cs = append(cs, ch)
		}
		return cs
	}
	out, _ := runGit(root, "diff-tree", "-r", "-z", "-M", "--name-status", "--root", "--no-commit-id", "--diff-merges=first-parent", hash)
	var cs []change
	f := strings.Split(out, "\x00")
	for i := 0; i+1 < len(f); i += 2 {
		if f[i] == "" {
			break
		}
		ch := change{status: f[i][0], path: f[i+1]}
		if ch.status == 'R' || ch.status == 'C' {
			ch.status, ch.oldPath, ch.path = 'R', f[i+1], f[i+2]
			i++
		}
		cs = append(cs, ch)
	}
	return cs
}

// readHistory reads the history again, keeping what shows until it is
// read, as the side bar shows it or git changes it.
func (a *app) readHistory() {
	root := a.git.root
	if root != a.hist.root {
		a.hist = history{root: root, gen: a.hist.gen}
	}
	if root == "" {
		return
	}
	a.hist.gen++
	gen := a.hist.gen
	a.background(func() func() {
		cs, ch := readCommits(root), readChanges(root, uncommitted)
		return func() {
			if a.hist.gen == gen {
				a.hist.commits, a.hist.changes = cs, ch
			}
		}
	})
}

// toggleCommit shows or hides the files hash changes.
func (a *app) toggleCommit(hash string) {
	h := &a.hist
	if h.open == nil {
		h.open, h.files = map[string]bool{}, map[string][]change{}
	}
	h.open[hash] = !h.open[hash]
	if _, ok := h.files[hash]; ok || hash == uncommitted || !h.open[hash] {
		return
	}
	root := h.root
	a.background(func() func() {
		cs := readChanges(root, hash)
		return func() {
			if a.hist.root == root {
				a.hist.files[hash] = cs
			}
		}
	})
}

// showChange opens a diff tab of what hash changed in ch, which neither
// side edits: the commit's first parent against it, or HEAD against the
// work tree.
func (a *app) showChange(hash, short string, ch change) {
	root := a.hist.root
	oldPath := ch.path
	if ch.oldPath != "" {
		oldPath = ch.oldPath
	}
	a.background(func() func() {
		oldRev, newRev := hash+"^", hash
		if hash == uncommitted {
			oldRev = "HEAD"
		}
		var oldText, newText string
		if ch.status != 'A' {
			oldText, _ = runGit(root, "show", oldRev+":"+oldPath)
		}
		switch {
		case ch.status == 'D':
		case hash == uncommitted:
			newText, _ = readText(filepath.Join(root, filepath.FromSlash(ch.path)))
		default:
			newText, _ = runGit(root, "show", newRev+":"+ch.path)
		}
		return func() {
			if binary(oldText) || binary(newText) {
				a.notify(locales.PreviewBinary())
				return
			}
			path := filepath.Join(root, filepath.FromSlash(ch.path))
			lang := editor.LanguageFor(path)
			old := editor.NewBuffer(oldText)
			old.SetLanguage(lang)
			src := newDoc("", newText)
			src.st.Buffer.SetLanguage(lang)
			name, oldName := filepath.Base(path), filepath.Base(oldPath)
			cmp := &comparison{src: src, fixed: true, oldPath: filepath.Join(root, filepath.FromSlash(oldPath)), newPath: path,
				diff: editor.DiffState{Old: old}}
			if hash == uncommitted {
				cmp.oldName, cmp.newName = oldName+" @ HEAD", name
			} else {
				cmp.oldName, cmp.newName = oldName+" @ "+short+"^", name+" @ "+short
			}
			a.compare(cmp)
		}
	})
}

// binary tells binary text by a NUL in its first 8000 bytes, as fileKind
// does.
func binary(s string) bool { return strings.IndexByte(s[:min(len(s), 8000)], 0) >= 0 }

// historyView lists the work tree's changes and the commits, each opening
// to the files it changes, which open their diffs.
func (a *app) historyView(c *ui.Context) {
	h := &a.hist
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Role(ui.RoleTree).Children(func() {
			if len(h.changes) > 0 {
				a.historyRow(c, uncommitted, locales.HistoryUncommitted(), locales.HistoryFiles(len(h.changes)))
			}
			for _, cm := range h.commits {
				a.historyRow(c, cm.hash, cm.subject, cm.short+" · "+cm.author+" · "+cm.date)
			}
		})
	})
}

func (a *app) historyRow(c *ui.Context, hash, title, detail string) {
	t := c.Theme()
	ch := a.shell(c)
	h := &a.hist
	open := h.open[hash]
	row := ui.Column(c.Key("commit "+hash)).FillWidth().Padding(t.Space(1), t.Space(2)).Gap(1).Radius(t.Radius).
		Role(ui.RoleTreeItem).Level(1).Label(title).Expanded(open).Transition(colorFade).OnClick(func() { a.toggleCommit(hash) })
	if row.Hovered() {
		row.Background(ch.hover)
	}
	row.Children(func() {
		ui.Text(c, title).SingleLine().Ellipsis("…").MinWidth(0)
		ui.Text(c, detail).FontSize(11).TextColor(ch.muted).SingleLine().Ellipsis("…").MinWidth(0)
	})
	if !open {
		return
	}
	files := h.changes
	if hash != uncommitted {
		files = h.files[hash]
	}
	short := ""
	for _, cm := range h.commits {
		if cm.hash == hash {
			short = cm.short
		}
	}
	ui.Column(c.Key("files " + hash)).FillWidth().Transition(treeMotion).Children(func() {
		for _, f := range files {
			a.changeRow(c, f, func() { a.showChange(hash, short, f) })
		}
	})
}

// changeRow is a file of a commit: its status, its name, and its folder.
func (a *app) changeRow(c *ui.Context, f change, onClick func()) {
	t := c.Theme()
	ch := a.shell(c)
	tint := a.scheme.accent
	switch f.status {
	case 'A':
		tint = a.scheme.tokens[editor.TokenInserted].Color
	case 'D':
		tint = a.scheme.danger()
	}
	row := ui.Row(c.Key(f.path)).FillWidth().Height(t.Space(7)).Gap(treeGap).AlignItems(ui.Center).
		Padding(0, t.Space(2)).PaddingStart(t.Space(2) + treeIcon + treeGap).Radius(t.Radius).
		Role(ui.RoleTreeItem).Level(2).Label(f.path).Transition(colorFade).OnClick(onClick)
	if row.Hovered() {
		row.Background(ch.hover)
	}
	row.Children(func() {
		ui.Text(c, string(f.status)).FontSize(11).TextColor(tint).Width(treeIcon).Shrink(0).TextAlign(ui.Center)
		ui.Text(c, filepath.Base(f.path)).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(200)
		if dir := filepath.Dir(filepath.FromSlash(f.path)); dir != "." {
			ui.Text(c, dir).FontSize(11).TextColor(ch.muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
	})
}

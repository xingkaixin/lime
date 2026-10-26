package main

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// commitView is what a commit's tab shows, as Zed's commit view: who made
// it, when and its message over the diffs of all the files it changes,
// one under another; or the work tree's changes, without the commit.
type commitView struct {
	commit
	root   string
	body   string // the whole message
	files  []*commitFile
	loaded bool
	list   ui.ListState
	layout editor.DiffLayout
}

// commitFile is a file of a commit's tab: its counts, and its diff once
// read. Neither side of the diff edits.
type commitFile struct {
	change
	added, deleted int
	binary         bool // shown folded, without its text, as Zed does
	folded         bool
	diff           *editor.DiffState
}

func (cv *commitView) title() string {
	if cv.hash == uncommitted {
		return locales.HistoryUncommitted()
	}
	return cv.short + " — " + cv.subject
}

// showCommit opens a tab of cm's changes, or shows the one open.
func (a *app) showCommit(cm commit) {
	root := a.hist.root
	for _, d := range a.allDocs() {
		if cv := d.commit; cv != nil && cv.hash == cm.hash && cv.root == root {
			a.show(d)
			return
		}
	}
	cv := &commitView{commit: cm, root: root}
	d := newDoc("", "")
	d.kind, d.commit = commitDoc, cv
	g := a.group()
	g.docs = append(g.docs, d)
	a.show(d)
	a.background(func() func() {
		body, files := readCommit(root, cm.hash)
		return func() { cv.body, cv.files, cv.loaded = body, files, true }
	})
}

// readCommit reads the message of hash and the files it changes, with
// their texts before and after. It blocks, so call it off the main thread.
func readCommit(root, hash string) (string, []*commitFile) {
	var body string
	oldRev, newRev := hash+"^", hash
	if hash == uncommitted {
		oldRev, newRev = "HEAD", ""
	} else {
		body, _ = runGit(root, "show", "-s", "--format=%B", hash)
	}
	counts := readNumstat(root, hash)
	var files []*commitFile
	var specs []string
	for _, ch := range readChanges(root, hash) {
		f := &commitFile{change: ch}
		n, ok := counts[ch.path]
		f.added, f.deleted, f.binary = n[0], n[1], ok && n[0] < 0
		f.folded = f.binary
		files = append(files, f)
		old := ch.path
		if ch.oldPath != "" {
			old = ch.oldPath
		}
		specs = append(specs, oldRev+":"+old, newRev+":"+ch.path)
	}
	texts := catFiles(root, specs)
	for i, f := range files {
		if f.binary {
			continue
		}
		oldText, newText := texts[2*i], texts[2*i+1]
		// The work tree's text is on disk, not in git.
		if hash == uncommitted {
			newText = ""
			if f.status != 'D' {
				newText, _ = readText(filepath.Join(root, filepath.FromSlash(f.path)))
			}
		}
		if binary(oldText) || binary(newText) {
			f.binary, f.folded = true, true
			continue
		}
		// The work tree's untracked files are in no numstat.
		if _, ok := counts[f.path]; !ok && f.status == 'A' {
			f.added = lineCount(newText)
		}
		lang := editor.LanguageFor(f.path)
		old, cur := editor.NewBuffer(oldText), editor.NewBuffer(newText)
		old.SetLanguage(lang)
		cur.SetLanguage(lang)
		f.diff = &editor.DiffState{Old: old, New: cur, FitContent: true}
	}
	return strings.TrimSpace(body), files
}

func lineCount(s string) int {
	n := strings.Count(s, "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// binary tells binary text by a NUL in its first 8000 bytes, as fileKind
// does.
func binary(s string) bool { return strings.IndexByte(s[:min(len(s), 8000)], 0) >= 0 }

// readNumstat counts the lines hash adds and deletes in each file, by its
// path; -1 for a binary file.
func readNumstat(root, hash string) map[string][2]int {
	args := []string{"diff-tree", "-r", "-z", "-M", "--numstat", "--root", "--no-commit-id", "--diff-merges=first-parent", hash}
	if hash == uncommitted {
		args = []string{"diff", "-z", "-M", "--numstat", "HEAD"}
	}
	out, _ := runGit(root, args...)
	counts := map[string][2]int{}
	f := strings.Split(out, "\x00")
	for i := 0; i < len(f); i++ {
		parts := strings.SplitN(f[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		// A rename's paths follow its counts: the old, then the new.
		if path == "" && i+2 < len(f) {
			path = f[i+2]
			i += 2
		}
		added, err1 := strconv.Atoi(parts[0])
		deleted, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			added, deleted = -1, -1
		}
		counts[path] = [2]int{added, deleted}
	}
	return counts
}

// catFiles reads the objects named rev:path with one git, "" for each
// that is missing, as a file a commit adds has no text before it.
func catFiles(root string, specs []string) []string {
	texts := make([]string, len(specs))
	var in strings.Builder
	for _, s := range specs {
		in.WriteString(s)
		in.WriteByte('\n')
	}
	out, err := gitWith(root, in.String(), "cat-file", "--batch")
	if err != nil {
		return texts
	}
	b := []byte(out)
	for i := range specs {
		nl := bytes.IndexByte(b, '\n')
		if nl < 0 {
			break
		}
		head := strings.Fields(string(b[:nl]))
		b = b[nl+1:]
		if len(head) != 3 {
			continue // missing
		}
		size, err := strconv.Atoi(head[2])
		if err != nil || size > len(b) {
			break
		}
		texts[i] = string(b[:size])
		b = b[min(size+1, len(b)):]
	}
	return texts
}

// commitTab shows the commit's tab d in place of the editor of the group
// at gi: a bar over the commit's message and its files' diffs, each under
// its header, which stays at the top while its diff scrolls under it.
func (a *app) commitTab(c *ui.Context, gi int, d *doc) {
	cv := d.commit
	col := ui.Column(c.Key(key(d))).Grow(1).FillWidth().MinHeight(0).Background(a.scheme.bg)
	// The diffs' editors are their own, out of FocusBind's reach.
	if col.FocusWithin() && a.current() != d {
		a.show(d)
	}
	col.Children(func() {
		a.commitBar(c, cv)
		if !cv.loaded {
			return
		}
		cv.list.Header = func(row int) bool { return row > 0 && row%2 == 1 }
		cv.list.Key = func(row int) any {
			if row == 0 {
				return "message"
			}
			return [2]any{row % 2, cv.files[(row-1)/2].path}
		}
		ui.List(c.Key("files"), &cv.list, 1+2*len(cv.files), func(row int) {
			switch {
			case row == 0:
				a.commitMessage(c, cv)
			case row%2 == 1:
				a.fileHeader(c, cv.files[(row-1)/2])
			default:
				a.fileBody(c, cv, cv.files[(row-1)/2])
			}
		}).Grow(1).FillWidth().Label(cv.title())
	})
}

var (
	chevronIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<path fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round" d="m4.5 6 3.5 3.5L11.5 6" />
</svg>`))
	foldAllIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<path fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round" d="m5 2.5 3 3 3-3M5 13.5l3-3 3 3M2.5 8h11" />
</svg>`))
	unfoldAllIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<path fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round" d="m5 5.5 3-3 3 3M5 10.5l3 3 3-3M2.5 8h11" />
</svg>`))
	splitIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<g fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round">
		<rect x="1.5" y="2.5" width="13" height="11" rx="1.5" />
		<path d="M8 2.5v11" />
	</g>
</svg>`))
	unifiedIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<g fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round">
		<rect x="1.5" y="2.5" width="13" height="11" rx="1.5" />
		<path d="M4.5 6h7M4.5 10h7" />
	</g>
</svg>`))
)

// commitBar folds or unfolds all the files, lays the diffs out side by
// side or one over the other, and counts the lines they add and delete.
func (a *app) commitBar(c *ui.Context, cv *commitView) {
	ch := a.scheme.chrome()
	added, deleted, open := 0, 0, false
	for _, f := range cv.files {
		added, deleted = added+max(f.added, 0), deleted+max(f.deleted, 0)
		open = open || !f.folded
	}
	ui.Row(c).FillWidth().Height(30).Shrink(0).Padding(0, 8).Gap(2).AlignItems(ui.Center).Background(ch.bg).
		BorderWidth(0, 0, 1, 0).BorderColor(ch.line).Children(func() {
		if open {
			a.barButton(c, foldAllIcon, locales.CommitCollapseAll(), false, func() { cv.foldAll(true) })
		} else {
			a.barButton(c, unfoldAllIcon, locales.CommitExpandAll(), false, func() { cv.foldAll(false) })
		}
		a.barButton(c, splitIcon, locales.CompareSideBySide(), cv.layout == editor.DiffSplit, func() { cv.layout = editor.DiffSplit })
		a.barButton(c, unifiedIcon, locales.CompareUnified(), cv.layout == editor.DiffUnified, func() { cv.layout = editor.DiffUnified })
		ui.Box(c).Grow(1)
		if cv.loaded {
			a.counts(c, added, deleted)
		}
	})
}

func (cv *commitView) foldAll(fold bool) {
	for _, f := range cv.files {
		f.folded = fold
	}
}

// barButton is an icon button of a tab's bar, tinted while on.
func (a *app) barButton(c *ui.Context, icon *ui.SVG, label string, on bool, onClick func()) {
	ch := a.scheme.chrome()
	btn := ui.ButtonBase(c.Key(label)).Size(24, 22).Radius(4).Center().Transition(colorFade).
		Label(label).Tooltip(label).OnClick(onClick)
	tint := ch.muted
	switch {
	case on:
		btn.Background(a.scheme.accent.Alpha(0.15))
		tint = a.scheme.accent
	case btn.Hovered():
		btn.Background(ch.hover)
	}
	btn.Children(func() { ui.Icon(c, icon).Size(14, 14).TextColor(tint) })
}

func (a *app) counts(c *ui.Context, added, deleted int) {
	ui.Text(c, "+"+strconv.Itoa(added)).FontSize(12).TextColor(a.scheme.tokens[editor.TokenInserted].Color).Shrink(0)
	ui.Text(c, "−"+strconv.Itoa(deleted)).FontSize(12).TextColor(a.scheme.danger()).Shrink(0)
}

// commitMessage is the first row: the author's picture, name and email,
// when, the message, and the hash to copy. The work tree's has none.
func (a *app) commitMessage(c *ui.Context, cv *commitView) {
	if cv.hash == uncommitted {
		ui.Box(c).FillWidth().Height(0)
		return
	}
	ch := a.scheme.chrome()
	ui.Row(c).FillWidth().Padding(16).Gap(12).AlignItems(ui.Start).Background(a.scheme.bg).Children(func() {
		a.avatar(c, cv.email, 40)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, cv.author).FontSize(13).Bold().TextColor(ch.text)
			ui.Text(c, cv.when.Format("2006-01-02 15:04")+" · "+cv.email).FontSize(12).TextColor(ch.muted).SingleLine().Ellipsis("…")
			ui.Text(c, cv.body).FontSize(13).TextColor(ch.text).Margin(6, 0, 0, 0)
		})
		label := locales.CommitCopySHA()
		btn := ui.ButtonBase(c).Height(22).Padding(0, 8).Radius(4).Shrink(0).Transition(colorFade).
			Label(label).Tooltip(cv.hash)
		if btn.Hovered() {
			btn.Background(ch.hover)
		}
		services, hash := c.Services(), cv.hash
		btn.OnClick(func() {
			services.WriteClipboard(hash)
			a.notify(locales.CommitCopied(cv.short))
		})
		btn.Children(func() { ui.Text(c, label).FontSize(12).TextColor(ch.muted) })
	})
}

// fileHeader names a file of the commit with its counts; a click folds
// or unfolds its diff.
func (a *app) fileHeader(c *ui.Context, f *commitFile) {
	ch := a.scheme.chrome()
	row := ui.Row(c).FillWidth().Height(32).Padding(0, 12).Gap(8).AlignItems(ui.Center).Background(ch.bg).
		BorderWidth(1, 0, 1, 0).BorderColor(ch.line).Role(ui.RoleButton).Label(f.path).Expanded(!f.folded).
		OnClick(func() { f.folded = !f.folded })
	row.Children(func() {
		chevron := ui.Icon(c, chevronIcon).Size(14, 14).Shrink(0).TextColor(ch.muted)
		if f.folded {
			chevron.Rotate(-90)
		}
		fileIcon(f.path, a.scheme.dark).view(c, ch.muted)
		name, dir := filepath.Base(f.path), filepath.Dir(filepath.FromSlash(f.path))
		ui.Text(c, name).FontSize(13).TextColor(ch.text).SingleLine().Shrink(0).MaxWidth(320).Ellipsis("…")
		if dir != "." {
			ui.Text(c, dir).FontSize(12).TextColor(ch.muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		ui.Box(c).Grow(1)
		if !f.binary {
			a.counts(c, f.added, f.deleted)
		}
	})
}

// fileBody is a file's diff, or why it shows none.
func (a *app) fileBody(c *ui.Context, cv *commitView, f *commitFile) {
	switch {
	case f.folded:
		ui.Box(c).FillWidth().Height(0)
	case f.binary || f.diff == nil:
		ui.Text(c, locales.PreviewBinary()).FontSize(12).TextColor(a.scheme.chrome().muted).Padding(10, 16)
	default:
		df := f.diff
		df.Layout, df.Theme = cv.layout, a.scheme.editorTheme()
		df.Wrap, df.TabSize = a.set.WordWrap, a.set.TabSize
		df.Font.Family, df.Font.Size = a.set.ContentFont, a.set.FontSize
		editor.DiffView(c.Key(f.path), df).FillWidth()
	}
}

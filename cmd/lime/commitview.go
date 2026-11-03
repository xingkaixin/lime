package main

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// commitView is what a commit's tab shows, as Zed's commit view: who made
// it, when and its message over the diffs of all the files it changes,
// one under another; or the changes staged in the index, or those not,
// without the commit. Each
// line of the diffs is a row of one list, as godiff's are, so that only
// the lines in view are laid out, however long the diffs.
type commitView struct {
	commit
	root    string
	body    string // the whole message
	more    bool   // the whole message shows, not its subject alone
	files   []*commitFile
	loaded  bool
	unified bool
	rows    []diffRow
	dirty   bool // the rows are to be built again
	list    ui.ListState
	gen     int    // counts the readings, for the last to win
	at      string // the file to scroll to, once its rows are built
	github  string // the repository's web address on GitHub, "" for none
}

// The tabs of the work tree's changes stand for commits of these hashes,
// which no commit has.
const (
	stagedChanges   = "staged"
	unstagedChanges = "unstaged"
)

// worktree reports whether cv shows the work tree's changes, not a
// commit's.
func (cv *commitView) worktree() bool {
	return cv.hash == stagedChanges || cv.hash == unstagedChanges
}

// commitFile is a file of a commit's tab: its hunks, as git prints them,
// and its texts before and after, from which the rows take their lines.
type commitFile struct {
	change
	added, deleted int
	binary         bool // shown folded, without its text, as Zed does
	folded         bool
	hunks          []editor.PatchHunk
	old, new       fileText
	shown          map[int]bool // the gaps unfolded, by the hunk after them
}

// fileText is a side of a file's diff: its lines and their tokens.
type fileText struct {
	lines  []string
	tokens [][]editor.Token // nil for a language MyGo does not know
}

type rowKind uint8

const (
	rowMessage rowKind = iota
	rowHeader
	rowNote   // why a file shows no lines
	rowGap    // unchanged lines, folded
	rowSame   // an unchanged line
	rowChange // a line deleted or added, or side by side, one of each
	rowHunk   // the head of the hunk gap of the work tree's, to stage or unstage it
)

// diffRow is a row of a commit's tab. old and new number its lines before
// and after from 1, 0 for none; a gap's are its first, of count, before
// the hunk gap.
type diffRow struct {
	kind       rowKind
	file       int
	old, new   int
	count, gap int
}

func (cv *commitView) title() string {
	switch cv.hash {
	case stagedChanges:
		return locales.ChangesStagedTitle()
	case unstagedChanges:
		return locales.ChangesUnstagedTitle()
	}
	return cv.short + " — " + cv.subject
}

// showCommit opens a tab of cm's changes, or shows the one open.
func (a *app) showCommit(cm commit) *commitView { return a.showCommitIn(a.git.root, cm) }

// showCommitIn opens a tab of cm's changes in the repository at root, or
// shows the one open.
func (a *app) showCommitIn(root string, cm commit) *commitView {
	for _, d := range a.allDocs() {
		if cv := d.commit; cv != nil && cv.hash == cm.hash && cv.root == root {
			a.show(d)
			return cv
		}
	}
	cv := &commitView{commit: cm, root: root, unified: a.set.DiffUnified}
	d := newDoc("", "")
	d.kind, d.commit = commitDoc, cv
	g := a.group()
	g.docs = append(g.docs, d)
	a.show(d)
	a.loadCommit(cv)
	return cv
}

// loadCommit reads cv's message and files, again for the work tree's,
// whose files stay folded as they were.
func (a *app) loadCommit(cv *commitView) {
	root, hash := cv.root, cv.hash
	cv.gen++
	gen := cv.gen
	a.background(func() func() {
		var github string
		var wg sync.WaitGroup
		if hash != stagedChanges && hash != unstagedChanges && runtime.GOOS != "js" {
			wg.Go(func() { github, _ = githubRepo(root) })
		}
		body, files := readCommit(root, hash)
		wg.Wait()
		return func() {
			if cv.gen != gen {
				return
			}
			folded := map[string]bool{}
			for _, f := range cv.files {
				folded[f.path] = f.folded
			}
			for _, f := range files {
				if v, ok := folded[f.path]; ok {
					f.folded = v
				}
			}
			cv.body, cv.files, cv.loaded, cv.dirty, cv.github = body, files, true, true, github
		}
	})
}

// readCommit reads the message of hash and the files it changes, with
// their hunks and texts. It blocks, so call it off the main thread.
func readCommit(root, hash string) (string, []*commitFile) {
	var body string
	var changes []change
	// Porcelain, unlike diff-tree, reads the user's diff settings, but
	// not textconv: the hunks apply back to the bytes, to stage or discard.
	diff := []string{"diff", "-M", "--no-color", "--no-ext-diff", "--no-textconv", "--src-prefix=a/", "--dst-prefix=b/"}
	// An empty rev names the index.
	var args []string
	var oldRev, newRev string
	var wg sync.WaitGroup
	switch hash {
	case stagedChanges:
		args, oldRev, newRev = append(diff, "--cached"), "HEAD", ""
		wg.Go(func() { changes = readStatus(root)[staged] })
	case unstagedChanges:
		args, oldRev, newRev = diff, "", ""
		wg.Go(func() { changes = readStatus(root)[unstaged] })
	default:
		args = []string{"diff-tree", "-r", "-p", "-M", "--root", "--no-commit-id", "--diff-merges=first-parent", hash}
		oldRev, newRev = hash+"^", hash
		wg.Go(func() { body, _ = runGit(root, "show", "-s", "--format=%B", hash) })
		wg.Go(func() { changes = commitChanges(root, hash) })
	}
	out, _ := runGit(root, args...)
	wg.Wait()
	patches, _ := editor.ParsePatch(out)
	byPath := map[string]editor.FilePatch{}
	for _, p := range patches {
		name := p.NewName
		if name == "" {
			name = p.OldName
		}
		byPath[name] = p
	}
	var files []*commitFile
	var specs []string
	for _, ch := range changes {
		p, ok := byPath[ch.path]
		f := &commitFile{change: ch, hunks: p.Hunks, binary: p.Binary, folded: p.Binary}
		files = append(files, f)
		// The work tree's untracked files are in no diff.
		if !ok && ch.status == '?' {
			text, _ := readText(filepath.Join(root, filepath.FromSlash(ch.path)))
			f.binary, f.folded = binary(text), binary(text)
			if !f.binary {
				f.new = splitText(text, ch.path)
				f.hunks = addedHunk(f.new.lines)
			}
		}
		if f.binary || !ok {
			continue
		}
		old := ch.path
		if ch.oldPath != "" {
			old = ch.oldPath
		}
		specs = append(specs, oldRev+":"+old, newRev+":"+ch.path)
	}
	texts := catFiles(root, specs)
	for _, f := range files {
		if _, ok := byPath[f.path]; !ok || f.binary {
			continue
		}
		oldText, newText := texts[0], texts[1]
		texts = texts[2:]
		// The work tree's text is on disk, not in git.
		if hash == unstagedChanges {
			newText = ""
			if f.status != 'D' {
				newText, _ = readText(filepath.Join(root, filepath.FromSlash(f.path)))
			}
		}
		f.old, f.new = splitText(oldText, f.path), splitText(newText, f.path)
	}
	for _, f := range files {
		for _, h := range f.hunks {
			for _, l := range h.Lines {
				switch {
				case strings.HasPrefix(l, "+"):
					f.added++
				case strings.HasPrefix(l, "-"):
					f.deleted++
				}
			}
		}
	}
	return strings.TrimSpace(body), files
}

// addedHunk is the hunk of a file added with lines.
func addedHunk(lines []string) []editor.PatchHunk {
	if len(lines) == 0 {
		return nil
	}
	h := editor.PatchHunk{NewStart: 1, NewLines: len(lines), Lines: make([]string, len(lines))}
	for i, l := range lines {
		h.Lines[i] = "+" + l
	}
	return []editor.PatchHunk{h}
}

// splitText splits text into lines and highlights them, as the language
// of the file name highlights.
func splitText(text, name string) fileText {
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	t := fileText{lines: lines}
	lang := editor.LanguageFor(name)
	if lang == nil || lang.Tokenizer == nil {
		return t
	}
	t.tokens = make([][]editor.Token, len(lines))
	var state uint64
	for i, l := range lines {
		t.tokens[i], state = lang.Tokenizer.Tokenize(l, state, nil)
	}
	return t
}

// binary tells binary text by a NUL in its first 8000 bytes, as fileKind
// does.
func binary(s string) bool { return strings.IndexByte(s[:min(len(s), 8000)], 0) >= 0 }

// hunkStart is where a hunk starts on a side, from 1: git numbers the
// line before a hunk of no lines.
func hunkStart(start, lines int) int {
	if lines == 0 {
		return start + 1
	}
	return start
}

// buildRows lays the files out as rows: each one's header, then its lines,
// or why it has none.
func (cv *commitView) buildRows() {
	rows := append(cv.rows[:0], diffRow{kind: rowMessage})
	for fi, f := range cv.files {
		rows = append(rows, diffRow{kind: rowHeader, file: fi})
		switch {
		case f.folded:
		case f.binary:
			rows = append(rows, diffRow{kind: rowNote, file: fi})
		default:
			rows = f.appendRows(rows, fi, cv.unified, cv.worktree() && f.status == 'M')
		}
	}
	cv.rows, cv.dirty = rows, false
}

// appendRows appends the rows of f's hunks, each under its head when
// heads, and of the unchanged lines around them, folded unless shown.
// Side by side, a run of lines deleted pairs with the run added after it,
// line by line.
func (f *commitFile) appendRows(rows []diffRow, fi int, unified, heads bool) []diffRow {
	old, new := 1, 1
	gap := func(i, count int) {
		if count <= 0 {
			return
		}
		if !f.shown[i] {
			rows = append(rows, diffRow{kind: rowGap, file: fi, old: old, new: new, count: count, gap: i})
			return
		}
		for k := range count {
			rows = append(rows, diffRow{kind: rowSame, file: fi, old: old + k, new: new + k})
		}
	}
	var dels, adds []int
	flush := func() {
		if unified {
			for _, n := range dels {
				rows = append(rows, diffRow{kind: rowChange, file: fi, old: n})
			}
			for _, n := range adds {
				rows = append(rows, diffRow{kind: rowChange, file: fi, new: n})
			}
		} else {
			for k := range max(len(dels), len(adds)) {
				r := diffRow{kind: rowChange, file: fi}
				if k < len(dels) {
					r.old = dels[k]
				}
				if k < len(adds) {
					r.new = adds[k]
				}
				rows = append(rows, r)
			}
		}
		dels, adds = dels[:0], adds[:0]
	}
	for i, h := range f.hunks {
		gap(i, hunkStart(h.OldStart, h.OldLines)-old)
		if heads {
			rows = append(rows, diffRow{kind: rowHunk, file: fi, gap: i})
		}
		old, new = hunkStart(h.OldStart, h.OldLines), hunkStart(h.NewStart, h.NewLines)
		for _, l := range h.Lines {
			switch {
			case strings.HasPrefix(l, "-"):
				dels = append(dels, old)
				old++
			case strings.HasPrefix(l, "+"):
				adds = append(adds, new)
				new++
			case strings.HasPrefix(l, "\\"):
			default:
				flush()
				rows = append(rows, diffRow{kind: rowSame, file: fi, old: old, new: new})
				old, new = old+1, new+1
			}
		}
		flush()
	}
	gap(len(f.hunks), len(f.old.lines)-old+1)
	return rows
}

// catFiles reads the objects named rev:path with one git, "" for each
// that is missing, as a file a commit adds has no text before it.
func catFiles(root string, specs []string) []string {
	texts := make([]string, len(specs))
	for i, o := range catObjects(root, specs) {
		texts[i] = o.text
	}
	return texts
}

// object is a file's text in git, by its id: "" for one missing.
type object struct{ id, text string }

// catObjects reads the objects named rev:path with one git run in dir,
// all missing when it fails, as outside a repository.
func catObjects(dir string, specs []string) []object {
	objs := make([]object, len(specs))
	var in strings.Builder
	for _, s := range specs {
		in.WriteString(s)
		in.WriteByte('\n')
	}
	out, err := gitWith(dir, in.String(), "cat-file", "--batch")
	if err != nil {
		return objs
	}
	b := []byte(out)
	for i := range specs {
		nl := bytes.IndexByte(b, '\n')
		if nl < 0 {
			break
		}
		line := string(b[:nl])
		b = b[nl+1:]
		// The name of a missing object may hold spaces, as a path does.
		head := strings.Fields(line)
		if strings.HasSuffix(line, " missing") || len(head) != 3 {
			continue
		}
		size, err := strconv.Atoi(head[2])
		if err != nil || size > len(b) {
			break
		}
		objs[i] = object{head[0], string(b[:size])}
		b = b[min(size+1, len(b)):]
	}
	return objs
}

// commitTab shows the commit's tab d in place of the editor of the group
// at gi: a bar over the commit's message and its files' lines, each file
// under its header, which stays at the top while its lines scroll under it.
func (a *app) commitTab(c *ui.Context, gi int, d *doc) {
	cv := d.commit
	col := ui.Column(c.Key(key(d))).Grow(1).FillWidth().MinHeight(0).Background(a.scheme.bg)
	if col.FocusWithin() && a.current() != d {
		a.show(d)
	}
	col.Children(func() {
		a.commitBar(c, cv)
		if !cv.loaded {
			return
		}
		if cv.dirty {
			cv.buildRows()
		}
		if cv.at != "" {
			for i, r := range cv.rows {
				if r.kind == rowHeader && cv.files[r.file].path == cv.at {
					cv.list.ScrollTo(i, ui.Start)
				}
			}
			cv.at = ""
		}
		cv.list.Header = func(i int) bool { return cv.rows[i].kind == rowHeader }
		cv.list.Key = func(i int) any { return cv.rows[i] }
		st := a.codeStyle(c)
		ui.List(c.Key("rows"), &cv.list, len(cv.rows), func(i int) { a.diffRow(c, cv, cv.rows[i], st) }).
			Grow(1).FillWidth().Selectable().Label(cv.title())
	})
}

// codeStyle is how the lines of a commit's tab look, the same for each.
type codeStyle struct {
	font, features     string
	size, line, digit  float32
	tab                string // a tab, as spaces
	theme              *editor.Theme
	addLine, addGutter ui.Color
	delLine, delGutter ui.Color
	empty              ui.Color // a side with no line
}

func (a *app) codeStyle(c *ui.Context) *codeStyle {
	th := a.scheme.editorTheme()
	st := &codeStyle{font: a.codeFont(), features: a.set.font().Features, size: a.set.FontSize, theme: th, tab: strings.Repeat(" ", max(a.set.TabSize, 1))}
	st.line = float32(math.Round(float64(st.size * a.set.lineHeight())))
	w, _ := c.MeasureText(0, ui.Span{Text: "0000000000", Font: st.font, Size: st.size - 1})
	st.digit = w / 10
	// The tints of MyGo's diffs.
	line, gutter := float32(0.12), float32(0.09)
	if a.scheme.dark {
		line, gutter = 0.2, 0.15
	}
	bg := a.scheme.bg
	st.addLine, st.addGutter = bg.Mix(th.Added, line), bg.Mix(th.Added, gutter)
	st.delLine, st.delGutter = bg.Mix(th.Deleted, line), bg.Mix(th.Deleted, gutter)
	st.empty = bg.Mix(a.scheme.fg, 0.04)
	return st
}

// codeFont is the family of the content font, as the editor takes it.
func (a *app) codeFont() string {
	if a.set.ContentFont == "" {
		return "monospace"
	}
	return a.set.ContentFont
}

// gutter is the width of a column of f's line numbers.
func (st *codeStyle) gutter(f *commitFile) float32 {
	digits := len(strconv.Itoa(max(len(f.old.lines), len(f.new.lines), 99)))
	return float32(digits)*st.digit + 16
}

// spans colors the line numbered n of t by its tokens.
func (st *codeStyle) spans(t *fileText, n int) []ui.Span {
	if n < 1 || n > len(t.lines) {
		return nil
	}
	line := t.lines[n-1]
	var tokens []editor.Token
	if t.tokens != nil {
		tokens = t.tokens[n-1]
	}
	spans := make([]ui.Span, 0, len(tokens)+1)
	at := 0
	for _, tk := range tokens {
		end := min(tk.End, len(line))
		if end <= at {
			continue
		}
		ts := st.theme.Tokens[tk.Kind]
		sp := ui.Span{Text: strings.ReplaceAll(line[at:end], "\t", st.tab), Color: ts.Color, Italic: ts.Italic}
		if ts.Bold {
			sp.Weight = 700
		}
		spans = append(spans, sp)
		at = end
	}
	if at < len(line) {
		spans = append(spans, ui.Span{Text: strings.ReplaceAll(line[at:], "\t", st.tab)})
	}
	return spans
}

func (a *app) diffRow(c *ui.Context, cv *commitView, r diffRow, st *codeStyle) {
	if r.kind == rowMessage {
		a.commitMessage(c, cv)
		return
	}
	f := cv.files[r.file]
	switch r.kind {
	case rowHeader:
		a.fileHeader(c, cv, f)
	case rowNote:
		ui.Text(c, locales.PreviewBinary()).FontSize(12).TextColor(a.scheme.chrome().muted).Padding(10, 16).Unselectable()
	case rowGap:
		a.gapRow(c, cv, f, r)
	case rowHunk:
		a.hunkRow(c, cv, f, r.gap)
	case rowSame, rowChange:
		a.lineRow(c, cv, f, r, st)
	}
}

// lineRow shows a line, with its numbers before and after; side by side,
// the line before on the left and after on the right.
func (a *app) lineRow(c *ui.Context, cv *commitView, f *commitFile, r diffRow, st *codeStyle) {
	gutter := st.gutter(f)
	changed := r.kind == rowChange
	row := ui.Row(c).FillWidth().MinHeight(st.line).AlignItems(ui.Stretch).Background(st.theme.Background)
	row.Children(func() {
		if !cv.unified {
			a.lineHalf(c, &f.old, r.old, gutter, changed, st.delLine, st.delGutter, st)
			ui.Box(c).Width(1).Shrink(0).Background(a.scheme.chrome().line)
			a.lineHalf(c, &f.new, r.new, gutter, changed, st.addLine, st.addGutter, st)
			return
		}
		text, n := &f.new, r.new
		bg, gutterBg := st.theme.Background, st.theme.Background
		switch {
		case !changed:
		case r.new == 0:
			text, n = &f.old, r.old
			bg, gutterBg = st.delLine, st.delGutter
		default:
			bg, gutterBg = st.addLine, st.addGutter
		}
		row.Background(bg)
		a.lineNumber(c, r.old, gutter, gutterBg, st)
		a.lineNumber(c, r.new, gutter, gutterBg, st)
		a.code(c, text, n, st)
	})
}

// lineHalf is a side of a row side by side: the line numbered n of t,
// tinted when changed, or nothing when n is 0.
func (a *app) lineHalf(c *ui.Context, t *fileText, n int, gutter float32, changed bool, bg, gutterBg ui.Color, st *codeStyle) {
	half := ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Stretch)
	switch {
	case n == 0:
		half.Background(st.empty)
		return
	case !changed:
		bg, gutterBg = st.theme.Background, st.theme.Background
	}
	half.Background(bg).Children(func() {
		a.lineNumber(c, n, gutter, gutterBg, st)
		a.code(c, t, n, st)
	})
}

func (a *app) lineNumber(c *ui.Context, n int, width float32, bg ui.Color, st *codeStyle) {
	text := ""
	if n > 0 {
		text = strconv.Itoa(n)
	}
	ui.Text(c, text).Font(st.font).FontSize(st.size-1).FixedLineHeight(st.line).TextColor(st.theme.LineNumber).
		Width(width).Shrink(0).TextAlign(ui.End).Padding(0, 8, 0, 0).Background(bg).Unselectable()
}

func (a *app) code(c *ui.Context, t *fileText, n int, st *codeStyle) {
	ui.RichText(c, st.spans(t, n)...).Font(st.font).FontSize(st.size).FontFeatures(st.features).FixedLineHeight(st.line).TextColor(st.theme.Text).
		Grow(1).Basis(0).MinWidth(0).Padding(0, 10)
}

// gapRow stands for unchanged lines, which a click shows.
func (a *app) gapRow(c *ui.Context, cv *commitView, f *commitFile, r diffRow) {
	ch := a.scheme.chrome()
	label := locales.CommitUnchangedLines(r.count)
	row := ui.Row(c).FillWidth().Height(28).Padding(0, 12).Gap(8).AlignItems(ui.Center).Background(a.scheme.bg.Mix(a.scheme.fg, 0.04)).
		Role(ui.RoleButton).Label(label).Unselectable().OnClick(func() {
		if f.shown == nil {
			f.shown = map[int]bool{}
		}
		f.shown[r.gap], cv.dirty = true, true
	})
	if row.Hovered() {
		row.Background(ch.hover)
	}
	row.Children(func() {
		ui.Icon(c, unfoldAllIcon).Size(14, 14).Shrink(0).TextColor(ch.muted)
		ui.Text(c, label).FontSize(12).TextColor(ch.muted).Shrink(0)
		if r.gap < len(f.hunks) && f.hunks[r.gap].Section != "" {
			ui.Text(c, f.hunks[r.gap].Section).Font(a.codeFont()).FontSize(12).TextColor(ch.muted).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
	})
}

// hunkRow stages hunk i of f, or unstages it in the tab of the changes
// staged; in the tab of those not, it discards it too.
func (a *app) hunkRow(c *ui.Context, cv *commitView, f *commitFile, i int) {
	ch := a.scheme.chrome()
	unstage := cv.hash == stagedChanges
	label := locales.ChangesStage()
	if unstage {
		label = locales.ChangesUnstage()
	}
	button := func(label string, onClick func()) {
		btn := ui.ButtonBase(c).Height(20).Padding(0, 8).Radius(4).Shrink(0).Border(1, ch.line).Transition(colorFade).
			Label(label).OnClick(onClick)
		if btn.Hovered() {
			btn.Background(ch.hover)
		}
		btn.Children(func() { ui.Text(c, label).FontSize(12).TextColor(ch.muted) })
	}
	ui.Row(c).FillWidth().Height(28).Padding(0, 12).Gap(6).AlignItems(ui.Center).Background(a.scheme.bg).Unselectable().Children(func() {
		ui.Box(c).Grow(1)
		// The website's git applies to the index alone.
		if !unstage && runtime.GOOS != "js" {
			button(locales.ButtonDiscard(), func() {
				h := f.hunks[i]
				a.confirm(locales.DialogDiscardHunk(filepath.Base(f.path)), locales.DialogDiscardLost(), locales.ButtonDiscard(),
					func() { a.discardHunk(f.path, h) })
			})
		}
		button(label, func() { a.stageHunk(f.path, f.hunks[i], unstage) })
	})
}

// stageHunk applies h, of the file at p, to the index, or takes it out.
// Its start on the index's side is where it is there, as its tab diffs
// the index with HEAD or with the work tree, so it applies alone.
func (a *app) stageHunk(p string, h editor.PatchHunk, unstage bool) {
	args := []string{"apply", "--cached", "--recount"}
	if unstage {
		args = append(args, "--reverse")
	}
	a.gitRunWith(hunkPatch(p, h), nil, append(args, "-")...)
}

// discardHunk takes h, of the unstaged changes of the file at p, out of
// the work tree.
func (a *app) discardHunk(p string, h editor.PatchHunk) {
	a.gitRunWith(hunkPatch(p, h), nil, "apply", "--recount", "--reverse", "-")
}

func hunkPatch(p string, h editor.PatchHunk) string {
	var patch strings.Builder
	fmt.Fprintf(&patch, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", p, p, p, p)
	fmt.Fprintf(&patch, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
	for _, l := range h.Lines {
		patch.WriteString(l + "\n")
	}
	return patch.String()
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
		a.barButton(c, splitIcon, locales.CompareSideBySide(), !cv.unified, func() { a.layDiffs(cv, false) })
		a.barButton(c, unifiedIcon, locales.CompareUnified(), cv.unified, func() { a.layDiffs(cv, true) })
		ui.Box(c).Grow(1)
		if cv.loaded {
			a.counts(c, added, deleted)
		}
	})
}

// layDiffs lays cv's diffs out side by side or unified, as commits opened
// next will be.
func (a *app) layDiffs(cv *commitView, unified bool) {
	cv.unified, cv.dirty = unified, true
	a.set.DiffUnified = unified
	a.l.saveSettings()
}

func (cv *commitView) foldAll(fold bool) {
	for _, f := range cv.files {
		f.folded = fold
	}
	cv.dirty = true
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

// moreButton shows the whole message, or its subject alone again.
func (a *app) moreButton(c *ui.Context, cv *commitView) {
	ch := a.scheme.chrome()
	label := locales.CommitShowMessage()
	if cv.more {
		label = locales.CommitHideMessage()
	}
	btn := ui.ButtonBase(c).Size(20, 20).Radius(4).Center().Transition(colorFade).
		Label(label).Tooltip(label).OnClick(func() { cv.more = !cv.more })
	if btn.Hovered() {
		btn.Background(ch.hover)
	}
	btn.Children(func() {
		chevron := ui.Icon(c, chevronIcon).Size(14, 14).TextColor(ch.muted)
		if cv.more {
			chevron.Rotate(180)
		}
	})
}

// commitMessage is the first row: the author's picture, name and email,
// when, the message's subject, or the whole message while more, and the
// hash to copy. The work tree's has none.
func (a *app) commitMessage(c *ui.Context, cv *commitView) {
	if cv.worktree() {
		ui.Box(c).FillWidth().Height(0)
		return
	}
	ch := a.scheme.chrome()
	ui.Row(c).FillWidth().Padding(16).Gap(12).AlignItems(ui.Start).Background(a.scheme.bg).Children(func() {
		a.avatar(c, cv.email, 40)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			body := strings.TrimSpace(cv.body)
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cv.author).FontSize(13).Bold().TextColor(ch.text)
				if strings.Contains(body, "\n") {
					a.moreButton(c, cv)
				}
			})
			ui.Text(c, cv.when.Format("2006-01-02 15:04")+" · "+cv.email).FontSize(12).TextColor(ch.muted).SingleLine().Ellipsis("…")
			message := cv.subject
			if cv.more {
				message = body
			}
			ui.Text(c, message).FontSize(13).TextColor(ch.text).Margin(6, 0, 0, 0)
		})
		button := func(label, tooltip string, click func()) {
			btn := ui.ButtonBase(c).Height(22).Padding(0, 8).Radius(4).Shrink(0).Transition(colorFade).
				Label(label).Tooltip(tooltip)
			if btn.Hovered() {
				btn.Background(ch.hover)
			}
			btn.OnClick(click)
			btn.Children(func() { ui.Text(c, label).FontSize(12).TextColor(ch.muted) })
		}
		ui.Row(c).Gap(4).Shrink(0).Children(func() {
			if cv.github != "" {
				link := cv.github + "/commit/" + cv.hash
				button(locales.MenuOpenOnGitHub(), link, func() { mygo.Shell.OpenExternal(link) })
			}
			services, hash := c.Services(), cv.hash
			button(locales.CommitCopySHA(), hash, func() {
				services.WriteClipboard(hash)
				a.notify(locales.CommitCopied(cv.short))
			})
		})
	})
}

// fileHeader names a file of the commit with its counts; a click folds
// or unfolds its diff.
func (a *app) fileHeader(c *ui.Context, cv *commitView, f *commitFile) {
	ch := a.scheme.chrome()
	row := ui.Row(c).FillWidth().Height(32).Padding(0, 12).Gap(8).AlignItems(ui.Center).Background(ch.bg).
		BorderWidth(1, 0, 1, 0).BorderColor(ch.line).Role(ui.RoleButton).Label(f.path).Expanded(!f.folded).Unselectable().
		OnClick(func() { f.folded, cv.dirty = !f.folded, true })
	row.Children(func() {
		chevron := ui.Icon(c, chevronIcon).Size(14, 14).Shrink(0).TextColor(ch.muted)
		if f.folded {
			chevron.Rotate(-90)
		}
		ic := fileIcon(f.path, a.scheme.dark)
		ic.view(c, treeIcon, ic.tint(ch.muted))
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

package main

import (
	"bytes"
	"cmp"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// Blame tells who last changed the lines, as Zed's does: the caret's line
// at its end once the caret rests there (inline_blame), and each block of
// lines in a column beside the line numbers (Toggle Blame), a click on
// which opens its commit. git blames the file as saved, so the lines
// edited since show none.

// maxBlameLines is the most lines a file blames: git reads the history of
// each, and its output parses whole.
const maxBlameLines = 50000

const blameDelay = 500 * time.Millisecond

const uncommitted = "0000000000000000000000000000000000000000"

type blamed struct {
	commit
	path string
}

type blaming struct {
	shown bool

	root    string
	lines   []*blamed
	saved   uint64
	read    bool
	stale   bool
	reading bool

	diff   *editor.DiffState
	diffOf uint64
	now    uint64
	mapped bool
	hunks  []editor.DiffHunk

	at     int
	ver    uint64
	since  time.Time
	inline bool
}

// blameable reports whether d's file shows its blame: a text git has,
// whose lines git splits as the tab does. The website's git has no blame.
func (d *doc) blameable() bool {
	return runtime.GOOS != "js" && d.plain() && d.path != "" && d.base != "" && !d.cr && d.saved.LineCount() <= maxBlameLines
}

func currentBlameable(a *app) bool {
	d := a.current()
	return d != nil && d.blameable()
}

func (a *app) toggleBlame() {
	d := a.current()
	if d == nil || !d.blameable() {
		return
	}
	if d.blame == nil {
		d.blame = &blaming{at: -1}
	}
	d.blame.shown = !d.blame.shown
}

func (a *app) syncBlame(c *ui.Context, d *doc) {
	inline := a.set.InlineBlame && d == a.current() && d.editorHandle.Focused(c)
	b := d.blame
	if b == nil {
		if !inline || !d.blameable() {
			return
		}
		b = &blaming{at: -1}
		d.blame = b
	}
	if !d.blameable() {
		b.shown = false
		b.hideInline(d.st)
		return
	}
	if inline || b.shown {
		a.readBlame(d)
		b.mapLines(d)
	}
	a.restBlame(c, d, inline)
}

func (a *app) readBlame(d *doc) {
	b := d.blame
	v := d.saved.Version()
	if b.reading || b.read && b.saved == v && !b.stale {
		return
	}
	b.reading, b.stale = true, false
	path, saved, enc := d.path, d.saved, d.encoding()
	a.background(func() func() {
		root, lines := blameFile(path, saved, enc)
		return func() {
			b.reading = false
			// Saved again since: the next frame reads it again.
			if d.saved.Version() != v || d.path != path {
				return
			}
			b.root, b.lines, b.saved, b.read, b.mapped = root, lines, v, true, false
			b.hideInline(d.st)
		}
	})
}

// gitLineCount is how many lines git splits s into: none after a final
// newline.
func gitLineCount(s editor.Snapshot) int {
	n := s.LineCount()
	if s.Len() == 0 || s.Slice(s.Len()-1, s.Len()) == "\n" {
		n--
	}
	return n
}

// blameFile reads the commit of each line of saved, the text of the file at
// path as saved in enc, and the top of its repository. git blames the
// text given, not the file, which git may have changed on disk since. It
// reads none where git splits the text into other lines than the tab's,
// as it does UTF-16's. It blocks, so call it off the main thread.
func blameFile(path string, saved editor.Snapshot, enc *fileEncoding) (string, []*blamed) {
	var text bytes.Buffer
	if _, err := saved.WriteTo(&text); err != nil {
		return "", nil
	}
	raw, err := enc.encode(text.Bytes())
	if err != nil {
		return "", nil
	}
	real := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		real = r
	}
	dir, name := filepath.Dir(real), filepath.Base(real)
	var top string
	var topErr error
	var wg sync.WaitGroup
	wg.Go(func() { top, topErr = runGit(dir, "rev-parse", "--show-toplevel") })
	out, err := gitWith(dir, string(raw), "-c", "core.quotePath=false", "blame", "--porcelain", "--contents", "-", "--", name)
	wg.Wait()
	if topErr != nil || err != nil {
		return "", nil
	}
	lines := parseBlame(out)
	if len(lines) != gitLineCount(saved) {
		return "", nil
	}
	return filepath.Clean(strings.TrimSpace(top)), lines
}

// parseBlame reads the commit of each line from git blame --porcelain,
// which tells a commit's author and summary at its first line alone.
func parseBlame(out string) []*blamed {
	commits := map[string]*blamed{}
	var lines []*blamed
	var cur *blamed
	for line := range strings.Lines(out) {
		line = strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(line, "\t") {
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		if f := strings.Fields(line); len(f) >= 3 && isHash(key) {
			final, err := strconv.Atoi(f[2])
			if err != nil || final < 1 {
				continue
			}
			cur = commits[key]
			if cur == nil {
				cur = &blamed{commit: commit{hash: key, short: key[:7]}}
				commits[key] = cur
			}
			for len(lines) < final {
				lines = append(lines, nil)
			}
			lines[final-1] = cur
			continue
		}
		if cur == nil {
			continue
		}
		switch key {
		case "author":
			cur.author = val
		case "author-mail":
			cur.email = strings.TrimSuffix(strings.TrimPrefix(val, "<"), ">")
		case "author-time":
			t, _ := strconv.ParseInt(val, 10, 64)
			cur.when = time.Unix(t, 0)
		case "summary":
			cur.subject = val
		case "filename":
			if cur.path == "" {
				if p, err := strconv.Unquote(val); err == nil {
					val = p
				}
				cur.path = val
			}
		}
	}
	return lines
}

func isHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') {
			return false
		}
	}
	return true
}

func (b *blaming) mapLines(d *doc) {
	v := d.st.Buffer.Version()
	if !b.read || b.saved != d.saved.Version() || b.mapped && b.now == v {
		return
	}
	b.now, b.mapped, b.hunks = v, true, nil
	if v == b.saved {
		return
	}
	if b.diff == nil || b.diffOf != b.saved {
		b.diff, b.diffOf = &editor.DiffState{Old: editor.NewBuffer(d.saved.String()), New: d.st.Buffer}, b.saved
	}
	b.hunks = b.diff.Hunks()
}

func (b *blaming) lineOf(d *doc, line int) *blamed {
	if !b.read || !b.mapped || b.saved != d.saved.Version() || b.now != d.st.Buffer.Version() {
		return nil
	}
	s := line
	for _, h := range b.hunks {
		if line < h.NewStart {
			break
		}
		if line < h.NewEnd {
			return nil
		}
		s = line - h.NewEnd + h.OldEnd
	}
	if s >= len(b.lines) {
		return nil
	}
	return b.lines[s]
}

func (a *app) restBlame(c *ui.Context, d *doc, inline bool) {
	b, st := d.blame, d.st
	sel := st.Selection()
	if !inline || sel.Anchor != sel.Head || len(st.Selections()) > 1 {
		b.hideInline(st)
		b.at = -1
		return
	}
	if v := st.Buffer.Version(); sel.Head != b.at || v != b.ver {
		b.hideInline(st)
		b.at, b.ver, b.since = sel.Head, v, c.Now()
	}
	if b.inline {
		return
	}
	if wait := blameDelay - c.Now().Sub(b.since); wait > 0 {
		c.After(wait)
		return
	}
	t := st.Buffer.Snapshot()
	line := t.LineOf(sel.Head)
	bl := b.lineOf(d, line)
	if bl == nil {
		return
	}
	st.SetInlays([]editor.Inlay{{
		Offset:   t.LineEnd(line),
		Text:     inlineBlame(bl, time.Now()),
		Trailing: true,
		Ellipsis: "…",
	}})
	b.inline = true
}

func (b *blaming) hideInline(st *editor.State) {
	if b.inline {
		st.SetInlays(nil)
		b.inline = false
	}
}

const blameSubject = 60

// inlineBlame is what shows at the end of a line: who changed it, when and
// why, as Zed shows it, with at most 60 runes of the subject.
func inlineBlame(bl *blamed, now time.Time) string {
	const gap, sep = "      ", " • "
	if bl.hash == uncommitted {
		return gap + locales.BlameNotCommitted()
	}
	who := gap + bl.author + ", " + ago(bl.when, now)
	subject := []rune(bl.subject)
	if len(subject) == 0 {
		return who
	}
	if len(subject) > blameSubject {
		subject = append(subject[:blameSubject-1], '…')
	}
	return who + sep + string(subject)
}

const blameAuthor = 12

func blameLabel(bl *blamed) string {
	if bl.hash == uncommitted {
		return locales.BlameNotCommitted()
	}
	author := []rune(bl.author)
	if len(author) > blameAuthor {
		author = append(author[:blameAuthor-1], '…')
	}
	return bl.short + " " + string(author) + strings.Repeat(" ", blameAuthor-len(author)) + " " + bl.when.Format("2006-01-02")
}

// withBlame builds d's editor with view, after the column of its blame
// while Toggle Blame shows it. The column paints after the editor, which
// scrolls its lines as it paints.
func (a *app) withBlame(c *ui.Context, d *doc, view func()) {
	b := d.blame
	if b == nil || !b.shown {
		view()
		return
	}
	st := d.st
	font, size := a.codeFont(), st.Font.Size-1
	span := ui.Span{Text: strings.Repeat("0", len("0000000  2006-01-02")+blameAuthor), Font: font, Size: size}
	w, h := c.MeasureText(0, span)
	w += 16
	// The editor's lines are LineHeight times as high as its font, 1.5 for 0.
	pad := (st.Font.Size*cmp.Or(st.LineHeight, 1.5) - h) / 2
	color := st.Theme.LineNumber
	row := ui.Row(c).Grow(1).FillWidth().MinHeight(0).AlignItems(ui.Stretch)
	row.DrawOver(func(p *ui.Painter, r ui.Rect) {
		col := ui.Rect{X: r.X, Y: r.Y, W: w, H: r.H}
		p.Clip(col, 0, func() { b.paint(p, col, d, ui.Span{Font: font, Size: size, Color: color}, pad) })
	})
	row.Children(func() {
		ui.Box(c).Width(w).Shrink(0).Background(st.Theme.Background).BorderWidth(0, 1, 0, 0).BorderColor(color.Alpha(0.2)).
			Cursor(ui.CursorPointer).HandleInput(func(ev ui.InputEvent) bool {
			switch ev.Kind {
			case ui.InputScroll:
				st.SetScrollY(st.ScrollY() + float64(ev.DY))
				return true
			case ui.InputPointerDown:
				if bl := b.lineOf(d, lineAt(st, float64(ev.Y))); ev.Button == 0 && bl != nil && bl.hash != uncommitted {
					a.showCommitIn(b.root, bl.commit).at = bl.path
				}
				return true
			}
			return false
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(view)
	})
}

func (b *blaming) paint(p *ui.Painter, r ui.Rect, d *doc, style ui.Span, pad float32) {
	scroll := d.st.ScrollY()
	b.eachBlock(d, func(top float64, bl *blamed) {
		label := style
		label.Text = blameLabel(bl)
		p.RichText(r.X+8, r.Y+float32(top-scroll)+pad, 0, label)
	})
}

func (b *blaming) eachBlock(d *doc, fn func(top float64, bl *blamed)) {
	st := d.st
	first, last := st.VisibleLines()
	n := st.Buffer.LineCount()
	var prev *blamed
	for line := first; line <= last; line++ {
		top := st.LineTop(line)
		// A line a fold hides takes no room, at the top of the line after it.
		if line+1 < n && st.LineTop(line+1) == top {
			continue
		}
		bl := b.lineOf(d, line)
		if bl != nil && bl != prev {
			fn(top, bl)
		}
		prev = bl
	}
}

func lineAt(st *editor.State, y float64) int {
	first, last := st.VisibleLines()
	y += st.ScrollY()
	line := first
	for l := first; l <= last && st.LineTop(l) <= y; l++ {
		line = l
	}
	return line
}

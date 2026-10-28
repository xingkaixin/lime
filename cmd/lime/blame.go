package main

import (
	"cmp"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

// blameDelay is how long the caret rests on a line before its blame shows.
const blameDelay = 500 * time.Millisecond

// uncommitted is the hash git blame gives the lines no commit has.
const uncommitted = "0000000000000000000000000000000000000000"

// blamed is a commit git blame names, with the file's path in it.
type blamed struct {
	commit
	path string
}

// blaming is what a doc keeps of its blame.
type blaming struct {
	shown bool // the column shows, as Toggle Blame left it

	root    string    // the top of the file's repository
	lines   []*blamed // by line of the saved text, nil for none
	saved   uint64    // the version of the saved text lines are of
	read    bool      // lines are of saved
	stale   bool      // git may have changed them: read them again
	reading bool

	// diff compares the saved text, its Old, with the text now, whose
	// edits are hunks as of version now.
	diff   *editor.DiffState
	diffOf uint64 // the version of the saved text in diff
	now    uint64
	mapped bool
	hunks  []editor.DiffHunk

	// The caret rests at offset at, of version ver, since since; inline is
	// set once its line shows its blame, or has no room for it.
	at     int
	ver    uint64
	since  time.Time
	inline bool

	width float32 // the editor's, as the last frame laid it out
}

// blameable reports whether d's file shows its blame: a text git has,
// whose lines git splits as the tab does. The website's git has no blame.
func (d *doc) blameable() bool {
	return runtime.GOOS != "js" && d.plain() && d.path != "" && d.base != "" && !d.cr && d.saved.LineCount() <= maxBlameLines
}

// currentBlameable reports whether the current tab shows its blame.
func currentBlameable(a *app) bool {
	d := a.current()
	return d != nil && d.blameable()
}

// toggleBlame shows or hides the column of the current tab's blame.
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

// syncBlame reads d's blame where it shows, and shows it at the end of the
// caret's line once the caret rests there.
func (a *app) syncBlame(c *ui.Context, d *doc) {
	inline := a.set.InlineBlame && d == a.current()
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

// readBlame has git blame d's file as saved, unless it has since git last
// changed.
func (a *app) readBlame(d *doc) {
	b := d.blame
	v := d.saved.Version()
	if b.reading || b.read && b.saved == v && !b.stale {
		return
	}
	b.reading, b.stale = true, false
	path, enc, n := d.path, d.encoding(), gitLineCount(d.saved)
	a.background(func() func() {
		root, lines := blameFile(path, enc, n)
		return func() {
			b.reading = false
			// Saved again since: the next frame reads it again.
			if d.saved.Version() != v || d.path != path {
				return
			}
			b.root, b.lines, b.saved, b.read, b.mapped = root, lines, v, true, false
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

// blameFile reads the commit of each of the n lines of the file at path,
// saved in enc, and the top of its repository. It reads none where git
// splits the file into other lines. It blocks, so call it off the main
// thread.
func blameFile(path string, enc *fileEncoding, n int) (string, []*blamed) {
	real := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		real = r
	}
	dir, name := filepath.Dir(real), filepath.Base(real)
	top, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil
	}
	out, err := runGit(dir, "-c", "core.quotePath=false", "blame", "--porcelain", "--", name)
	if err != nil {
		return "", nil
	}
	lines := parseBlame(out)
	if len(lines) != n {
		// UTF-16's code units hold the byte of a newline, where git splits
		// a line, in other characters too.
		be := enc.name == "UTF-16 BE with BOM"
		if !be && enc.name != "UTF-16 LE with BOM" {
			return "", nil
		}
		raw, err := os.ReadFile(real)
		if err != nil {
			return "", nil
		}
		starts := utf16Lines(raw, be)
		if len(starts) < n {
			return "", nil
		}
		text := make([]*blamed, n)
		for i, at := range starts[:n] {
			if at < len(lines) {
				text[i] = lines[at]
			}
		}
		lines = text
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
		// The head of a line: its commit, its line there and here.
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

// isHash reports whether s is a commit's full hash, of SHA-1 or SHA-256.
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

// utf16Lines is the line git splits raw, UTF-16 text, into at the start
// of each of its lines: git splits after every byte 0x0A.
func utf16Lines(raw []byte, bigEndian bool) []int {
	starts := []int{0}
	n := 0
	for i := 0; i+1 < len(raw); i += 2 {
		lo, hi := raw[i], raw[i+1]
		if bigEndian {
			lo, hi = hi, lo
		}
		if raw[i] == '\n' {
			n++
		}
		if raw[i+1] == '\n' {
			n++
		}
		if lo == '\n' && hi == 0 {
			starts = append(starts, n)
		}
	}
	return starts
}

// mapLines finds the lines edited since the text was saved, as it is
// edited.
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

// lineOf is the commit of a line of the text now: nil for a line edited
// since the text was saved, or one git has no commit for yet.
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

// restBlame shows the blame of the caret's line at its end once the caret
// rests there, as a suggestion: the editor drops it as the caret moves or
// the text changes, and shows the caret before it.
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
	room := -1
	if cols := a.rowColumns(c, d); cols > 0 {
		room = cols - columns(t.Slice(t.LineStart(line), t.LineEnd(line)), st.TabSize)
	}
	if text := inlineBlame(bl, time.Now(), room); text != "" {
		st.SetSuggestion(t.LineEnd(line), text)
	}
	b.inline = true
}

// rowColumns is how many columns a row of d's editor holds before it
// wraps, as the last frame laid it out, or 0 for no end.
func (a *app) rowColumns(c *ui.Context, d *doc) int {
	if !d.st.SoftWrap || d.blame.width <= 0 {
		return 0
	}
	cw, _ := c.MeasureText(0, ui.Span{Text: "0", Font: a.codeFont(), Size: a.set.FontSize})
	// The gutter's fold marks and change bars, its padding and the scroll
	// bar take some eight columns besides the line numbers; two spare.
	digits := len(strconv.Itoa(d.st.Buffer.LineCount()))
	return max(int(d.blame.width/cw)-digits-10, 1)
}

// columns is how many columns s takes, with tabs to stops of tab: two for
// each character from U+1100 on, as wide as CJK's, to err toward too many.
func columns(s string, tab int) int {
	n := 0
	for _, r := range s {
		switch {
		case r == '\t':
			n += tab - n%tab
		case r >= 0x1100:
			n += 2
		default:
			n++
		}
	}
	return n
}

func (b *blaming) hideInline(st *editor.State) {
	if b.inline {
		st.SetSuggestion(0, "")
		b.inline = false
	}
}

// blameKey keeps Tab, which accepts a suggestion, from inserting the blame
// shown as one.
func blameKey(d *doc, ev ui.InputEvent) {
	if d.blame != nil && ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyTab {
		d.blame.hideInline(d.st)
	}
}

// inlineBlame is what shows at the end of a line: who changed it, when and
// why, as Zed shows it, in room columns, or 60 of the subject for room -1;
// "" when who and when don't fit.
func inlineBlame(bl *blamed, now time.Time, room int) string {
	const gap, sep = "      ", " • "
	who := gap + locales.BlameNotCommitted()
	if bl.hash != uncommitted {
		who = gap + bl.author + ", " + ago(bl.when, now)
	}
	if room < 0 {
		room = columns(who, 1) + columns(sep, 1) + 60
	}
	if columns(who, 1) > room {
		return ""
	}
	if bl.hash == uncommitted {
		return who
	}
	subject := []rune(bl.subject)
	n := room - columns(who, 1) - columns(sep, 1)
	if columns(bl.subject, 1) > n {
		// Too few columns for a word or two of it.
		if n < 10 {
			return who
		}
		for columns(string(subject), 1) > n-1 {
			subject = subject[:len(subject)-1]
		}
		subject = append(subject, '…')
	}
	return who + sep + string(subject)
}

// blameAuthor is how many characters of an author's name the column shows.
const blameAuthor = 12

// blameLabel is what the column shows of a block's commit: its hash,
// author and date, in columns.
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
func (a *app) withBlame(c *ui.Context, d *doc, view func() ui.Element) {
	b := d.blame
	if b == nil || !b.shown {
		e := view()
		if b != nil {
			b.width = e.LayoutBounds().W
		}
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
		ui.Column(c).Grow(1).MinWidth(0).Children(func() { b.width = view().LayoutBounds().W })
	})
}

// paint labels the first line in view of each block of lines of a
// commit, in the column r.
func (b *blaming) paint(p *ui.Painter, r ui.Rect, d *doc, style ui.Span, pad float32) {
	scroll := d.st.ScrollY()
	b.eachBlock(d, func(top float64, bl *blamed) {
		label := style
		label.Text = blameLabel(bl)
		p.RichText(r.X+8, r.Y+float32(top-scroll)+pad, 0, label)
	})
}

// eachBlock calls fn with the commit of each block of lines in view and
// the top of its first line.
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

// lineAt is the line in view at y, from the editor's top.
func lineAt(st *editor.State, y float64) int {
	first, last := st.VisibleLines()
	y += st.ScrollY()
	line := first
	for l := first; l <= last && st.LineTop(l) <= y; l++ {
		line = l
	}
	return line
}

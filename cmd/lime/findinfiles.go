package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// The places of the Where field that are no path, as Sublime Text writes
// them.
const (
	openFolders = "<open folders>"
	openFiles   = "<open files>"
)

// maxResults bounds the matches a search finds: it stops at as many.
const maxResults = 10000

// maxResultLine is how much of a matched line the results show, for a
// minified file all on one line.
const maxResultLine = 500

// findInFiles is Sublime Text's Find in Files panel, along the bottom of
// the window, and the search it runs.
type findInFiles struct {
	open                       bool
	query, where               string
	caseSens, wholeWord, regex bool
	focus                      bool // the query takes the focus as the panel next shows
	input                      ui.Handle
	tab                        *doc // the Find Results tab, which each search fills anew
	cancel                     context.CancelFunc
}

func (f *findInFiles) stop() {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
}

// showFindInFiles opens the panel, looking in where when given. The query
// is the selection on one line, else the last one, and the options are
// the find panel's.
func (a *app) showFindInFiles(where string) {
	f := &a.fif
	if !f.open {
		f.caseSens, f.wholeWord, f.regex = a.find.caseSens, a.find.wholeWord, a.find.regex
	}
	f.open, f.focus = true, true
	if d := a.current(); d != nil && d.kind == textDoc {
		if t := selectionQuery(d.st); t != "" {
			f.query = t
		}
	}
	if f.query == "" {
		f.query = a.find.query
	}
	switch {
	case where != "":
		f.where = where
	case f.where == "" && len(a.proj.folders) > 0:
		f.where = openFolders
	case f.where == "":
		f.where = openFiles
	}
}

func (a *app) closeFindInFiles() {
	a.fif.open = false
	a.focusGroup(a.focus)
}

// matcher finds the query of the find panels, with their options.
type matcher struct {
	re        *regexp.Regexp
	wholeWord bool
}

func newMatcher(query string, regex, caseSens, wholeWord bool) (*matcher, error) {
	if !regex {
		query = regexp.QuoteMeta(query)
	}
	if !caseSens {
		query = "(?i)" + query
	}
	re, err := regexp.Compile("(?m)" + query)
	if err != nil {
		return nil, err
	}
	return &matcher{re, wholeWord}, nil
}

// lineHit is a line of a file with matches: their bytes in it, the first
// one's where the results go to.
type lineHit struct {
	line  int
	text  string
	spans [][2]int
}

// lines finds the lines of text with matches, at most limit matches.
func (m *matcher) lines(text string, limit int) ([]lineHit, int) {
	var hits []lineHit
	n, line, counted := 0, 0, 0
	for _, sub := range m.re.FindAllStringIndex(text, -1) {
		a, b := sub[0], sub[1]
		if a == b || m.wholeWord && !wholeWordAt(text, a, b) {
			continue
		}
		line += strings.Count(text[counted:a], "\n")
		counted = a
		start := strings.LastIndexByte(text[:a], '\n') + 1
		end := len(text)
		if i := strings.IndexByte(text[a:], '\n'); i >= 0 {
			end = a + i
		}
		span := [2]int{a - start, min(b, end) - start}
		if k := len(hits) - 1; k >= 0 && hits[k].line == line {
			hits[k].spans = append(hits[k].spans, span)
		} else {
			hits = append(hits, lineHit{line, text[start:end], [][2]int{span}})
		}
		if n++; n == limit {
			break
		}
	}
	return hits, n
}

// wholeWordAt reports whether text from a to b is no part of a longer
// word.
func wholeWordAt(text string, a, b int) bool {
	if r, _ := utf8.DecodeLastRuneInString(text[:a]); a > 0 && isWordRune(r, "") {
		return false
	}
	if r, _ := utf8.DecodeRuneInString(text[b:]); b < len(text) && isWordRune(r, "") {
		return false
	}
	return true
}

// scope is where a search looks, as the Where field has it: folders and
// files, the open files, and patterns of the names to look in, or not.
type scope struct {
	paths            []string
	include, exclude []string
}

func parseWhere(where string, folders []string) scope {
	var sc scope
	placed := false
	for p := range strings.SplitSeq(where, ",") {
		switch p = strings.TrimSpace(p); {
		case p == "":
		case p == openFolders:
			sc.paths, placed = append(sc.paths, folders...), true
		case p == openFiles, filepath.IsAbs(p):
			sc.paths, placed = append(sc.paths, p), true
		case strings.HasPrefix(p, "-"):
			sc.exclude = append(sc.exclude, strings.TrimSuffix(p[1:], "/"))
		default:
			sc.include = append(sc.include, p)
		}
	}
	switch {
	case placed:
	case len(folders) > 0:
		sc.paths = folders
	default:
		sc.paths = []string{openFiles}
	}
	return sc
}

func (sc *scope) wants(name string) bool {
	return !excluded(name, sc.exclude) && (len(sc.include) == 0 || excluded(name, sc.include))
}

// source is a file a search looks in: its tab's text when open.
type source struct {
	name, path string
	d          *doc
	text       editor.Snapshot
}

type fileHits struct {
	src   source
	lines []lineHit
}

// found is what a search found: the files with matches, and how many
// files it looked in and matches it found.
type found struct {
	files    []fileHits
	searched int
	matches  int
}

// searchFiles looks for m in sc until ctx is done, in the text of the
// open files' tabs rather than on disk. It blocks, so call it off the
// main thread.
func searchFiles(ctx context.Context, m *matcher, sc scope, open []source) found {
	var res found
	seen := make(map[string]bool)
	byPath := make(map[string]source)
	for _, s := range open {
		if s.path != "" {
			byPath[s.path] = s
		}
	}
	look := func(s source) bool {
		if s.path != "" {
			if seen[s.path] {
				return true
			}
			seen[s.path] = true
		}
		var text string
		var ok bool
		if s.d != nil {
			text = s.text.String()
			ok = !binary(text)
		} else {
			text, ok = readSearched(s.path)
		}
		if !ok {
			return true
		}
		res.searched++
		lines, n := m.lines(text, maxResults-res.matches)
		if n > 0 {
			res.files = append(res.files, fileHits{s, lines})
			res.matches += n
		}
		return res.matches < maxResults && ctx.Err() == nil
	}
	for _, p := range sc.paths {
		if p == openFiles {
			for _, s := range open {
				if sc.wants(filepath.Base(s.name)) && !look(s) {
					return res
				}
			}
			continue
		}
		more := true
		filepath.WalkDir(p, func(path string, e fs.DirEntry, err error) error {
			if !more {
				return filepath.SkipAll
			}
			if err != nil {
				return nil
			}
			switch name := e.Name(); {
			case e.IsDir():
				if path != p && (excluded(name, folderExclude) || excluded(name, sc.exclude)) {
					return filepath.SkipDir
				}
			case !regular(path, e.Type()):
			case sc.wants(name) && !excluded(name, fileExclude) && !excluded(name, binaryFiles):
				s, ok := byPath[path]
				if !ok {
					s = source{name: path, path: path}
				}
				more = look(s)
			}
			return nil
		})
		if !more {
			break
		}
	}
	return res
}

// regular reports whether the file at path, of type t, is a file of its
// own, or a link to one: a FIFO would block reading it.
func regular(path string, t fs.FileMode) bool {
	if t&fs.ModeSymlink != 0 {
		fi, err := os.Stat(path)
		return err == nil && fi.Mode().IsRegular()
	}
	return t.IsRegular()
}

// readSearched reads the file at path as its tab would have it, \r\n as
// \n, unless it is binary: a NUL in its first 8000 bytes, as fileKind
// tells, read before the rest.
func readSearched(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	head := make([]byte, 8000)
	n, _ := io.ReadFull(f, head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return "", false
	}
	var b strings.Builder
	b.Write(head[:n])
	if _, err := io.Copy(&b, f); err != nil {
		return "", false
	}
	return strings.ReplaceAll(b.String(), "\r\n", "\n"), true
}

// searchInFiles runs the panel's search into the Find Results tab, off
// the main thread in the app, stopping the one going on.
func (a *app) searchInFiles() {
	f := &a.fif
	if f.query == "" {
		return
	}
	m, err := newMatcher(f.query, f.regex, f.caseSens, f.wholeWord)
	if err != nil {
		a.notifyError(locales.StatusInvalidRegex())
		return
	}
	sc := parseWhere(f.where, a.proj.folders)
	var open []source
	for _, d := range a.allDocs() {
		if d.kind == textDoc {
			s := source{name: d.name(), path: d.path, d: d, text: d.st.Buffer.Snapshot()}
			if d.path != "" {
				s.name = d.path
			}
			open = append(open, s)
		}
	}
	f.stop()
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel, f.open = cancel, false
	d := a.resultsTab()
	d.results.show(d, locales.FindResultsSearching(f.query)+"\n", nil, nil)
	query, opts := f.query, searchOptions(f)
	a.background(func() func() {
		res := searchFiles(ctx, m, sc, open)
		return func() {
			if ctx.Err() != nil {
				return
			}
			cancel()
			f.cancel = nil
			text, hits, spans := formatResults(locales.FindResultsHeader(res.searched, query)+opts, res)
			d.results.show(d, text, hits, spans)
		}
	})
}

// searchOptions follows the first line of a search's results, as Sublime
// Text names the options there.
func searchOptions(f *findInFiles) string {
	var opts []string
	if f.regex {
		opts = append(opts, locales.FindRegex())
	}
	if f.caseSens {
		opts = append(opts, locales.FindCaseSensitive())
	}
	if f.wholeWord {
		opts = append(opts, locales.FindWholeWord())
	}
	if len(opts) == 0 {
		return ""
	}
	return " (" + strings.Join(opts, ", ") + ")"
}

// formatResults writes what a search found as Sublime Text's Find
// Results: the matched lines of each file under its path, numbered.
func formatResults(header string, res found) (string, []hit, [][2]int) {
	var b strings.Builder
	var hits []hit
	var spans [][2]int
	row := 0
	line := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
		row++
	}
	line(header)
	line("")
	for _, f := range res.files {
		head := row
		line(f.src.name + ":")
		for _, l := range f.lines {
			prefix := fmt.Sprintf("%5d: ", l.line+1)
			text := l.text
			if len(text) > maxResultLine {
				cut := maxResultLine
				for !utf8.RuneStart(text[cut]) {
					cut--
				}
				text = text[:cut] + "…"
			}
			at := b.Len() + len(prefix)
			for _, sp := range l.spans {
				if sp[0] < len(text) {
					spans = append(spans, [2]int{at + sp[0], at + min(sp[1], len(text))})
				}
			}
			h := hit{row: row, head: head, path: f.src.path, line: l.line, start: l.spans[0][0], end: l.spans[0][1]}
			if h.path == "" {
				h.d = f.src.d
			}
			hits = append(hits, h)
			line(prefix + text)
		}
		line("")
	}
	if res.matches >= maxResults {
		line(locales.FindResultsLimit(maxResults))
	}
	line(locales.FindResultsSummary(res.matches, len(res.files)))
	return b.String(), hits, spans
}

// results is what a Find Results tab shows: a search's matched lines,
// each a hit to go to.
type results struct {
	hits    []hit
	spans   [][2]int // the matches in the text
	marks   []editor.TextBackground
	color   ui.Color // of marks
	at      int      // the hit next_result went to last, -1 before the first
	clicked bool     // a double click is going on, which goes to its hit as it ends
}

// hit is a matched line of the results: its row, under its file's at
// head, and where its first match is in the file.
type hit struct {
	row, head  int
	path       string
	d          *doc // the tab of a file never saved
	line       int
	start, end int // bytes of the line
}

func (r *results) show(d *doc, text string, hits []hit, spans [][2]int) {
	d.st.Buffer.Reset(text)
	d.saved = d.st.Buffer.Snapshot()
	d.st.Select(0, 0)
	r.hits, r.spans, r.marks, r.color, r.at = hits, spans, nil, ui.Color{}, -1
}

// resultsTab shows the Find Results tab, made in the focused group when
// there is none.
func (a *app) resultsTab() *doc {
	d := a.fif.tab
	if gi, _ := a.locate(d); d == nil || gi < 0 {
		d = newDoc("", "")
		d.kind, d.results = findDoc, &results{at: -1}
		d.st.ReadOnly, d.st.LineNumbers, d.st.NoFolding, d.st.NoIndentGuides = true, editor.LineNumbersOff, true, true
		g := a.group()
		g.docs = append(g.docs, d)
		a.fif.tab = d
	}
	a.show(d)
	return d
}

// resultsView shows the Find Results tab d of g, its matches highlighted.
// Enter or a double click on a line goes to its match.
func (a *app) resultsView(c *ui.Context, g *group, d *doc) {
	r := d.results
	if col := a.scheme.find.Alpha(0.3); col != r.color {
		r.color, r.marks = col, make([]editor.TextBackground, len(r.spans))
		for i, sp := range r.spans {
			r.marks[i] = editor.TextBackground{Start: sp[0], End: sp[1], Color: col}
		}
	}
	d.st.TextBackgrounds = r.marks
	d.st.OnInput = func(ev ui.InputEvent) bool {
		switch {
		case ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods == 0:
			a.openResult(d, d.st.CursorPos().Line)
			return true
		case ev.Kind == ui.InputPointerDown:
			r.clicked = ev.Button == 0 && ev.Clicks == 2
		case ev.Kind == ui.InputPointerUp && r.clicked:
			r.clicked = false
			a.openResult(d, d.st.CursorPos().Line)
		}
		return false
	}
	editor.View(c.Key(key(d)), d.st).Grow(1).FillWidth().MinHeight(0).FocusBind(&a.focused, place{g, d})
}

// openResult goes to the hit of the row of the results d, or to the first
// of a file's on its path's row.
func (a *app) openResult(d *doc, row int) {
	r := d.results
	i, _ := slices.BinarySearchFunc(r.hits, row, func(h hit, row int) int { return h.row - row })
	if i == len(r.hits) || r.hits[i].row != row && r.hits[i].head != row {
		return
	}
	r.at = i
	a.openHit(r.hits[i])
}

// stepResult is Sublime Text's next_result, or prev_result: it goes to
// the next hit of the Find Results, wrapping, and puts their caret on it.
func (a *app) stepResult(step int) {
	d := a.fif.tab
	if gi, _ := a.locate(d); d == nil || gi < 0 || len(d.results.hits) == 0 {
		return
	}
	r := d.results
	n := len(r.hits)
	switch {
	case r.at < 0 && step > 0:
		r.at = 0
	case r.at < 0:
		r.at = n - 1
	default:
		r.at = ((r.at+step)%n + n) % n
	}
	h := r.hits[r.at]
	d.st.GoTo(editor.Pos{Line: h.row})
	a.openHit(h)
}

// openHit shows the file of h with its match selected, as the file has
// it now.
func (a *app) openHit(h hit) {
	d := h.d
	if h.path != "" {
		a.open([]string{h.path})
		d = a.docAt(h.path)
	} else if gi, _ := a.locate(d); gi >= 0 {
		a.show(d)
	} else {
		return
	}
	if d == nil || d.kind != textDoc {
		return
	}
	t := d.st.Buffer.Snapshot()
	line := min(h.line, t.LineCount()-1)
	from, to := t.LineStart(line), t.LineEnd(line)
	d.st.GoTo(editor.Pos{Line: line})
	d.st.Select(min(from+h.start, to), min(from+h.end, to))
}

// findInFilesBar builds the Find in Files panel as Sublime Text's: the
// options, the query and the button, and where to look below the query.
func (a *app) findInFilesBar(c *ui.Context) {
	f := &a.fif
	if !f.open {
		return
	}
	sc, ch := a.scheme, a.scheme.chrome()
	field := func(value *string, label string) ui.Element {
		return ui.TextInputBase(c, value).Grow(1).MinWidth(findQueryW).Height(24).Padding(0, 8).Placeholder(label).Label(label).
			FontSize(13).Background(sc.bg).Border(1, ch.line).Radius(3).OnSubmit(a.searchInFiles)
	}
	toggle := func(on *bool, label, tip string) {
		b := ui.ToggleBase(c, on).Width(findToggleW).Height(24).Center().Radius(3).Transition(colorFade).Label(tip).Tooltip(tip).KeepFocus()
		fg := ch.muted
		switch {
		case *on:
			b.Background(sc.accent.Alpha(0.25))
			fg = ch.text
		case b.Hovered():
			b.Background(ch.hover)
		}
		b.Children(func() { ui.Text(c, label).FontSize(12).TextColor(fg) })
	}
	row := func(fill func()) { ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(fill) }

	bar := ui.Column(c).FillWidth().Shrink(0).Gap(6).Padding(6, 8).Background(ch.bg).
		BorderWidth(1, 0, 0, 0).BorderColor(ch.line).Transition(findBarMotion)
	bar.OnShortcut(0, ui.KeyEscape, a.closeFindInFiles)
	bar.Children(func() {
		row(func() {
			ui.Row(c).Gap(2).Shrink(0).Children(func() {
				toggle(&f.regex, ".*", locales.FindRegex())
				toggle(&f.caseSens, "Aa", locales.FindCaseSensitive())
				toggle(&f.wholeWord, "ab", locales.FindWholeWord())
			})
			field(&f.query, locales.Find()).Bind(&f.input)
			b := ui.ButtonBase(c).Width(findSlotW).Height(24).Center().Radius(3).Background(ch.panel).Border(1, ch.line).
				Transition(colorFade).Label(locales.Find()).KeepFocus().OnClick(a.searchInFiles)
			if b.Hovered() {
				b.Background(ch.hover)
			}
			b.Children(func() { ui.Text(c, locales.Find()).FontSize(12).TextColor(ch.text) })
		})
		row(func() {
			ui.Box(c).Width(3*findToggleW + 2*2).Shrink(0)
			field(&f.where, locales.FindWhere())
			ui.Box(c).Width(findSlotW).Shrink(0)
		})
	})
	if f.focus {
		f.focus = false
		f.input.Focus(c)
	}
}

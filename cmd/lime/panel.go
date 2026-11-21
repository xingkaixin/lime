package main

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// item is a row of the quick panel. A command's alias is its English
// label, which a query matches in any language.
type item struct {
	label, alias, detail, hint string
	pick                       func()
	preview                    func()
	tab                        *doc
	path                       string
}

type panel struct {
	query       string
	placeholder string
	list        func(query string) []item
	cancel      func()
	shown       []item
	listed      string
	loading     bool
	stale       bool
	sel         int
	start       int
	reveal      bool
	// submit makes the panel Sublime Text's input panel, without a list:
	// Enter gives it the query, which prompt captions.
	submit func(query string)
	prompt string
	// peek is the current tab's caret from before the rows that show as
	// they are selected moved it: it goes back as the panel closes.
	peek *peek
}

type peek struct {
	d    *doc
	sels []editor.Selection
	y    float64
}

const maxShown = 100

func (a *app) openPanel(p *panel) {
	p.stale = true
	a.panel = p
}

// refresh lists the items for the query. A query typed selects the first
// and shows it; items changed for the same query, as the project's files
// listed, keep the row selected.
func (p *panel) refresh() {
	if !p.stale && p.listed == p.query {
		return
	}
	old, typed := p.shown, p.listed != p.query
	p.shown, p.listed, p.stale = p.list(p.query), p.query, false
	switch {
	case old == nil:
		p.sel = min(p.start, max(len(p.shown)-1, 0))
	case typed:
		p.sel = 0
		if p.query != "" {
			p.preview()
		}
	case p.sel < len(old):
		was := old[p.sel]
		p.sel = max(slices.IndexFunc(p.shown, func(it item) bool { return it.label == was.label && it.detail == was.detail }), 0)
	}
	p.reveal = true
}

func (p *panel) preview() {
	if p.sel < len(p.shown) && p.shown[p.sel].preview != nil {
		p.shown[p.sel].preview()
	}
}

func (a *app) movePanel(step int) {
	p := a.panel
	if n := len(p.shown); n > 0 {
		p.sel = ((p.sel+step)%n + n) % n
		p.reveal = true
		p.preview()
	}
}

func (a *app) unpeek(p *panel) {
	if k := p.peek; k != nil {
		k.d.st.SetSelections(k.sels...)
		a.scroll = k
	}
}

// scrollBack scrolls the editor unpeek put the selections back in to
// where it was, after the editor built has revealed them, as it does
// selections set: unless they moved again.
func (a *app) scrollBack() {
	if k := a.scroll; k != nil {
		a.scroll = nil
		if slices.Equal(k.d.st.Selections(), k.sels) {
			k.d.st.SetScrollY(k.y)
		}
	}
}

func (a *app) pickPanel() {
	p := a.panel
	if p.loading {
		return
	}
	a.panel = nil
	a.unpeek(p)
	switch {
	case p.submit != nil:
		p.submit(p.query)
	case p.sel < len(p.shown) && p.shown[p.sel].pick != nil:
		p.shown[p.sel].pick()
	case p.cancel != nil:
		p.cancel()
	}
}

func (a *app) cancelPanel() {
	p := a.panel
	a.panel = nil
	a.unpeek(p)
	if p.cancel != nil {
		p.cancel()
	}
}

func lower(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + 'a' - 'A'
	}
	return b
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func isBoundaryByte(s string, i int) bool {
	if i == 0 {
		return true
	}
	p := s[i-1]
	if p == '/' || p == '\\' || p == '_' || p == '-' || p == '.' || p == ' ' {
		return true
	}
	return s[i] >= 'A' && s[i] <= 'Z' && !(p >= 'A' && p <= 'Z')
}

func isBoundaryRune(sr []rune, i int) bool {
	if i == 0 {
		return true
	}
	p := sr[i-1]
	if p == '/' || p == '\\' || p == '_' || p == '-' || p == '.' || p == ' ' {
		return true
	}
	return unicode.IsUpper(sr[i]) && !unicode.IsUpper(p)
}

const (
	fuzzyNegInf    = -1 << 29
	fuzzyDPLimit   = 65536
	nameMatchBonus = 1_000_000
)

// fuzzy scores s for the letters of q in order, as Sublime Text matches
// them: more for runs and for the starts of words. Spaces in q match
// anything.
func fuzzy(q, s string) (int, bool) {
	var qStack [64]byte
	qb := qStack[:0]
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c >= utf8.RuneSelf {
			return fuzzyRunes(q, s)
		}
		if c == ' ' {
			continue
		}
		qb = append(qb, lower(c))
	}
	m := len(qb)
	if m == 0 {
		if !isASCII(s) {
			return -utf8.RuneCountInString(s), true
		}
		return -len(s), true
	}
	n := len(s)
	lo, qi := -1, 0
	for i := 0; i < n; i++ {
		c := s[i]
		if c >= utf8.RuneSelf {
			return fuzzyRunes(q, s)
		}
		if qi < m && lower(c) == qb[qi] {
			if qi == 0 {
				lo = i
			}
			qi++
		}
	}
	if qi < m {
		return 0, false
	}
	hi := n - 1
	qi = m - 1
	for i := n - 1; i >= lo; i-- {
		if lower(s[i]) == qb[qi] {
			if qi == m-1 {
				hi = i
			}
			qi--
			if qi < 0 {
				break
			}
		}
	}
	w := hi - lo + 1
	if m*w > fuzzyDPLimit {
		return fuzzyGreedyBytes(qb, s), true
	}
	var dStack, mStack [256]int
	var bStack [256]int8
	var d, best []int
	var bonus []int8
	if w <= len(dStack) {
		d, best, bonus = dStack[:w], mStack[:w], bStack[:w]
	} else {
		d, best, bonus = make([]int, w), make([]int, w), make([]int8, w)
	}
	for k := 0; k < w; k++ {
		if isBoundaryByte(s, lo+k) {
			bonus[k] = 3
		} else {
			bonus[k] = 0
		}
	}
	curM := fuzzyNegInf
	q0 := qb[0]
	for k := 0; k < w; k++ {
		v := fuzzyNegInf
		if lower(s[lo+k]) == q0 {
			v = 1 + int(bonus[k])
		}
		if v > curM {
			curM = v
		}
		d[k], best[k] = v, curM
	}
	for j := 1; j < m; j++ {
		qj := qb[j]
		diagD, diagM := fuzzyNegInf, fuzzyNegInf
		curM = fuzzyNegInf
		for k := 0; k < w; k++ {
			oldD, oldM := d[k], best[k]
			nextD := fuzzyNegInf
			if lower(s[lo+k]) == qj {
				if diagM > fuzzyNegInf {
					nextD = diagM + 1 + int(bonus[k])
				}
				if diagD > fuzzyNegInf {
					if c := diagD + 5; c > nextD {
						nextD = c
					}
				}
			}
			if nextD > curM {
				curM = nextD
			}
			d[k], best[k] = nextD, curM
			diagD, diagM = oldD, oldM
		}
	}
	return best[w-1]*100 - n, true
}

func fuzzyGreedyBytes(qb []byte, s string) int {
	score, last, i := 0, -2, 0
	for _, qc := range qb {
		for lower(s[i]) != qc {
			i++
		}
		score++
		switch {
		case i == last+1:
			score += 4
		case isBoundaryByte(s, i):
			score += 3
		}
		last = i
		i++
	}
	return score*100 - len(s)
}

func fuzzyRunes(q, s string) (int, bool) {
	var qr []rune
	for _, r := range q {
		if r == ' ' {
			continue
		}
		qr = append(qr, unicode.ToLower(r))
	}
	sr := []rune(s)
	m, n := len(qr), len(sr)
	if m == 0 {
		return -n, true
	}
	sl := make([]rune, n)
	lo, qi := -1, 0
	for i, r := range sr {
		lr := unicode.ToLower(r)
		sl[i] = lr
		if qi < m && lr == qr[qi] {
			if qi == 0 {
				lo = i
			}
			qi++
		}
	}
	if qi < m {
		return 0, false
	}
	hi := n - 1
	qi = m - 1
	for i := n - 1; i >= lo; i-- {
		if sl[i] == qr[qi] {
			if qi == m-1 {
				hi = i
			}
			qi--
			if qi < 0 {
				break
			}
		}
	}
	w := hi - lo + 1
	if m*w > fuzzyDPLimit {
		return fuzzyGreedyRunes(qr, sr, sl), true
	}
	d, best := make([]int, w), make([]int, w)
	bonus := make([]int8, w)
	for k := 0; k < w; k++ {
		if isBoundaryRune(sr, lo+k) {
			bonus[k] = 3
		}
	}
	curM := fuzzyNegInf
	q0 := qr[0]
	for k := 0; k < w; k++ {
		v := fuzzyNegInf
		if sl[lo+k] == q0 {
			v = 1 + int(bonus[k])
		}
		if v > curM {
			curM = v
		}
		d[k], best[k] = v, curM
	}
	for j := 1; j < m; j++ {
		qj := qr[j]
		diagD, diagM := fuzzyNegInf, fuzzyNegInf
		curM = fuzzyNegInf
		for k := 0; k < w; k++ {
			oldD, oldM := d[k], best[k]
			nextD := fuzzyNegInf
			if sl[lo+k] == qj {
				if diagM > fuzzyNegInf {
					nextD = diagM + 1 + int(bonus[k])
				}
				if diagD > fuzzyNegInf {
					if c := diagD + 5; c > nextD {
						nextD = c
					}
				}
			}
			if nextD > curM {
				curM = nextD
			}
			d[k], best[k] = nextD, curM
			diagD, diagM = oldD, oldM
		}
	}
	return best[w-1]*100 - n, true
}

func fuzzyGreedyRunes(qr, sr, sl []rune) int {
	score, last, i := 0, -2, 0
	for _, qc := range qr {
		for sl[i] != qc {
			i++
		}
		score++
		switch {
		case i == last+1:
			score += 4
		case isBoundaryRune(sr, i):
			score += 3
		}
		last = i
		i++
	}
	return score*100 - len(sr)
}

type rankedHit struct {
	idx   int
	score int
}

func worseHit(a, b rankedHit) bool {
	return a.score < b.score || a.score == b.score && a.idx > b.idx
}

func siftDownHit(h []rankedHit, i int) {
	n := len(h)
	for {
		l := 2*i + 1
		if l >= n {
			return
		}
		smallest := l
		if r := l + 1; r < n && worseHit(h[r], h[l]) {
			smallest = r
		}
		if !worseHit(h[smallest], h[i]) {
			return
		}
		h[i], h[smallest] = h[smallest], h[i]
		i = smallest
	}
}

func rankTop[T any](items []T, limit int, score func(*T) (int, bool)) []T {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	var top []rankedHit
	heapified := false
	for i := range items {
		s, ok := score(&items[i])
		if !ok {
			continue
		}
		if len(top) < limit {
			if top == nil {
				top = make([]rankedHit, 0, min(len(items), limit))
			}
			top = append(top, rankedHit{idx: i, score: s})
			continue
		}
		if !heapified {
			for k := len(top)/2 - 1; k >= 0; k-- {
				siftDownHit(top, k)
			}
			heapified = true
		}
		// Since i > top[0].idx always holds, only a strictly higher score beats top[0].
		if s > top[0].score {
			top[0] = rankedHit{idx: i, score: s}
			siftDownHit(top, 0)
		}
	}
	slices.SortFunc(top, func(x, y rankedHit) int {
		if c := cmp.Compare(y.score, x.score); c != 0 {
			return c
		}
		return cmp.Compare(x.idx, y.idx)
	})
	out := make([]T, len(top))
	for i, h := range top {
		out[i] = items[h.idx]
	}
	return out
}

func ranked(items []item, score func(it *item) (int, bool)) []item {
	return rankTop(items, maxShown, score)
}

func byLabel(q string) func(it *item) (int, bool) {
	return func(it *item) (int, bool) {
		if q == "" {
			return 0, true
		}
		s, ok := fuzzy(q, it.label)
		if as, aok := fuzzy(q, it.alias); aok && (!ok || as > s) {
			return as, true
		}
		return s, ok
	}
}

type gotoFile struct {
	tab    *doc
	path   string
	label  string
	detail string
}

type gotoCache struct {
	valid    bool
	gen      int
	filesLen int
	filesPtr *string
	entries  []gotoFile
}

func (c *gotoCache) list(a *app) []gotoFile {
	var ptr *string
	if len(a.proj.files) > 0 {
		ptr = &a.proj.files[0]
	}
	if c.valid && c.gen == a.proj.gen && c.filesLen == len(a.proj.files) && c.filesPtr == ptr {
		return c.entries
	}
	entries := make([]gotoFile, 0, len(a.mru)+len(a.proj.files))
	seen := make(map[string]bool, len(a.mru))
	for _, d := range a.mru {
		detail := d.path
		if d.path != "" {
			seen[d.path] = true
			detail = a.proj.rel(d.path)
		}
		entries = append(entries, gotoFile{tab: d, label: d.name(), detail: detail})
	}
	for _, p := range a.proj.files {
		if seen[p] {
			continue
		}
		entries = append(entries, gotoFile{path: p, label: filepath.Base(p), detail: a.proj.rel(p)})
	}
	c.valid = true
	c.gen = a.proj.gen
	c.filesLen = len(a.proj.files)
	c.filesPtr = ptr
	c.entries = entries
	return entries
}

func parseGotoInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// parseGotoLoc splits optional :line or :line:col suffixes from a Goto Anything
// query while leaving filenames with non-numeric colons intact.
func parseGotoLoc(query string) (name string, line, col int, hasColon bool) {
	last := strings.LastIndexByte(query, ':')
	if last < 0 {
		return query, 0, 0, false
	}
	head, tail := query[:last], query[last+1:]
	if tail == "" {
		if prev := strings.LastIndexByte(head, ':'); prev >= 0 {
			if n, ok := parseGotoInt(head[prev+1:]); ok {
				return head[:prev], n, 0, true
			}
		}
		return head, 0, 0, true
	}
	n, ok := parseGotoInt(tail)
	if !ok {
		return query, 0, 0, false
	}
	if prev := strings.LastIndexByte(head, ':'); prev >= 0 {
		if l, ok := parseGotoInt(head[prev+1:]); ok {
			return head[:prev], l, n, true
		}
	}
	return head, n, 0, true
}

// showGoto opens Goto Anything with query, as ":" for Goto Line or "@"
// for Goto Symbol: the open files and the project's, with ":line[:col]"
// or "@symbol" after a name or alone.
func (a *app) showGoto(query string) {
	if len(a.proj.folders) > 0 {
		a.indexFolders()
	}
	var syms symbolFile
	var files gotoCache
	p := &panel{query: query}
	p.list = func(q string) []item {
		p.loading = false
		return a.gotoList(q, &syms, &files)
	}
	a.openPanel(p)
}

func (a *app) gotoList(query string, syms *symbolFile, cache *gotoCache) []item {
	if at := strings.LastIndexByte(query, '@'); at >= 0 {
		return a.symbolList(query[:at], query[at+1:], syms, cache)
	}
	name, line, col, hasColon := parseGotoLoc(query)
	if hasColon && name == "" {
		d := a.current()
		if d == nil {
			return nil
		}
		it := item{label: locales.PanelTypeLine(), detail: d.name()}
		if line > 0 {
			it.label = locales.PanelGoToLine(line)
			it.pick = func() { goToLine(d, line, col) }
		}
		return []item{it}
	}
	entries := cache.list(a)
	var top []gotoFile
	if name == "" {
		top = entries[:min(len(entries), maxShown)]
	} else {
		top = rankTop(entries, maxShown, func(f *gotoFile) (int, bool) {
			return scoreByName(name, f.label, f.detail)
		})
	}
	items := make([]item, len(top))
	for i, f := range top {
		items[i] = a.gotoItem(f, line, col)
	}
	return items
}

func (a *app) gotoItem(f gotoFile, line, col int) item {
	if f.tab != nil {
		d := f.tab
		return item{label: f.label, detail: f.detail, tab: d, pick: func() {
			a.show(d)
			goToLine(d, line, col)
		}}
	}
	p := f.path
	return item{label: f.label, detail: f.detail, path: p, pick: func() {
		a.open([]string{p})
		if d := a.docAt(p); d != nil {
			goToLine(d, line, col)
		}
	}}
}

type symbolFile struct {
	d       *doc
	path    string
	buf     *editor.Buffer
	syms    []symbol
	gen     int
	loading bool
}

// symbolList lists for Goto Symbol the symbols of the current tab, or of
// the file Goto Anything lists first for name. The current tab's show as
// they are selected.
func (a *app) symbolList(name, query string, f *symbolFile, cache *gotoCache) []item {
	cur := a.current()
	d, path := cur, ""
	if name != "" {
		top := rankTop(cache.list(a), 1, func(g *gotoFile) (int, bool) {
			return scoreByName(name, g.label, g.detail)
		})
		if len(top) == 0 {
			return nil
		}
		d, path = top[0].tab, top[0].path
	}
	if d != nil && !d.plain() {
		return nil
	}
	var buf *editor.Buffer
	var syms []symbol
	if d != nil {
		if f.d != d || f.path != "" || f.loading {
			f.gen++
			f.d, f.path, f.buf, f.syms, f.loading = d, "", nil, nil, false
		}
		buf = d.st.Buffer
		syms = cachedSymbols(d)
	} else if path != "" {
		if f.d != nil || f.path != path {
			f.gen++
			gen, p := f.gen, a.panel
			f.d, f.path, f.buf, f.syms, f.loading = nil, path, nil, nil, true
			a.background(func() func() {
				loaded := readSymbols(path)
				var found []symbol
				if loaded != nil {
					found = symbols(loaded.Language(), loaded.String())
				}
				return func() {
					if f.gen != gen || f.path != path {
						return
					}
					f.buf, f.syms, f.loading = loaded, found, false
					if a.panel == p && p != nil {
						p.stale = true
					}
				}
			})
		}
		if f.loading && a.panel != nil {
			a.panel.loading = true
		}
		if f.loading || f.buf == nil {
			return nil
		}
		buf, syms = f.buf, f.syms
	}
	if buf == nil {
		return nil
	}
	items := make([]item, 0, len(syms))
	for _, s := range syms {
		pos, n := buf.Pos(s.at), len(s.name)
		it := item{label: strings.Repeat("  ", max(s.level-1, 0)) + s.name, pick: func() {
			o := d
			if o == nil {
				a.open([]string{path})
				o = a.docAt(path)
			}
			if o != nil {
				a.show(o)
				selectSymbol(o, pos, n)
			}
		}}
		if d != nil && d == cur {
			it.preview = func() {
				if p := a.panel; p.peek == nil {
					p.peek = &peek{d, d.st.Selections(), d.st.ScrollY()}
				}
				selectSymbol(d, pos, n)
			}
		}
		items = append(items, it)
	}
	return ranked(items, byLabel(query))
}

func readSymbols(path string) *editor.Buffer {
	l := editor.LanguageFor(path)
	if fi, err := os.Stat(path); err != nil || fi.Size() > largeFile || l == nil || fileKind(path) != textDoc {
		return nil
	}
	text, err := readText(path)
	if err != nil {
		return nil
	}
	buf := editor.NewBuffer(text)
	buf.SetLanguage(l)
	return buf
}

func selectSymbol(d *doc, p editor.Pos, n int) {
	d.st.GoTo(p)
	at := d.st.Cursor()
	d.st.Select(at, at+n)
}

func scoreByName(name, label, detail string) (int, bool) {
	if name == "" {
		return 0, true
	}
	// A match in the name outranks one across the path without inverting negative scores.
	if s, ok := fuzzy(name, label); ok {
		return s + nameMatchBonus, true
	}
	return fuzzy(name, detail)
}

func byName(name string) func(it *item) (int, bool) {
	return func(it *item) (int, bool) {
		return scoreByName(name, it.label, it.detail)
	}
}

func goToLine(d *doc, line, col int) {
	if line <= 0 {
		return
	}
	ln := min(line, d.st.Buffer.LineCount()) - 1
	byteCol := 0
	if col > 1 {
		s := d.st.Buffer.Line(ln)
		for rem := col - 1; rem > 0 && byteCol < len(s); rem-- {
			_, n := utf8.DecodeRuneInString(s[byteCol:])
			byteCol += n
		}
	}
	d.st.GoTo(editor.Pos{Line: ln, Col: byteCol})
}

func (a *app) showCommandPalette() {
	tx, en := locales.In(i18n.Locale()), locales.In("en")
	var items []item
	for i := range commands {
		c := &commands[i]
		if c.caption != nil && (c.enabled == nil || c.enabled(a)) {
			items = append(items, item{label: c.caption(tx), alias: c.caption(en), hint: a.l.keys.text(c.id), pick: func() { c.run(a) }})
		}
	}
	for _, l := range syntaxes() {
		name := syntaxName(l)
		items = append(items, item{label: tx.CmdSetSyntaxTo(name), alias: en.CmdSetSyntaxTo(name), pick: func() { a.setSyntax(l) }})
	}
	a.openPanel(&panel{placeholder: tx.PanelCommandPalette(), list: func(q string) []item { return ranked(items, byLabel(q)) }})
}

func (a *app) showSchemes() {
	before := *a.set
	var items []item
	start := 0
	for i, sc := range schemes {
		if sc == a.scheme {
			start = i
		}
		detail := locales.Light()
		if sc.dark {
			detail = locales.Dark()
		}
		items = append(items, item{
			label: sc.name, detail: detail,
			pick:    func() { a.setScheme(sc) },
			preview: func() { a.set.showScheme(sc, a.scheme.dark) },
		})
	}
	a.openPanel(&panel{
		placeholder: locales.PanelSelectColorScheme(), start: start,
		list:   func(q string) []item { return ranked(items, byLabel(q)) },
		cancel: func() { *a.set = before },
	})
}

func (a *app) setScheme(sc *scheme) {
	a.set.showScheme(sc, a.scheme.dark)
	a.l.saveSettings()
}

func syntaxes() []*editor.Language { return append([]*editor.Language{nil}, editor.Languages()...) }

func syntaxName(l *editor.Language) string {
	if l == nil {
		return locales.PlainText()
	}
	return l.Name
}

func (a *app) setSyntax(l *editor.Language) {
	if d := a.current(); d != nil {
		d.st.Buffer.SetLanguage(l)
	}
}

func (a *app) showSyntaxes() {
	d := a.current()
	if d == nil {
		return
	}
	var items []item
	start := 0
	for i, l := range syntaxes() {
		if l == d.st.Buffer.Language() {
			start = i
		}
		items = append(items, item{label: syntaxName(l), pick: func() { d.st.Buffer.SetLanguage(l) }})
	}
	a.openPanel(&panel{placeholder: locales.PanelSetSyntax(), start: start, list: func(q string) []item { return ranked(items, byLabel(q)) }})
}

func (a *app) panelView(c *ui.Context) {
	p := a.panel
	if p == nil {
		return
	}
	s, ch := a.scheme, a.scheme.chrome()
	w, _ := c.Size()
	width := min(600, w-32)
	ui.Overlay(c, func() {
		box := ui.Column(c).Absolute().Top(40).Left((w-width)/2).Width(width).Padding(8).Gap(6).
			Background(ch.panel).Radius(8).Border(1, ch.line).Shadow(0, 8, 28, 0, black.Alpha(0.35))
		if box.PressedOutside() || box.OverlayShortcut(0, ui.KeyEscape) {
			a.cancelPanel()
		}
		box.OnShortcut(0, ui.KeyUp, func() { a.movePanel(-1) })
		box.OnShortcut(0, ui.KeyDown, func() { a.movePanel(1) })
		box.Children(func() {
			if p.submit != nil {
				ui.Text(c, p.prompt).FontSize(12).TextColor(ch.muted).Padding(0, 2)
			}
			ui.TextInput(c, &p.query).AutoFocus().Placeholder(p.placeholder).FillWidth().Label(locales.PanelLabel()).
				OnSubmit(a.pickPanel)
			if p.submit != nil {
				return
			}
			p.refresh()
			if len(p.shown) == 0 {
				ui.Text(c, locales.PanelNoMatches()).FontSize(12).TextColor(ch.muted).Padding(6, 10)
				return
			}
			ui.Scroll(c).FillWidth().MaxHeight(440).Children(func() {
				for i, it := range p.shown {
					row := ui.Column(c.Key(i)).FillWidth().Padding(6, 10).Gap(2).Radius(4).Label(it.label).
						OnClick(func() {
							p.sel = i
							a.pickPanel()
						})
					if i == p.sel {
						row.Background(ch.panel.Mix(s.accent, 0.28))
						if p.reveal {
							row.ScrollIntoView()
						}
					}
					row.Children(func() {
						ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
							ui.Text(c, it.label).FontSize(13).TextColor(ch.text).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
							if it.hint != "" {
								ui.Text(c, it.hint).FontSize(12).TextColor(ch.muted).Shrink(0)
							}
						})
						if it.detail != "" {
							ui.Text(c, it.detail).FontSize(11).TextColor(ch.muted).SingleLine().Ellipsis("…")
						}
					})
				}
			})
			p.reveal = false
		})
	})
}

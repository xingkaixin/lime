package main

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

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
	preview                    func() // as the row is selected, a color scheme's
}

// panel is Sublime Text's quick panel: Goto Anything, the command
// palette and the lists they open.
type panel struct {
	query       string
	placeholder string
	list        func(query string) []item
	cancel      func() // on Escape or a press outside
	shown       []item
	listed      string
	stale       bool // the items changed: list them again
	sel         int
	start       int // the row selected first
	reveal      bool
	// submit makes the panel Sublime Text's input panel, without a list:
	// Enter gives it the query, which prompt captions.
	submit func(query string)
	prompt string
}

const maxShown = 100

func (a *app) openPanel(p *panel) {
	p.stale = true
	a.panel = p
}

func (p *panel) refresh() {
	if !p.stale && p.listed == p.query {
		return
	}
	first := p.shown == nil
	p.shown, p.listed, p.stale = p.list(p.query), p.query, false
	p.sel = 0
	if first {
		p.sel = min(p.start, max(len(p.shown)-1, 0))
	}
	p.reveal = true
}

func (a *app) movePanel(step int) {
	p := a.panel
	if n := len(p.shown); n > 0 {
		p.sel = ((p.sel+step)%n + n) % n
		p.reveal = true
		if fn := p.shown[p.sel].preview; fn != nil {
			fn()
		}
	}
}

func (a *app) pickPanel() {
	p := a.panel
	a.panel = nil
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

// fuzzy scores s for the letters of q in order, as Sublime Text matches
// them: more for runs and for the starts of words. Spaces in q match
// anything.
func fuzzy(q, s string) (int, bool) {
	score, last := 0, -2
	i := 0
	for j := 0; j < len(q); j++ {
		if q[j] == ' ' {
			continue
		}
		for i < len(s) && lower(s[i]) != lower(q[j]) {
			i++
		}
		if i == len(s) {
			return 0, false
		}
		score++
		switch {
		case i == last+1:
			score += 4
		case i == 0 || strings.IndexByte("/\\_-. ", s[i-1]) >= 0 || s[i] >= 'A' && s[i] <= 'Z' && !(s[i-1] >= 'A' && s[i-1] <= 'Z'):
			score += 3
		}
		last = i
		i++
	}
	return score*100 - len(s), true
}

// ranked keeps the items score matches, best first.
func ranked(items []item, score func(it *item) (int, bool)) []item {
	type hit struct {
		it    item
		score int
	}
	var hits []hit
	for i := range items {
		if s, ok := score(&items[i]); ok {
			hits = append(hits, hit{items[i], s})
		}
	}
	slices.SortStableFunc(hits, func(x, y hit) int { return y.score - x.score })
	out := make([]item, 0, min(len(hits), maxShown))
	for _, h := range hits[:min(len(hits), maxShown)] {
		out = append(out, h.it)
	}
	return out
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

// showGoto opens Goto Anything with query, as ":" for Goto Line: the
// open files and the project's, with ":line" after a name or alone.
func (a *app) showGoto(query string) {
	a.indexFolders()
	a.openPanel(&panel{query: query, list: a.gotoList})
}

func (a *app) gotoList(query string) []item {
	name, line := query, 0
	colon := strings.LastIndexByte(query, ':')
	if colon >= 0 {
		n, err := strconv.Atoi(query[colon+1:])
		if err == nil || colon == len(query)-1 {
			name, line = query[:colon], n
		}
	}
	if colon >= 0 && name == "" {
		d := a.current()
		if d == nil {
			return nil
		}
		it := item{label: locales.PanelTypeLine(), detail: d.name()}
		if line > 0 {
			it.label = locales.PanelGoToLine(line)
			it.pick = func() { goToLine(d, line) }
		}
		return []item{it}
	}
	var items []item
	seen := make(map[string]bool)
	for _, d := range a.mru {
		detail := d.path
		if d.path != "" {
			seen[d.path] = true
			detail = a.proj.rel(d.path)
		}
		items = append(items, item{label: d.name(), detail: detail, pick: func() {
			a.show(d)
			goToLine(d, line)
		}})
	}
	for _, p := range a.proj.files {
		if seen[p] {
			continue
		}
		items = append(items, item{label: filepath.Base(p), detail: a.proj.rel(p), pick: func() {
			a.open([]string{p})
			if d := a.docAt(p); d != nil {
				goToLine(d, line)
			}
		}})
	}
	return ranked(items, func(it *item) (int, bool) {
		if name == "" {
			return 0, true
		}
		// A match in the name counts for more than one across the path.
		s, ok := fuzzy(name, it.label)
		if ok {
			return s * 2, true
		}
		return fuzzy(name, it.detail)
	})
}

func goToLine(d *doc, line int) {
	if line > 0 {
		d.st.GoTo(editor.Pos{Line: min(line, d.st.Buffer.LineCount()) - 1})
	}
}

func (a *app) showCommandPalette() {
	tx, en := locales.In(i18n.Locale()), locales.In("en")
	var items []item
	for i := range commands {
		c := &commands[i]
		if c.caption != nil {
			items = append(items, item{label: c.caption(tx), alias: c.caption(en), hint: c.keyText(), pick: func() { c.run(a) }})
		}
	}
	for _, l := range syntaxes() {
		name := syntaxName(l)
		items = append(items, item{label: tx.CmdSetSyntaxTo(name), alias: en.CmdSetSyntaxTo(name), pick: func() { a.setSyntax(l) }})
	}
	a.openPanel(&panel{placeholder: tx.PanelCommandPalette(), list: func(q string) []item { return ranked(items, byLabel(q)) }})
}

// showSchemes lists the color schemes, each showing as it is selected.
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
		list: func(q string) []item {
			shown := ranked(items, byLabel(q))
			if len(shown) > 0 && q != "" {
				shown[0].preview()
			}
			return shown
		},
		cancel: func() { *a.set = before },
	})
}

func (a *app) setScheme(sc *scheme) {
	a.set.showScheme(sc, a.scheme.dark)
	a.l.saveSettings()
}

// syntaxes are Plain Text, as nil, and the editor's languages.
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

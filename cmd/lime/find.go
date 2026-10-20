package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// finding is what Sublime Text's find panel keeps across the editors of
// a window: the query, the replacement and the options of the panel last
// used, which a panel opened in another editor starts with.
type finding struct {
	query, replace             string
	caseSens, wholeWord, regex bool
	replacing                  bool
	pending                    bool      // the current editor's panel takes them as it next shows
	seen                       string    // the query of the current editor's panel, as last read
	rowsAt                     time.Time // when the current editor's panel last showed or hid the replace row
}

// showFind opens the find panel of the current editor, with the replace
// row when replace is set. The query is the selection on one line, as in
// Sublime Text, else the last one.
func (a *app) showFind(replace bool) {
	d := a.current()
	if d == nil {
		return
	}
	s := &a.find
	s.replacing, s.pending = replace, true
	if d.cmp != nil {
		d.cmp.diff.Find(s.query)
		return
	}
	d.st.Run(editor.Find)
	if t := selectionQuery(d.st); t != "" {
		s.query = t
	} else {
		d.st.Find(s.query)
	}
}

// selectionQuery returns the text of the newest selection, when it is on
// one line, as Sublime Text takes a query from the selections.
func selectionQuery(st *editor.State) string {
	s := st.Selection()
	t := st.Buffer.Slice(min(s.Anchor, s.Head), max(s.Anchor, s.Head))
	if strings.Contains(t, "\n") {
		return ""
	}
	return t
}

// quickFind makes a command of run, one of Sublime Text's commands that
// find from the selections, as Quick Add Next: the newest selection it
// leaves becomes the query of the find panels to come.
func quickFind(run func(st *editor.State)) func(a *app) {
	return withDoc(func(a *app, d *doc) {
		run(d.st)
		if t := selectionQuery(d.st); t != "" {
			a.find.query = t
		}
	})
}

// skipNext is Sublime's find_under_expand_skip: the newest selection
// gives way to the next occurrence.
func skipNext(st *editor.State) {
	old := st.Selection()
	if old.Anchor == old.Head {
		st.Run(editor.SelectNext)
		return
	}
	if !st.Run(editor.SelectNext) {
		return
	}
	next := st.Selection()
	rest := slices.DeleteFunc(st.Selections(), func(s editor.Selection) bool { return s == old || s == next })
	st.SetSelections(append(rest, next)...)
}

const (
	findToggleW = 28
	// The status and the buttons share a width, which they give up to
	// findSlotMin in a narrow group before the query does.
	findSlotW   = 84
	findSlotMin = 48
	findQueryW  = 120

	findMotionTime = 200 * time.Millisecond
)

// The find panel fades in sliding up and out sliding down, and its replace
// row opens and closes. Colors alone keeps the editor's resizing, as a
// divider dragged, from animating them: Enter and Exit play regardless.
var (
	findBarMotion = ui.ElementTransition{Duration: findMotionTime, Colors: true, Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{Y: 8}}
	findRowMotion = ui.ElementTransition{Duration: findMotionTime, Colors: true, Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}}
)

// findBar builds the find panel of d's editor as Sublime Text's, along
// the bottom of the editor: the options, the query and the buttons, and
// the replacement below them while replacing.
func (a *app) findBar(c *ui.Context, bar ui.Element, f *editor.FindBar, d *doc) {
	s := &a.find
	switch {
	case d != a.current():
		// A panel left open in another group keeps its own.
	case s.pending:
		s.pending, s.seen = false, *f.Query()
		*f.Replacement() = s.replace
		*f.CaseSensitive(), *f.WholeWord(), *f.Regexp() = s.caseSens, s.wholeWord, s.regex
		if f.Replacing() != s.replacing {
			s.rowsAt = c.Now()
		}
		f.SetReplacing(s.replacing)
	default:
		// Only a query typed in the panel replaces the window's: Quick Add
		// Next sets that while the panel still shows another.
		if q := *f.Query(); q != s.seen {
			s.query, s.seen = q, q
		}
		s.replace = *f.Replacement()
		s.caseSens, s.wholeWord, s.regex = *f.CaseSensitive(), *f.WholeWord(), *f.Regexp()
	}

	sc, ch := a.scheme, a.scheme.chrome()
	// The editor places the bar over its top right corner: Attach moves it
	// to the bottom, and Top and Right take back the editor's offsets.
	bar.Attach(ui.AnchorBottomLeft, ui.AnchorBottomLeft).Top(0).Right(0).WidthPercent(100).
		Gap(6).Padding(6, 8).Background(ch.bg).BorderWidth(1, 0, 0, 0).BorderColor(ch.line)
	motion := findBarMotion
	if c.Now().Sub(s.rowsAt) < findMotionTime {
		// Only the replace row showing or hiding grows and shrinks the panel.
		motion.Position, motion.Size = true, true
	}
	bar.Transition(motion)
	field := func(value *string, label string) ui.Element {
		return ui.TextInputBase(c, value).Grow(1).MinWidth(findQueryW).Height(24).Padding(0, 8).Placeholder(label).Label(label).
			FontSize(13).Background(sc.bg).Border(1, ch.line).Radius(3)
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
	slot := func(e ui.Element) ui.Element { return e.Width(findSlotW).Shrink(1).MinWidth(findSlotMin) }
	button := func(label string, run func()) {
		b := slot(ui.ButtonBase(c)).Height(24).Center().Radius(3).Background(ch.panel).Border(1, ch.line).
			Transition(colorFade).Label(label).KeepFocus().OnClick(run)
		if b.Hovered() {
			b.Background(ch.hover)
		}
		b.Children(func() { ui.Text(c, label).FontSize(12).TextColor(ch.text).SingleLine().Ellipsis("…") })
	}
	row := func(fill func()) ui.Element { return ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(fill) }

	bar.Children(func() {
		row(func() {
			ui.Row(c).Gap(2).Shrink(0).Children(func() {
				toggle(f.Regexp(), ".*", locales.FindRegex())
				toggle(f.CaseSensitive(), "Aa", locales.FindCaseSensitive())
				toggle(f.WholeWord(), "ab", locales.FindWholeWord())
				toggle(f.InSelection(), "[ ]", locales.FindInSelection())
			})
			f.QueryInput(field(f.Query(), locales.Find()))
			slot(ui.Text(c, f.Status())).FontSize(12).TextColor(ch.muted).FontFeatures("tnum").
				SingleLine().Ellipsis("…").Role(ui.RoleStatus)
			button(locales.Find(), f.Next)
			button(locales.FindPrevious(), f.Previous)
			button(locales.FindAll(), f.SelectAll)
		})
		if f.Replacing() {
			row(func() {
				ui.Box(c).Width(4*findToggleW + 3*2).Shrink(0)
				f.ReplacementInput(field(f.Replacement(), locales.Replace()))
				slot(ui.Box(c))
				button(locales.Replace(), f.Replace)
				button(locales.ReplaceAll(), f.ReplaceAll)
				slot(ui.Box(c))
			}).Transition(findRowMotion)
		}
	})
}

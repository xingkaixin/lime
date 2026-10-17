package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const (
	findToggleW = 28
	// The status and the buttons share a width, which they give up to
	// findSlotMin in a narrow group before the query does.
	findSlotW   = 84
	findSlotMin = 48
	findQueryW  = 120
)

// findBar builds an editor's find panel as Sublime Text's, along the
// bottom of the editor: the options, the query and the buttons, and the
// replacement below them while replacing.
func (a *app) findBar(c *ui.Context, bar ui.Element, f *editor.FindBar) {
	sc, ch := a.scheme, a.scheme.chrome()
	// The editor places the bar over its top right corner: Attach moves it
	// to the bottom, and Top and Right take back the editor's offsets.
	bar.Attach(ui.AnchorBottomLeft, ui.AnchorBottomLeft).Top(0).Right(0).WidthPercent(100).
		Gap(6).Padding(6, 8).Background(ch.bg).BorderWidth(1, 0, 0, 0).BorderColor(ch.line)
	field := func(value *string, label string) ui.Element {
		return ui.TextInputBase(c, value).Grow(1).MinWidth(findQueryW).Height(24).Padding(0, 8).Placeholder(label).Label(label).
			FontSize(13).Background(sc.bg).Border(1, ch.line).Radius(3)
	}
	toggle := func(on *bool, label, tip string) {
		b := ui.ToggleBase(c, on).Width(findToggleW).Height(24).Center().Radius(3).Label(tip).Tooltip(tip).KeepFocus()
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
			Label(label).KeepFocus().OnClick(run)
		if b.Hovered() {
			b.Background(ch.hover)
		}
		b.Children(func() { ui.Text(c, label).FontSize(12).TextColor(ch.text).SingleLine().Ellipsis("…") })
	}
	row := func(fill func()) { ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(fill) }

	bar.Children(func() {
		row(func() {
			ui.Row(c).Gap(2).Shrink(0).Children(func() {
				toggle(f.Regexp(), ".*", "Regular expression")
				toggle(f.CaseSensitive(), "Aa", "Case sensitive")
				toggle(f.WholeWord(), "ab", "Whole word")
				toggle(f.InSelection(), "[ ]", "In selection")
			})
			f.QueryInput(field(f.Query(), "Find"))
			slot(ui.Text(c, f.Status())).FontSize(12).TextColor(ch.muted).FontFeatures("tnum").
				SingleLine().Ellipsis("…").Role(ui.RoleStatus)
			button("Find", f.Next)
			button("Find Prev", f.Previous)
			button("Find All", f.SelectAll)
		})
		if f.Replacing() {
			row(func() {
				ui.Box(c).Width(4*findToggleW + 3*2).Shrink(0)
				f.ReplacementInput(field(f.Replacement(), "Replace"))
				slot(ui.Box(c))
				button("Replace", f.Replace)
				button("Replace All", f.ReplaceAll)
				slot(ui.Box(c))
			})
		}
	})
}

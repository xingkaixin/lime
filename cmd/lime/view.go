package main

import (
	"fmt"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func (a *app) view(c *ui.Context) {
	a.syncThemeSource()
	// The window's theme follows the system here, before SetTheme, which
	// Auto goes by.
	s := resolveScheme(a.set.ColorScheme, c.Theme().Dark)
	// Another color scheme shows at once, as the editor's colors do,
	// rather than fading the colors around them.
	if s != a.scheme {
		c.DisableAnimations()
	}
	a.scheme = s
	c.SetTheme(a.scheme.uiTheme(c.Theme()))
	a.syncFocus(c.Modifiers()&ui.Ctrl != 0)
	if t := a.drag; t != nil {
		if gi, _ := a.locate(t.d); gi < 0 {
			a.drag = nil
		}
	}
	a.shortcuts(c)
	a.syncTitle()
	a.syncMenu()

	ch := a.shell(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	root := ui.Column(c).Fill().Background(ch.bg)
	if files := root.DroppedFiles(); files != nil {
		a.open(files)
	}
	root.Children(func() {
		if a.sideBarShown() {
			// The window controls sit over the side bar's top.
			if w := c.TitleBar().Left; w > 0 {
				a.set.SideBarWidth = max(a.set.SideBarWidth, w+controlsGap)
			}
			ui.Split(c, &a.set.SideBarWidth, func() { a.sideBar(c) }, func() { a.groupsView(c) }).Grow(1).FillWidth().MinHeight(0)
		} else {
			a.groupsView(c)
		}
		a.statusBar(c)
		a.draggedTab(c)
	})
	a.panelView(c)
}

func (a *app) sideBarShown() bool { return a.set.SideBar && len(a.proj.folders) > 0 }

const tabBarHeight = 34

// tabMinW is the width tabs shrink to before they no longer fit.
const tabMinW = 72

// controlsGap is the room left after the window controls.
const controlsGap = 12

// titleBand is the height of the band along the window's top that the
// window controls sit in, over the side bar and the top groups' tab bars;
// zero where the window has its own title bar, or in full screen.
func titleBand(c *ui.Context) float32 {
	if h := c.TitleBar().Height; h > 0 {
		return max(h, tabBarHeight)
	}
	return 0
}

// themeSource is the appearance of the window's title bar for a scheme.
func themeSource(name string) mygo.ThemeSource {
	switch s := schemeNamed(name); {
	case s == nil:
		return mygo.ThemeSystem
	case s.dark:
		return mygo.ThemeDark
	}
	return mygo.ThemeLight
}

func (a *app) syncThemeSource() {
	if src := themeSource(a.set.ColorScheme); src != a.l.themeSource && a.win != nil {
		a.l.themeSource = src
		mygo.Theme.SetSource(src)
	}
}

// groupsView lays out the groups in rows and columns, with dividers that
// the user drags. The rows share the columns' dividers, as Sublime Text's
// grid does.
func (a *app) groupsView(c *ui.Context) {
	l := a.layout
	if len(a.cols) != l.cols-1 {
		a.cols = even(l.cols)
	}
	if len(a.rows) != l.rows-1 {
		a.rows = even(l.rows)
	}
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
		splits(c, a.rows, true, func(r int) {
			splits(c, a.cols, false, func(col int) { a.groupView(c, r*l.cols+col) })
		})
	})
}

// even places the dividers of n panes of the same size: each percentage
// is of the room the panes before it leave.
func even(n int) []float32 {
	p := make([]float32, max(n-1, 0))
	for i := range p {
		p[i] = 100 / float32(n-i)
	}
	return p
}

// splits lays out len(p)+1 panes, the ones after the first in the second
// pane of a split with the first.
func splits(c *ui.Context, p []float32, vertical bool, pane func(i int)) {
	var from func(i int)
	from = func(i int) {
		if i == len(p) {
			pane(i)
			return
		}
		split := ui.SplitPercent
		if vertical {
			split = ui.SplitVerticalPercent
		}
		split(c, &p[i], func() { pane(i) }, func() { from(i + 1) }).Fill()
	}
	from(0)
}

func (a *app) groupView(c *ui.Context, gi int) {
	g := a.groups[gi]
	s := a.scheme
	col := ui.Column(c.Key(key(g))).Fill().MinWidth(0).MinHeight(0)
	if !c.Vibrancy() {
		col.Background(s.bg)
	}
	if files := col.DroppedFiles(); files != nil {
		a.focusGroup(gi)
		a.open(files)
	}
	a.adopt(g)
	col.Children(func() {
		a.tabBar(c, g, gi)
		d := g.current()
		if d == nil {
			d = g.placeholder()
		}
		d.st.Theme = s.editorTheme()
		d.st.SoftWrap = a.set.WordWrap
		d.st.Font.Size = a.set.FontSize
		d.st.RenderFindBar = func(c *ui.Context, bar ui.Element, f *editor.FindBar) { a.findBar(c, bar, f, d) }
		e := editor.View(c.Key(key(d)), d.st).Grow(1).FillWidth().MinHeight(0).FocusBind(&a.focused, place{g, d})
		a.input(e, d)
	})
}

// tabBar shows the tabs of g, the group at gi, which the pointer drags
// (dragTab).
func (a *app) tabBar(c *ui.Context, g *group, gi int) {
	a.makeWay(g)
	var band float32
	if gi < a.layout.cols {
		band = titleBand(c)
	}
	ui.Row(c).FillWidth().Height(max(band, tabBarHeight)).Shrink(0).AlignItems(ui.Stretch).Background(a.shell(c).bg).Children(func() {
		// Without the side bar, the window controls sit before the first
		// group's tabs, apart from them and outside the bar that dragged
		// tabs measure.
		if w := c.TitleBar().Left; gi == 0 && w > 0 && !a.sideBarShown() {
			ui.Box(c).Width(w + controlsGap).Shrink(0).DragWindow()
		}
		// Pressing a tab leaves the keyboard in the editor, as in Sublime Text.
		bar := ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).KeepFocus()
		// A bar that the layout resized, as a split's divider dragged does,
		// moves its tabs at once instead of trailing behind.
		if b := bar.LayoutBounds(); b.W != g.bar.W {
			c.DisableAnimations()
		}
		g.bar = bar.LayoutBounds()
		bar.Children(func() {
			// Tabs that do not fit go out of sight before the new tab button
			// does. Tabs that fit are not clipped: the row shrinks at once as
			// one closes, and would cut off those still sliding into place.
			tabs := ui.Row(c).Shrink(1).MinWidth(0).AlignItems(ui.Stretch)
			if tabs.LayoutBounds().W < float32(len(g.docs))*tabMinW {
				tabs.ClipX()
			}
			tabs.Children(func() {
				for i, d := range g.docs {
					a.tab(c, g, i, d, i == g.active, gi == a.focus)
				}
			})
			a.newTabButton(c, gi)
			// Along the window's top, the room past the tabs is the title bar,
			// which drags the window. Elsewhere a double click there opens a
			// new tab, as in Sublime Text.
			rest := ui.Box(c).Grow(1).Label("Tab bar")
			if band > 0 {
				rest.DragWindow()
			} else if rest.DoubleClicked() {
				a.focusGroup(gi)
				a.newFile()
			}
		})
	})
}

func (a *app) newTabButton(c *ui.Context, gi int) {
	ch := a.shell(c)
	btn := ui.Box(c).Width(24).Height(24).Shrink(0).AlignSelf(ui.Center).MarginStart(4).Radius(4).Center().
		Transition(ui.ElementTransition{Position: true, Colors: true}).Label("New File").Tooltip("New File").OnClick(func() {
		a.focusGroup(gi)
		a.newFile()
	})
	if btn.Hovered() {
		btn.Background(ch.hover)
	}
	btn.Children(func() {
		ui.Text(c, "+").FontSize(16).TextColor(ch.muted)
	})
}

// colorFade fades a control's colors as the pointer comes and goes.
var colorFade = ui.ElementTransition{Colors: true}

// tabMotion slides and resizes the tabs that make way for one dragged,
// opened or closed, which grows in or collapses away.
var tabMotion = ui.ElementTransition{
	Position: true,
	Size:     true,
	Enter:    &ui.Motion{Collapse: true},
	Exit:     &ui.Motion{Collapse: true},
}

func (a *app) tab(c *ui.Context, g *group, i int, d *doc, active, focused bool) {
	ch := a.shell(c)
	// The tab shown changes colors at once, as the editor does: only
	// hovering fades them.
	motion := tabMotion
	motion.Colors = !active
	row := ui.Row(c.Key(key(d))).Transition(motion).AlignItems(ui.Center).Gap(6).Padding(0, 6, 0, 14).Width(200).Shrink(1).MinWidth(tabMinW)
	if i == 0 {
		g.tabW = row.LayoutBounds().W
	}
	// The tab dragged leaves a gap, and shows under the pointer.
	if a.dragTab(row, g, i, d) {
		row.Opacity(0)
	}
	hovered := row.Hovered()
	bg, fg := ch.bg, ch.muted
	switch {
	case active && focused:
		bg, fg = ch.tab, ch.text
	case active:
		bg = ch.tab
	case hovered:
		bg = ch.hover
	}
	row.Background(bg).Label(d.name()).OnClick(func() { a.show(d) })
	row.Children(func() {
		ui.Text(c, d.name()).FontSize(12).TextColor(fg).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
		glyph := "×"
		if d.dirty() && !hovered {
			glyph = "●"
		}
		btn := ui.Box(c).Width(18).Height(18).Radius(4).Center().Transition(colorFade).Label("Close " + d.name()).OnClick(func() { a.closeDoc(d) })
		if btn.Hovered() {
			btn.Background(ch.muted.Alpha(0.2))
		}
		btn.Children(func() {
			ui.Text(c, glyph).FontSize(12).TextColor(fg)
		})
	})
}

func (a *app) statusBar(c *ui.Context) {
	ch := a.shell(c)
	d := a.current()
	var left string
	if d != nil {
		st := d.st
		if n := len(st.Selections()); n > 1 {
			left = fmt.Sprintf("%d selection regions", n)
		} else {
			pos := st.CursorPos()
			line := st.Buffer.Line(pos.Line)
			col := utf8.RuneCountInString(line[:min(pos.Col, len(line))]) + 1
			left = fmt.Sprintf("Line %d, Column %d", pos.Line+1, col)
		}
	}
	ui.Row(c).FillWidth().Height(24).Shrink(0).Padding(0, 6, 0, 12).Gap(16).AlignItems(ui.Center).Background(ch.bg).Children(func() {
		ui.Text(c, left).FontSize(12).TextColor(ch.muted)
		ui.Box(c).Grow(1)
		if d != nil {
			a.syntaxButton(c, d)
		}
	})
}

// syntaxButton shows the syntax of d and changes it from a menu, as
// Sublime Text's status bar does.
func (a *app) syntaxButton(c *ui.Context, d *doc) {
	ch := a.shell(c)
	lang := d.st.Buffer.Language()
	btn := ui.ButtonBase(c).Height(20).Padding(0, 8).Radius(4).Transition(colorFade).Label("Syntax: " + syntaxName(lang))
	if btn.Hovered() {
		btn.Background(ch.hover)
	}
	btn.Menu(func(m *ui.Menu) {
		for i, l := range syntaxes() {
			if m.Item(syntaxName(l)).Checked(l == lang).Chosen() {
				d.st.Buffer.SetLanguage(l)
			}
			if i == 0 {
				m.Separator()
			}
		}
	})
	btn.Children(func() {
		ui.Text(c, syntaxName(lang)).FontSize(12).TextColor(ch.muted)
	})
}

// shortcuts binds the window's keys: tabs, groups, panels, and the
// chords after Cmd+K, which go before the editor's.
func (a *app) shortcuts(c *ui.Context) {
	if a.chord {
		for _, k := range appKeys {
			if k.chord {
				c.OnShortcut(k.mods, k.key, func() {
					a.chord = false
					a.run(k.id)
				})
			}
		}
		return
	}
	c.OnShortcut(ui.Cmd, ui.KeyK, func() { a.chord = true })
	for _, k := range appKeys {
		if !k.chord {
			c.OnShortcut(k.mods, k.key, func() { a.run(k.id) })
		}
	}
}

// syncTitle shows the current file in the window's title.
func (a *app) syncTitle() {
	title := "Lime"
	if d := a.current(); d != nil {
		title = d.name()
		if d.path != "" {
			title = tildePath(d.path)
		}
		if d.dirty() {
			title += " •"
		}
	}
	if title != a.title && a.win != nil {
		a.title = title
		a.win.SetTitle(title)
	}
}

// key is the identity of p as an element's key. MyGo keys a pointer by
// what it points to as printed, which changes as the struct does.
func key(p any) string { return fmt.Sprintf("%p", p) }

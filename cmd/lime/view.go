package main

import (
	"cmp"
	"fmt"
	"math"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

func (a *app) view(c *ui.Context) {
	a.syncThemeSource()
	// The window's theme follows the system here, before SetTheme, which
	// the appearance goes by while it follows the system.
	s := a.set.scheme(c.Theme().Dark)
	// Another color scheme shows at once, as the editor's colors do,
	// rather than fading the colors around them.
	if s != a.scheme {
		c.DisableAnimations()
	}
	a.scheme = s
	base := c.Theme()
	if a.uiThemeCached == nil || a.uiThemeScheme != s || a.uiThemeFont != a.set.UIFont || a.uiThemeBase != base {
		th := s.uiTheme(base)
		th.Font = a.set.UIFont
		a.uiThemeScheme, a.uiThemeFont, a.uiThemeBase, a.uiThemeCached = s, a.set.UIFont, base, th
	}
	c.SetTheme(a.uiThemeCached)
	a.syncFocus(c.Modifiers()&ui.Ctrl != 0)
	panelFocused := a.term.panelFocused(c)
	if panelFocused && !a.termPanelFocused {
		a.focused = place{}
	}
	a.termFocused = a.term.viewFocused(c)
	a.termPanelFocused = panelFocused
	if a.gitDir() != a.git.dir {
		a.readGit()
	}
	a.trackJumps()
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
	if a.chord {
		c.Root().ObserveInput(func(ev ui.InputEvent) {
			if !a.chord {
				return
			}
			switch ev.Kind {
			case ui.InputPointerDown:
				a.chord = false
			case ui.InputKeyDown:
				if ev.Key == ui.KeyUnknown && ev.PhysicalKey == ui.KeyUnknown {
					return
				}
				if !a.isChordKey(ev.Mods, ev.Key) {
					a.chordKeyUp = cmp.Or(ev.Key, ev.PhysicalKey)
				}
			}
		})
	}
	root := ui.Column(c).Fill().Background(ch.bg)
	if files := root.DroppedFiles(); files != nil {
		a.open(files)
	}
	root.Children(func() {
		if a.side != noPanel {
			// The window controls sit over the side bar's top.
			if w := c.TitleBar().Left; w > 0 {
				a.set.SideBarWidth = max(a.set.SideBarWidth, w+controlsGap)
			}
			hide, show := a.dividers(c)
			hide()
			ui.Split(c, &a.set.SideBarWidth, func() { show(); a.sideBar(c) }, func() { show(); a.workspaceView(c) }).Grow(1).FillWidth().MinHeight(0)
		} else {
			a.workspaceView(c)
		}
		a.findInFilesBar(c)
		a.statusBar(c)
		a.draggedTab(c)
	})
	a.scrollBack()
	a.panelView(c)
	a.tabSwitcherView(c)
	a.settingsView(c)
}

const (
	tabBarHeight = 34
	tabMinW      = 92
	tabIcon      = 14
	tabBtnSize   = 24
	tabNavBtnW   = 22
	tabNavGap    = 2
	tabNavMargin = 4
	tabNavWidth  = tabNavMargin + 3*tabNavBtnW + 2*tabNavGap
	newTabSlotW  = 4 + tabBtnSize
	tabFadeW     = 16
	controlsGap  = 12
)

var (
	tabLeftIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10 3.5 5.5 8 10 12.5"/></svg>`))
	tabRightIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M6 3.5 10.5 8 6 12.5"/></svg>`))
	tabListIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 6 8 10.5 12.5 6"/></svg>`))
)

const cardGap, cardRadius = 6, 8

// titleBand is the height of the band along the window's top that the
// window controls sit in, over the side bar and the top groups' tab bars;
// zero where the window has its own title bar, or in full screen.
func titleBand(c *ui.Context) float32 {
	if h := c.TitleBar().Height; h > 0 {
		return max(h, tabBarHeight)
	}
	return 0
}

func themeSource(appearance string) mygo.ThemeSource {
	switch appearance {
	case lightMode:
		return mygo.ThemeLight
	case darkMode:
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}

func (a *app) syncThemeSource() {
	if src := themeSource(a.set.Appearance); src != a.l.themeSource && a.win != nil {
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
	hide, show := a.dividers(c)
	groups := ui.Column(c).Grow(1).FillWidth().MinHeight(0)
	if a.comfortable() {
		groups.Padding(0, cardGap/2, cardGap/2, cardGap/2)
	}
	groups.Children(func() {
		splits(c, a.rows, true, hide, show, func(r int) {
			splits(c, a.cols, false, hide, show, func(col int) { a.groupView(c, r*l.cols+col) })
		})
	})
}

func (a *app) comfortable() bool { return a.set.LayoutStyle == comfortableLayout }

func (a *app) dividers(c *ui.Context) (hide, show func()) {
	if !a.comfortable() {
		return func() {}, func() {}
	}
	th := c.Theme()
	lineless := *th
	lineless.Border = ui.Transparent
	return func() { c.SetTheme(&lineless) }, func() { c.SetTheme(th) }
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

func splits(c *ui.Context, p []float32, vertical bool, hide, show func(), pane func(i int)) {
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
		hide()
		split(c, &p[i], func() { show(); pane(i) }, func() { show(); from(i + 1) }).Fill()
	}
	from(0)
}

func (a *app) groupView(c *ui.Context, gi int) {
	g := a.groups[gi]
	col := ui.Column(c.Key(key(g))).Fill().MinWidth(0).MinHeight(0)
	if a.comfortable() {
		col.Padding(0, cardGap/2, cardGap/2, cardGap/2)
	}
	if files := col.DroppedFiles(); files != nil {
		a.focusGroup(gi)
		a.open(files)
	}
	a.adopt(g)
	col.Children(func() {
		a.tabBar(c, g, gi)
		card := ui.Column(c).Grow(1).FillWidth().MinHeight(0)
		if !c.Vibrancy() {
			card.Background(a.scheme.bg)
		}
		if a.comfortable() {
			card.Radius(cardRadius).Clip()
		}
		card.Children(func() { a.groupContent(c, g, gi) })
	})
}

func (a *app) groupContent(c *ui.Context, g *group, gi int) {
	s := a.scheme
	d := g.current()
	if d == nil {
		d = g.placeholder()
	}
	d.st.Theme, d.st.Bindings = s.editorTheme(), a.l.keys.editor
	d.st.SoftWrap = a.set.WordWrap
	a.applyIndent(d)
	d.st.Font, d.st.LineHeight = a.set.font(), a.set.lineHeight()
	d.st.RenderFindBar = func(c *ui.Context, bar ui.Element, f *editor.FindBar) { a.findBar(c, bar, f, d) }
	if d.cmp != nil {
		a.diffView(c, gi, d)
		return
	}
	if d.commit != nil {
		a.commitTab(c, gi, d)
		return
	}
	if d.results != nil {
		a.resultsView(c, g, d)
		return
	}
	if d.md != nil {
		a.previewTab(c, d)
		return
	}
	if d.kind != textDoc {
		a.preview(c, gi, d)
		return
	}
	a.set.display(d.st)
	if a.set.cursorStyle().draw != nil {
		d.st.Theme = s.caretlessTheme()
	}
	if d == g.blank {
		d.st.LineNumbers = editor.LineNumbersOff
	}
	d.st.OnDefinition = func(off int) { a.gotoDefinition(d, off) }
	d.st.OnCompletion = func(r editor.CompletionRequest) { a.complete(d, r) }
	// An input method would take the second key of a chord without
	// modifiers, as V of Cmd+K V, and only commit its text: GTK's sends
	// no key at all, which would leave the chord waiting.
	d.st.NoInputMethod = a.chord
	d.st.OnInput = func(ev ui.InputEvent) bool {
		// That key is the window's, and its text comes before the frame
		// runs it: the text is not typed.
		if a.chord && ev.Kind == ui.InputText {
			return true
		}
		a.typing(d, ev)
		return false
	}
	d.st.ContextMenu = nil
	if d.linkable() {
		d.st.ContextMenu = func(m *ui.Menu) { a.permalinkMenu(m, d) }
	}
	a.syncBlame(c, d)
	a.withBlame(c, d, func() {
		d.st.NoScrollbars = !a.set.ShowScrollBars
		e := editor.View(c.Key(key(d)), d.st).Bind(&d.editorHandle).Grow(1).FillWidth().MinHeight(0).FocusBind(&a.focused, place{g, d})
		e.DrawOver(func(p *ui.Painter, _ ui.Rect) {
			a.drawRampage(p, d)
			a.drawCarets(p, d)
		})
		// A layout of more groups builds the editor anew, in a split, and
		// FocusBind asks for the focus only as a.focused changes.
		if a.focused == (place{g, d}) {
			e.AutoFocus()
		}
		a.input(e, d, c.Services())
	})
}

func (a *app) tabBar(c *ui.Context, g *group, gi int) {
	a.makeWay(g)
	var band float32
	if gi < a.layout.cols {
		band = titleBand(c)
	}
	row := ui.Row(c).FillWidth().Height(max(band, tabBarHeight)).Shrink(0).AlignItems(ui.Stretch).Background(a.shell(c).bg)
	// The tab shown meets the card under it, whose rounded corner would
	// leave a notch under a tab over it.
	if a.comfortable() {
		row.PaddingStart(cardRadius)
	}
	row.Children(func() {
		// Without the side bar, the window controls sit before the first
		// group's tabs, apart from them and outside the bar that dragged
		// tabs measure.
		if w := c.TitleBar().Left; gi == 0 && w > 0 && a.side == noPanel {
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
			// Decide overflow against the bar width without the nav buttons so
			// showing or hiding them never oscillates.
			avail := g.bar.W - newTabSlotW
			overflow := avail > 0 && float32(len(g.docs))*tabMinW > avail
			dirs := g.tabDirs()
			// Tabs that do not fit scroll, and leave the overflow controls and
			// new tab button in sight. Tabs that fit are not clipped: the row
			// shrinks at once as one closes, and would cut off those still
			// sliding into place.
			tabs := ui.Row(c).Shrink(1).MinWidth(0).AlignItems(ui.Stretch)
			room := tabs.LayoutBounds().W
			if overflow {
				room = max(avail-tabNavWidth, 0)
			}
			over := a.scrollTabs(c, g, room)
			if overflow && over > 0 {
				tabs.ClipX().HandleInput(func(ev ui.InputEvent) bool { return g.wheel(ev, over) })
				maskTabEdges(tabs, g.scroll, over, room)
			}
			tabs.Children(func() {
				// A tab's menu closes tabs as the loop builds them.
				g.docsBuf = append(g.docsBuf[:0], g.docs...)
				for i, d := range g.docsBuf {
					a.tab(c, g, i, d, dirs[d], i == g.active, gi == a.focus)
				}
				clear(g.docsBuf)
				g.docsBuf = g.docsBuf[:0]
			})
			if overflow && over > 0 {
				a.tabOverflowControls(c, g, dirs, room, over)
			}
			a.newTabButton(c, gi)
			// Along the window's top, the room past the tabs is the title bar,
			// which drags the window. Elsewhere a double click there opens a
			// new tab, as in Sublime Text.
			rest := ui.Box(c).Grow(1).Label(locales.TabBar())
			if band > 0 {
				rest.DragWindow()
			} else if rest.DoubleClicked() {
				a.focusGroup(gi)
				a.newFile()
			}
		})
	})
}

func maskTabEdges(tabs ui.Element, scroll, over, room float32) {
	if room <= 0 {
		return
	}
	canLeft, canRight := scroll > 0.5, scroll < over-0.5
	if !canLeft && !canRight {
		return
	}
	fade := min(tabFadeW/room, 0.2)
	var stops [4]ui.GradientStop
	n := 0
	if canLeft {
		stops[n] = ui.GradientStop{Color: ui.Transparent, At: 0}
		stops[n+1] = ui.GradientStop{Color: black, At: fade}
		n += 2
	} else {
		stops[n] = ui.GradientStop{Color: black, At: 0}
		n++
	}
	if canRight {
		stops[n] = ui.GradientStop{Color: black, At: 1 - fade}
		stops[n+1] = ui.GradientStop{Color: ui.Transparent, At: 1}
		n += 2
	} else {
		stops[n] = ui.GradientStop{Color: black, At: 1}
		n++
	}
	tabs.MaskLinearGradient(ui.LinearGradient{Angle: 90, Stops: stops[:n]})
}

func (a *app) tabOverflowControls(c *ui.Context, g *group, dirs map[*doc]string, room, over float32) {
	ch := a.shell(c)
	step := max(room-tabMinW, tabMinW)
	scrollBtn := func(icon *ui.SVG, label string, disabled bool, onClick func()) {
		btn := ui.ButtonBase(c).Width(tabNavBtnW).Height(tabBtnSize).Shrink(0).Radius(4).Center().
			Transition(colorFade).Label(label).Tooltip(label).Disabled(disabled).OnClick(onClick)
		fg := ch.muted
		if disabled {
			fg = ch.muted.Alpha(0.35)
		} else if btn.Hovered() {
			btn.Background(ch.hover)
			fg = ch.text
		}
		btn.Children(func() {
			ui.Icon(c, icon).Size(12, 12).TextColor(fg)
		})
	}
	ui.Row(c).Shrink(0).AlignSelf(ui.Center).AlignItems(ui.Center).MarginStart(tabNavMargin).Gap(tabNavGap).Children(func() {
		scrollBtn(tabLeftIcon, locales.TabScrollLeft(), g.scroll <= 0.5, func() {
			g.scroll = max(g.scroll-step, 0)
		})
		scrollBtn(tabRightIcon, locales.TabScrollRight(), g.scroll >= over-0.5, func() {
			g.scroll = min(g.scroll+step, over)
		})
		listBtn := ui.ButtonBase(c).Width(tabNavBtnW).Height(tabBtnSize).Shrink(0).Radius(4).Center().
			Transition(colorFade).Label(locales.TabList()).Tooltip(locales.TabList())
		fg := ch.muted
		if listBtn.Hovered() {
			listBtn.Background(ch.hover)
			fg = ch.text
		}
		listBtn.Menu(func(m *ui.Menu) {
			m.MaxWidth(320)
			for i, d := range g.docs {
				title := d.name()
				if dir := dirs[d]; dir != "" {
					title += " — " + dir
				}
				if d.dirty() {
					title += " •"
				}
				it := m.Item(title).Checked(i == g.active)
				if d.path != "" {
					it.Tooltip(d.path)
				}
				if it.Chosen() {
					g.shown = nil
					a.show(d)
				}
			}
		})
		listBtn.Children(func() {
			ui.Icon(c, tabListIcon).Size(12, 12).TextColor(fg)
		})
	})
}

func (a *app) tabSwitcherView(c *ui.Context) {
	if len(a.cycle) < 2 {
		return
	}
	s, ch := a.scheme, a.scheme.chrome()
	w, h := c.Size()
	width := min(480, w-32)
	maxH := min(360, max(h-120, 120))
	ui.Overlay(c, func() {
		box := ui.Column(c).Absolute().Top(48).Left((w-width)/2).Width(width).Padding(6).Gap(2).
			Background(ch.panel).Radius(8).Border(1, ch.line).Shadow(0, 8, 28, 0, black.Alpha(0.35)).
			KeepFocus().Label(locales.TabSwitcher())
		if box.PressedOutside() {
			a.commitCycle()
		}
		box.Children(func() {
			ui.Scroll(c).FillWidth().MaxHeight(maxH).Children(func() {
				for i, d := range a.cycle {
					selected := i == a.cycleAt
					row := ui.Row(c.Key(key(d))).FillWidth().Padding(6, 10).Gap(8).Radius(4).
						AlignItems(ui.Center).Label(d.name()).OnClick(func() {
						a.cycleAt = i
						a.commitCycle()
					})
					switch {
					case selected:
						row.Background(ch.panel.Mix(s.accent, 0.28))
						if a.cycleReveal {
							row.ScrollIntoView()
						}
					case row.Hovered():
						row.Background(ch.hover)
					}
					row.Children(func() {
						fg := ch.muted
						if selected {
							fg = ch.text
						}
						a.tabIcon(c, d, fg)
						ui.Text(c, d.name()).FontSize(13).TextColor(ch.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
						if dir := a.tabDirLabel(d); dir != "" {
							ui.Text(c, dir).FontSize(12).TextColor(ch.muted).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
						} else {
							ui.Box(c).Grow(1)
						}
						if d.dirty() {
							ui.Text(c, "●").FontSize(10).TextColor(ch.muted).Shrink(0)
						}
					})
				}
			})
			a.cycleReveal = false
		})
	})
}

func (a *app) tabDirLabel(d *doc) string {
	if d.path == "" {
		return ""
	}
	rel := a.proj.rel(d.path)
	if dir := filepath.Dir(rel); dir != "." {
		return dir
	}
	return ""
}

// scrollTabs scrolls the tabs of g that do not fit in room, the width the
// bar has for them: to show the current tab once it changes or moves, as
// tabs before it close, and along while the tab dragged is held at either
// end (dragScroll). It returns how far they scroll.
func (a *app) scrollTabs(c *ui.Context, g *group, room float32) float32 {
	// Tabs that do not fit have all shrunk to tabMinW.
	over := float32(len(g.docs))*tabMinW - room
	// The first frame has not laid the bar out yet.
	if d := g.current(); (d != g.shown || g.active != g.at) && (room > 0 || d == nil) {
		g.shown, g.at = d, g.active
		x := float32(g.active) * tabMinW
		g.scroll = min(max(g.scroll, x+tabMinW-room), x)
	}
	a.dragScroll(c, g, room, over)
	g.scroll = max(min(g.scroll, over), 0)
	return over
}

// wheel scrolls the tabs of g, over DIPs wider than the room they show in,
// either way a wheel or touchpad turns, as in Sublime Text and Zed.
func (g *group) wheel(ev ui.InputEvent, over float32) bool {
	if ev.Kind != ui.InputScroll {
		return false
	}
	d := ev.DX
	if math.Abs(float64(ev.DY)) > math.Abs(float64(ev.DX)) {
		d = ev.DY
	}
	g.scroll = max(min(g.scroll+d, over), 0)
	return true
}

func (a *app) newTabButton(c *ui.Context, gi int) {
	ch := a.shell(c)
	btn := ui.Box(c).Width(24).Height(24).Shrink(0).AlignSelf(ui.Center).MarginStart(4).Radius(4).Center().
		Transition(ui.ElementTransition{Position: true, Colors: true}).Label(locales.MenuNewFile()).Tooltip(locales.MenuNewFile()).OnClick(func() {
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

var colorFade = ui.ElementTransition{Colors: true}

var tabMotion = ui.ElementTransition{
	Position: true,
	Size:     true,
	Enter:    &ui.Motion{Collapse: true},
	Exit:     &ui.Motion{Collapse: true},
}

func (a *app) tab(c *ui.Context, g *group, i int, d *doc, dir string, active, focused bool) {
	ch := a.shell(c)
	// The tab shown changes colors at once, as the editor does: only
	// hovering fades them.
	motion := tabMotion
	motion.Colors = !active
	row := ui.Row(c.Key(key(d))).Transition(motion).AlignItems(ui.Center).Gap(6).Padding(0, 6, 0, 14).Width(200).Shrink(1).MinWidth(tabMinW)
	if i == 0 {
		g.tabW = row.LayoutBounds().W
	}
	if g.scroll != 0 {
		row.Translate(-g.scroll, 0)
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
	row.Bind(&d.tabHandle).Background(bg).Label(d.name()).Tooltip(d.path).OnClick(func() { a.show(d) }).
		ContextMenu(func(m *ui.Menu) { a.tabMenu(c, m, g, d) }).
		HandleInput(func(ev ui.InputEvent) bool {
			if ev.Button != 2 {
				return false
			}
			switch ev.Kind {
			case ui.InputPointerDown:
				return true
			case ui.InputPointerUp:
				bounds := d.tabHandle.Bounds(c)
				if !ev.Canceled && ev.X >= 0 && ev.Y >= 0 && ev.X < bounds.W && ev.Y < bounds.H {
					a.closeDoc(d)
				}
				return true
			}
			return false
		})
	row.Children(func() {
		a.tabIcon(c, d, fg)
		tabTitle(c, d, dir, fg)
		glyph := "×"
		if d.dirty() && !hovered {
			glyph = "●"
		}
		btn := ui.Box(c).Width(18).Height(18).Radius(4).Center().Transition(colorFade).Label(locales.TabClose(d.name())).OnClick(func() { a.closeDoc(d) })
		if btn.Hovered() {
			btn.Background(ch.muted.Alpha(0.2))
		}
		btn.Children(func() {
			ui.Text(c, glyph).FontSize(12).TextColor(fg).ContentTransition(ui.ContentTransition{})
		})
	})
}

func tabTitle(c *ui.Context, d *doc, dir string, fg ui.Color) {
	name := ui.Text(c, d.name()).FontSize(12).TextColor(fg).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	if dir == "" {
		name.Grow(1)
		return
	}
	ui.Box(c).Basis(0).Grow(1).MinWidth(0).ClipX().Children(func() {
		ui.Text(c, dir).FontSize(12).TextColor(fg.Alpha(0.6)).SingleLine().Ellipsis("…")
	})
}

func (a *app) tabIcon(c *ui.Context, d *doc, fg ui.Color) {
	ic := rowIcon{svg: commitIcon}
	if d.commit == nil {
		ic = a.fileIcon(d.source().path)
	}
	ic.view(c, tabIcon, ic.tint(fg))
}

func (a *app) statusBar(c *ui.Context) {
	ch := a.shell(c)
	d := a.current()
	var left, sel string
	if d != nil && d.img != nil {
		w, h := d.img.Size()
		left = fmt.Sprintf("%d × %d", w, h)
	} else if d != nil && d.kind == textDoc {
		st := d.st
		sels := st.Selections()
		if n := len(sels); n > 1 {
			left = locales.StatusSelections(n)
		} else {
			pos := st.CursorPos()
			line := st.Buffer.Line(pos.Line)
			col := utf8.RuneCountInString(line[:min(pos.Col, len(line))]) + 1
			loc := i18n.Locale()
			if a.posText == "" || a.posLine != pos.Line+1 || a.posCol != col || a.posLocale != loc {
				a.posLine, a.posCol, a.posLocale = pos.Line+1, col, loc
				a.posText = locales.StatusPosition(a.posLine, a.posCol)
			}
			left = a.posText
		}
		sel = d.selectionStatus(st, sels)
	}
	ui.Row(c).FillWidth().Height(24).Shrink(0).Padding(0, 6).Gap(8).AlignItems(ui.Center).Background(ch.bg).Children(func() {
		a.panelButtons(c)
		ui.Text(c, left).FontSize(12).TextColor(ch.muted).SingleLine().Shrink(0)
		if sel != "" {
			ui.Text(c, sel).FontSize(12).TextColor(ch.muted).SingleLine().Shrink(0)
		}
		if a.chord {
			ui.Text(c, shortcutText(ui.Cmd, ui.KeyK)+"…").FontSize(12).TextColor(a.scheme.accent).SingleLine().Shrink(0)
		}
		if a.remote != "" {
			ui.Text(c, a.remote).FontSize(12).TextColor(ch.muted).SingleLine().Shrink(0)
		}
		if d != nil && d.cmp != nil {
			a.diffCounts(c, d.cmp)
		}
		if a.notice.text != "" {
			fg := ch.muted
			if a.notice.err {
				fg = a.scheme.danger()
			}
			ui.Row(c).Shrink(1).MinWidth(0).Gap(5).AlignItems(ui.Center).Children(func() {
				if a.notice.err {
					ui.Icon(c, warningIcon).Size(12, 12).TextColor(fg).Shrink(0)
				}
				ui.Text(c, a.notice.text).FontSize(12).TextColor(fg).SingleLine().Ellipsis("…")
			})
		}
		ui.Box(c).Grow(1)
		switch {
		case d == nil:
		case d.cmp != nil:
			a.diffLayoutButton(c, d.cmp)
		case d.kind == textDoc:
			a.encodingButton(c, d)
			a.lineEndingButton(c, d)
			a.indentButton(c, d)
			a.syntaxButton(c, d)
		}
	})
}

const noticeTime = 4 * time.Second

type notice struct {
	text string
	err  bool
	n    int // counts the notices, so that only the last one's timer clears it
}

func (a *app) notify(text string) {
	a.notice.n++
	a.notice.text, a.notice.err = text, false
	n := a.notice.n
	// A window that never opened, as in tests, has no frames to update.
	if a.win == nil {
		return
	}
	time.AfterFunc(noticeTime, func() {
		a.win.Update(func() {
			if a.notice.n == n {
				a.notice.text = ""
			}
		})
	})
}

func (a *app) notifyError(text string) {
	a.notify(text)
	a.notice.err = true
}

func (a *app) syntaxButton(c *ui.Context, d *doc) {
	lang := d.st.Buffer.Language()
	a.statusMenu(c, syntaxName(lang), locales.StatusSyntax(syntaxName(lang)), func(m *ui.Menu) {
		for i, l := range syntaxes() {
			if m.Item(syntaxName(l)).Checked(l == lang).Chosen() {
				d.st.Buffer.SetLanguage(l)
			}
			if i == 0 {
				m.Separator()
			}
		}
	})
}

func (a *app) statusMenu(c *ui.Context, text, label string, menu func(m *ui.Menu)) ui.Element {
	ch := a.shell(c)
	btn := ui.ButtonBase(c).Height(20).Padding(0, 8).Radius(4).Transition(colorFade).Label(label)
	if btn.Hovered() {
		btn.Background(ch.hover)
	}
	btn.Menu(menu)
	btn.Children(func() {
		ui.Text(c, text).FontSize(12).TextColor(ch.muted).SingleLine().Ellipsis("…").MinWidth(0)
	})
	return btn
}

func formatSelectionStatus(chars, lines int) string {
	if chars == 0 {
		return ""
	}
	if lines > 1 {
		return locales.StatusSelectedLines(lines, chars)
	}
	return locales.StatusSelectedChars(chars)
}

func selectionCounts(st *editor.State, sels []editor.Selection) (chars, lines int) {
	anyNonEmpty := false
	for _, s := range sels {
		if s.Anchor != s.Head {
			anyNonEmpty = true
			break
		}
	}
	if !anyNonEmpty {
		return 0, 0
	}
	snap := st.Buffer.Snapshot()
	lastLine := -1
	for _, s := range sels {
		a, z := min(s.Anchor, s.Head), max(s.Anchor, s.Head)
		if a == z {
			continue
		}
		snap.Chunks(a, z, func(chunk string) bool {
			chars += utf8.RuneCountInString(chunk)
			return true
		})
		from, to := snap.LineOf(a), snap.LineOf(z)
		if to > from && z == snap.LineStart(to) {
			to--
		}
		if from <= lastLine {
			from = lastLine + 1
		}
		if from <= to {
			lines += to - from + 1
			lastLine = to
		}
	}
	return chars, lines
}

func (d *doc) selectionStatus(st *editor.State, sels []editor.Selection) string {
	if len(sels) == 1 {
		s := sels[0]
		if s.Anchor == s.Head {
			d.selStatValid = false
			return ""
		}
		ver := st.Buffer.Version()
		loc := i18n.Locale()
		if !d.selStatValid || d.selStatVer != ver || d.selStatAnchor != s.Anchor || d.selStatHead != s.Head {
			d.selStatVer, d.selStatAnchor, d.selStatHead, d.selStatValid = ver, s.Anchor, s.Head, true
			d.selStatChars, d.selStatLines = selectionCounts(st, sels)
			d.selStatLocale = loc
			d.selStatText = formatSelectionStatus(d.selStatChars, d.selStatLines)
		} else if d.selStatLocale != loc {
			d.selStatLocale = loc
			d.selStatText = formatSelectionStatus(d.selStatChars, d.selStatLines)
		}
		return d.selStatText
	}
	d.selStatValid = false
	return selectionStatus(st, sels)
}

func selectionStatus(st *editor.State, sels []editor.Selection) string {
	chars, lines := selectionCounts(st, sels)
	return formatSelectionStatus(chars, lines)
}

func (a *app) isChordKey(mods ui.Modifiers, key ui.Key) bool {
	for _, k := range a.l.keys.window {
		if k.chord && k.mods == mods && k.key == key {
			return true
		}
	}
	return false
}

func (a *app) ensureBoundShortcuts() {
	if a.boundKM == a.l.keys {
		return
	}
	a.boundKM = a.l.keys
	a.boundKeys = a.boundKeys[:0]
	a.boundChordKeys = a.boundChordKeys[:0]
	for _, k := range a.l.keys.window {
		id := k.id
		if k.chord {
			a.boundChordKeys = append(a.boundChordKeys, boundShortcut{
				id:   id,
				mods: k.mods,
				key:  k.key,
				fn: func() {
					a.chord = false
					a.run(id)
				},
			})
		} else {
			a.boundKeys = append(a.boundKeys, boundShortcut{
				id:   id,
				mods: k.mods,
				key:  k.key,
				fn:   func() { a.run(id) },
			})
		}
	}
}

// shortcuts binds the window's keys: tabs, groups, panels, and the
// chords after Cmd+K, which go before the editor's.
func (a *app) shortcuts(c *ui.Context) {
	a.ensureBoundShortcuts()
	if a.chordKeyUp != ui.KeyUnknown {
		a.chord = false
		// Focus changes can rebuild and reroute the same press before key-up.
		if c.KeyDown(a.chordKeyUp) {
			return
		}
		a.chordKeyUp = ui.KeyUnknown
	}
	if a.chord {
		if !c.Root().FocusWithin() {
			mods := c.Modifiers()
			for _, k := range c.KeysDown() {
				if k != ui.KeyK && !a.isChordKey(mods, k) && !(mods == 0 && k == ui.KeyEscape) {
					a.chord, a.chordKeyUp = false, k
					c.Invalidate()
					return
				}
			}
		}
		for _, k := range a.boundChordKeys {
			c.OnShortcut(k.mods, k.key, k.fn)
		}
		c.OnShortcut(0, ui.KeyEscape, func() { a.chord = false })
		c.OnShortcut(0, ui.KeyK, func() { a.chord = false })
		return
	}
	if len(a.cycle) > 1 {
		c.OnShortcut(ui.Ctrl, ui.KeyDown, func() { a.cycleTabs(1) })
		c.OnShortcut(ui.Ctrl, ui.KeyUp, func() { a.cycleTabs(-1) })
		c.OnShortcut(0, ui.KeyDown, func() { a.cycleTabs(1) })
		c.OnShortcut(0, ui.KeyUp, func() { a.cycleTabs(-1) })
		c.OnShortcut(ui.Ctrl, ui.KeyEscape, a.cancelCycle)
		c.OnShortcut(0, ui.KeyEscape, a.cancelCycle)
		c.OnShortcut(ui.Ctrl, ui.KeyEnter, a.commitCycle)
		c.OnShortcut(0, ui.KeyEnter, a.commitCycle)
	}
	if !a.termFocused {
		c.OnShortcut(ui.Cmd, ui.KeyK, func() { a.chord = true })
		// A mouse's side buttons, as in Zed: Sublime Text's switch tabs.
		c.OnShortcut(0, ui.KeyBack, func() { a.run("jump_back") })
		c.OnShortcut(0, ui.KeyForward, func() { a.run("jump_forward") })
	}
	for _, k := range a.boundKeys {
		if a.termFocused && !terminalAllowsShortcut(k.id, k.mods, k.key) {
			continue
		}
		c.OnShortcut(k.mods, k.key, k.fn)
	}
}

func terminalAllowsShortcut(id string, mods ui.Modifiers, key ui.Key) bool {
	if isTerminalCommand(id) {
		return true
	}
	if mac {
		if mods&ui.Super != 0 {
			return true
		}
		return id == "toggle_git_panel" && mods&ui.Shift != 0
	}
	if mods == ui.Ctrl && key >= ui.KeyA && key <= ui.KeyZ {
		return false
	}
	switch id {
	case "show_command_palette", "toggle_files_panel", "toggle_git_panel",
		"show_panel find_in_files", "show_settings", "new_window", "close_window",
		"increase_font_size", "decrease_font_size", "reset_font_size",
		"next_view", "prev_view", "close":
		return true
	}
	return false
}

func (a *app) syncTitle() {
	if a.win == nil {
		return
	}
	d := a.current()
	if d == nil {
		if a.title != "Lime" {
			a.titleDoc, a.title = nil, "Lime"
			a.win.SetTitle("Lime")
		}
		return
	}
	dirty := d.dirty()
	if d == a.titleDoc && d.path == a.titlePath && dirty == a.titleDirty && (d.path != "" || d.name() == a.titleName) {
		return
	}
	name := d.name()
	a.titleDoc, a.titlePath, a.titleName, a.titleDirty = d, d.path, name, dirty
	title := name
	if d.path != "" {
		title = tildePath(d.path)
	}
	if dirty {
		title += " •"
	}
	if title != a.title {
		a.title = title
		a.win.SetTitle(title)
	}
}

// key is the identity of p as an element's key. MyGo keys a pointer by
// what it points to as printed, which changes as the struct does.
func key(p any) any {
	switch v := p.(type) {
	case *doc:
		if v.keyAny == nil {
			v.keyAny = docElemKey(nextElemID.Add(1))
		}
		return v.keyAny
	case *group:
		if v.keyAny == nil {
			v.keyAny = groupElemKey(nextElemID.Add(1))
		}
		return v.keyAny
	case *termTab:
		if v.keyAny == nil {
			v.keyAny = termTabElemKey(nextElemID.Add(1))
		}
		return v.keyAny
	default:
		return fmt.Sprintf("%p", p)
	}
}

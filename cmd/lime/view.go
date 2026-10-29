package main

import (
	"fmt"
	"math"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
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
	th := a.scheme.uiTheme(c.Theme())
	th.Font = a.set.UIFont
	c.SetTheme(th)
	a.syncFocus(c.Modifiers()&ui.Ctrl != 0)
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
			ui.Split(c, &a.set.SideBarWidth, func() { a.sideBar(c) }, func() { a.groupsView(c) }).Grow(1).FillWidth().MinHeight(0)
		} else {
			a.groupsView(c)
		}
		a.findInFilesBar(c)
		a.statusBar(c)
		a.draggedTab(c)
	})
	a.scrollBack()
	a.panelView(c)
	a.settingsView(c)
}

const tabBarHeight = 34

// tabMinW is the width tabs shrink to before they no longer fit.
const tabMinW = 92

const tabIcon = 14

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

// themeSource is the appearance of the window's title bar for an
// appearance of the settings.
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
		d.st.Theme, d.st.Bindings = s.editorTheme(), a.l.keys.editor
		d.st.SoftWrap = a.set.WordWrap
		a.applyIndent(d)
		d.st.Font.Family, d.st.Font.Size = a.set.ContentFont, a.set.FontSize
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
			// Tabs that do not fit scroll, and leave the new tab button in
			// sight. Tabs that fit are not clipped: the row shrinks at once as
			// one closes, and would cut off those still sliding into place.
			tabs := ui.Row(c).Shrink(1).MinWidth(0).AlignItems(ui.Stretch)
			if over := a.scrollTabs(c, g, tabs.LayoutBounds().W); over > 0 {
				tabs.ClipX().HandleInput(func(ev ui.InputEvent) bool { return g.wheel(ev, over) })
			}
			tabs.Children(func() {
				// A tab's menu closes tabs as the loop builds them.
				for i, d := range slices.Clone(g.docs) {
					a.tab(c, g, i, d, i == g.active, gi == a.focus)
				}
			})
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
	row.Background(bg).Label(d.name()).OnClick(func() { a.show(d) }).ContextMenu(func(m *ui.Menu) { a.tabMenu(m, g, d) })
	row.Children(func() {
		a.tabIcon(c, d, fg)
		ui.Text(c, d.name()).FontSize(12).TextColor(fg).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
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

// tabIcon is the icon of d's file, as the side bar shows it, or of its
// commit, tinted fg where it has no colour.
func (a *app) tabIcon(c *ui.Context, d *doc, fg ui.Color) {
	ic := rowIcon{svg: commitIcon}
	if d.commit == nil {
		ic = fileIcon(d.source().path, a.scheme.dark)
	}
	ic.view(c, tabIcon, ic.tint(fg))
}

func (a *app) statusBar(c *ui.Context) {
	ch := a.shell(c)
	d := a.current()
	var left string
	if d != nil && d.img != nil {
		w, h := d.img.Size()
		left = fmt.Sprintf("%d × %d", w, h)
	} else if d != nil && d.kind == textDoc {
		st := d.st
		if n := len(st.Selections()); n > 1 {
			left = locales.StatusSelections(n)
		} else {
			pos := st.CursorPos()
			line := st.Buffer.Line(pos.Line)
			col := utf8.RuneCountInString(line[:min(pos.Col, len(line))]) + 1
			left = locales.StatusPosition(pos.Line+1, col)
		}
	}
	ui.Row(c).FillWidth().Height(24).Shrink(0).Padding(0, 6).Gap(8).AlignItems(ui.Center).Background(ch.bg).Children(func() {
		a.panelButtons(c)
		ui.Text(c, left).FontSize(12).TextColor(ch.muted).SingleLine().Shrink(0)
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

// noticeTime is how long the status bar shows a notice.
const noticeTime = 4 * time.Second

// notice is a message of the status bar, as why a command failed.
type notice struct {
	text string
	err  bool
	n    int // counts the notices, so that only the last one's timer clears it
}

// notify shows text in the status bar for noticeTime, as Sublime Text
// shows its status messages.
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

// notifyError shows text as notify does, as what went wrong.
func (a *app) notifyError(text string) {
	a.notify(text)
	a.notice.err = true
}

// syntaxButton shows the syntax of d and changes it from a menu, as
// Sublime Text's status bar does.
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

// statusMenu is a button of the status bar showing text, which opens
// menu. Its text ends in an ellipsis where it is narrower than the text.
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

// shortcuts binds the window's keys: tabs, groups, panels, and the
// chords after Cmd+K, which go before the editor's.
func (a *app) shortcuts(c *ui.Context) {
	keys := a.l.keys.window
	if a.chord {
		for _, k := range keys {
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
	// A mouse's side buttons, as in Zed: Sublime Text's switch tabs.
	c.OnShortcut(0, ui.KeyBack, func() { a.run("jump_back") })
	c.OnShortcut(0, ui.KeyForward, func() { a.run("jump_forward") })
	for _, k := range keys {
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

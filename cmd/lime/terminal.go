package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"lime/locales"
)

const (
	defaultTerminalSplit = 68
	termHeaderHeight     = 30
	termTabMinW          = 84
	termTabMaxW          = 180
)

type termSearchStatus struct {
	Text      string
	Selected  int
	Total     int
	Searching bool
}

type termSessionOpts struct {
	dir      string
	scheme   *scheme
	set      *settings
	onTitle  func(title string)
	onExit   func(code int)
	onSearch func(s termSearchStatus)
	openLink func(link string) bool
}

type termSession interface {
	view(c *ui.Context) ui.Element
	syncAppearance(s *scheme, set *settings)
	dir() string
	clearScreen() bool
	search(query string)
	searchNext()
	searchPrev()
	endSearch()
	close() error
}

type termTab struct {
	keyAny    any
	handle    ui.Handle
	tabHandle ui.Handle
	sess      termSession
	title     string
	initDir   string
	closed    bool
}

func (t *termTab) name() string {
	if s := strings.TrimSpace(t.title); s != "" {
		return s
	}
	if t.initDir != "" {
		if base := filepath.Base(t.initDir); base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return locales.TerminalDefaultTitle()
}

type terminalDock struct {
	open        bool
	zoomed      bool
	tabs        []*termTab
	active      int
	wantFocus   bool
	panelHandle ui.Handle

	findOpen   bool
	findQuery  string
	findFocus  bool
	findStatus termSearchStatus
	findHandle ui.Handle

	newSession func(termSessionOpts) (termSession, error)
}

var (
	terminalIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<g fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">
		<rect x="1.75" y="2.75" width="12.5" height="10.5" rx="2" />
		<path d="M4.75 6.25 7.25 8.25 4.75 10.25M8.75 10.25h2.5" />
	</g>
</svg>`))
	termZoomIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 10 8 5.5 12.5 10"/></svg>`))
	termRestoreIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 6 8 10.5 12.5 6"/></svg>`))
)

func realTerminal(caption func(locales.Messages) string) func(locales.Messages) string {
	if !terminalSupported {
		return nil
	}
	return caption
}

func isTerminalCommand(id string) bool {
	switch id {
	case "toggle_terminal", "toggle_bottom_dock", "new_terminal", "close_terminal",
		"clear_terminal", "toggle_terminal_zoom", "next_terminal", "prev_terminal",
		"find_in_terminal":
		return true
	}
	return false
}

func (d *terminalDock) current() *termTab {
	if d.active < 0 || d.active >= len(d.tabs) {
		return nil
	}
	return d.tabs[d.active]
}

func (d *terminalDock) viewFocused(c *ui.Context) bool {
	if !d.open {
		return false
	}
	t := d.current()
	return t != nil && t.handle.Focused(c)
}

func (d *terminalDock) panelFocused(c *ui.Context) bool {
	return d.open && d.panelHandle.FocusWithin(c)
}

func (a *app) terminalDir() string {
	if d := a.current(); d != nil && d.path != "" {
		for _, f := range a.proj.folders {
			if within(d.path, f) {
				return f
			}
		}
		return filepath.Dir(d.path)
	}
	if len(a.proj.folders) > 0 {
		return a.proj.folders[0]
	}
	return ""
}

func (a *app) toggleTerminal() {
	if !terminalSupported && a.term.newSession == nil {
		return
	}
	if !a.term.open {
		a.showTerminal()
		return
	}
	if !a.termPanelFocused {
		a.focusTerminal()
		return
	}
	a.hideTerminal()
}

func (a *app) toggleBottomDock() {
	if !terminalSupported && a.term.newSession == nil {
		return
	}
	if a.term.open {
		a.hideTerminal()
		return
	}
	a.showTerminal()
}

func (a *app) showTerminal() {
	if !terminalSupported && a.term.newSession == nil {
		return
	}
	if len(a.term.tabs) == 0 {
		if a.openTerminalInDir(a.terminalDir()) == nil {
			return
		}
		return
	}
	a.term.open = true
	a.focusTerminal()
}

func (a *app) hideTerminal() {
	a.endTerminalFind(false)
	a.term.open = false
	a.term.wantFocus = false
	a.focusGroup(a.focus)
}

func (a *app) focusTerminal() {
	if !a.term.open || len(a.term.tabs) == 0 {
		return
	}
	a.focused = place{}
	a.term.wantFocus = true
}

func (a *app) newTerminal() {
	a.openTerminalInDir(a.terminalDir())
}

func (a *app) openTerminalInDir(dir string) *termTab {
	if !terminalSupported && a.term.newSession == nil {
		return nil
	}
	makeSess := a.term.newSession
	if makeSess == nil {
		makeSess = newNativeTermSession
	}
	tab := &termTab{
		keyAny:  termTabElemKey(nextElemID.Add(1)),
		initDir: dir,
	}
	win := a.win
	sess, err := makeSess(termSessionOpts{
		dir:    dir,
		scheme: a.scheme,
		set:    a.set,
		onTitle: func(title string) {
			if win == nil {
				tab.title = title
				return
			}
			win.Update(func() {
				if !tab.closed {
					tab.title = title
				}
			})
		},
		onExit: func(int) {
			if win == nil {
				return
			}
			win.Update(func() {
				a.closeTerminalTab(tab)
			})
		},
		onSearch: func(s termSearchStatus) {
			if a.term.current() == tab {
				a.term.findStatus = s
			}
		},
		openLink: func(link string) bool {
			return a.openTerminalLink(tab, link)
		},
	})
	if err != nil {
		a.notifyError(locales.TerminalStartFailed(err.Error()))
		return nil
	}
	tab.sess = sess
	a.term.tabs = append(a.term.tabs, tab)
	a.term.active = len(a.term.tabs) - 1
	a.term.open = true
	a.endTerminalFind(false)
	a.focusTerminal()
	return tab
}

func (a *app) selectTerminal(i int) {
	n := len(a.term.tabs)
	if n == 0 {
		return
	}
	i = ((i % n) + n) % n
	if i != a.term.active {
		a.endTerminalFind(false)
		a.term.active = i
	}
	a.focusTerminal()
}

func (a *app) stepTerminal(delta int) {
	if len(a.term.tabs) < 2 {
		return
	}
	a.selectTerminal(a.term.active + delta)
}

func (a *app) closeTerminal() {
	if t := a.term.current(); t != nil {
		a.closeTerminalTab(t)
	}
}

func (a *app) closeTerminalTab(tab *termTab) {
	i := slices.Index(a.term.tabs, tab)
	if i < 0 {
		return
	}
	if tab == a.term.current() {
		a.endTerminalFind(false)
	}
	tab.closed = true
	if tab.sess != nil {
		_ = tab.sess.close()
	}
	a.term.tabs = slices.Delete(a.term.tabs, i, i+1)
	if len(a.term.tabs) == 0 {
		a.term.active = -1
		a.term.open = false
		a.term.zoomed = false
		a.term.wantFocus = false
		a.focusGroup(a.focus)
		return
	}
	if i < a.term.active || a.term.active >= len(a.term.tabs) {
		a.term.active = max(a.term.active-1, 0)
	}
	if a.term.open {
		a.focusTerminal()
	}
}

func (a *app) clearTerminal() {
	if t := a.term.current(); t != nil && t.sess != nil {
		t.sess.clearScreen()
	}
}

func (a *app) toggleTerminalZoom() {
	if !a.term.open {
		if len(a.term.tabs) == 0 {
			return
		}
		a.showTerminal()
	}
	a.term.zoomed = !a.term.zoomed
	a.focusTerminal()
}

func (a *app) closeAllTerminals() {
	for _, tab := range a.term.tabs {
		tab.closed = true
		if tab.sess != nil {
			_ = tab.sess.close()
		}
	}
	a.term.tabs = nil
	a.term.active = -1
	a.term.open = false
	a.term.zoomed = false
	a.term.findOpen = false
	a.term.wantFocus = false
}

func (a *app) showTerminalFind() {
	if !a.term.open || a.term.current() == nil {
		return
	}
	a.term.findOpen = true
	a.term.findFocus = true
	a.focused = place{}
	if a.term.findQuery != "" {
		a.term.current().sess.search(a.term.findQuery)
	}
}

func (a *app) endTerminalFind(refocus bool) {
	if !a.term.findOpen {
		return
	}
	a.term.findOpen = false
	a.term.findFocus = false
	a.term.findStatus = termSearchStatus{}
	if t := a.term.current(); t != nil && t.sess != nil {
		t.sess.endSearch()
	}
	if refocus {
		a.focusTerminal()
	}
}

func (a *app) terminalFindNext() {
	if t := a.term.current(); t != nil && t.sess != nil && a.term.findQuery != "" {
		t.sess.searchNext()
	}
}

func (a *app) terminalFindPrev() {
	if t := a.term.current(); t != nil && t.sess != nil && a.term.findQuery != "" {
		t.sess.searchPrev()
	}
}

func (a *app) openTerminalLink(tab *termTab, link string) bool {
	if link == "" {
		return false
	}
	lower := strings.ToLower(link)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") {
		return false
	}
	baseDir := ""
	if tab != nil {
		if tab.sess != nil {
			baseDir = tab.sess.dir()
		}
		if baseDir == "" {
			baseDir = tab.initDir
		}
	}
	if baseDir == "" && len(a.proj.folders) > 0 {
		baseDir = a.proj.folders[0]
	}
	resolved, line, col, ok := resolveTerminalLink(link, baseDir)
	if !ok {
		return false
	}
	a.open([]string{resolved})
	if line > 0 {
		if d := a.docAt(resolved); d != nil {
			goToPos(d, line, col)
		}
	}
	return true
}

func resolveTerminalLink(link, baseDir string) (path string, line, col int, ok bool) {
	raw := strings.TrimSpace(link)
	if raw == "" {
		return "", 0, 0, false
	}
	if strings.HasPrefix(raw, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			raw = filepath.Join(home, raw[2:])
		}
	}
	arg := parseOpenArg(raw)
	p := arg.path
	if !filepath.IsAbs(p) && baseDir != "" {
		p = filepath.Join(baseDir, p)
	}
	p, err := filepath.Abs(p)
	if err != nil {
		return "", 0, 0, false
	}
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return "", 0, 0, false
	}
	return p, arg.line, arg.col, true
}

func (a *app) workspaceView(c *ui.Context) {
	if !a.term.open {
		a.groupsView(c)
		return
	}
	if a.term.zoomed {
		a.terminalPanel(c)
		return
	}
	if a.set.TerminalSplit < 20 || a.set.TerminalSplit > 85 {
		a.set.TerminalSplit = defaultTerminalSplit
	}
	hide, show := a.dividers(c)
	hide()
	ui.SplitVerticalPercent(c, &a.set.TerminalSplit, func() {
		show()
		a.groupsView(c)
	}, func() {
		show()
		a.terminalPanel(c)
	}).Grow(1).FillWidth().MinHeight(0)
}

func (a *app) terminalPanel(c *ui.Context) {
	ch := a.shell(c)
	panel := ui.Column(c.Key("terminal_panel")).Bind(&a.term.panelHandle).Grow(1).FillWidth().MinHeight(0)
	if a.comfortable() {
		panel.Padding(cardGap/2, cardGap/2, cardGap/2, cardGap/2)
	}
	panel.Children(func() {
		card := ui.Column(c).Grow(1).FillWidth().MinHeight(0).Background(a.scheme.bg)
		if a.comfortable() {
			card.Radius(cardRadius).Clip()
		}
		card.Children(func() {
			a.terminalHeader(c, ch)
			if a.term.findOpen {
				a.terminalFindBar(c)
			}
			a.terminalBody(c)
		})
	})
}

func (a *app) terminalHeader(c *ui.Context, ch chrome) {
	bar := ui.Row(c).FillWidth().Height(termHeaderHeight).Shrink(0).Padding(0, 6).Gap(4).
		AlignItems(ui.Center).Background(ch.bg).KeepFocus()
	bar.Children(func() {
		tabs := ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Gap(2)
		tabs.Children(func() {
			for i, tab := range slices.Clone(a.term.tabs) {
				a.terminalTabItem(c, i, tab, i == a.term.active)
			}
			tip := locales.TerminalNew()
			if k := a.l.keys.text("new_terminal"); k != "" {
				tip += "  " + k
			}
			add := ui.ButtonBase(c).Width(22).Height(22).Shrink(0).AlignSelf(ui.Center).Radius(4).Center().
				Transition(colorFade).Label(locales.TerminalNew()).Tooltip(tip).OnClick(a.newTerminal)
			if add.Hovered() {
				add.Background(ch.hover)
			}
			add.Children(func() {
				ui.Text(c, "+").FontSize(15).TextColor(ch.muted)
			})
		})
		a.terminalHeaderButtons(c, ch)
	})
	if !a.comfortable() {
		ui.Box(c).FillWidth().Height(1).Shrink(0).Background(ch.line)
	}
}

func (a *app) terminalTabItem(c *ui.Context, i int, tab *termTab, active bool) {
	ch := a.shell(c)
	bg, fg := ch.bg, ch.muted
	row := ui.Row(c.Key(key(tab))).Bind(&tab.tabHandle).AlignItems(ui.Center).Gap(6).Padding(0, 6, 0, 8).
		MinWidth(termTabMinW).MaxWidth(termTabMaxW).Shrink(1).Height(24).AlignSelf(ui.Center).Radius(5).
		Transition(colorFade).Label(tab.name()).OnClick(func() { a.selectTerminal(i) }).
		HandleInput(func(ev ui.InputEvent) bool {
			if ev.Button != 2 {
				return false
			}
			switch ev.Kind {
			case ui.InputPointerDown:
				return true
			case ui.InputPointerUp:
				bounds := tab.tabHandle.Bounds(c)
				if !ev.Canceled && ev.X >= 0 && ev.Y >= 0 && ev.X < bounds.W && ev.Y < bounds.H {
					a.closeTerminalTab(tab)
				}
				return true
			}
			return false
		})
	hovered := row.Hovered()
	switch {
	case active:
		bg, fg = ch.tab, ch.text
	case hovered:
		bg = ch.hover
	}
	row.Background(bg)
	row.Children(func() {
		ui.Icon(c, terminalIcon).Size(12, 12).TextColor(fg).Shrink(0)
		ui.Text(c, tab.name()).FontSize(12).TextColor(fg).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
		btn := ui.Box(c).Width(16).Height(16).Radius(4).Center().Transition(colorFade).
			Label(locales.TabClose(tab.name())).OnClick(func() { a.closeTerminalTab(tab) })
		if btn.Hovered() {
			btn.Background(ch.muted.Alpha(0.2))
		}
		btn.Children(func() {
			ui.Text(c, "×").FontSize(12).TextColor(fg)
		})
	})
}

func (a *app) terminalHeaderButtons(c *ui.Context, ch chrome) {
	zoomIcon := termZoomIcon
	zoomLabel := locales.TerminalMaximize()
	if a.term.zoomed {
		zoomIcon = termRestoreIcon
		zoomLabel = locales.TerminalRestore()
	}
	zoomTip := zoomLabel
	if k := a.l.keys.text("toggle_terminal_zoom"); k != "" {
		zoomTip += "  " + k
	}
	closeTip := locales.TerminalHide()
	if k := a.l.keys.text("toggle_terminal"); k != "" {
		closeTip += "  " + k
	}
	btn := func(icon *ui.SVG, label, tip string, click func()) {
		b := ui.ButtonBase(c.Key(label)).Width(22).Height(22).Shrink(0).Radius(4).Center().
			Transition(colorFade).Label(label).Tooltip(tip).OnClick(click)
		fg := ch.muted
		if b.Hovered() {
			b.Background(ch.hover)
			fg = ch.text
		}
		b.Children(func() {
			ui.Icon(c, icon).Size(12, 12).TextColor(fg)
		})
	}
	ui.Row(c).Shrink(0).Gap(2).AlignItems(ui.Center).Children(func() {
		btn(zoomIcon, zoomLabel, zoomTip, a.toggleTerminalZoom)
		btn(closeIcon, locales.TerminalHide(), closeTip, a.hideTerminal)
	})
}

func (a *app) terminalFindBar(c *ui.Context) {
	ch := a.scheme.chrome()
	t := a.term.current()
	ui.Row(c.Key("terminal_find")).FillWidth().Height(32).Shrink(0).Padding(0, 8).Gap(6).
		AlignItems(ui.Center).Background(ch.panel).Children(func() {
		in := ui.TextInputBase(c, &a.term.findQuery).Bind(&a.term.findHandle).Width(220).Height(22).
			Padding(0, 6).Radius(4).Background(a.scheme.bg).Border(1, ch.line).TextColor(ch.text).
			FontSize(12).Placeholder(locales.Find()).Label(locales.Find()).
			OnSubmit(a.terminalFindNext).
			OnShortcut(ui.Shift, ui.KeyEnter, a.terminalFindPrev).
			OnShortcut(0, ui.KeyEscape, func() { a.endTerminalFind(true) })
		if a.term.findFocus {
			a.term.findFocus = false
			a.term.findHandle.Focus(c)
			a.term.findHandle.SelectAll(c)
		}
		if in.Changed() && t != nil && t.sess != nil {
			if a.term.findQuery == "" {
				a.term.findStatus = termSearchStatus{}
				t.sess.endSearch()
			} else {
				t.sess.search(a.term.findQuery)
			}
		}
		if a.term.findQuery != "" {
			status := "0/0"
			if a.term.findStatus.Total > 0 {
				status = fmt.Sprintf("%d/%d", a.term.findStatus.Selected, a.term.findStatus.Total)
			}
			ui.Text(c, status).FontSize(11).TextColor(ch.muted).SingleLine().Shrink(0)
		}
		a.smallButton(c, locales.FindPrevious(), false, a.terminalFindPrev)
		a.smallButton(c, locales.Find(), false, a.terminalFindNext)
		ui.Box(c).Grow(1)
		closeBtn := ui.ButtonBase(c).Width(20).Height(20).Radius(4).Center().Transition(colorFade).
			Label(locales.SettingsClose()).OnClick(func() { a.endTerminalFind(true) })
		if closeBtn.Hovered() {
			closeBtn.Background(ch.hover)
		}
		closeBtn.Children(func() {
			ui.Icon(c, closeIcon).Size(10, 10).TextColor(ch.muted)
		})
	})
	ui.Box(c).FillWidth().Height(1).Shrink(0).Background(ch.line)
}

func (a *app) terminalBody(c *ui.Context) {
	tab := a.term.current()
	if tab == nil || tab.sess == nil {
		return
	}
	tab.sess.syncAppearance(a.scheme, a.set)
	el := tab.sess.view(c.Key(key(tab))).Bind(&tab.handle).Grow(1).FillWidth().MinHeight(0)
	if a.term.wantFocus {
		a.term.wantFocus = false
		tab.handle.Focus(c)
	}
	_ = el
}

func (a *app) terminalPanelButton(c *ui.Context) {
	if !terminalSupported && a.term.newSession == nil {
		return
	}
	ch := a.shell(c)
	on := a.term.open
	tint := ch.muted
	if on {
		tint = ch.text
	}
	tip := locales.TerminalPanel()
	if k := a.l.keys.text("toggle_terminal"); k != "" {
		tip += "  " + k
	}
	btn := ui.ToggleBase(c.Key("toggle_terminal"), &on).Width(22).Height(20).Radius(4).Center().Transition(colorFade).
		Label(locales.TerminalPanel()).Tooltip(tip).OnClick(func() { a.run("toggle_terminal") })
	if btn.Hovered() {
		btn.Background(ch.hover)
	}
	btn.Children(func() {
		ui.Icon(c, terminalIcon).Size(treeIcon, treeIcon).TextColor(tint)
	})
}

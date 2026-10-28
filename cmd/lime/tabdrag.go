package main

import (
	"runtime"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// tabDrag is a tab the pointer drags, as in Sublime Text: along its tab
// bar, the other tabs making way, or pulled out of it, to another group or
// window, or let go elsewhere, to a window of its own. A window's only tab
// moves the window, and goes into the tab bar of another it is let go on.
type tabDrag struct {
	d     *doc
	g     *group
	grab  ui.Point // where the pointer took the tab, from its top left
	start ui.Point // where the press was, in the window
	at    ui.Point // the pointer, in the window
	on    bool     // the pointer moved far enough to drag
	out   bool     // pulled out of the tab bar
	off   mygo.Point
}

// dragTab follows the pointer pressing row, the tab of d at i of g, and
// reports whether it drags it.
func (a *app) dragTab(row ui.Element, g *group, i int, d *doc) bool {
	t := a.drag
	if t != nil && t.d != d {
		return false
	}
	if _, _, held := row.Dragged(); !held {
		if t != nil {
			a.drag = nil
			if t.on && a.win != nil {
				// After the frame, which goes through the tabs.
				a.win.Update(func() {
					a.dropTab(t)
					a.l.redraw()
				})
			}
		}
		return false
	}
	x, y, _ := row.PointerPosition()
	wx, wy := row.LocalToWindow(x, y)
	if t == nil {
		a.drag = &tabDrag{d: d, g: g, grab: ui.Point{X: x, Y: y}, start: ui.Point{X: wx, Y: wy}}
		return false
	}
	t.g, t.at = g, ui.Point{X: wx, Y: wy}
	if !t.on {
		if dx, dy := wx-t.start.X, wy-t.start.Y; dx*dx+dy*dy < 16 {
			return false
		}
		t.on = true
		a.show(d)
		if a.alone() {
			p := mygo.Screen.CursorScreenPoint()
			wx, wy := a.win.Position()
			t.off = mygo.Point{X: p.X - wx, Y: p.Y - wy}
		}
	}
	if a.alone() {
		p := mygo.Screen.CursorScreenPoint()
		a.win.SetPosition(p.X-t.off.X, p.Y-t.off.Y)
		return false
	}
	// Out of the tab bar by half a tab's height, the tab comes away.
	b := g.bar
	t.out = t.at.Y < b.Y-b.H/2 || t.at.Y > b.Y+b.H*3/2 || t.at.X < b.X || t.at.X > b.X+b.W
	return true
}

// alone reports whether the window shows one tab, which moves the window.
func (a *app) alone() bool { return a.win != nil && a.docCount() == 1 }

// makeWay moves the tab dragged along g's tab bar to where its middle is.
func (a *app) makeWay(g *group) {
	t := a.drag
	if t == nil || !t.on || t.out || t.g != g || g.tabW <= 0 {
		return
	}
	i := slices.Index(g.docs, t.d)
	if i < 0 {
		return
	}
	to := int((t.at.X - t.grab.X + g.tabW/2 - g.bar.X + g.scroll) / g.tabW)
	to = min(max(to, 0), len(g.docs)-1)
	if to == i {
		return
	}
	cur := g.current()
	g.docs = slices.Insert(slices.Delete(g.docs, i, i+1), to, t.d)
	g.active = slices.Index(g.docs, cur)
}

// draggedTab shows the tab dragged under the pointer: in its tab bar
// while along it, else where the pointer is.
func (a *app) draggedTab(c *ui.Context) {
	t := a.drag
	if t == nil || !t.on || a.alone() {
		return
	}
	s, ch := a.scheme, a.scheme.chrome()
	b := t.g.bar
	x, y := t.at.X-t.grab.X, t.at.Y-t.grab.Y
	if !t.out {
		x, y = min(max(x, b.X), b.X+b.W-t.g.tabW), b.Y
	}
	ghost := ui.Row(c).Absolute().Left(x).Top(y).Width(t.g.tabW).Height(b.H).PassThrough().
		AlignItems(ui.Center).Gap(6).Padding(0, 6, 0, 14).Background(s.bg).Shadow(0, 1, 4, 0, ui.RGB(0, 0, 0).Alpha(0.15))
	if t.out {
		ghost.Opacity(0.9).Shadow(0, 4, 12, 0, ui.RGB(0, 0, 0).Alpha(0.25))
	}
	ghost.Children(func() {
		a.tabIcon(c, t.d, ch.text)
		ui.Text(c, t.d.name()).FontSize(12).TextColor(ch.text).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
	})
}

// dropTab puts the tab let go out of its tab bar into the tab bar under
// the pointer, of this window or another, or else into a window of its
// own there.
func (a *app) dropTab(t *tabDrag) {
	alone := a.alone()
	if a.win == nil || !t.out && !alone {
		return
	}
	if !alone {
		if gi, i, ok := a.barAt(t.at.X, t.at.Y); ok {
			a.moveTab(t.d, gi, i)
			return
		}
	}
	p := mygo.Screen.CursorScreenPoint()
	for _, w := range a.l.wins {
		if w.win == nil || w == a {
			continue
		}
		cb := w.win.ContentBounds()
		if gi, i, ok := w.barAt(float32(p.X-cb.X), float32(p.Y-cb.Y)); ok {
			w.moveTab(t.d, gi, i)
			w.win.Focus()
			return
		}
	}
	// Browsers know no cursor on the screen, and show one window: a tab
	// let go there off the tab bars stays.
	if alone || runtime.GOOS == "js" {
		return
	}
	a.take(t.d)
	b := a.l.newWindow()
	b.groups[0].docs = []*doc{t.d}
	b.show(t.d)
	r, cb := a.win.Bounds(), a.win.ContentBounds()
	b.makeWindow(mygo.Rectangle{X: p.X - int(t.grab.X), Y: p.Y - int(t.grab.Y) - (cb.Y - r.Y), Width: r.Width, Height: r.Height})
}

// barAt finds the tab bar at x, y in the window, and where in it a tab
// let go there goes.
func (a *app) barAt(x, y float32) (gi, i int, ok bool) {
	for gi, g := range a.groups {
		if b := g.bar; x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H {
			i := len(g.docs)
			if g.tabW > 0 {
				i = int((x - b.X + g.scroll) / g.tabW)
			}
			return gi, i, true
		}
	}
	return 0, 0, false
}

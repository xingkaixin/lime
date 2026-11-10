package main

import (
	"math"
	"math/rand/v2"
	"slices"
	"time"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// MyGo tells no caret's place, which drawings at the carets need: the
// editor's own caretRect gives it in the content, linked as the input is
// (input.go). TestCaretRects guards the link across MyGo upgrades.
//
//go:linkname editorCaretRect github.com/egoist/mygo/ui/editor.(*editor).caretRect
func editorCaretRect(ed unsafe.Pointer, off int) (ui.Rect, bool)

var scrollLeft = editorField[float32]("scrollX")

// caretRects returns the boxes of the carets of st, one per selection's
// head, in the window, as bars fill them, for drawings over the editor
// at its carets: those of lines out of view or scrolled past are left
// out. It reads where the editor painted last, so call it after the
// editor paints, as its element's DrawOver does.
func caretRects(st *editor.State) []ui.Rect {
	if st.Buffer == nil {
		return nil
	}
	ed := unsafe.Add(unsafe.Pointer(st), editorOffset)
	box, o := *editorBox(st), caretOrigin(st)
	left := box.X + *textLeft(st) - 1
	first, last := st.VisibleLines()
	var out []ui.Rect
	for _, s := range st.Selections() {
		if l := st.Buffer.Pos(s.Head).Line; l < first || l > last {
			continue
		}
		cr, ok := editorCaretRect(ed, s.Head)
		if !ok {
			continue
		}
		r := ui.Rect{X: o.X + cr.X - 1, Y: o.Y + cr.Y, W: cr.W, H: cr.H}
		if r.X < left || r.X > box.X+box.W || r.Y+r.H < box.Y || r.Y > box.Y+box.H {
			continue
		}
		out = append(out, r)
	}
	return out
}

// caretOrigin is where the content's origin is in the window.
func caretOrigin(st *editor.State) ui.Point {
	box := *editorBox(st)
	return ui.Point{X: box.X + *textLeft(st) - *scrollLeft(st), Y: box.Y - float32(st.ScrollY())}
}

// cursorStyle is cursor_style: a shape of the editor's carets, or a
// drawing of them over it.
type cursorStyle struct {
	name  string
	label func(locales.Messages) string
	shape editor.CursorShape
	draw  func(fx *caretFx, p *ui.Painter, st *editor.State, color ui.Color)
}

func (s cursorStyle) id() string { return "cursor_style " + s.name }

var cursorStyles = []cursorStyle{
	{name: "bar", label: locales.Messages.CursorBar, shape: editor.CursorBar},
	{name: "block", label: locales.Messages.CursorBlock, shape: editor.CursorBlock},
	{name: "underline", label: locales.Messages.CursorUnderline, shape: editor.CursorUnderline},
	{name: "glow", label: locales.Messages.CursorGlow, draw: (*caretFx).glow},
	{name: "spark", label: locales.Messages.CursorSpark, draw: (*caretFx).spark},
	{name: "smooth", label: locales.Messages.CursorSmooth, draw: (*caretFx).smooth},
}

// caretFx is what a drawn cursor style of a tab keeps between frames.
type caretFx struct {
	last time.Time
	// at is where the carets were drawn, from caretOrigin; seen is where
	// the editor had them.
	at, seen []ui.Rect
	sparks   []spark
}

type spark struct {
	x, y, vx, vy float32 // from caretOrigin, and DIPs a second
	age, life    float32 // seconds
}

// drawCarets draws the carets of d, focused, in the cursor style; the
// editor's own are the theme's color, which caretless hides.
func (a *app) drawCarets(p *ui.Painter, d *doc) {
	s := a.set.cursorStyle()
	fx := &d.caret
	if s.draw == nil || !d.st.Focused() {
		*fx = caretFx{}
		return
	}
	now := p.Now()
	text := *editorBox(d.st)
	text.X += *textLeft(d.st) - 2
	text.W -= *textLeft(d.st) - 2
	p.Clip(text, 0, func() { s.draw(fx, p, d.st, a.scheme.caret) })
	fx.last = now
}

// elapsed is the seconds since the last frame drawn, at most a frame's
// worth after a pause.
func (fx *caretFx) elapsed(p *ui.Painter) float32 {
	if fx.last.IsZero() {
		return 0
	}
	return min(float32(p.Now().Sub(fx.last).Seconds()), 0.05)
}

func (fx *caretFx) glow(p *ui.Painter, st *editor.State, c ui.Color) {
	for _, r := range caretRects(st) {
		p.Shadow(r, 1, 0, 0, 10, 2, c.Alpha(0.55))
		p.Fill(r, c, 1)
	}
}

// spark draws bars that send up a few sparks as they move, typing
// sending them on.
func (fx *caretFx) spark(p *ui.Painter, st *editor.State, c ui.Color) {
	o, rs, dt := caretOrigin(st), caretRects(st), fx.elapsed(p)
	moved := !slices.Equal(relative(rs, o), fx.seen)
	fx.seen = relative(rs, o)
	if moved && !fx.last.IsZero() {
		for _, r := range rs {
			for range 5 {
				a := -math.Pi/2 + (rand.Float64()-0.5)*1.4
				v := 30 + rand.Float32()*40
				fx.sparks = append(fx.sparks, spark{
					x: r.X + 1 - o.X, y: r.Y + r.H*rand.Float32() - o.Y,
					vx: v * float32(math.Cos(a)), vy: v * float32(math.Sin(a)),
					life: 0.4 + rand.Float32()*0.4,
				})
			}
		}
	}
	live := fx.sparks[:0]
	for _, s := range fx.sparks {
		s.age += dt
		if s.age >= s.life {
			continue
		}
		s.x, s.y = s.x+s.vx*dt, s.y+s.vy*dt
		s.vy += 20 * dt
		k := 1 - s.age/s.life
		size := 1.5 + 2*k
		p.Fill(ui.Rect{X: o.X + s.x - size/2, Y: o.Y + s.y - size/2, W: size, H: size}, c.Alpha(k), size/2)
		live = append(live, s)
	}
	fx.sparks = live
	for _, r := range rs {
		p.Fill(r, c, 1)
	}
	if len(fx.sparks) > 0 {
		p.AnimationFrame()
	}
}

// smooth draws bars that glide to where the carets go rather than jump,
// as the content scrolls under them.
func (fx *caretFx) smooth(p *ui.Painter, st *editor.State, c ui.Color) {
	o := caretOrigin(st)
	to := relative(caretRects(st), o)
	if len(to) != len(fx.at) || fx.last.IsZero() {
		fx.at = to
	}
	// Each frame closes the gap as an exponential decay of 30 ms.
	k := 1 - float32(math.Exp(-float64(fx.elapsed(p))/0.03))
	moving := false
	for i, r := range to {
		at := &fx.at[i]
		at.X += (r.X - at.X) * k
		at.Y += (r.Y - at.Y) * k
		at.H = r.H
		if math.Hypot(float64(r.X-at.X), float64(r.Y-at.Y)) < 0.5 {
			*at = r
		} else {
			moving = true
		}
		p.Fill(ui.Rect{X: o.X + at.X, Y: o.Y + at.Y, W: at.W, H: at.H}, c, 1)
	}
	if moving {
		p.AnimationFrame()
	}
}

func relative(rs []ui.Rect, o ui.Point) []ui.Rect {
	out := make([]ui.Rect, len(rs))
	for i, r := range rs {
		out[i] = ui.Rect{X: r.X - o.X, Y: r.Y - o.Y, W: r.W, H: r.H}
	}
	return out
}

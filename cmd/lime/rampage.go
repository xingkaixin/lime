package main

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// A held key bursts at every caret each repeat: past this many, the
// oldest particles go.
const maxParticles = 600

const trailLife = 0.22 // seconds

var warm = []ui.Color{ui.Hex("#ff5e3a"), ui.Hex("#ff9f1c"), ui.Hex("#ffd23f")}

// rampageFx is what the rampage typing mode of a tab keeps between
// frames. Places are from caretOrigin, so that they scroll with the text.
type rampageFx struct {
	last    time.Time
	buf     *editor.Buffer
	version uint64
	length  int
	carets  []ui.Rect
	bits    []particle
	trails  []trail
}

type particle struct {
	x, y, vx, vy float32 // DIPs, and DIPs a second
	age, life    float32 // seconds
	size         float32
	c            ui.Color
}

type trail struct {
	from, to ui.Rect
	age      float32
}

// drawRampage bursts particles at the carets of d as its text grows or
// shrinks, and draws a trail behind each caret that jumps.
func (a *app) drawRampage(p *ui.Painter, d *doc) {
	fx := &d.rampage
	st := d.st
	if !a.set.rampage() || !st.Focused() || st.Buffer == nil {
		*fx = rampageFx{}
		return
	}
	o := caretOrigin(st)
	carets := relative(caretRects(st), o)
	b := st.Buffer
	version, length := b.Version(), b.Len()
	if !fx.last.IsZero() {
		fx.advance(min(float32(p.Now().Sub(fx.last).Seconds()), 0.05))
	}
	// New particles and trails start after the old move: after a pause,
	// that move is the pause's, not theirs.
	if fx.buf == b && !fx.last.IsZero() {
		edited := version != fx.version
		if edited {
			fx.burst(carets, length < fx.length, a.scheme)
		}
		if len(carets) == len(fx.carets) {
			for i, to := range carets {
				from := fx.carets[i]
				if from.Y != to.Y || !edited && math.Abs(float64(to.X-from.X)) > float64(to.H) {
					fx.trails = append(fx.trails, trail{from: from, to: to})
				}
			}
		}
	}
	fx.buf, fx.version, fx.length, fx.carets, fx.last = b, version, length, carets, p.Now()

	p.Clip(textArea(st), 0, func() {
		fx.drawTrails(p, o, a.scheme.caret)
		fx.drawBits(p, o)
	})
	if len(fx.bits) > 0 || len(fx.trails) > 0 {
		p.AnimationFrame()
	}
}

// burst sends particles out of each caret: a few of the caret's colors
// for typing, more, faster and warm for deleting.
func (fx *rampageFx) burst(carets []ui.Rect, deleted bool, s *scheme) {
	n, speed, life, size := 7, float32(70), float32(0.45), float32(2.5)
	if deleted {
		n, speed, life, size = 16, 220, 0.6, 3.5
	}
	for _, r := range carets {
		for range n {
			angle := rand.Float64() * 2 * math.Pi
			if !deleted {
				angle = -math.Pi/2 + (rand.Float64()-0.5)*math.Pi
			}
			v := speed * (0.5 + rand.Float32())
			c := s.caret.Mix(s.accent, rand.Float32())
			if deleted {
				c = warm[rand.IntN(len(warm))]
			}
			fx.bits = append(fx.bits, particle{
				x: r.X + r.W/2, y: r.Y + r.H/2,
				vx: v * float32(math.Cos(angle)), vy: v * float32(math.Sin(angle)),
				life: life * (0.6 + 0.8*rand.Float32()), size: size * (0.6 + 0.8*rand.Float32()), c: c,
			})
		}
	}
	if extra := len(fx.bits) - maxParticles; extra > 0 {
		fx.bits = fx.bits[extra:]
	}
}

// advance moves the particles and ages the trails dt seconds, dropping
// those whose time is up.
func (fx *rampageFx) advance(dt float32) {
	bits := fx.bits[:0]
	for _, b := range fx.bits {
		b.age += dt
		if b.age < b.life {
			b.vy += 420 * dt
			b.x, b.y = b.x+b.vx*dt, b.y+b.vy*dt
			bits = append(bits, b)
		}
	}
	fx.bits = bits
	trails := fx.trails[:0]
	for _, t := range fx.trails {
		t.age += dt
		if t.age < trailLife {
			trails = append(trails, t)
		}
	}
	fx.trails = trails
}

func (fx *rampageFx) drawBits(p *ui.Painter, o ui.Point) {
	for _, b := range fx.bits {
		k := 1 - b.age/b.life
		size := b.size * (0.4 + 0.6*k)
		p.Fill(ui.Rect{X: o.X + b.x - size/2, Y: o.Y + b.y - size/2, W: size, H: size}, b.c.Alpha(k), size/2)
	}
}

// drawTrails draws each trail as a wedge from where the caret was, thin,
// to where it is, as tall as the caret; its tail runs after the caret.
func (fx *rampageFx) drawTrails(p *ui.Painter, o ui.Point, c ui.Color) {
	for _, t := range fx.trails {
		k := t.age / trailLife
		x1, y1 := o.X+t.to.X+t.to.W/2, o.Y+t.to.Y+t.to.H/2
		x0 := o.X + t.from.X + t.from.W/2 + (t.to.X-t.from.X)*k*k
		y0 := o.Y + t.from.Y + t.from.H/2 + (t.to.Y-t.from.Y)*k*k
		dx, dy := x1-x0, y1-y0
		n := float32(math.Hypot(float64(dx), float64(dy)))
		if n < 1 {
			continue
		}
		w := t.to.H * 0.3
		nx, ny := -dy/n*w, dx/n*w
		var path ui.Path
		path.MoveTo(x0, y0).LineTo(x1+nx, y1+ny).LineTo(x1-nx, y1-ny).Close()
		angle := float32(math.Atan2(float64(dx), float64(-dy)) * 180 / math.Pi)
		p.FillPathGradient(&path, ui.LinearGradient{From: c.Alpha(0), To: c.Alpha(0.8 * (1 - k)), Angle: angle})
	}
}

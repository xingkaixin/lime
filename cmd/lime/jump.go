package main

import (
	"slices"

	"github.com/egoist/mygo/ui/editor"
)

// jumpLines is how far the caret moves at once for where it was to be a
// jump, as in Zed.
const jumpLines = 10

// spot is where the caret was in a tab.
type spot struct {
	d   *doc
	pos editor.Pos
}

// jumps is a window's history of jumps, as Zed keeps it: where the caret
// was before it went to another tab, or jumpLines or more at once.
type jumps struct {
	back, forward []spot
	last          spot // the caret as the last frame left it
}

func (a *app) here() spot {
	d := a.current()
	if d == nil {
		return spot{}
	}
	return spot{d, d.st.CursorPos()}
}

// trackJumps keeps where the caret was as it jumps. It goes by the caret
// the frames show, so that whatever moves it is kept, but not while a
// panel is open: the symbols that show as they are selected are not
// jumps, and the caret they leave is compared with the one before.
func (a *app) trackJumps() {
	if a.panel != nil {
		return
	}
	j, now := &a.jumps, a.here()
	if l := j.last; l.d != nil && (l.d != now.d || abs(l.pos.Line-now.pos.Line) >= jumpLines) {
		j.back, j.forward = append(j.back, l), nil
	}
	j.last = now
}

func abs(n int) int { return max(n, -n) }

// jump goes back to the last spot of from, keeping where the caret is in
// to, as Sublime Text's jump_back and jump_forward do.
func (a *app) jump(from, to *[]spot) {
	n := len(*from)
	if n == 0 {
		return
	}
	s := (*from)[n-1]
	*from = (*from)[:n-1]
	if here := a.here(); here.d != nil {
		*to = append(*to, here)
	}
	a.show(s.d)
	s.d.st.GoTo(s.pos)
	a.jumps.last = a.here()
}

// forget takes d's spots out as its tab leaves the window.
func (j *jumps) forget(d *doc) {
	gone := func(s spot) bool { return s.d == d }
	j.back = slices.DeleteFunc(j.back, gone)
	j.forward = slices.DeleteFunc(j.forward, gone)
	if j.last.d == d {
		j.last = spot{}
	}
}

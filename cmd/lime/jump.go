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
// the frames show, so that whatever moves it is kept. Nothing is while
// the caret is being chosen: the rows of a panel that show as they are
// selected, the matches the find panel goes to as its query is typed,
// the tabs Ctrl+Tab passes. Where that leaves it is compared with where
// it was before.
func (a *app) trackJumps() {
	d := a.current()
	if a.panel != nil || a.cycle != nil || d != nil && d.plain() && !d.st.Focused() {
		return
	}
	a.keepJump()
}

// keepJump keeps where the caret was when it is now a jump away.
func (a *app) keepJump() {
	j, now := &a.jumps, a.here()
	if l := j.last; l.d != nil && !near(l, now) {
		j.back, j.forward = append(j.back, l), nil
	}
	j.last = now
}

// near reports whether s and t are less than a jump apart.
func near(s, t spot) bool { return s.d == t.d && abs(s.pos.Line-t.pos.Line) < jumpLines }

func abs(n int) int { return max(n, -n) }

// jump goes back to the last spot of from, keeping where the caret is in
// to, as Sublime Text's jump_back and jump_forward do. Spots near the
// caret are passed over, as those a tab closed leaves side by side.
func (a *app) jump(from, to *[]spot) {
	if a.panel != nil {
		return
	}
	a.keepJump()
	here := a.jumps.last
	for n := len(*from); n > 0; n-- {
		s := (*from)[n-1]
		*from = (*from)[:n-1]
		if near(s, here) {
			continue
		}
		if here.d != nil {
			*to = append(*to, here)
		}
		a.show(s.d)
		s.d.st.GoTo(s.pos)
		a.jumps.last = a.here()
		return
	}
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

package main

import (
	"reflect"
	"runtime"
	"slices"
	"time"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// gesture is what a press of the mouse does in Sublime Text, which the
// editor, after Zed, gives other modifiers.
type gesture int

const (
	pressAsIs gesture = iota
	pressAddCaret
	pressSubtract
	pressColumns
)

// mouseGesture reads Sublime Text's mousemap of the platform.
func mouseGesture(button int, mods ui.Modifiers) gesture {
	if mac {
		switch {
		case button == 2:
			return pressColumns
		case button != 0:
		case mods == ui.Super:
			return pressAddCaret
		case mods == ui.Super|ui.Shift:
			return pressSubtract
		case mods == ui.Alt, mods == ui.Alt|ui.Super:
			return pressColumns
		}
		return pressAsIs
	}
	switch {
	case button == 2 && runtime.GOOS == "windows", button == 1 && mods&ui.Shift != 0:
		return pressColumns
	case button != 0:
	case mods == ui.Ctrl:
		return pressAddCaret
	case mods == ui.Alt:
		return pressSubtract
	case mods == ui.Ctrl|ui.Alt|ui.Shift:
		return pressColumns
	}
	return pressAsIs
}

// The editor takes no remapping of its mouse, so the app replaces its
// input handler with one that adjusts events and then calls the editor's
// own handler. linkname reaches that handler; TestSublimeInput guards the
// link across MyGo upgrades.
//
//go:linkname editorInput github.com/egoist/mygo/ui/editor.(*editor).input
func editorInput(ed unsafe.Pointer, ev ui.InputEvent) bool

var editorOffset = func() uintptr {
	f, ok := reflect.TypeFor[editor.State]().FieldByName("ed")
	if !ok {
		panic("editor.State has no ed field")
	}
	return f.Offset
}()

// editorField reaches the field of MyGo's editor at the path names, from
// State, failing at start rather than misreading memory when an upgrade
// moves or retypes it.
func editorField[T any](names ...string) func(*editor.State) *T {
	t, off := reflect.TypeFor[editor.State](), uintptr(0)
	for _, n := range append([]string{"ed"}, names...) {
		f, ok := t.FieldByName(n)
		if !ok {
			panic("MyGo's editor has no " + n)
		}
		t, off = f.Type, off+f.Offset
	}
	if t != reflect.TypeFor[T]() {
		panic("MyGo's editor retyped " + t.String())
	}
	return func(st *editor.State) *T { return (*T)(unsafe.Add(unsafe.Pointer(st), off)) }
}

var (
	dragging  = editorField[bool]("gesture", "active")
	textLeft  = editorField[float32]("textX")
	editorBox = editorField[ui.Rect]("r")
	// State has no switch for the scroll bars, and View sets the editor's
	// options from State: set it after View.
	noScrollBars = editorField[bool]("o", "noScrollbar")
)

// pastSide is how far x, in the editor, is left (< 0) or right (> 0) of
// its text.
func pastSide(st *editor.State, x float32) float32 {
	switch l, r := *textLeft(st), editorBox(st).W; {
	case x < l:
		return x - l
	case x > r:
		return x - r
	}
	return 0
}

// input takes the input of the editor element e of d, as Sublime Text's
// mousemap has it, and ends a chord whose second key is not one.
func (a *app) input(e ui.Element, d *doc) {
	st := d.st
	ed := unsafe.Add(unsafe.Pointer(st), editorOffset)
	e.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputKeyDown:
			if a.chord {
				a.chord = false
				return true
			}
		case ui.InputPointerDown:
			a.chord = false
			switch mouseGesture(ev.Button, ev.Mods) {
			case pressAddCaret:
				ev.Mods = ui.Alt
			case pressColumns:
				ev.Button, ev.Mods, ev.Clicks = 0, ui.Alt|ui.Shift, 1
				d.columns = true
			case pressSubtract:
				subtract(ed, st, ev)
				return true
			}
		case ui.InputPointerMove, ui.InputPointerUp:
			if d.columns {
				ev.Button = 0
				d.columns = ev.Kind == ui.InputPointerMove
			}
		}
		ok := editorInput(ed, ev)
		if ev.Kind == ui.InputPointerMove {
			d.drag = ev
			if !d.sliding && a.win != nil && *dragging(st) && pastSide(st, ev.X) != 0 {
				a.slide(d, ed)
			}
		}
		return ok
	})
}

// slide scrolls the editor of d sideways while a drag goes past the left
// or the right of its text, as Sublime Text does; MyGo's editor scrolls
// only up and down for a drag. Until it does, a ticker sends the editor
// the scroll and the pointer's move a frame would.
func (a *app) slide(d *doc, ed unsafe.Pointer) {
	d.sliding = true
	win := a.win
	go func() {
		for more := true; more; {
			time.Sleep(time.Second / 60)
			ch := make(chan bool, 1)
			win.Update(func() { ch <- a.slideStep(d, ed) })
			more = <-ch
		}
	}()
}

// slideStep scrolls a frame's worth, at MyGo's speed for up and down, and
// reports whether to go on.
func (a *app) slideStep(d *doc, ed unsafe.Pointer) bool {
	st, ev := d.st, d.drag
	dx := pastSide(st, ev.X)
	ev.Time = time.Now()
	scroll := ui.InputEvent{Kind: ui.InputScroll, X: ev.X, Y: ev.Y, DX: max(-300, min(dx, 300)) / 4, Time: ev.Time}
	if dx == 0 || !*dragging(st) || a.l.owner(d) != a || !editorInput(ed, scroll) {
		d.sliding = false
		return false
	}
	// The scroll extends a column drag as a plain one; the move puts the
	// columns back.
	editorInput(ed, ev)
	return true
}

// subtract takes away the selection under the press ev, as Sublime
// Text's Cmd+Shift+click does, leaving a caret there when it was the
// only one.
func subtract(ed unsafe.Pointer, st *editor.State, ev ui.InputEvent) {
	sels := slices.Clone(st.Selections())
	ev.Mods, ev.Clicks = 0, 1
	editorInput(ed, ev)
	ev.Kind = ui.InputPointerUp
	editorInput(ed, ev)
	at := st.Cursor()
	rest := slices.DeleteFunc(sels, func(s editor.Selection) bool {
		return min(s.Anchor, s.Head) <= at && at <= max(s.Anchor, s.Head)
	})
	if len(rest) > 0 {
		st.SetSelections(rest...)
	}
}

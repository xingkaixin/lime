package main

import (
	"reflect"
	"runtime"
	"slices"
	"strings"
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

//go:linkname editorInput github.com/egoist/mygo/ui/editor.(*editor).input
func editorInput(ed unsafe.Pointer, ev ui.InputEvent) bool

var editorOffset = func() uintptr {
	f, ok := reflect.TypeFor[editor.State]().FieldByName("ed")
	if !ok {
		panic("editor.State has no ed field")
	}
	return f.Offset
}()

var diffEdsOffset, diffEdSize = func() (uintptr, uintptr) {
	f, ok := reflect.TypeFor[editor.DiffState]().FieldByName("eds")
	if !ok || f.Type.Kind() != reflect.Array || f.Type.Len() != 2 {
		panic("editor.DiffState has no eds [2]editor field")
	}
	ed, _ := reflect.TypeFor[editor.State]().FieldByName("ed")
	if f.Type.Elem() != ed.Type {
		panic("editor.DiffState.eds retyped")
	}
	return f.Offset, f.Type.Elem().Size()
}()

func diffActiveEditor(d *editor.DiffState) unsafe.Pointer {
	return unsafe.Add(unsafe.Pointer(d), diffEdsOffset+uintptr(d.ActiveSide())*diffEdSize)
}

func editorFieldAt[T any](names ...string) func(unsafe.Pointer) *T {
	ed, ok := reflect.TypeFor[editor.State]().FieldByName("ed")
	if !ok {
		panic("editor.State has no ed field")
	}
	t, off := ed.Type, uintptr(0)
	for _, n := range names {
		f, ok := t.FieldByName(n)
		if !ok {
			panic("MyGo's editor has no " + n)
		}
		t, off = f.Type, off+f.Offset
	}
	if t != reflect.TypeFor[T]() {
		panic("MyGo's editor retyped " + t.String())
	}
	return func(ed unsafe.Pointer) *T { return (*T)(unsafe.Add(ed, off)) }
}

func editorField[T any](names ...string) func(*editor.State) *T {
	at := editorFieldAt[T](names...)
	return func(st *editor.State) *T { return at(unsafe.Add(unsafe.Pointer(st), editorOffset)) }
}

var (
	textLeft  = editorField[float32]("textX")
	editorBox = editorField[ui.Rect]("r")

	findQueryAt     = editorFieldAt[string]("find", "query")
	findCaseSensAt  = editorFieldAt[bool]("find", "caseSens")
	findWholeWordAt = editorFieldAt[bool]("find", "wholeWord")
	findRegexAt     = editorFieldAt[bool]("find", "regex")
	findStaleAt     = editorFieldAt[bool]("find", "stale")
)

// input takes the input of the editor element e of d, as Sublime Text's
// mousemap has it, and ends a chord whose second key is not one.
func (a *app) input(e ui.Element, d *doc, services ui.Services) {
	st := d.st
	ed := unsafe.Add(unsafe.Pointer(st), editorOffset)
	e.HandleInput(func(ev ui.InputEvent) bool {
		if pastes(st, ev) {
			splitPaste(st, services)
		}
		switch ev.Kind {
		case ui.InputKeyDown:
			// A modifier pressed alone, as Cmd pressed again for Cmd+K Cmd+U,
			// comes with no key and is part of the chord's second key.
			if a.chord && (ev.Key != ui.KeyUnknown || ev.PhysicalKey != ui.KeyUnknown) {
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
		if ev.Kind == ui.InputPointerDown && ev.Button == 1 {
			splitPaste(st, services)
		}
		if trimEditorClip() {
			scheduleRelease()
		}
		return ok
	})
}

//go:linkname editorClip github.com/egoist/mygo/ui/editor.lastClip
var editorClip struct {
	text   string
	pieces []string
	lines  []bool
}

func init() {
	t := reflect.TypeOf(editorClip)
	want := []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[[]string](), reflect.TypeFor[[]bool]()}
	if t.NumField() != len(want) {
		panic("editorClip field count mismatch")
	}
	for i, wt := range want {
		if t.Field(i).Type != wt {
			panic("editorClip field type mismatch")
		}
	}
}

func pastes(st *editor.State, ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputCommand:
		return ev.Text == "paste" || ev.Text == "pasteAndMatchStyle"
	case ui.InputKeyDown:
		return slices.Contains(st.Bindings, bind(ev.Mods, ev.Key, editor.Paste))
	}
	return false
}

func trimEditorClip() bool {
	if len(editorClip.text) <= largeFile {
		return false
	}
	editorClip.text, editorClip.pieces, editorClip.lines = "", nil, nil
	return true
}

func splitPaste(st *editor.State, services ui.Services) {
	n := len(st.Selections())
	if n < 2 {
		return
	}
	text := strings.ReplaceAll(services.ReadClipboard(), "\r\n", "\n")
	if len(text) > largeFile {
		return
	}
	if text == editorClip.text && len(editorClip.pieces) == n {
		return
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != n {
		return
	}
	editorClip.text, editorClip.pieces, editorClip.lines = text, lines, make([]bool, n)
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

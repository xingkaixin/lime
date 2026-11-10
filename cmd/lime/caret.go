package main

import (
	"unsafe"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
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

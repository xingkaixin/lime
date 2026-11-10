package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// TestCaretRects guards caretRects' link into MyGo's editor: the bars it
// paints are where caretRects has them.
func TestCaretRects(t *testing.T) {
	a := newApp("")
	a.add("", "alpha\nbeta gamma\n")
	tt := ui.NewTester(a.view, 800, 500)
	st := a.current().st
	st.SetSelections(editor.Selection{Anchor: 2, Head: 2}, editor.Selection{Anchor: 11, Head: 11})
	tt.Frame()
	rs := caretRects(st)
	if len(rs) != 2 || rs[1].Y <= rs[0].Y || rs[1].X <= rs[0].X {
		t.Fatalf("caretRects: %v", rs)
	}
	img := tt.Image()
	want := a.scheme.caret
	for _, r := range rs {
		got := img.RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2))
		if got.R != want.R || got.G != want.G || got.B != want.B {
			t.Errorf("caret at %v is %v, want %v", r, got, want)
		}
	}
}

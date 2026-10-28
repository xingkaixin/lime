package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// draw_white_space takes Sublime Text's options, or its older strings, to
// the nearest the editor draws.
func TestDrawWhiteSpace(t *testing.T) {
	for _, tc := range []struct {
		json string
		want editor.Whitespace
	}{
		{`"none"`, editor.WhitespaceNone},
		{`"all"`, editor.WhitespaceAll},
		{`[]`, editor.WhitespaceNone},
		{`["selection"]`, editor.WhitespaceSelection},
		{`["selection", "trailing", "isolated"]`, editor.WhitespaceTrailing},
		{`["all_tabs", "selection"]`, editor.WhitespaceAll},
		{`["all", "leading_none", "enclosed_none"]`, editor.WhitespaceTrailing},
	} {
		var w whiteSpace
		if err := json.Unmarshal([]byte(tc.json), &w); err != nil || w.mode() != tc.want {
			t.Errorf("%s: %v %v, want %v", tc.json, w.mode(), err, tc.want)
		}
	}
}

// The display settings show in the open tabs at once, but an empty
// group's editor numbers no lines.
func TestDisplaySettings(t *testing.T) {
	a := newApp("")
	a.add("", "alpha\n")
	d := a.current()
	a.setLayout(layouts[1])
	tt := ui.NewTester(a.view, 900, 500)

	a.set.Rulers, a.set.RelativeLineNumbers = []int{80}, true
	tt.Frame()
	if !slices.Equal(d.st.WrapGuides, []int{80}) || d.st.LineNumbers != editor.LineNumbersRelative {
		t.Fatalf("rulers %v, line numbers %v", d.st.WrapGuides, d.st.LineNumbers)
	}
	if b := a.groups[1].blank; b == nil || b.st.LineNumbers != editor.LineNumbersOff {
		t.Fatal("the empty group's editor numbers its lines")
	}
}

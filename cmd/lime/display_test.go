package main

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

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

func TestSublimeDisplaySettings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(settingsPath(dir), []byte(`{"rulers": [[80, "solid"], 100], "scroll_past_end": 0.5}`), 0o644)
	s, err := loadSettings(dir)
	if err != nil || !slices.Equal(s.Rulers, []int{80, 100}) || !bool(s.ScrollPastEnd) {
		t.Fatalf("rulers %v, scroll past end %v: %v", s.Rulers, s.ScrollPastEnd, err)
	}
}

func TestDisplaySettings(t *testing.T) {
	a := newApp("")
	a.add("", "alpha\n")
	d := a.current()
	a.setLayout(layouts[1])
	tt := ui.NewTester(a.view, 900, 500)

	if !d.st.NoScrollbars {
		t.Fatal("scroll bars shown by default")
	}
	a.set.Rulers, a.set.HighlightLine, a.set.ShowScrollBars = []int{80}, true, true
	tt.Frame()
	if !slices.Equal(d.st.WrapGuides, []int{80}) || d.st.NoActiveLine || d.st.NoScrollbars {
		t.Fatalf("rulers %v, no line highlight %v, no scroll bars %v", d.st.WrapGuides, d.st.NoActiveLine, d.st.NoScrollbars)
	}
	if b := a.groups[1].blank; b == nil || b.st.LineNumbers != editor.LineNumbersOff {
		t.Fatal("the empty group's editor numbers its lines")
	}
	rd := a.resultsTab()
	a.set.ShowScrollBars = false
	tt.Frame()
	if !rd.st.NoScrollbars {
		t.Fatal("results tab shows scroll bars when disabled")
	}
}

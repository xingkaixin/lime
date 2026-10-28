package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestGuessIndent(t *testing.T) {
	for _, tc := range []struct {
		text  string
		size  int
		style editor.IndentStyle
	}{
		{"a\nb\n", 0, editor.IndentAuto},
		{"func f() {\n\tx\n\t\ty\n}\n", 0, editor.IndentTabs},
		{"a:\n  b:\n    c: 1\n  d: 2\n", 2, editor.IndentSpaces},
		{"/*\n * doc\n */\nf() {\n    if x {\n        y\n    }\n}\n", 4, editor.IndentSpaces},
		{"/*\n * doc\n */\nfunc f() {\n\tx\n}\n", 0, editor.IndentTabs},
	} {
		size, style := guessIndent(editor.NewBuffer(tc.text).Snapshot())
		if size != tc.size || style != tc.style {
			t.Errorf("%q: %d, %d; want %d, %d", tc.text, size, style, tc.size, tc.style)
		}
	}
}

// A file opens with its own indentation, which View > Indentation changes
// and the session keeps.
func TestIndentation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.yaml")
	os.WriteFile(p, []byte("a:\n  b:\n    c: 1\n"), 0o644)
	a := newApp(dir)
	a.open([]string{p})
	d := a.current()
	if size, spaces := a.indentOf(d); size != 2 || !spaces {
		t.Fatalf("detected %d, spaces %v; want 2 spaces", size, spaces)
	}
	a.run("unexpand_tabs")
	if got := d.st.Buffer.String(); got != "a:\n\tb:\n\t\tc: 1\n" {
		t.Fatalf("to tabs: %q", got)
	}
	a.run(tabWidth(8))
	a.run("expand_tabs")
	if got := d.st.Buffer.String(); got != "a:\n        b:\n                c: 1\n" {
		t.Fatalf("to spaces: %q", got)
	}
	a.run("toggle_setting translate_tabs_to_spaces")
	if err := a.l.saveSession(); err != nil {
		t.Fatal(err)
	}
	l := newLime(dir)
	l.restoreSession()
	d = l.wins[0].current()
	if size, spaces := l.wins[0].indentOf(d); size != 8 || spaces {
		t.Fatalf("restored %d, spaces %v; want tabs of 8", size, spaces)
	}
}

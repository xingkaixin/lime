package main

import (
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

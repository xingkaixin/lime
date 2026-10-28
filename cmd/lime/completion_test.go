package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Typing completes the words of the tabs, and Enter makes a new line
// after a word that completes only to itself.
func TestWordCompletion(t *testing.T) {
	a := newApp("")
	a.add("", "alpaca\n")
	st := a.add("", "alphabet\n").st
	st.Buffer.SetLanguage(editor.LanguageNamed("Go"))
	st.Select(st.Buffer.Len(), st.Buffer.Len())
	tt := ui.NewTester(a.view, 800, 500)

	tt.Type("alp")
	tt.Key(0, ui.KeyEnter)
	if got := st.Buffer.String(); got != "alphabet\nalpaca" {
		t.Fatalf("Enter after alp: %q, want the other tab's alpaca", got)
	}
	tt.Type(" ")
	tt.Type("alphabet")
	tt.Key(0, ui.KeyEnter)
	if got := st.Buffer.String(); got != "alphabet\nalpaca alphabet\n" {
		t.Fatalf("Enter after alphabet: %q, want a new line", got)
	}
}

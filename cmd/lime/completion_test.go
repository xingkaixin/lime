package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Typing completes the words of the tabs, but not in comments, nor to the
// word typed, nor at carets after different words: Enter makes a new
// line there. Ctrl+Space shows them all.
func TestWordCompletion(t *testing.T) {
	a := newApp("")
	a.add("", "alpaca\n")
	goTab := func(text string) *editor.State {
		st := a.add("", text).st
		st.Buffer.SetLanguage(editor.LanguageNamed("Go"))
		st.Select(st.Buffer.Len(), st.Buffer.Len())
		return st
	}
	st := goTab("alphabet\n")
	tt := ui.NewTester(a.view, 800, 500)
	typeKeys := func(s string) {
		for _, r := range s {
			tt.Type(string(r))
		}
	}
	enter := func(want, why string) {
		t.Helper()
		tt.Key(0, ui.KeyEnter)
		if got := st.Buffer.String(); !strings.HasSuffix(got, want) {
			t.Fatalf("%s, Enter: %q, want it to end %q", why, got, want)
		}
	}

	typeKeys("alp")
	enter("\nalpaca", "alp")
	typeKeys(" alphabet")
	enter(" alphabet\n", "alphabet, a word of the text")
	typeKeys("// alp")
	enter("// alp\n// ", "a comment")
	tt.Key(ui.Ctrl, ui.KeySpace)
	enter("// alphabet", "Ctrl+Space")

	a.add("", "getName\n")
	st = goTab("get\nset\n")
	st.SetSelections(editor.Selection{Anchor: 7, Head: 7}, editor.Selection{Anchor: 3, Head: 3})
	tt.Frame()
	typeKeys("N")
	enter("getN\n\nsetN\n\n", "carets after get and set")
}

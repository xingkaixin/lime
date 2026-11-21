package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

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

func TestMultilineCommentCompletion(t *testing.T) {
	a := newApp("")
	a.add("", "alpaca\n")
	d := a.add("", "/*\n")
	st := d.st
	st.Buffer.SetLanguage(editor.LanguageNamed("Go"))
	st.Select(st.Buffer.Len(), st.Buffer.Len())
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
			t.Fatalf("%s, Enter: %q, want suffix %q", why, got, want)
		}
	}

	typeKeys("alpa")
	enter("/*\nalpa\n", "inside Go block comment")
	typeKeys("*/ alpac")
	enter("*/ alpaca", "same line after closing */")
	tt.Key(0, ui.KeyEnter)
	typeKeys("alpac")
	enter("\nalpaca", "next line after closing */")
}

func TestInCommentMultiline(t *testing.T) {
	cases := []struct {
		name string
		lang string
		text string
		pos  editor.Pos
		want bool
	}{
		{"go before block", "Go", "x /*\nfoo\n*/\nbar", editor.Pos{Line: 0, Col: 1}, false},
		{"go inside block line 0", "Go", "/*\nfoo\n*/\nbar", editor.Pos{Line: 0, Col: 2}, true},
		{"go inside block col 0", "Go", "/*\nfoo\n*/\nbar", editor.Pos{Line: 1, Col: 0}, true},
		{"go inside block line 1", "Go", "/*\nfoo\n*/\nbar", editor.Pos{Line: 1, Col: 3}, true},
		{"go right after close", "Go", "/*\nfoo\n*/\nbar", editor.Pos{Line: 2, Col: 2}, false},
		{"go after close same line", "Go", "/*\nfoo\n*/ bar", editor.Pos{Line: 2, Col: 6}, false},
		{"go after close next line", "Go", "/*\nfoo\n*/\nbar", editor.Pos{Line: 3, Col: 3}, false},
		{"go open in string", "Go", "s := \"/*\"\nbar", editor.Pos{Line: 1, Col: 3}, false},
		{"go open in raw string", "Go", "s := `\n/*\n`\nbar", editor.Pos{Line: 3, Col: 3}, false},
		{"go open in line comment", "Go", "// /*\nbar", editor.Pos{Line: 1, Col: 3}, false},
		{"css inside block", "CSS", "/*\ncolor: red\n*/\nbody {}", editor.Pos{Line: 1, Col: 5}, true},
		{"css after block", "CSS", "/*\ncolor: red\n*/\nbody {}", editor.Pos{Line: 3, Col: 4}, false},
		{"html inside comment", "HTML", "<!--\ndiv\n-->\ndiv", editor.Pos{Line: 1, Col: 3}, true},
		{"html right after close", "HTML", "<!--\ndiv\n-->\ndiv", editor.Pos{Line: 2, Col: 3}, false},
		{"html after comment", "HTML", "<!--\ndiv\n-->\ndiv", editor.Pos{Line: 3, Col: 3}, false},
		{"rust nested still open", "Rust", "/*\n/* inner */\nstill\n*/\ncode", editor.Pos{Line: 2, Col: 5}, true},
		{"rust nested closed", "Rust", "/*\n/* inner */\nstill\n*/\ncode", editor.Pos{Line: 4, Col: 4}, false},
	}
	for _, tc := range cases {
		b := editor.NewBuffer(tc.text)
		b.SetLanguage(editor.LanguageNamed(tc.lang))
		if got := inComment(b, tc.pos); got != tc.want {
			t.Errorf("%s: inComment(%+v) = %v, want %v", tc.name, tc.pos, got, tc.want)
		}
	}
}

func TestLexCacheInvalidationAndReuse(t *testing.T) {
	goLang := *editor.LanguageNamed("Go")
	baseTok := goLang.Tokenizer
	calls := 0
	goLang.Tokenizer = editor.TokenizerFunc(func(line string, state uint64, toks []editor.Token) ([]editor.Token, uint64) {
		calls++
		return baseTok.Tokenize(line, state, toks)
	})

	var cache lexCache
	b := editor.NewBuffer("/*\nline1\nline2\nline3")
	b.SetLanguage(&goLang)

	if !inCommentAt(b, editor.Pos{Line: 3, Col: 5}, &cache) {
		t.Fatal("expected line 3 inside block comment")
	}
	if len(cache.states) != 4 || calls != 4 {
		t.Fatalf("initial cached states=%d calls=%d, want 4 and 4", len(cache.states), calls)
	}

	calls = 0
	end := b.Len()
	b.Edit(editor.Edit{Start: end, End: end, Text: "x"})
	if !inCommentAt(b, editor.Pos{Line: 3, Col: 6}, &cache) {
		t.Fatal("expected line 3 still inside block comment after edit on line 3")
	}
	if calls != 1 {
		t.Fatalf("edit on line 3 tokenized %d lines, want 1", calls)
	}

	calls = 0
	line1End := b.Offset(editor.Pos{Line: 1, Col: 5})
	b.Edit(editor.Edit{Start: line1End, End: line1End, Text: " */"})
	if inCommentAt(b, editor.Pos{Line: 3, Col: 6}, &cache) {
		t.Fatal("expected line 3 outside block comment after closing on line 1")
	}
	if calls != 3 {
		t.Fatalf("edit on line 1 tokenized %d lines, want 3 (lines 1, 2, 3)", calls)
	}

	b.Set("<!--\nline1")
	b.SetLanguage(editor.LanguageNamed("HTML"))
	if !inCommentAt(b, editor.Pos{Line: 1, Col: 5}, &cache) {
		t.Fatal("expected HTML comment after language switch")
	}
}

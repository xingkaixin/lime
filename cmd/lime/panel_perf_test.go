package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestParseGotoLocAndJump(t *testing.T) {
	cases := []struct {
		query    string
		name     string
		line     int
		col      int
		hasColon bool
	}{
		{"app.go", "app.go", 0, 0, false},
		{":", "", 0, 0, true},
		{":42", "", 42, 0, true},
		{":42:", "", 42, 0, true},
		{":42:10", "", 42, 10, true},
		{"app.go:42", "app.go", 42, 0, true},
		{"app.go:42:", "app.go", 42, 0, true},
		{"app.go:42:10", "app.go", 42, 10, true},
		{"foo:bar", "foo:bar", 0, 0, false},
		{"foo:bar:12", "foo:bar", 12, 0, true},
		{"foo:bar:12:5", "foo:bar", 12, 5, true},
		{"foo:12:bar", "foo:12:bar", 0, 0, false},
	}
	for _, tc := range cases {
		name, line, col, hasColon := parseGotoLoc(tc.query)
		if name != tc.name || line != tc.line || col != tc.col || hasColon != tc.hasColon {
			t.Errorf("parseGotoLoc(%q) = (%q, %d, %d, %v), want (%q, %d, %d, %v)",
				tc.query, name, line, col, hasColon, tc.name, tc.line, tc.col, tc.hasColon)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "app.go")
	os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"), 0o644)
	a := newApp("")
	d := a.add("", "first\nsecond line\nthird\n")
	a.addFolder(dir)
	tt := ui.NewTester(a.view, 900, 600)

	// :line:col in current tab.
	a.showGoto(":2:8")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if got := d.st.CursorPos(); got != (editor.Pos{Line: 1, Col: 7}) {
		t.Fatalf(":2:8 placed caret at %+v, want {Line:1 Col:7}", got)
	}

	// Column past end of line clamps to line length.
	a.showGoto(":1:99")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if got := d.st.CursorPos(); got != (editor.Pos{Line: 0, Col: len("first")}) {
		t.Fatalf(":1:99 placed caret at %+v, want {Line:0 Col:%d}", got, len("first"))
	}

	// file:line:col opens the file and jumps to (line-1, col-1).
	a.showGoto("app.go:3:6")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	cur := a.current()
	if cur.path != path || cur.st.CursorPos() != (editor.Pos{Line: 2, Col: 5}) {
		t.Fatalf("app.go:3:6 landed in %q at %+v, want %q at {Line:2 Col:5}", cur.path, cur.st.CursorPos(), path)
	}
}

func TestFuzzyContiguousAndUnicode(t *testing.T) {
	// Contiguous boundary "app" in "a/path/to/app.go" must outrank scattered matches.
	contig, ok1 := fuzzy("app", "a/path/to/app.go")
	scattered, ok2 := fuzzy("app", "a/path/top/x.go")
	if !ok1 || !ok2 {
		t.Fatalf("expected both to match: ok1=%v ok2=%v", ok1, ok2)
	}
	if contig <= scattered {
		t.Fatalf("contiguous boundary score %d should beat scattered score %d", contig, scattered)
	}
	// Exact optimal score: boundary 'a'(+4) + 'p'(+5) + 'p'(+5) = 14 -> 1400 - len.
	want := 14*100 - len("a/path/to/app.go")
	if contig != want {
		t.Fatalf("fuzzy(\"app\", \"a/path/to/app.go\") = %d, want %d", contig, want)
	}

	// Unicode case folding.
	if _, ok := fuzzy("über", "Über.txt"); !ok {
		t.Fatal("fuzzy should match Unicode case-insensitively (über vs Über.txt)")
	}
	if _, ok := fuzzy("мод", "src/Модуль.go"); !ok {
		t.Fatal("fuzzy should match Cyrillic case-insensitively")
	}
	if _, ok := fuzzy("üz", "Über.txt"); ok {
		t.Fatal("fuzzy should reject non-matching Unicode subsequence")
	}
}

func TestByNameLongFilenameBeatsPathMatch(t *testing.T) {
	longName := item{
		label:  strings.Repeat("component_", 15) + "x_handler.go",
		detail: "pkg/" + strings.Repeat("component_", 15) + "x_handler.go",
	}
	pathMatch := item{
		label:  "main.go",
		detail: "x/main.go",
	}
	score := byName("x")
	sLong, okLong := score(&longName)
	sPath, okPath := score(&pathMatch)
	if !okLong || !okPath {
		t.Fatalf("expected both to match: okLong=%v okPath=%v", okLong, okPath)
	}
	if sLong <= sPath {
		t.Fatalf("long filename score %d should beat path match score %d", sLong, sPath)
	}
	got := ranked([]item{pathMatch, longName}, score)
	if len(got) != 2 || got[0].label != longName.label {
		t.Fatalf("ranked put %q first, want %q", got[0].label, longName.label)
	}
}

func TestRankedTopKParity(t *testing.T) {
	const total = maxShown * 5
	items := make([]item, total)
	scores := make([]int, total)
	for i := range total {
		items[i] = item{label: strconv.Itoa(i)}
		scores[i] = (i * 37) % 23 // many ties
	}
	scoreFn := func(it *item) (int, bool) {
		idx, _ := strconv.Atoi(it.label)
		if idx%3 == 0 {
			return 0, false
		}
		return scores[idx], true
	}

	got := ranked(items, scoreFn)

	type hit struct {
		it    item
		score int
	}
	var all []hit
	for i := range items {
		if s, ok := scoreFn(&items[i]); ok {
			all = append(all, hit{items[i], s})
		}
	}
	slices.SortStableFunc(all, func(x, y hit) int { return y.score - x.score })
	if len(got) != maxShown {
		t.Fatalf("len(got) = %d, want %d", len(got), maxShown)
	}
	for i := range maxShown {
		if got[i].label != all[i].it.label {
			t.Fatalf("rank %d: got %q (score %d), want %q (score %d)",
				i, got[i].label, scores[mustAtoi(got[i].label)], all[i].it.label, all[i].score)
		}
	}
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func TestSymbolCacheAndGoGroups(t *testing.T) {
	src := `package p

type (
	// Outer comment
	Alpha struct {
		FieldOne int
		FieldTwo string
	}
	Beta interface {
		Do(x int) error
	}
	Gamma = int
)

const (
	First = 1.
	Second, Third = 2, 3
	_ = iota
	Fourth string = "a(b)c"
	Flags = First |
		Second
	Fifth = (
		10 + 20
	)
)

func helper() {}
`
	a := newApp("")
	d := a.add("/p/grouped.go", src)
	syms := cachedSymbols(d)
	var names []string
	for _, s := range syms {
		names = append(names, s.name)
	}
	want := []string{"Alpha", "Beta", "Gamma", "First", "Second", "Third", "Fourth", "Flags", "Fifth", "helper"}
	if !slices.Equal(names, want) {
		t.Fatalf("Go grouped symbols = %v, want %v", names, want)
	}

	// Cache hit returns the same cached slice without rescanning.
	syms2 := cachedSymbols(d)
	if len(syms2) != len(syms) || (len(syms) > 0 && &syms2[0] != &syms[0]) {
		t.Fatal("docSymbols did not reuse cached symbol slice for unchanged doc")
	}

	// Editing invalidates the cache.
	end := d.st.Buffer.Len()
	d.st.Buffer.Edit(editor.Edit{Start: end, End: end, Text: "\nfunc added() {}\n"})
	syms3 := cachedSymbols(d)
	if !slices.ContainsFunc(syms3, func(s symbol) bool { return s.name == "added" }) {
		t.Fatalf("docSymbols did not invalidate cache after edit: %v", syms3)
	}

}

func TestSymbolsAfterUnfinishedStruct(t *testing.T) {
	syms := symbols(editor.LanguageNamed("Go"), "type (\n Pair struct {\n X int\n)\nfunc helper() {}")
	if !slices.ContainsFunc(syms, func(s symbol) bool { return s.name == "helper" }) {
		t.Fatalf("missing helper after incomplete type: %v", syms)
	}
}

func TestCompletionWordListCacheAndCaretExclusion(t *testing.T) {
	a := newApp("")
	d := a.add("", "alpha beta alpha\n")
	d.st.Buffer.SetLanguage(editor.LanguageNamed("Go"))

	// Moving caret must reuse the exact same *wordList pointer.
	d.st.Select(0, 0)
	w1 := a.wordList(d)
	d.st.Select(6, 6)
	w2 := a.wordList(d)
	if w1 != w2 {
		t.Fatal("wordList rescanned buffer when only caret moved")
	}
	if w1.counts[w1.index["alpha"]] != 2 || w1.counts[w1.index["beta"]] != 1 {
		t.Fatalf("unexpected word counts: %v", w1.counts)
	}

	// Single-occurrence word at caret is excluded; multi-occurrence word is kept.
	if w1.has("beta", "beta") {
		t.Fatal("single-occurrence word at caret should be excluded")
	}
	if !w1.has("alpha", "alpha") {
		t.Fatal("multi-occurrence word at caret should remain available")
	}

	// Pre-filter matches ASCII and Unicode subsequences case-insensitively.
	if !matchesPrefix("ab", "AlphaBeta") || matchesPrefix("az", "AlphaBeta") {
		t.Fatal("matchesPrefix ASCII check failed")
	}
	if !matchesPrefix("üb", "Über") || matchesPrefix("üz", "Über") {
		t.Fatal("matchesPrefix Unicode check failed")
	}

	// End-to-end: a word typed for the first time does not complete to itself,
	// while a word appearing elsewhere in the buffer does suppress completions on exact match.
	st := a.add("", "uniqueOther\n").st
	st.Buffer.SetLanguage(editor.LanguageNamed("Go"))
	st.Select(st.Buffer.Len(), st.Buffer.Len())
	tt := ui.NewTester(a.view, 800, 500)
	for _, r := range "brandNew" {
		tt.Type(string(r))
	}
	tt.Key(0, ui.KeyEnter)
	if got := st.Buffer.String(); got != "uniqueOther\nbrandNew\n" {
		t.Fatalf("typing new single-occurrence word + Enter: %q", got)
	}
}

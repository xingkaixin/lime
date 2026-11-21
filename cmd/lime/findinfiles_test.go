package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"

	"lime/locales"
)

func TestFindInFiles(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"a.go": "package p\n", "a_test.go": "alpha\n", "bin.dat": "alpha\x00", "sub/b.txt": "one\nAlpha beta alpha\n",
	} {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(text), 0o644)
	}
	a := newApp("")
	a.addFolder(dir)
	a.open([]string{filepath.Join(dir, "a.go")})
	src := a.current()
	src.st.Buffer.Set("package p\n\nvar alpha = 1\n")
	src.st.Select(0, 0)
	tt := ui.NewTester(a.view, 900, 600)

	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	if !a.fif.open || a.fif.where != openFolders {
		t.Fatalf("Cmd+Shift+F: panel open %v, where %q", a.fif.open, a.fif.where)
	}
	tt.Type("alpha")
	a.fif.where += ", -*_test.go"
	tt.Key(0, ui.KeyEnter)
	res := a.current()
	b := filepath.Join(dir, "sub", "b.txt")
	want := locales.FindResultsHeader(2, "alpha") + "\n\n" +
		filepath.Join(dir, "a.go") + ":\n    3: var alpha = 1\n\n" +
		b + ":\n    2: Alpha beta alpha\n\n" +
		locales.FindResultsSummary(3, 2) + "\n"
	if got := res.st.Buffer.String(); res.results == nil || got != want {
		t.Fatalf("results:\n%s\nwant:\n%s", got, want)
	}

	tt.Frame()
	res.st.GoTo(editor.Pos{Line: 6})
	tt.Key(0, ui.KeyEnter)
	if d := a.current(); d.path != b || d.st.SelectedText() != "Alpha" {
		t.Fatalf("Enter on b.txt's line: %q selects %q", d.name(), d.st.SelectedText())
	}
	tt.Key(0, ui.KeyF4)
	if a.current() != src || src.st.SelectedText() != "alpha" || src.st.CursorPos().Line != 2 {
		t.Fatalf("F4: %q selects %q", a.current().name(), a.current().st.SelectedText())
	}

	n := a.docCount()
	a.showFindInFiles("")
	a.searchInFiles()
	if a.docCount() != n || a.current() != res {
		t.Fatalf("a second search opened another tab: %d tabs, want %d", a.docCount(), n)
	}
}

func TestFindInLinkedFiles(t *testing.T) {
	dir := t.TempDir()
	agents, claude := filepath.Join(dir, "AGENTS.md"), filepath.Join(dir, "CLAUDE.md")
	os.WriteFile(agents, []byte("foo\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "crlf.txt"), []byte("foo\r\nbar\r\n"), 0o644)
	if err := os.Symlink("AGENTS.md", claude); err != nil {
		t.Skip(err)
	}
	a := newApp("")
	a.addFolder(dir)
	a.open([]string{agents})
	a.current().st.Buffer.Set("foo\nfoo\n")
	a.fif.query, a.fif.regex, a.fif.where = "foo$", true, openFolders
	a.searchInFiles()
	text := a.current().st.Buffer.String()
	if want := locales.FindResultsHeader(2, "foo$"); !strings.HasPrefix(text, want) {
		t.Fatalf("results:\n%s\nwant them to start %q", text, want)
	}
	if want := locales.FindResultsSummary(3, 2) + "\n"; !strings.HasSuffix(text, want) {
		t.Fatalf("results:\n%s\nwant them to end %q", text, want)
	}
}

func TestReplaceInFiles(t *testing.T) {
	dir := t.TempDir()
	one, two := filepath.Join(dir, "one.go"), filepath.Join(dir, "two.go")
	os.WriteFile(one, []byte("foo(1) foo(2)\n"), 0o644)
	os.WriteFile(two, []byte("x := foo(3)\n"), 0o644)
	a := newApp("")
	a.addFolder(dir)
	a.open([]string{one})
	d := a.current()
	d.st.Buffer.Set("foo(1) foo(2) foo(9)\n")

	a.fif.query, a.fif.regex, a.fif.where = `foo\((\d)\)`, true, openFolders
	m, sc, open, ctx := a.prepareSearch(&a.fif.replacing)
	res := searchFiles(ctx, m, sc, open, a.set.FallbackEncoding, math.MaxInt)
	if res.matches != 4 || len(res.files) != 2 {
		t.Fatalf("found %d matches in %d files, want 4 in 2", res.matches, len(res.files))
	}
	a.replaceFound(m, "bar[$1]", res)
	if got := d.st.Buffer.String(); got != "bar[1] bar[2] bar[9]\n" || !d.dirty() {
		t.Fatalf("one.go: %q, unsaved %v", got, d.dirty())
	}
	d2 := a.docAt(two)
	if d2 == nil || d2.st.Buffer.String() != "x := bar[3]\n" || !d2.dirty() {
		t.Fatalf("two.go not opened with its match replaced, unsaved")
	}
	if b, _ := os.ReadFile(two); string(b) != "x := foo(3)\n" {
		t.Fatalf("two.go saved: %q", b)
	}
}

func TestReplaceInLinkedFile(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(agents, []byte("foo\n"), 0o644)
	if err := os.Symlink("AGENTS.md", filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Skip(err)
	}
	a := newApp("")
	a.addFolder(dir)
	a.open([]string{agents})
	d := a.current()
	d.st.Buffer.Set("foo\nunsaved foo\n")
	a.fif.query, a.fif.where = "foo", openFolders
	m, sc, open, ctx := a.prepareSearch(&a.fif.replacing)
	res := searchFiles(ctx, m, sc, open, a.set.FallbackEncoding, math.MaxInt)
	a.replaceFound(m, "bar", res)
	if got := d.st.Buffer.String(); a.docCount() != 1 || got != "bar\nunsaved bar\n" {
		t.Fatalf("%d tabs, AGENTS.md %q; want one tab with both replaced", a.docCount(), got)
	}
}

func TestMatcherLiteralParity(t *testing.T) {
	texts := []string{
		"alpha Alpha ALPHA _alpha alpha_ alpha1 (alpha)\n",
		"aaa aab aaab aaaa\n",
		"foo\nbar FOO\nBAR foo\nbar\n",
		"Café café CAFÉ Kelvin kelvin ſong song 中文 中文\n",
		"ask asK stack ſtick\n",
		"x_a a a_x\n",
	}
	queries := []string{
		"alpha", "Alpha", "aa", "aab", "foo\nbar", "café", "CAFÉ", "k", "kelvin", "ask", "s", "stack", "中文", "a",
	}
	for _, caseSens := range []bool{false, true} {
		for _, wholeWord := range []bool{false, true} {
			for _, q := range queries {
				lit, err := newMatcher(q, false, caseSens, wholeWord)
				if err != nil {
					t.Fatalf("newMatcher(%q) literal: %v", q, err)
				}
				re, err := newMatcher(regexp.QuoteMeta(q), true, caseSens, wholeWord)
				if err != nil {
					t.Fatalf("newMatcher(%q) regex: %v", q, err)
				}
				for _, text := range texts {
					for _, limit := range []int{1, 2, 100} {
						got := lit.find(text, limit)
						want := re.find(text, limit)
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("q=%q case=%v word=%v limit=%d text=%q:\n got  %v\n want %v",
								q, caseSens, wholeWord, limit, text, got, want)
						}
					}
				}
			}
		}
	}
}

func TestMatcherRegexZeroWidthAndAnchors(t *testing.T) {
	cases := []struct {
		query string
		text  string
	}{
		{"^", "abc\ndef\n中文\n"},
		{"$", "abc\ndef\n"},
		{"a*", "bbbaabbb中文"},
		{".*", "abc\ndef\n\nghi"},
		{"^foo", "foofoo\nfoo\nbarfoo\n"},
		{"a|^b", "ab\nb\nab"},
		{"^|a", "aa\na"},
		{`a|\bfoo`, "afoo foo barfoo"},
		{"^", strings.Repeat("x", 50000)},
		{"a*", strings.Repeat("b", 50000)},
	}
	for _, tc := range cases {
		m, err := newMatcher(tc.query, true, true, false)
		if err != nil {
			t.Fatalf("newMatcher(%q): %v", tc.query, err)
		}
		got := m.find(tc.text, 100)
		ref := regexp.MustCompile("(?m)" + tc.query)
		var want [][]int
		for _, s := range ref.FindAllStringIndex(tc.text, -1) {
			if s[0] != s[1] {
				want = append(want, s)
				if len(want) == 100 {
					break
				}
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("query=%q text=%q:\n got  %v\n want %v", tc.query, tc.text[:min(len(tc.text), 40)], got, want)
		}
	}
}

func TestFindInFilesGitignore(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		".gitignore":        "# comment\n*.log\n!keep.log\nbuild/\n/rootonly.txt\ndocs/**/draft.md\n",
		"main.go":           "needle\n",
		"skip.log":          "needle\n",
		"keep.log":          "needle\n",
		"build/out.txt":     "needle\n",
		"rootonly.txt":      "needle\n",
		"sub/.gitignore":    "*.tmp\n!allow.log\n",
		"sub/rootonly.txt":  "needle\n",
		"sub/a.tmp":         "needle\n",
		"sub/allow.log":     "needle\n",
		"sub/other.log":     "needle\n",
		"other/b.tmp":       "needle\n",
		"docs/a/b/draft.md": "needle\n",
		"docs/draft.md":     "needle\n",
		"docs/a/final.md":   "needle\n",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	m, err := newMatcher("needle", false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	res := searchFiles(context.Background(), m, scope{paths: []string{dir}}, nil, defaultFallback, maxResults)
	var got []string
	for _, fh := range res.files {
		rel, _ := filepath.Rel(dir, fh.src.path)
		got = append(got, filepath.ToSlash(rel))
	}
	want := []string{
		"docs/a/final.md",
		"keep.log",
		"main.go",
		"other/b.tmp",
		"sub/allow.log",
		"sub/rootonly.txt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gitignore matched files:\n got  %v\n want %v", got, want)
	}

	skipLog := filepath.Join(dir, "skip.log")
	resExplicit := searchFiles(context.Background(), m, scope{paths: []string{skipLog}}, nil, defaultFallback, maxResults)
	if resExplicit.matches != 1 || len(resExplicit.files) != 1 {
		t.Fatalf("explicit ignored file: got %d matches in %d files, want 1 in 1", resExplicit.matches, len(resExplicit.files))
	}
	a := newApp("")
	a.open([]string{skipLog})
	a.fif.query, a.fif.where = "needle", openFiles
	m2, sc2, open2, ctx2 := a.prepareSearch(&a.fif.finding)
	resOpen := searchFiles(ctx2, m2, sc2, open2, a.set.FallbackEncoding, maxResults)
	if resOpen.matches != 1 {
		t.Fatalf("<open files> ignored file: got %d matches, want 1", resOpen.matches)
	}
}

func TestFindInFilesEncodingsAndLimits(t *testing.T) {
	dir := t.TempDir()

	utf16le, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte("hello 世界\r\nsecond\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	utf16be, err := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewEncoder().Bytes([]byte("hello 世界\n"))
	if err != nil {
		t.Fatal(err)
	}
	gbkBytes, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("你好 世界\n"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "a_utf16le.txt"), utf16le, 0o644)
	os.WriteFile(filepath.Join(dir, "b_utf16be.txt"), utf16be, 0o644)
	os.WriteFile(filepath.Join(dir, "c_gbk.txt"), gbkBytes, 0o644)
	os.WriteFile(filepath.Join(dir, "d_cr.txt"), []byte("one\r世界\rthree\r"), 0o644)

	hugePath := filepath.Join(dir, "e_huge.log")
	f, err := os.Create(hugePath)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("世界\n")
	if err := f.Truncate(largeFile + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	m, err := newMatcher("世界$", true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	res := searchFiles(context.Background(), m, scope{paths: []string{dir}}, nil, "GBK", maxResults)
	if res.searched != 4 || res.matches != 4 || len(res.files) != 4 {
		t.Fatalf("encoding search: searched=%d matches=%d files=%d, want 4/4/4", res.searched, res.matches, len(res.files))
	}
	if gotLine := res.files[3].lines[0].line; gotLine != 1 {
		t.Fatalf("CR file line = %d, want 1", gotLine)
	}
}

func TestFindInFilesParallelOrderAndLimit(t *testing.T) {
	dir := t.TempDir()
	for i := range 40 {
		name := fmt.Sprintf("file_%02d.txt", i)
		os.WriteFile(filepath.Join(dir, name), []byte("hit hit\n"), 0o644)
	}
	m, _ := newMatcher("hit", false, true, false)
	res := searchFiles(context.Background(), m, scope{paths: []string{dir}}, nil, defaultFallback, 15)
	if res.matches != 15 {
		t.Fatalf("matches = %d, want 15", res.matches)
	}
	if len(res.files) != 8 {
		t.Fatalf("files = %d, want 8", len(res.files))
	}
	for i, fh := range res.files {
		want := fmt.Sprintf("file_%02d.txt", i)
		if filepath.Base(fh.src.path) != want {
			t.Fatalf("file[%d] = %q, want %q", i, filepath.Base(fh.src.path), want)
		}
	}
	if got := len(res.files[7].lines[0].spans); got != 1 {
		t.Fatalf("last file spans = %d, want 1", got)
	}
}

func TestFindInFilesWhereTrailingSlash(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	os.WriteFile(file, []byte("disk only\n"), 0o644)

	a := newApp("")
	a.open([]string{file})
	a.current().st.Buffer.Set("unsaved_token\n")
	a.fif.query = "unsaved_token"
	a.fif.where = dir + string(filepath.Separator) + ", " + openFiles

	m, sc, open, ctx := a.prepareSearch(&a.fif.finding)
	res := searchFiles(ctx, m, sc, open, a.set.FallbackEncoding, maxResults)
	if res.searched != 1 || res.matches != 1 || len(res.files) != 1 {
		t.Fatalf("trailing slash Where: searched=%d matches=%d files=%d, want 1/1/1", res.searched, res.matches, len(res.files))
	}
}

func TestFindInFilesWherePathGlobs(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{
		"root.go",
		"sub/keep.go",
		"sub/skip.txt",
		"sub/ignored_dir/nope.go",
		"other/sub/nope.go",
		"tests/fixtures/data.go",
		"tests/unit_test.go",
		"ignored_dir/nope.go",
	} {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("needle\n"), 0o644)
	}

	m, _ := newMatcher("needle", false, true, false)

	sc1 := parseWhere(dir+", sub/*.go, -ignored_dir/", nil)
	res1 := searchFiles(context.Background(), m, sc1, nil, defaultFallback, maxResults)
	var got1 []string
	for _, fh := range res1.files {
		rel, _ := filepath.Rel(dir, fh.src.path)
		got1 = append(got1, filepath.ToSlash(rel))
	}
	if !reflect.DeepEqual(got1, []string{"sub/keep.go"}) {
		t.Fatalf("Where sub/*.go, -ignored_dir/: got %v, want [sub/keep.go]", got1)
	}

	sc2 := parseWhere(dir+", -tests/fixtures, -ignored_dir/", nil)
	res2 := searchFiles(context.Background(), m, sc2, nil, defaultFallback, maxResults)
	var got2 []string
	for _, fh := range res2.files {
		rel, _ := filepath.Rel(dir, fh.src.path)
		got2 = append(got2, filepath.ToSlash(rel))
	}
	want2 := []string{"other/sub/nope.go", "root.go", "sub/keep.go", "sub/skip.txt", "tests/unit_test.go"}
	if !reflect.DeepEqual(got2, want2) {
		t.Fatalf("Where -tests/fixtures, -ignored_dir/: got %v, want %v", got2, want2)
	}
}

func TestFormatResultsUTF8TruncationSpans(t *testing.T) {
	longLine := strings.Repeat("a", maxResultLine-2) + "needle" + strings.Repeat("x", 20)
	res := found{
		searched: 1,
		matches:  3,
		files: []fileHits{{
			src: source{name: "long.txt", path: "/long.txt"},
			lines: []lineHit{{
				line: 0,
				text: longLine,
				spans: [][2]int{
					{maxResultLine - 2, maxResultLine + 4},
					{maxResultLine, maxResultLine + 2},
					{maxResultLine + 1, maxResultLine + 3},
				},
			}},
		}},
	}
	text, _, spans := formatResults("header", res)
	if len(spans) != 1 {
		t.Fatalf("got %d spans (%v), want 1 (only the span starting before cut)", len(spans), spans)
	}
	sp := spans[0]
	if !utf8.RuneStart(text[sp[0]]) || (sp[1] < len(text) && !utf8.RuneStart(text[sp[1]])) {
		t.Fatalf("span %v lands mid-rune in %q", sp, text[max(0, sp[0]-4):min(len(text), sp[1]+6)])
	}
	if got := text[sp[0]:sp[1]]; got != "ne" {
		t.Fatalf("highlighted slice = %q, want %q (must not include ellipsis)", got, "ne")
	}
	if !strings.HasPrefix(text[sp[1]:], "…\n") {
		t.Fatalf("expected ellipsis immediately after clamped span, got %q", text[sp[1]:min(len(text), sp[1]+6)])
	}
}

func TestFindInFilesReplaceEnterSubmitsReplace(t *testing.T) {
	a := newApp("")
	a.newFile()
	a.current().st.Buffer.Set("alpha\n")
	tt := ui.NewTester(a.view, 900, 600)

	tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	tt.Type("nomatch")
	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeyTab)
	tt.Type("replacement")
	if a.fif.replace != "replacement" {
		t.Fatalf("Replace field did not receive focus via Tab: replace=%q where=%q query=%q", a.fif.replace, a.fif.where, a.fif.query)
	}
	tt.Key(0, ui.KeyEnter)
	if a.fif.tab != nil {
		t.Fatalf("Enter in Replace field opened Find Results tab instead of running replaceInFiles")
	}
	if !a.fif.open {
		t.Fatalf("Enter in Replace field closed panel (searchInFiles behavior) instead of running replaceInFiles")
	}
	if a.notice.text != locales.PanelNoMatches() {
		t.Fatalf("notice = %q, want %q", a.notice.text, locales.PanelNoMatches())
	}
}

func TestFindInFilesContextAndScope(t *testing.T) {
	m, err := newMatcher(`a|(\Bfoo)`, true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	b := editor.NewBuffer("afoo")
	if got := m.find(b.String(), 2); len(got) != 2 {
		t.Fatalf("matches %v", got)
	}
	if m.replace(b, "<$1>") != 2 || b.String() != "<><foo>" {
		t.Fatalf("replacement %q", b.String())
	}
	m, err = newMatcher("�", false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.find("\xff", 1); len(got) != 1 || got[0][1] != 1 {
		t.Fatalf("invalid UTF-8: %v", got)
	}
	dir := t.TempDir()
	sc := parseWhere(openFolders+", "+openFiles+", -sub/*.go", []string{dir})
	if sc.wantsOpen(source{name: "a.go", path: filepath.Join(dir, "sub", "a.go")}) {
		t.Fatal("included excluded open tab")
	}
	ig := fifIgnore{layers: []fifIgnoreLayer{{dir: dir, rules: fifParseIgnore("file[!0-9].txt")}}}
	if !ig.ignored(filepath.Join(dir, "filea.txt"), false) || ig.ignored(filepath.Join(dir, "file1.txt"), false) {
		t.Fatal("wrong negated class")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.go"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, alias); err == nil && sc.wantsOpen(source{name: "a.go", path: filepath.Join(alias, "sub", "a.go")}) {
		t.Fatal("included excluded alias")
	}
	if !fifMatchGlob(fifIgnorePattern("file[!]].txt"), "filea.txt") || fifMatchGlob(fifIgnorePattern("dir/a[!x]b"), "dir/a/b") {
		t.Fatal("incorrect class or path separator match")
	}
	subAlias := filepath.Join(t.TempDir(), "subAlias")
	if err := os.Symlink(filepath.Join(dir, "sub"), subAlias); err == nil {
		sc := parseWhere(dir+", "+subAlias+", "+openFiles+", -nested/*.go", []string{dir})
		nested := filepath.Join(dir, "sub", "nested")
		if err := os.Mkdir(nested, 0755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(nested, "a.go")
		if err := os.WriteFile(file, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if sc.wantsOpen(source{name: "a.go", path: file}) {
			t.Fatal("lost explicit root through alias")
		}
	}
	if !fifMatchGlob(fifIgnorePattern("dir/a[!x/]b"), "dir/azb") || !fifMatchGlob(`dir\/file`, "dir/file") {
		t.Fatal("lost slash in class or escaped separator")
	}
	for _, name := range []string{"dir/keep.txt", "dir/x/keep.txt", "dir/x/y/keep.txt"} {
		if got := fifMatchGlob(`dir/**\/keep.txt`, name); got != (name != "dir/keep.txt") {
			t.Fatalf("escaped globstar %s: %v", name, got)
		}
	}
	p := filepath.Join(dir, "mixed")
	if err := os.WriteFile(p, []byte("first\rneedle\r\nthird\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, ok := readSearched(p, defaultFallback); !ok || got != "first\nneedle\nthird\n" {
		t.Fatalf("mixed endings %q", got)
	}
}

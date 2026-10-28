package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// Find in Files looks in the open folders, in a tab's unsaved text rather
// than its file, past binary files and the names Where leaves out. Its
// matched lines go to one Find Results tab, from which Enter and F4 go to
// the matches.
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

// A file on disk matches a regular expression as its tab would, \r\n
// read as \n, and a link to a file is the file: its tab's text is searched
// once.
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

// Replace in Files replaces in the tabs' text, a regular expression's $1
// expanded, opening the files not open, and saves none.
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
	res := searchFiles(ctx, m, sc, open, math.MaxInt)
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

// Replacing in a file and a link to it replaces in the file's tab once,
// with its unsaved text, and opens no second tab that Save All would
// write over it with.
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
	res := searchFiles(ctx, m, sc, open, math.MaxInt)
	a.replaceFound(m, "bar", res)
	if got := d.st.Buffer.String(); a.docCount() != 1 || got != "bar\nunsaved bar\n" {
		t.Fatalf("%d tabs, AGENTS.md %q; want one tab with both replaced", a.docCount(), got)
	}
}

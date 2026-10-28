package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// Find in Files looks in the open folders, in a tab's unsaved text rather
// than its file, past binary files and the names Where leaves out. Its
// matched lines go to one Find Results tab, from which Enter goes to the
// matches.
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

	n := a.docCount()
	a.showFindInFiles("")
	a.searchInFiles()
	if a.docCount() != n || a.current() != res {
		t.Fatalf("a second search opened another tab: %d tabs, want %d", a.docCount(), n)
	}
}

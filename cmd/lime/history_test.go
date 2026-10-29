package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// File History lists the commits of a file across its renames, each of
// which opens at the file's diff. An untracked file has none to show.
func TestFileHistory(t *testing.T) {
	dir, git := newRepo(t)
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", strings.Repeat("a\n", 200))
	write("old name.txt", "one\n")
	git("add", ".")
	git("commit", "-q", "-m", "first")
	write("a.txt", "b\n")
	git("commit", "-q", "-am", "only a")
	git("mv", "old name.txt", "new name.txt")
	git("commit", "-q", "-m", "rename")
	write("new name.txt", "one\ntwo\n")
	git("commit", "-q", "-am", "edit")
	write("untracked.txt", "u\n")
	file, untracked := filepath.Join(dir, "new name.txt"), filepath.Join(dir, "untracked.txt")

	a := newApp("")
	a.open([]string{dir, untracked, file})
	a.reloadChanged()
	tt := ui.NewTester(a.view, 900, 600)
	if a.tracked(untracked) || !a.tracked(file) {
		t.Fatalf("tracked: %v, %v", a.tracked(untracked), a.tracked(file))
	}
	a.run("sublime_merge_file_history")
	tt.Frame()
	var subjects []string
	for _, cm := range a.hist.commits {
		subjects = append(subjects, cm.subject)
	}
	if strings.Join(subjects, ",") != "edit,rename,first" || strings.Join(a.hist.paths, ",") != "new name.txt,new name.txt,old name.txt" {
		t.Fatalf("history: %q at %q", subjects, a.hist.paths)
	}

	tt.Click("first")
	tt.Frame()
	if d := a.current(); d.commit == nil || d.commit.subject != "first" || !tt.HasText("old name.txt") {
		t.Fatalf("the commit's tab shows no old name.txt: %q", tt.Texts())
	}

	tt.Click(locales.HistoryAllCommits())
	tt.Frame()
	if a.hist.file != "" || len(a.hist.commits) != 4 {
		t.Fatalf("all commits: %d", len(a.hist.commits))
	}
}

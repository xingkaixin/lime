package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The side bar marks files and folders as git status has them, in the
// work tree each folder is in, from its top or below it, and in the
// repositories in it; and none outside a repository.
func TestGitMarks(t *testing.T) {
	dir, git := newRepo(t)
	write := func(name, text string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "node_modules\n*.log\n")
	write("src/a.go", "a\n")
	write("src/b c.go", "b\n")
	write("keep.txt", "k\n")
	git("add", ".")
	git("commit", "-q", "-m", "base")
	write("src/a.go", "a2\n")
	git("rm", "-q", "--cached", "src/b c.go")
	write("new/n.txt", "n\n")
	write("node_modules/x/i.js", "i\n")
	write("debug.log", "d\n")
	inner := filepath.Join(dir, "inner")
	write("inner/f", "f\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-q", "-m", "inner"}} {
		if _, err := runGit(inner, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	write("inner/f", "f2\n")
	wt := filepath.Join(t.TempDir(), "wt")
	git("worktree", "add", "-q", wt)
	os.WriteFile(filepath.Join(wt, "keep.txt"), []byte("k2\n"), 0o644)
	plain := t.TempDir()
	os.WriteFile(filepath.Join(plain, "p.txt"), nil, 0o644)

	a := newApp("")
	a.side = filesPanel
	src := filepath.Join(dir, "src")
	for _, f := range []string{src, dir, wt, plain} {
		a.addFolder(f)
	}
	ui.NewTester(a.view, 800, 500).Frame()
	for p, want := range map[string]gitMark{
		src:                                gitModified,
		filepath.Join(src, "a.go"):         gitModified,
		filepath.Join(src, "b c.go"):       gitDeleted,
		dir:                                gitModified,
		filepath.Join(dir, "keep.txt"):     gitClean,
		filepath.Join(dir, "new"):          gitAdded,
		filepath.Join(dir, "new", "n.txt"): gitAdded,
		filepath.Join(dir, "node_modules"): gitIgnored,
		filepath.Join(dir, "node_modules", "x", "i.js"): gitIgnored,
		filepath.Join(dir, "debug.log"):                 gitIgnored,
		inner:                                           gitAdded,
		filepath.Join(inner, "f"):                       gitModified,
		filepath.Join(wt, "keep.txt"):                   gitModified,
		filepath.Join(wt, "src", "a.go"):                gitClean,
		filepath.Join(plain, "p.txt"):                   gitClean,
	} {
		if got := a.marks.mark(p); got != want {
			t.Errorf("%s: mark %d, want %d", p, got, want)
		}
	}
}

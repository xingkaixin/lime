package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The gutter marks a tracked file's changes against the index, or HEAD,
// which saving leaves as they are, and an untracked file's against the
// text on disk. Show Unsaved Changes compares with the text on disk, as
// saving leaves it.
func TestMiniDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	file, untracked := filepath.Join(dir, "a.txt"), filepath.Join(dir, "new file.txt")
	git := func(args ...string) {
		t.Helper()
		if _, err := runGit(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(file, []byte("one\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "first")
	os.WriteFile(file, []byte("one\ntwo\n"), 0o644)
	git("add", ".")
	os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(untracked, []byte("new\n"), 0o644)

	a := newApp("")
	// Before the file, whose text git reads after it.
	a.open([]string{untracked, file})
	a.reloadChanged()
	base := func(path string) string { return a.docAt(path).st.DiffBase.String() }
	if base(file) != "one\ntwo\n" || base(untracked) != "new\n" {
		t.Fatalf("bases: %q, %q", base(file), base(untracked))
	}

	d := a.docAt(file)
	d.st.Buffer.Set("one\ntwo\nthree\nfour\n")
	a.save(d)
	if base(file) != "one\ntwo\n" {
		t.Fatalf("saved: %q", base(file))
	}

	d.st.Buffer.Set("zero\n")
	a.showUnsavedChanges(d)
	if cmp := a.current().cmp; cmp == nil || cmp.diff.Old.String() != "one\ntwo\nthree\nfour\n" {
		t.Fatal("Show Unsaved Changes compares with no text on disk")
	}
	a.save(d)
	if got := a.current().cmp.diff.Old.String(); got != "zero\n" {
		t.Fatalf("Show Unsaved Changes after saving: %q", got)
	}

	a.set.GitDiffTarget = "head"
	a.reloadChanged()
	if base(file) != "one\n" {
		t.Fatalf("head: %q", base(file))
	}
}

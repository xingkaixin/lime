package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A permalink names the file at HEAD's commit on GitHub, from a remote's
// URL in any of its forms, and tells whether the remote has the commit and
// whether the file has changed since.
func TestPermalink(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:o/r.git":         "https://github.com/o/r",
		"ssh://git@github.com:22/o/r":    "https://github.com/o/r",
		"https://github.com/o/r.git":     "https://github.com/o/r",
		"https://u:token@github.com/o/r": "https://github.com/o/r",
		"git@gitlab.com:o/r.git":         "",
		"https://github.com/o":           "",
	} {
		if got, _ := githubWeb(remote); got != want {
			t.Errorf("githubWeb(%q) = %q, want %q", remote, got, want)
		}
	}

	dir, git := newRepo(t)
	file := filepath.Join(dir, "a dir", "b.go")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "first")
	if _, _, _, err := readPermalink(file, 0, 0); err != errNoGitHub {
		t.Fatalf("no GitHub: %v", err)
	}
	git("remote", "add", "origin", "git@github.com:o/r.git")
	head, _ := runGit(dir, "rev-parse", "HEAD")
	head = strings.TrimSpace(head)
	link, pushed, same, err := readPermalink(file, 0, 1)
	if want := "https://github.com/o/r/blob/" + head + "/a%20dir/b.go#L1-L2"; err != nil || link != want || pushed || !same {
		t.Fatalf("permalink: %q, %v, %v, %v", link, pushed, same, err)
	}
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	if err := os.WriteFile(file, []byte("a\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if link, pushed, same, _ = readPermalink(file, 2, 2); !strings.HasSuffix(link, "#L3") || !pushed || same {
		t.Fatalf("pushed and changed: %q, %v, %v", link, pushed, same)
	}
}

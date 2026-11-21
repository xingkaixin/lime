package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"golang.org/x/text/encoding/simplifiedchinese"

	"lime/locales"
)

func TestParseCommitChangesTruncatedRename(t *testing.T) {
	for _, out := range []string{
		"R100\x00old.txt",
		"C095\x00src.txt",
		"M\x00a.txt\x00R100\x00only_old.txt",
	} {
		got := parseCommitChanges(out)
		if strings.HasPrefix(out, "M\x00") {
			if len(got) != 1 || got[0] != (change{status: 'M', path: "a.txt"}) {
				t.Fatalf("parseCommitChanges(%q) = %+v", out, got)
			}
		} else if len(got) != 0 {
			t.Fatalf("parseCommitChanges(%q) = %+v, want empty", out, got)
		}
	}

	got := parseCommitChanges("R100\x00old.txt\x00new.txt\x00M\x00a.txt\x00")
	want := []change{
		{status: 'R', oldPath: "old.txt", path: "new.txt"},
		{status: 'M', path: "a.txt"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parseCommitChanges = %+v, want %+v", got, want)
	}
}

func TestChangesRowsCacheInvalidation(t *testing.T) {
	w := changes{
		files: [3][]change{
			unstaged: {
				{status: 'M', path: "src/b.go"},
				{status: 'M', path: "src/a.go"},
			},
		},
	}

	w.ensureRows(false)
	if len(w.rows) != 3 || w.rowsTree {
		t.Fatalf("flat rows = %+v (tree=%v)", w.rows, w.rowsTree)
	}
	w.rows[0].depth = 99
	w.ensureRows(false)
	if w.rows[0].depth != 99 {
		t.Fatal("ensureRows rebuilt rows on cache hit")
	}

	w.ensureRows(true)
	if !w.rowsTree || w.rows[0].depth != 0 || len(w.rows) != 4 {
		t.Fatalf("tree rows = %+v (tree=%v)", w.rows, w.rowsTree)
	}

	w.toggle(changeKey{unstaged, "src/"}, false)
	w.ensureRows(true)
	if len(w.rows) != 2 {
		t.Fatalf("after closing src/: rows = %+v", w.rows)
	}

	w.files[unstaged] = append(w.files[unstaged], change{status: '?', path: "top.txt"})
	w.dataGen++
	w.ensureRows(true)
	if len(w.rows) != 3 {
		t.Fatalf("after adding top.txt: rows = %+v", w.rows)
	}
}

func TestGitDirMultiFolder(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	nested := filepath.Join(dirA, "sub")
	os.MkdirAll(nested, 0o755)
	fileA := filepath.Join(dirA, "a.txt")
	fileB := filepath.Join(dirB, "b.txt")
	fileNested := filepath.Join(nested, "n.txt")
	outside := filepath.Join(t.TempDir(), "out.txt")
	for _, p := range []string{fileA, fileB, fileNested, outside} {
		os.WriteFile(p, []byte("x\n"), 0o644)
	}

	a := newApp("")
	if got := a.gitDir(); got != "" {
		t.Fatalf("0 folders: gitDir = %q, want empty", got)
	}

	a.addFolder(dirA)
	if got := a.gitDir(); got != dirA {
		t.Fatalf("1 folder: gitDir = %q, want %q", got, dirA)
	}

	a.addFolder(dirB)
	a.addFolder(nested)
	if got := a.gitDir(); got != dirA {
		t.Fatalf("multi-folder fallback: gitDir = %q, want %q", got, dirA)
	}

	a.open([]string{fileB})
	if got := a.gitDir(); got != dirB {
		t.Fatalf("active file in dirB: gitDir = %q, want %q", got, dirB)
	}

	a.open([]string{fileNested})
	if got := a.gitDir(); got != nested {
		t.Fatalf("active file in nested folder: gitDir = %q, want %q", got, nested)
	}

	a.open([]string{outside})
	if got := a.gitDir(); got != dirA {
		t.Fatalf("active file outside folders: gitDir = %q, want %q", got, dirA)
	}
}

func TestGitTintCacheConsistency(t *testing.T) {
	marks := []gitMark{gitAdded, gitDeleted, gitModified, gitConflict}
	for _, s := range schemes {
		for _, m := range marks {
			want := s.computeGitTint(m)
			first := s.gitTint(m)
			second := s.gitTint(m)
			if first != want || second != want {
				t.Fatalf("scheme %q mark %d: got %v / %v, want %v", s.name, m, first, second, want)
			}
		}
	}
}

func TestHunkActionsUseCommitViewRoot(t *testing.T) {
	repoA, gitA := newRepo(t)
	repoB, gitB := newRepo(t)
	if r, err := filepath.EvalSymlinks(repoA); err == nil {
		repoA = r
	}
	if r, err := filepath.EvalSymlinks(repoB); err == nil {
		repoB = r
	}
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, strconv.Itoa(i))
	}
	base := strings.Join(lines, "\n") + "\n"
	for _, pair := range []struct {
		dir string
		git func(...string)
	}{{repoA, gitA}, {repoB, gitB}} {
		os.WriteFile(filepath.Join(pair.dir, "a.txt"), []byte(base), 0o644)
		pair.git("add", ".")
		pair.git("commit", "-q", "-m", "base")
	}
	modA := slices.Clone(lines)
	modA[1], modA[17] = "twoA", "eighteenA"
	os.WriteFile(filepath.Join(repoA, "a.txt"), []byte(strings.Join(modA, "\n")+"\n"), 0o644)
	modB := slices.Clone(lines)
	modB[1] = "twoB"
	os.WriteFile(filepath.Join(repoB, "a.txt"), []byte(strings.Join(modB, "\n")+"\n"), 0o644)

	a := newApp("")
	a.open([]string{repoB})
	tt := ui.NewTester(a.view, 900, 600)
	cv := a.showCommitIn(repoA, commit{hash: unstagedChanges})
	tt.Frame()
	if cv.root != repoA || a.git.root != repoA || len(cv.files) != 1 || len(cv.files[0].hunks) != 2 {
		t.Fatalf("setup: cv.root=%q a.git.root=%q files=%+v", cv.root, a.git.root, cv.files)
	}

	if err := tt.Click(locales.ChangesStage()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if out, _ := runGit(repoA, "diff", "--cached", "--name-only"); strings.TrimSpace(out) != "a.txt" {
		t.Fatalf("repoA staged = %q, want a.txt", out)
	}
	if out, _ := runGit(repoB, "diff", "--cached", "--name-only"); strings.TrimSpace(out) != "" {
		t.Fatalf("repoB should remain unstaged, got %q", out)
	}

	a.discardHunkIn(cv.root, "a.txt", cv.files[0].hunks[0])
	tt.Frame()
	if out, _ := runGit(repoA, "diff", "--name-only"); strings.TrimSpace(out) != "" {
		t.Fatalf("repoA unstaged after discard = %q, want empty", out)
	}
	if out, _ := runGit(repoB, "diff", "--name-only"); strings.TrimSpace(out) != "a.txt" {
		t.Fatalf("repoB unstaged should remain a.txt, got %q", out)
	}
}

func TestCommittingDoubleSubmitGuard(t *testing.T) {
	dir, git := newRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "base")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("2\n"), 0o644)
	git("add", "a.txt")

	a := newApp("")
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	tt.Click(locales.SideBarGitPanel())
	tt.Frame()
	tt.Click(locales.ChangesMessage())
	tt.Type("Second commit")

	a.changes.committing = true
	if _, _, ready := a.changes.commitArgs(); ready {
		t.Fatal("commitArgs reported ready while committing is true")
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Click(locales.ChangesCommit())
	tt.Frame()
	if out, _ := runGit(dir, "rev-list", "--count", "HEAD"); strings.TrimSpace(out) != "1" {
		t.Fatalf("commit count = %q while committing was true, want 1", out)
	}

	a.changes.committing = false
	tt.Frame()
	tt.Click(locales.ChangesMessage())
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if a.changes.committing {
		t.Fatal("committing remained true after commit completed")
	}
	if out, _ := runGit(dir, "rev-list", "--count", "HEAD"); strings.TrimSpace(out) != "2" {
		t.Fatalf("commit count = %q after commit, want 2", out)
	}
}

func TestReadCommitBoundedAndFallbackEncoding(t *testing.T) {
	dir, git := newRepo(t)
	os.WriteFile(filepath.Join(dir, "init.txt"), []byte("init\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "init")

	gbkBytes, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("你好\n"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "gbk.txt"), gbkBytes, 0o644)

	big := filepath.Join(dir, "huge.txt")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(largeFile + 1024); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	_, files := readCommit(dir, unstagedChanges, "GBK")
	byName := map[string]*commitFile{}
	for _, cf := range files {
		byName[cf.path] = cf
	}
	if cf := byName["gbk.txt"]; cf == nil || cf.binary || len(cf.new.lines) != 1 || cf.new.lines[0] != "你好" {
		t.Fatalf("gbk.txt = %+v", cf)
	}
	if cf := byName["huge.txt"]; cf == nil || !cf.folded || len(cf.new.lines) != 0 {
		t.Fatalf("huge.txt should be folded and unread, got %+v", cf)
	}
	git("add", "huge.txt")
	if err := os.WriteFile(big, []byte("small"), 0600); err != nil {
		t.Fatal(err)
	}
	_, files = readCommit(dir, unstagedChanges, "GBK")
	for _, f := range files {
		if f.path == "huge.txt" {
			if !f.folded || len(f.old.lines) > 0 || len(f.hunks) > 0 {
				t.Fatal("loaded oversized index version")
			}
			return
		}
	}
	t.Fatal("oversized file missing from diff")

}

func TestRemoteDefaultNameAndRepoLock(t *testing.T) {
	dir, git := newRepo(t)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	upstream := t.TempDir()
	if _, err := runGit(upstream, "init", "-q", "--bare", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	git("remote", "add", "aaa", filepath.Join(dir, "missing"))
	git("remote", "add", "upstream", upstream)
	git("config", "remote.pushDefault", "upstream")
	git("commit", "--allow-empty", "-m", "init")
	a := newApp("")
	a.git = readRepo(dir)
	a.push()
	if got, _ := runGit(dir, "rev-parse", "--abbrev-ref", "@{upstream}"); strings.TrimSpace(got) != "upstream/main" {
		t.Fatalf("upstream=%q", got)
	}

	dir2 := t.TempDir()
	if repoLock(dir) != repoLock(filepath.Join(dir, ".")) {
		t.Fatal("repoLock returned different mutexes for the same cleaned dir")
	}
	if repoLock(dir) == repoLock(dir2) {
		t.Fatal("repoLock returned the same mutex for different dirs")
	}
}

func TestHistoryVirtualizedList(t *testing.T) {
	a := newApp("")
	a.side = gitPanel
	a.gitTab = historyTab
	a.proj.folders = []string{"/repo"}
	a.git = gitRepo{dir: "/repo", root: "/repo", tree: mainTree, branch: "main"}
	a.hist.root = "/repo"
	now := time.Now()
	for i := range maxCommits {
		a.hist.commits = append(a.hist.commits, commit{
			hash:    fmt.Sprintf("%040d", i),
			short:   fmt.Sprintf("%07d", i),
			subject: fmt.Sprintf("commit-%03d", i),
			author:  "dev",
			email:   "dev@example.com",
			when:    now.Add(-time.Duration(i) * time.Minute),
		})
	}
	tt := ui.NewTester(a.view, 900, 600)
	tt.Frame()
	if !tt.HasText("commit-000") {
		t.Fatalf("expected top commit to render, texts: %q", tt.Texts())
	}
	if tt.HasText("commit-299") {
		t.Fatal("expected history list to be virtualized and not build all 300 rows")
	}
}

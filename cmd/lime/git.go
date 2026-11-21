package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"lime/locales"
)

type gitRepo struct {
	dir           string
	root          string
	tree          string
	branch        string
	ahead, behind int
	gen           int
}

// mainTree names the repository's main work tree, as Zed does.
const mainTree = "main"

// runGit runs git in dir, returning what it printed, or what it said of
// a failure. It blocks, so call it off the main thread.
func runGit(dir string, args ...string) (string, error) { return gitWith(dir, "", args...) }

func readRepo(dir string) gitRepo {
	r := gitRepo{dir: dir}
	var out, head, counts string
	var err, headErr, countsErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		out, err = runGit(dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir")
	})
	wg.Go(func() { head, headErr = runGit(dir, "symbolic-ref", "--quiet", "--short", "HEAD") })
	wg.Go(func() { counts, countsErr = runGit(dir, "rev-list", "--left-right", "--count", "HEAD...@{upstream}") })
	wg.Wait()
	paths := strings.Split(strings.TrimSpace(out), "\n")
	if err != nil || len(paths) != 3 {
		return r
	}
	r.root, r.tree = filepath.Clean(paths[0]), mainTree
	// A linked work tree keeps its own git dir in the main one's.
	if paths[1] != paths[2] {
		r.tree = filepath.Base(r.root)
	}
	if headErr != nil {
		head, _ = runGit(dir, "rev-parse", "--short", "HEAD")
	}
	r.branch = strings.TrimSpace(head)
	if countsErr == nil {
		fmt.Sscan(counts, &r.ahead, &r.behind)
	}
	return r
}

func (a *app) gitDir() string {
	if d := a.current(); d != nil && d.commit != nil {
		return d.commit.root
	}
	switch len(a.proj.folders) {
	case 0:
		return ""
	case 1:
		return a.proj.folders[0]
	}
	if d := a.current(); d != nil && d.path != "" {
		best := ""
		for _, f := range a.proj.folders {
			if within(d.path, f) && len(f) > len(best) {
				best = f
			}
		}
		if best != "" {
			return best
		}
	}
	return a.proj.folders[0]
}

// readGit reads the repository of the project's folder again, keeping
// what shows until it is read.
func (a *app) readGit() {
	dir := a.gitDir()
	if dir != a.git.dir {
		a.git = gitRepo{dir: dir, gen: a.git.gen + 1}
	}
	if dir == "" {
		return
	}
	refresh := func() {
		if a.side == gitPanel {
			a.readHistory()
		}
		a.readWorktree()
	}
	// The repository rarely moves under a folder, so what it shows is read
	// at once, and again only if its root turns out to have changed.
	early := a.git.root != ""
	if early {
		refresh()
	}
	a.git.gen++
	gen := a.git.gen
	a.background(func() func() {
		r := readRepo(dir)
		return func() {
			if a.git.gen != gen {
				return
			}
			r.gen = gen
			moved := r.root != a.git.root
			a.git = r
			if !early || moved {
				refresh()
			}
		}
	})
}

// background runs work off the main thread, and what it returns on it.
// A window that never opened, as in tests, runs both at once.
func (a *app) background(work func() func()) {
	if a.win == nil {
		work()()
		return
	}
	go func() {
		done := work()
		a.win.Update(done)
	}()
}

func (a *app) gitRun(args ...string) { a.gitRunWith("", nil, args...) }

// repoLocks lets one git write run at a time per repository root: two at
// once, as check boxes clicked quickly start, would meet on index.lock,
// while writes in different repositories do not block one another.
var (
	repoLocksMu sync.Mutex
	repoLocks   = map[string]*sync.Mutex{}
)

func repoLock(dir string) *sync.Mutex {
	key := filepath.Clean(dir)
	repoLocksMu.Lock()
	defer repoLocksMu.Unlock()
	mu := repoLocks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		repoLocks[key] = mu
	}
	return mu
}

func (a *app) gitRunWith(input string, done func(), args ...string) {
	a.gitRunIn(a.git.root, input, done, args...)
}

func (a *app) gitRunIn(dir, input string, done func(), args ...string) {
	a.gitRunFinish(dir, input, func(err error) {
		if err == nil && done != nil {
			done()
		}
	}, args...)
}

func (a *app) gitRunFinish(dir, input string, finish func(error), args ...string) {
	a.background(func() func() {
		mu := repoLock(dir)
		mu.Lock()
		_, err := gitWith(dir, input, args...)
		mu.Unlock()
		return func() {
			if err != nil {
				msg, _, _ := strings.Cut(err.Error(), "\n")
				a.notifyError(msg)
			}
			if finish != nil {
				finish(err)
			}
			a.refreshFolders()
			for _, w := range a.l.wins {
				w.reloadChanged()
			}
			a.readGit()
			if dir != "" && dir != a.git.root {
				for _, d := range a.allDocs() {
					if cv := d.commit; cv != nil && cv.worktree() && cv.root == dir {
						a.loadCommit(cv)
					}
				}
			}
		}
	})
}

type branches struct {
	local, remote []string
	elsewhere     map[string]string
}

func readBranches(root string) branches {
	b := branches{elsewhere: map[string]string{}}
	// lstrip=2 keeps a branch's name whole, where :short would make it
	// heads/name if a tag shared it.
	out, _ := runGit(root, "for-each-ref", "--sort=-committerdate", "--format=%(refname:lstrip=2)%09%(worktreepath)", "refs/heads")
	have := map[string]bool{}
	for line := range strings.Lines(out) {
		name, tree, _ := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		if name == "" {
			continue
		}
		b.local, have[name] = append(b.local, name), true
		if tree != "" && filepath.Clean(tree) != root {
			b.elsewhere[name] = tree
		}
	}
	out, _ = runGit(root, "for-each-ref", "--sort=-committerdate", "--format=%(refname:lstrip=2)", "refs/remotes")
	for _, name := range strings.Fields(out) {
		_, branch, ok := strings.Cut(name, "/")
		if !ok || branch == "HEAD" || have[branch] {
			continue
		}
		have[branch] = true
		b.remote = append(b.remote, name)
	}
	return b
}

type stash struct{ ref, subject string }

func readStashes(root string) []stash {
	out, _ := runGit(root, "stash", "list", "--format=%gd%x09%gs")
	var ss []stash
	for line := range strings.Lines(out) {
		ref, subject, ok := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		if ok {
			ss = append(ss, stash{ref, subject})
		}
	}
	return ss
}

type worktree struct{ path, name, branch string }

func readWorktrees(root string) []worktree {
	out, _ := runGit(root, "worktree", "list", "--porcelain")
	var ws []worktree
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\n")
		switch k, v, _ := strings.Cut(line, " "); k {
		case "worktree":
			w := worktree{path: filepath.Clean(v), name: filepath.Base(v)}
			// git lists the main work tree first.
			if len(ws) == 0 {
				w.name = mainTree
			}
			ws = append(ws, w)
		case "branch":
			ws[len(ws)-1].branch = strings.TrimPrefix(v, "refs/heads/")
		case "bare":
			ws = ws[:len(ws)-1]
		}
	}
	return ws
}

// showBranches lists the branches to switch to and the stashes to apply,
// as Zed's branch picker does.
func (a *app) showBranches() {
	root, cur := a.git.root, a.git.branch
	a.background(func() func() {
		b, ss := readBranches(root), readStashes(root)
		return func() {
			var items []item
			for _, name := range b.local {
				it := item{label: name, pick: func() { a.gitRunIn(root, "", nil, "switch", name) }}
				switch tree, ok := b.elsewhere[name]; {
				case name == cur:
					it.hint, it.pick = locales.GitCurrent(), nil
				case ok:
					it.detail, it.pick = tildePath(tree), func() { a.switchWorktree(tree) }
				}
				items = append(items, it)
			}
			for _, name := range b.remote {
				items = append(items, item{label: name, detail: locales.GitRemoteBranch(), pick: func() {
					// The ref in full, which no tag of the same name shadows.
					a.gitRunIn(root, "", nil, "switch", "--track", "refs/remotes/"+name)
				}})
			}
			for _, s := range ss {
				items = append(items, item{label: s.subject, detail: s.ref, hint: locales.GitStash(), pick: func() {
					a.gitRunIn(root, "", nil, "stash", "apply", s.ref)
				}})
			}
			a.openPanel(&panel{placeholder: locales.GitBranchPlaceholder(), list: func(q string) []item { return ranked(items, byLabel(q)) }})
		}
	})
}

func inRepo(fn func(a *app)) func(a *app) {
	return func(a *app) {
		if a.git.root == "" {
			a.notifyError(locales.SideBarNoRepository())
			return
		}
		fn(a)
	}
}

// realGit keeps a command out of the website's command palette, whose
// git (website/git.js) has no branches or remotes.
func realGit(caption func(locales.Messages) string) func(locales.Messages) string {
	if runtime.GOOS == "js" {
		return nil
	}
	return caption
}

func (a *app) promptCreateBranch() {
	root := a.git.root
	a.openPanel(&panel{prompt: locales.PromptBranchName(), submit: func(name string) {
		if name = strings.TrimSpace(name); name != "" {
			a.gitRunIn(root, "", nil, "switch", "-c", name)
		}
	}})
}

func (a *app) promptRenameBranch() {
	root, old := a.git.root, a.git.branch
	a.openPanel(&panel{prompt: locales.PromptNewName(), query: old, submit: func(name string) {
		if name = strings.TrimSpace(name); name != "" && name != old {
			a.gitRunIn(root, "", nil, "branch", "-m", "--", name)
		}
	}})
}

func (a *app) showDeleteBranch() {
	root, cur := a.git.root, a.git.branch
	a.background(func() func() {
		b := readBranches(root)
		return func() {
			var items []item
			for _, name := range b.local {
				it := item{label: name, pick: func() {
					a.confirm(locales.DialogDelete(name), "", locales.ButtonDelete(), func() { a.deleteBranch(root, name, false) })
				}}
				switch tree, ok := b.elsewhere[name]; {
				case name == cur:
					continue
				case ok:
					it.detail, it.pick = tildePath(tree), nil
				}
				items = append(items, it)
			}
			a.openPanel(&panel{placeholder: locales.GitDeleteBranch(), list: func(q string) []item { return ranked(items, byLabel(q)) }})
		}
	})
}

func (a *app) deleteBranch(root, name string, force bool) {
	dir, flag := root, "-d"
	if force {
		flag = "-D"
	}
	a.background(func() func() {
		mu := repoLock(dir)
		mu.Lock()
		// The advice names -D, which tells a branch not merged.
		out, err := gitWith(dir, "", "-c", "advice.forceDeleteBranch=true", "branch", flag, "--", name)
		mu.Unlock()
		return func() {
			switch {
			case err == nil:
				a.notify(strings.TrimSpace(out))
			// git advises -D, in any language, only for a branch not merged.
			case !force && strings.Contains(err.Error(), "branch -D"):
				a.confirm(locales.DialogBranchNotMerged(name), locales.DialogBranchCommitsLost(), locales.ButtonDelete(),
					func() { a.deleteBranch(root, name, true) })
			default:
				msg, _, _ := strings.Cut(err.Error(), "\n")
				a.notifyError(msg)
			}
		}
	})
}

func (a *app) showWorktrees() {
	root := a.git.root
	a.background(func() func() {
		ws := readWorktrees(root)
		return func() {
			var items []item
			for _, w := range ws {
				it := item{label: w.name, detail: tildePath(w.path), hint: w.branch, pick: func() { a.switchWorktree(w.path) }}
				if w.path == root {
					it.pick = nil
				}
				items = append(items, it)
			}
			a.openPanel(&panel{placeholder: locales.GitSwitchWorktree(), list: func(q string) []item { return ranked(items, byLabel(q)) }})
		}
	})
}

func (a *app) switchWorktree(path string) {
	a.proj.unwatch()
	a.proj = project{gen: a.proj.gen + 1, jump: true}
	a.addFolder(path)
	if d := a.current(); d != nil && d.commit != nil && d.commit.root != path {
		next := slices.IndexFunc(a.mru, func(d *doc) bool {
			return d.commit != nil && d.commit.root == path || d.path != "" && within(d.path, path)
		})
		if next >= 0 {
			a.show(a.mru[next])
		} else {
			a.newFile()
		}
	}
	a.readGit()
}

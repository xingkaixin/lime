package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"lime/locales"
)

// gitRepo is what the side bar shows of the git repository of a project
// of one folder: its work tree and the branch checked out there.
type gitRepo struct {
	dir    string // the folder read
	root   string // the top of its work tree; "" outside a repository
	tree   string // the work tree's name: main, or a linked one's folder
	branch string // or the short hash of a detached HEAD
	// The commits HEAD has that its upstream lacks, and the other way
	// round, with upstream set when the branch has one.
	ahead, behind int
	upstream      bool
	detached      bool
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
		r.detached = true
	}
	r.branch = strings.TrimSpace(head)
	if countsErr == nil {
		_, err = fmt.Sscan(counts, &r.ahead, &r.behind)
		r.upstream = err == nil
	}
	return r
}

// gitDir is the folder whose repository the side bar shows: the
// project's, while it has one folder.
func (a *app) gitDir() string {
	if len(a.proj.folders) != 1 {
		return ""
	}
	return a.proj.folders[0]
}

// readGit reads the repository of the project's folder again, keeping
// what shows until it is read.
func (a *app) readGit() {
	dir := a.gitDir()
	if dir != a.git.dir {
		a.git = gitRepo{dir: dir}
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
	a.background(func() func() {
		r := readRepo(dir)
		return func() {
			if a.git.dir != dir {
				return
			}
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

// gitRun runs git in the repository, then shows the folders, the tabs
// and the branch as it left them, or what it said of a failure.
func (a *app) gitRun(args ...string) { a.gitRunWith("", nil, args...) }

// gitWrites lets one git that gitRun runs write at a time: two at once,
// as check boxes clicked quickly start, would meet on index.lock.
var gitWrites sync.Mutex

// gitRunWith runs git as gitRun does, with input on its standard input,
// and done after it succeeds.
func (a *app) gitRunWith(input string, done func(), args ...string) {
	dir := a.git.root
	a.background(func() func() {
		gitWrites.Lock()
		_, err := gitWith(dir, input, args...)
		gitWrites.Unlock()
		return func() {
			if err != nil {
				msg, _, _ := strings.Cut(err.Error(), "\n")
				a.notifyError(msg)
			} else if done != nil {
				done()
			}
			a.refreshFolders()
			for _, w := range a.l.wins {
				w.reloadChanged()
			}
			a.readGit()
		}
	})
}

type branches struct {
	local, remote []string
	elsewhere     map[string]string // the work trees of the local branches checked out in others
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
				it := item{label: name, pick: func() { a.gitRun("switch", name) }}
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
					a.gitRun("switch", "--track", "refs/remotes/"+name)
				}})
			}
			for _, s := range ss {
				items = append(items, item{label: s.subject, detail: s.ref, hint: locales.GitStash(), pick: func() {
					a.gitRun("stash", "apply", s.ref)
				}})
			}
			a.openPanel(&panel{placeholder: locales.GitBranchPlaceholder(), list: func(q string) []item { return ranked(items, byLabel(q)) }})
		}
	})
}

// inRepo runs fn when the project has a git repository.
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

// promptCreateBranch names a branch to make at HEAD and switch to.
func (a *app) promptCreateBranch() {
	a.openPanel(&panel{prompt: locales.PromptBranchName(), submit: func(name string) {
		if name = strings.TrimSpace(name); name != "" {
			a.gitRun("switch", "-c", name)
		}
	}})
}

// promptRenameBranch renames the branch checked out.
func (a *app) promptRenameBranch() {
	old := a.git.branch
	a.openPanel(&panel{prompt: locales.PromptNewName(), query: old, submit: func(name string) {
		if name = strings.TrimSpace(name); name != "" && name != old {
			a.gitRun("branch", "-m", "--", name)
		}
	}})
}

// showDeleteBranch lists the local branches to delete one: not the one
// checked out here or in another work tree, which git keeps.
func (a *app) showDeleteBranch() {
	root, cur := a.git.root, a.git.branch
	a.background(func() func() {
		b := readBranches(root)
		return func() {
			var items []item
			for _, name := range b.local {
				it := item{label: name, pick: func() {
					a.confirm(locales.DialogDelete(name), "", locales.ButtonDelete(), func() { a.deleteBranch(name, false) })
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

// deleteBranch deletes the branch name. Unless force, git refuses one
// not merged, which the user may then delete anyway.
func (a *app) deleteBranch(name string, force bool) {
	dir, flag := a.git.root, "-d"
	if force {
		flag = "-D"
	}
	a.background(func() func() {
		gitWrites.Lock()
		// The advice names -D, which tells a branch not merged.
		out, err := gitWith(dir, "", "-c", "advice.forceDeleteBranch=true", "branch", flag, "--", name)
		gitWrites.Unlock()
		return func() {
			switch {
			case err == nil:
				a.notify(strings.TrimSpace(out))
			// git advises -D, in any language, only for a branch not merged.
			case !force && strings.Contains(err.Error(), "branch -D"):
				a.confirm(locales.DialogBranchNotMerged(name), locales.DialogBranchCommitsLost(), locales.ButtonDelete(),
					func() { a.deleteBranch(name, true) })
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

// switchWorktree makes the work tree at path the project's folder.
func (a *app) switchWorktree(path string) {
	a.proj.unwatch()
	a.proj = project{gen: a.proj.gen + 1, jump: true}
	a.addFolder(path)
	a.readGit()
}

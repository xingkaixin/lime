package main

import (
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// maxCommits is how many commits the history lists.
const maxCommits = 300

type commit struct {
	hash, short, subject, author, email string
	when                                time.Time
}

// change is a file a commit or the work tree changes: added, modified,
// deleted or renamed (A, M, D, R), with its path before a rename; or in
// the work tree, untracked (?) or in conflict (U).
type change struct {
	status        byte
	path, oldPath string // relative to the work tree's top, with slashes
}

// history is the side bar's list of the repository's commits, read for
// root, or of file's alone, as File History shows them.
type history struct {
	root    string
	file    string // relative to root, with slashes; "" for all the commits
	github  bool   // a remote is on GitHub, which has its authors' avatars
	commits []commit
	paths   []string // file's path in each commit, as renames left it
	gen     int      // counts the readings, for the last to win
}

// logFormat prints the fields of a commit that parseCommit reads.
const logFormat = "%H%x1f%h%x1f%s%x1f%an%x1f%ae%x1f%at"

func parseCommit(rec string) (commit, bool) {
	f := strings.Split(rec, "\x1f")
	if len(f) != 6 {
		return commit{}, false
	}
	at, _ := strconv.ParseInt(f[5], 10, 64)
	return commit{f[0], f[1], f[2], f[3], f[4], time.Unix(at, 0)}, true
}

func readCommits(root string) []commit {
	out, _ := runGit(root, "log", "-n"+strconv.Itoa(maxCommits), "--format="+logFormat+"%x1e")
	var cs []commit
	for _, rec := range strings.Split(out, "\x1e") {
		if cm, ok := parseCommit(strings.TrimSpace(rec)); ok {
			cs = append(cs, cm)
		}
	}
	return cs
}

// readFileCommits reads the commits that change file, across its renames,
// with its path in each.
func readFileCommits(root, file string) ([]commit, []string) {
	out, _ := runGit(root, "--literal-pathspecs", "log", "--follow", "-n"+strconv.Itoa(maxCommits), "-z", "--name-only",
		"--format=%x1e"+logFormat, "--", file)
	var cs []commit
	var paths []string
	last := file
	for _, rec := range strings.Split(out, "\x1e") {
		head, names, _ := strings.Cut(rec, "\x00")
		cm, ok := parseCommit(head)
		if !ok {
			continue
		}
		// A merge lists no names: the path is the one after it.
		if p, _, _ := strings.Cut(strings.TrimPrefix(names, "\n"), "\x00"); p != "" {
			last = p
		}
		cs, paths = append(cs, cm), append(paths, last)
	}
	return cs, paths
}

// onGitHub reports whether a remote of the repository is on GitHub.
func onGitHub(root string) bool {
	out, _ := runGit(root, "remote", "-v")
	return strings.Contains(out, "github.com")
}

// commitChanges reads the files hash changes against its first parent.
func commitChanges(root, hash string) []change {
	out, _ := runGit(root, "diff-tree", "-r", "-z", "-M", "--name-status", "--root", "--no-commit-id", "--diff-merges=first-parent", hash)
	var cs []change
	f := strings.Split(out, "\x00")
	for i := 0; i+1 < len(f); i += 2 {
		if f[i] == "" {
			break
		}
		ch := change{status: f[i][0], path: f[i+1]}
		if ch.status == 'R' || ch.status == 'C' {
			ch.status, ch.oldPath, ch.path = 'R', f[i+1], f[i+2]
			i++
		}
		cs = append(cs, ch)
	}
	return cs
}

// readHistory reads the history again, keeping what shows until it is
// read, as the side bar shows it or git changes it.
func (a *app) readHistory() {
	root := a.git.root
	if root != a.hist.root {
		a.hist = history{root: root, gen: a.hist.gen}
	}
	if root == "" {
		return
	}
	a.hist.gen++
	gen, file := a.hist.gen, a.hist.file
	a.background(func() func() {
		var cs []commit
		var paths []string
		if file != "" {
			cs, paths = readFileCommits(root, file)
		} else {
			cs = readCommits(root)
		}
		gh := onGitHub(root)
		return func() {
			if a.hist.gen == gen {
				a.hist.commits, a.hist.paths, a.hist.github = cs, paths, gh
			}
		}
	})
}

// tracked reports whether File History can show the file at p: git has
// it in the project's repository, as far as the window knows. A text
// tab's gutter has its file's text in git.
func (a *app) tracked(p string) bool {
	if a.git.root == "" || p == "" || !within(p, a.git.root) && !within(p, a.git.dir) {
		return false
	}
	d := a.docAt(p)
	return d == nil || d.kind != textDoc || d.base != "" || d.saved.Len() > largeFile
}

// fileHistory shows the commits of the file at p in the git panel's
// History, as Sublime Merge's File History does.
func (a *app) fileHistory(p string) {
	root := a.git.root
	a.background(func() func() {
		// git keeps a link's target path, as the project's folder has it.
		real := p
		if r, err := filepath.EvalSymlinks(p); err == nil {
			real = r
		}
		rel, err := filepath.Rel(root, real)
		return func() {
			if err != nil || root != a.git.root || !filepath.IsLocal(rel) {
				return
			}
			if a.hist.root != root {
				a.hist = history{root: root, gen: a.hist.gen}
			}
			a.hist.file, a.hist.commits, a.hist.paths = filepath.ToSlash(rel), nil, nil
			a.gitTab = historyTab
			if a.side != gitPanel {
				a.showPanel(gitPanel)
			} else {
				a.readHistory()
			}
		}
	})
}

// ago tells how long ago t was, as Zed's history does.
func ago(t, now time.Time) string {
	const day = 24 * time.Hour
	switch d := now.Sub(t); {
	case d < time.Minute:
		return locales.TimeJustNow()
	case d < time.Hour:
		return locales.TimeMinutesAgo(int(d / time.Minute))
	case d < day:
		return locales.TimeHoursAgo(int(d / time.Hour))
	case d < 7*day:
		return locales.TimeDaysAgo(int(d / day))
	case d < 30*day:
		return locales.TimeWeeksAgo(int(d / (7 * day)))
	case d < 365*day:
		return locales.TimeMonthsAgo(int(d / (30 * day)))
	default:
		return locales.TimeYearsAgo(int(d / (365 * day)))
	}
}

// historyView lists the commits, each of which opens in a tab of its own:
// at the file's diff, under the file whose commits alone show.
func (a *app) historyView(c *ui.Context) {
	h := &a.hist
	shown := ""
	if d := a.current(); d != nil && d.commit != nil {
		shown = d.commit.hash
	}
	if h.file != "" {
		a.fileHistoryBar(c)
	}
	now := time.Now()
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(0, c.Theme().Space(1)).Role(ui.RoleList).Children(func() {
			for i, cm := range h.commits {
				at := ""
				if i < len(h.paths) {
					at = h.paths[i]
				}
				a.historyRow(c, cm, at, cm.author+" · "+ago(cm.when, now)+" · "+cm.short, shown == cm.hash)
			}
		})
	})
}

// fileHistoryBar names the file whose commits alone show, with the button
// that shows them all again.
func (a *app) fileHistoryBar(c *ui.Context) {
	th := c.Theme()
	ch := a.shell(c)
	name, dir := path.Base(a.hist.file), path.Dir(a.hist.file)
	ui.Row(c).FillWidth().Height(28).Shrink(0).Padding(0, th.Space(1), 0, th.Space(3)).Gap(th.Space(1.5)).AlignItems(ui.Center).Children(func() {
		ic := fileIcon(name, a.scheme.dark)
		ic.view(c, treeIcon, ic.tint(ch.muted))
		ui.Text(c, name).FontSize(12).TextColor(ch.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(200)
		if dir != "." {
			ui.Text(c, dir).FontSize(11).TextColor(ch.muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		ui.Box(c).Grow(1)
		a.barButton(c, closeIcon, locales.HistoryAllCommits(), false, func() {
			a.hist.file, a.hist.commits, a.hist.paths = "", nil, nil
			a.readHistory()
		})
	})
}

func (a *app) historyRow(c *ui.Context, cm commit, at, detail string, selected bool) {
	th := c.Theme()
	ch := a.shell(c)
	row := ui.Column(c.Key("commit "+cm.hash)).FillWidth().Padding(th.Space(1.5), th.Space(2)).Gap(th.Space(1)).Radius(th.Radius).
		Role(ui.RoleListItem).Label(cm.subject).Selected(selected).Transition(colorFade).OnClick(func() { a.showCommit(cm).at = at })
	text, muted := ch.text, ch.muted
	switch {
	case selected:
		row.Background(th.Accent)
		text, muted = th.AccentText, th.AccentText.Alpha(0.8)
	case row.Hovered():
		row.Background(ch.hover)
	}
	row.Children(func() {
		ui.Text(c, cm.subject).TextColor(text).SingleLine().Ellipsis("…").MinWidth(0)
		ui.Row(c).FillWidth().Gap(th.Space(1.5)).AlignItems(ui.Center).Children(func() {
			a.avatar(c, cm.email, 16)
			ui.Text(c, detail).FontSize(11).TextColor(muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		})
	})
}

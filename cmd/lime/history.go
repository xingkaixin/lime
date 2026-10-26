package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// maxCommits is how many commits the history lists.
const maxCommits = 300

// uncommitted stands in the history for the work tree's changes.
const uncommitted = ""

type commit struct {
	hash, short, subject, author, email string
	when                                time.Time
}

// change is a file a commit changes: added, modified, deleted or renamed
// (A, M, D, R), with its path before a rename.
type change struct {
	status        byte
	path, oldPath string // relative to the work tree's top, with slashes
}

// history is the side bar's list of the repository's commits, the work
// tree's changes first, read for root.
type history struct {
	root    string
	github  bool // a remote is on GitHub, which has its authors' avatars
	commits []commit
	changes []change // the work tree's
	gen     int      // counts the readings, for the last to win
}

func readCommits(root string) []commit {
	out, _ := runGit(root, "log", "-n"+strconv.Itoa(maxCommits), "--format=%H%x1f%h%x1f%s%x1f%an%x1f%ae%x1f%at%x1e")
	var cs []commit
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.Split(strings.TrimSpace(rec), "\x1f")
		if len(f) != 6 {
			continue
		}
		at, _ := strconv.ParseInt(f[5], 10, 64)
		cs = append(cs, commit{f[0], f[1], f[2], f[3], f[4], time.Unix(at, 0)})
	}
	return cs
}

// onGitHub reports whether a remote of the repository is on GitHub.
func onGitHub(root string) bool {
	out, _ := runGit(root, "remote", "-v")
	return strings.Contains(out, "github.com")
}

// readChanges reads the files hash changes against its first parent, or
// the work tree's changes against HEAD, staged or not, and the untracked
// files.
func readChanges(root, hash string) []change {
	if hash == uncommitted {
		out, _ := runGit(root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
		var cs []change
		f := strings.Split(out, "\x00")
		for i := 0; i < len(f); i++ {
			if len(f[i]) < 4 {
				continue
			}
			x, y, path := f[i][0], f[i][1], f[i][3:]
			ch := change{status: y, path: path}
			switch {
			case x == '?':
				ch.status = 'A'
			case x == 'R' || x == 'C':
				i++
				ch.status, ch.oldPath = 'R', f[i]
			case y == ' ':
				ch.status = x
			}
			cs = append(cs, ch)
		}
		return cs
	}
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
	gen := a.hist.gen
	a.background(func() func() {
		cs, ch, gh := readCommits(root), readChanges(root, uncommitted), onGitHub(root)
		return func() {
			if a.hist.gen == gen {
				a.hist.commits, a.hist.changes, a.hist.github = cs, ch, gh
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

// historyView lists the work tree's changes and the commits, each of
// which opens in a tab of its own.
func (a *app) historyView(c *ui.Context) {
	h := &a.hist
	shown := "-"
	if d := a.current(); d != nil && d.commit != nil {
		shown = d.commit.hash
	}
	now := time.Now()
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(0, c.Theme().Space(1)).Role(ui.RoleList).Children(func() {
			if len(h.changes) > 0 {
				a.historyRow(c, commit{subject: locales.HistoryUncommitted()}, locales.HistoryFiles(len(h.changes)), shown == uncommitted)
			}
			for _, cm := range h.commits {
				a.historyRow(c, cm, cm.author+" · "+ago(cm.when, now)+" · "+cm.short, shown == cm.hash)
			}
		})
	})
}

func (a *app) historyRow(c *ui.Context, cm commit, detail string, selected bool) {
	th := c.Theme()
	ch := a.shell(c)
	row := ui.Column(c.Key("commit "+cm.hash)).FillWidth().Padding(th.Space(1.5), th.Space(2)).Gap(th.Space(1)).Radius(th.Radius).
		Role(ui.RoleListItem).Label(cm.subject).Selected(selected).Transition(colorFade).OnClick(func() { a.showCommit(cm) })
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
			if cm.hash != uncommitted {
				a.avatar(c, cm.email, 16)
			}
			ui.Text(c, detail).FontSize(11).TextColor(muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		})
	})
}

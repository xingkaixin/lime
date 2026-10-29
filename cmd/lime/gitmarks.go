package main

import (
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"golang.org/x/text/unicode/norm"
)

// The side bar tints the names of files and folders as git status has
// them, as Zed's project panel does, and dims what git ignores, as
// Sublime Text's side bar does.

// gitMark is what git says of a file or folder. A folder takes the
// highest mark of what it holds, ignored files apart.
type gitMark uint8

const (
	gitClean gitMark = iota
	gitIgnored
	gitAdded // or untracked
	gitDeleted
	gitModified
	gitConflict
)

// gitMarks is what git says of the files of the side bar's folders.
type gitMarks struct {
	folders []string           // the folders read; nil reads them again as the side bar shows them
	bases   map[string]gitBase // by the folder of the side bar the files of a work tree are under
	// One reading runs at a time; again reads once more after it, for
	// what changed meanwhile, as a Save All or many changes on disk ask
	// for one each.
	reading, again bool
}

// gitBase is the files of a work tree from a folder of the side bar:
// their marks by path from the work tree's top, and the folder's path
// from there.
type gitBase struct {
	marks  map[string]gitMark
	prefix string // "" or ending in a slash
}

// readMarks reads what git says of the side bar's files again, while it
// shows them, keeping what shows until it is read.
func (a *app) readMarks() {
	g := &a.marks
	if a.side != filesPanel {
		g.folders = nil
		return
	}
	g.folders = slices.Clone(a.proj.folders)
	if g.reading {
		g.again = true
		return
	}
	g.reading = true
	folders := g.folders
	a.background(func() func() {
		bases := readGitBases(folders)
		return func() {
			g.bases, g.reading = bases, false
			if g.again {
				g.again = false
				a.readMarks()
			}
		}
	})
}

// readGitBases reads what git says of the files in folders: in the work
// tree each is in, and in the repositories in those that git status
// shows as one entry. It blocks, so call it off the main thread.
func readGitBases(folders []string) map[string]gitBase {
	bases := map[string]gitBase{}
	read := map[string]treeStatus{}
	var add func(dir, top, prefix string)
	add = func(dir, top, prefix string) {
		s, ok := read[top]
		if !ok {
			s = readTreeStatus(top)
			read[top] = s
		}
		bases[dir] = gitBase{s.marks, prefix}
		for _, p := range s.repos {
			if rest, ok := strings.CutPrefix(p, prefix); ok {
				sub := filepath.Join(dir, filepath.FromSlash(rest))
				add(sub, sub, "")
			}
		}
	}
	for _, f := range folders {
		// The prefix, not the top, places f in its work tree: git gives
		// the top with the links in its path resolved.
		out, err := runGit(f, "rev-parse", "--path-format=absolute", "--show-toplevel", "--show-prefix")
		top, prefix, ok := strings.Cut(strings.TrimSuffix(out, "\n"), "\n")
		if err == nil && ok {
			add(f, top, norm.NFC.String(prefix))
		}
	}
	return bases
}

// treeStatus is what git status says of a work tree: the marks of its
// files and the folders they are in, by path from its top, and the
// repositories in it that it shows as one entry, with changes.
type treeStatus struct {
	marks map[string]gitMark
	repos []string
}

// readTreeStatus reads git status of the work tree at top, with the
// folders it ignores as one entry each, as node_modules would be too
// many. It blocks, so call it off the main thread.
func readTreeStatus(top string) treeStatus {
	out, _ := runGit(top, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignored=matching")
	s := treeStatus{marks: map[string]gitMark{}}
	f := strings.Split(norm.NFC.String(out), "\x00")
	for i := 0; i < len(f); i++ {
		line := f[i]
		if len(line) < 3 {
			continue
		}
		var p string
		var m gitMark
		switch line[0] {
		case '!':
			// It marks no folder it is in.
			p = strings.TrimSuffix(line[2:], "/")
			s.marks[p] = max(s.marks[p], gitIgnored)
			continue
		case '?':
			p, m = line[2:], gitAdded
			// With all untracked files listed, a folder is a repository.
			if strings.HasSuffix(p, "/") {
				p = p[:len(p)-1]
				s.repos = append(s.repos, p)
			}
		case 'u':
			p, m = statusField(line, 10), gitConflict
		case '1', '2':
			if len(line) < 9 {
				continue
			}
			n := 8
			if line[0] == '2' {
				n, i = 9, i+1 // past the path it had
			}
			p, m = statusField(line, n), max(changeMark(line[2]), changeMark(line[3]))
			// A submodule, S, whose tracked files changed, M, or that has
			// untracked files, U.
			if line[5] == 'S' && (line[7] == 'M' || line[8] == 'U') {
				s.repos = append(s.repos, p)
			}
		default:
			continue
		}
		for {
			s.marks[p] = max(s.marks[p], m)
			if p == "" {
				break
			}
			if j := strings.LastIndexByte(p, '/'); j >= 0 {
				p = p[:j]
			} else {
				p = ""
			}
		}
	}
	return s
}

// changeMark is the mark of the letter git status gives a change.
func changeMark(c byte) gitMark {
	switch statusKind(c) {
	case '.':
		return gitClean
	case 'A', 'R':
		return gitAdded
	case 'D':
		return gitDeleted
	}
	return gitModified
}

// mark is what git says of the file or folder at path. The top of a
// repository in another takes the mark the other gives it, as one
// entry; what it holds, the marks its own gives them. Paths compare in
// NFC, as git on macOS prints them, where the names read from a folder
// may be in NFD.
func (g *gitMarks) mark(path string) gitMark {
	for p := path; ; {
		dir := filepath.Dir(p)
		if dir == p {
			break
		}
		if b, ok := g.bases[dir]; ok {
			return b.mark(norm.NFC.String(b.prefix + filepath.ToSlash(strings.TrimLeft(path[len(dir):], string(filepath.Separator)))))
		}
		p = dir
	}
	if b, ok := g.bases[path]; ok {
		return b.mark(strings.TrimSuffix(b.prefix, "/"))
	}
	return gitClean
}

func (b gitBase) mark(p string) gitMark {
	if m, ok := b.marks[p]; ok {
		return m
	}
	// What an ignored folder holds, which git status leaves out.
	for j := strings.LastIndexByte(p, '/'); j > 0; j = strings.LastIndexByte(p, '/') {
		if p = p[:j]; b.marks[p] == gitIgnored {
			return gitIgnored
		}
	}
	return gitClean
}

// gitTint is the color of the name of a file or folder git marks m: the
// gutter's, taken toward black on a light scheme, or white on a dark
// one, until it reads as text on the side bar, as some schemes' are too
// pale to.
func (s *scheme) gitTint(m gitMark) ui.Color {
	th := s.editorTheme()
	c := th.Modified
	switch m {
	case gitAdded:
		c = th.Added
	case gitDeleted, gitConflict:
		c = th.Deleted
	}
	to, bg := black, s.chrome().bg
	if s.dark {
		to = ui.RGB(255, 255, 255)
	}
	tint := c
	// 4.5 is WCAG's least contrast for text.
	for t := float32(0.05); t <= 1 && contrast(tint, bg) < 4.5; t += 0.05 {
		tint = c.Mix(to, t)
	}
	return tint
}

// contrast is the contrast ratio of a and b, as WCAG has it.
func contrast(a, b ui.Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

func luminance(c ui.Color) float64 {
	linear := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(c.R) + 0.7152*linear(c.G) + 0.0722*linear(c.B)
}

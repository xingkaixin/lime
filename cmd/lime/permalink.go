package main

import (
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// Permalinks link to lines of a file on GitHub, at the commit HEAD names,
// as Zed's Copy Permalink to Line and Open Permalink to Line do.

// errNoGitHub is the error of a repository without a remote on GitHub.
var errNoGitHub = errors.New("no remote on GitHub")

// linkable reports whether d's file has a permalink: a file git has. The
// website's git has no remotes.
func (d *doc) linkable() bool {
	return runtime.GOOS != "js" && d.plain() && d.path != "" && d.base != ""
}

func currentLinkable(a *app) bool {
	d := a.current()
	return d != nil && d.linkable()
}

// permalink copies the link on GitHub to the lines of d's selection, or of
// its caret, or opens it.
func (a *app) permalink(d *doc, open bool) {
	if d == nil || !d.linkable() {
		return
	}
	from, to := selectedLines(d.st)
	path, dirty := d.path, d.dirty()
	a.background(func() func() {
		link, pushed, same, err := readPermalink(path, from, to)
		return func() {
			switch {
			case errors.Is(err, errNoGitHub):
				a.notifyError(locales.StatusNoGitHub())
				return
			case err != nil:
				msg, _, _ := strings.Cut(err.Error(), "\n")
				a.notifyError(msg)
				return
			}
			if open {
				mygo.Shell.OpenExternal(link)
			} else {
				mygo.Clipboard.WriteText(link)
			}
			switch {
			case dirty || !same:
				a.notify(locales.StatusPermalinkChanged())
			case !pushed:
				a.notify(locales.StatusNotPushed())
			case !open:
				a.notify(locales.StatusPermalinkCopied())
			}
		}
	})
}

// permalinkMenu adds the items of d's permalink to m.
func (a *app) permalinkMenu(m *ui.Menu, d *doc) {
	if m.Item(locales.MenuCopyPermalink()).Disabled(!d.linkable()).Chosen() {
		a.permalink(d, false)
	}
	if m.Item(locales.MenuOpenOnGitHub()).Disabled(!d.linkable()).Chosen() {
		a.permalink(d, true)
	}
}

// selectedLines is the first and last line of st's selection, or its
// caret's line: a selection that ends at the start of a line leaves it out.
func selectedLines(st *editor.State) (int, int) {
	sel := st.Selection()
	start, end := min(sel.Anchor, sel.Head), max(sel.Anchor, sel.Head)
	t := st.Buffer.Snapshot()
	from, to := t.LineOf(start), t.LineOf(end)
	if to > from && t.LineStart(to) == end {
		to--
	}
	return from, to
}

// rendered are the extensions of the files GitHub renders, whose lines have
// anchors in its plain view alone.
var rendered = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true, ".mkdn": true, ".rst": true, ".adoc": true, ".asciidoc": true,
	".org": true, ".textile": true, ".rdoc": true, ".pod": true, ".creole": true, ".mediawiki": true, ".wiki": true,
	".ipynb": true, ".csv": true, ".tsv": true,
}

// readPermalink reads the link on GitHub to lines from to to of the file at
// path, at HEAD's commit; whether a branch of the remote has the commit,
// without which the link opens nothing; and whether the file is the same
// as at HEAD, without which its lines may be others. It blocks, so call it
// off the main thread.
func readPermalink(path string, from, to int) (link string, pushed, same bool, err error) {
	real := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		real = r
	}
	dir := filepath.Dir(real)
	web, remote := githubRepo(dir)
	if web == "" {
		return "", false, false, errNoGitHub
	}
	out, err := runGit(dir, "rev-parse", "--show-prefix", "HEAD")
	if err != nil {
		return "", false, false, err
	}
	prefix, head, _ := strings.Cut(strings.TrimSuffix(out, "\n"), "\n")
	var segs []string
	for _, s := range strings.Split(prefix+filepath.Base(real), "/") {
		segs = append(segs, url.PathEscape(s))
	}
	link = web + "/blob/" + head + "/" + strings.Join(segs, "/")
	if rendered[strings.ToLower(filepath.Ext(real))] {
		link += "?plain=1"
	}
	link += "#L" + strconv.Itoa(from+1)
	if to > from {
		link += "-L" + strconv.Itoa(to+1)
	}
	branches, _ := runGit(dir, "branch", "-r", "--contains", head, "--list", remote+"/*")
	_, err = runGit(dir, "--literal-pathspecs", "diff", "--quiet", "HEAD", "--", filepath.Base(real))
	return link, strings.TrimSpace(branches) != "", err == nil, nil
}

// githubRepo reads the web address on GitHub of the repository of dir, and
// the name of its remote there: origin, or else the first on GitHub; ""
// for none. It blocks, so call it off the main thread.
func githubRepo(dir string) (web, remote string) {
	out, _ := runGit(dir, "remote", "-v")
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		w, ok := githubWeb(f[1])
		if !ok {
			continue
		}
		if f[0] == "origin" {
			return w, f[0]
		}
		if web == "" {
			web, remote = w, f[0]
		}
	}
	return web, remote
}

// githubWeb reads the web address of a repository on GitHub from the URL
// of a remote: https://github.com/owner/repo, ssh://git@github.com/owner/repo
// or git@github.com:owner/repo, with .git or not.
func githubWeb(remote string) (string, bool) {
	var host, p string
	if u, err := url.Parse(remote); err == nil && u.Scheme != "" && u.Host != "" {
		host, p = u.Hostname(), u.Path
	} else if at, rest, ok := strings.Cut(remote, ":"); ok && !strings.Contains(at, "/") {
		// scp's form: [user@]host:path.
		host, p = at[strings.LastIndex(at, "@")+1:], rest
	}
	if !strings.EqualFold(host, "github.com") {
		return "", false
	}
	owner, repo, ok := strings.Cut(strings.TrimSuffix(strings.Trim(p, "/"), ".git"), "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", false
	}
	return "https://github.com/" + owner + "/" + repo, true
}

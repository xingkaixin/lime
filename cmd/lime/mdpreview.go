package main

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"lime/locales"
)

// mdPreview is what a Markdown preview tab shows, as Zed's: the text of
// another tab as it reads, following its edits, saved or not.
type mdPreview struct {
	src  *doc
	text string
	ver  uint64 // the version of src's text that text is
	// seen is the version of src's text the preview last saw, at at: it
	// shows it once it has stayed for previewPause.
	seen   uint64
	at     time.Time
	opts   *markdown.Options
	dir    string // the folder of src's file, which opts loads images from
	theme  markdown.Theme
	scroll ui.ScrollState
	// anchor is the heading a link asked for, by its slug, which the next
	// build pass scrolls to as heading.
	anchor, heading string
}

// previewPause is how long the text of a preview's tab stays as it is
// before the preview shows it, as in Zed: typing in a long file does not
// parse it all again at every key.
const previewPause = 200 * time.Millisecond

// markdown reports whether d's tab shows Markdown in an editor, which
// a preview can show.
func (d *doc) markdown() bool {
	l := d.st.Buffer.Language()
	return d.plain() && l != nil && l.Name == "Markdown"
}

// openPreview opens a tab previewing src in its group, or with side in
// the group beside it, the source keeping the keyboard there, as Zed's
// markdown: open preview to the side does. With one group, the window
// splits into two columns first. A preview of src already in the group
// comes to the front instead.
func (a *app) openPreview(src *doc, side bool) {
	gi, _ := a.locate(src)
	if gi < 0 || !src.markdown() {
		return
	}
	if side {
		if len(a.groups) == 1 {
			a.setLayout(layoutOf(2, 1))
		}
		gi = (gi + 1) % len(a.groups)
	}
	g := a.groups[gi]
	i := slices.IndexFunc(g.docs, func(d *doc) bool { return d.md != nil && d.md.src == src })
	if i < 0 {
		v := src.st.Buffer.Version()
		d := newDoc("", "")
		d.kind, d.md = mdDoc, &mdPreview{src: src, text: src.st.Buffer.String(), ver: v, seen: v}
		g.docs = append(g.docs, d)
		i = len(g.docs) - 1
	}
	if !side {
		a.show(g.docs[i])
		return
	}
	g.active = i
	a.show(src)
}

// refreshPreview takes the text of p's tab once it has stayed for
// previewPause since it changed.
func (a *app) refreshPreview(p *mdPreview) {
	b := p.src.st.Buffer
	v := b.Version()
	if v == p.ver {
		return
	}
	now := time.Now()
	if v != p.seen {
		p.seen, p.at = v, now
		// A window that never opened, as in tests, has no frames to update.
		if w := a.win; w != nil {
			time.AfterFunc(previewPause, func() { w.Update(func() {}) })
		}
		return
	}
	if now.Sub(p.at) >= previewPause {
		p.text, p.ver = b.String(), v
	}
}

// previewOptions are how p shows: in the colors of the scheme, its images
// loaded from beside its file, its links followed by followLink.
func (a *app) previewOptions(p *mdPreview, services ui.Services) *markdown.Options {
	dir := ""
	if p.src.path != "" {
		dir = filepath.Dir(p.src.path)
	}
	// Images load once for the Options: a file moved elsewhere loads them
	// from its new folder.
	if p.opts == nil || dir != p.dir {
		p.dir = dir
		p.opts = &markdown.Options{Block: p.scrollToAnchor}
		load := markdown.FileImages(dir)
		p.opts.LoadImage = func(u string) (ui.ImageSource, error) {
			if s, err := url.PathUnescape(u); err == nil {
				u = s
			}
			path, ok := localFile(dir, strings.TrimPrefix(u, "file:"))
			if !ok {
				return nil, os.ErrNotExist
			}
			return load(path)
		}
	}
	p.theme = markdown.Theme{CodeFont: a.set.ContentFont, CodeColors: a.scheme.editorTheme()}
	p.opts.Theme = &p.theme
	p.opts.OnLink = func(link string) { a.followLink(services, p, link) }
	return p.opts
}

// localFile returns the file at path, from dir when relative, if it is an
// ordinary file of this machine: Windows reads //host/share/x.png from
// another, which it sends the user's credentials to, and reading a device
// such as /dev/zero never ends.
func localFile(dir, path string) (string, bool) {
	if len(path) > 1 && strings.IndexByte(`/\`, path[0]) >= 0 && strings.IndexByte(`/\`, path[1]) >= 0 {
		return "", false
	}
	if !filepath.IsAbs(path) {
		if dir == "" {
			return "", false
		}
		path = filepath.Join(dir, filepath.FromSlash(path))
	}
	fi, err := os.Stat(path)
	return path, err == nil && fi.Mode().IsRegular()
}

// scrollToAnchor scrolls p.heading to the top of the view, as a link to
// it does in a browser, before the heading is built.
func (p *mdPreview) scrollToAnchor(c *ui.Context, d *parser.Document, i int) bool {
	if p.heading != "" && d.Block(i).Kind == parser.Heading && markdown.Slug(parser.PlainText(d.Inlines(i))) == strings.ToLower(p.heading) {
		// ScrollIntoView scrolls as little as shows a box: one as high as
		// the view, from the heading's top, shows from there.
		ui.Box(c).Absolute().Top(0).Size(1, p.scroll.Bounds(c).H).ScrollIntoView()
		p.heading = ""
	}
	return false
}

// followLink goes where a link of p points: a web page to the browser, a
// heading of p to the top, or a file, by its path from p's, to a tab, as
// Zed's preview does. Other links do nothing.
func (a *app) followLink(services ui.Services, p *mdPreview, link string) {
	if s, ok := strings.CutPrefix(link, "#"); ok {
		if u, err := url.PathUnescape(s); err == nil {
			s = u
		}
		p.anchor = s
		return
	}
	scheme := ""
	if u, err := url.Parse(link); err == nil {
		scheme = u.Scheme
	}
	if scheme == "http" || scheme == "https" {
		services.OpenURL(link)
		return
	}
	path, _, _ := strings.Cut(link, "#")
	if s, err := url.PathUnescape(path); err == nil {
		path = s
	}
	// A path of Windows, as C:\docs, parses as of a scheme.
	if scheme != "" && !filepath.IsAbs(path) {
		return
	}
	if path, ok := localFile(p.dir, path); ok {
		a.open([]string{path})
	}
}

// previewTab shows the preview tab d in place of the editor of its group.
func (a *app) previewTab(c *ui.Context, d *doc) {
	p := d.md
	a.refreshPreview(p)
	opts := a.previewOptions(p, c.Services())
	col := ui.Column(c.Key(key(d))).Grow(1).FillWidth().MinHeight(0).Background(a.scheme.bg)
	if col.FocusWithin() && a.current() != d {
		a.show(d)
	}
	col.Children(func() {
		ui.Scroll(c).TrackScroll(&p.scroll).Grow(1).FillWidth().MinHeight(0).Label(d.name()).Children(func() {
			// The pass after a link's builds what the click changed: the
			// heading comes into view in it.
			p.heading, p.anchor = p.anchor, ""
			ui.Column(c).Padding(24, 32).Children(func() { markdown.View(c, p.text, opts) })
		})
	})
}

func (p *mdPreview) title() string { return locales.TabPreview(p.src.name()) }

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
	seen uint64
	at   time.Time
	// doc is text parsed, when it is cut in parts.
	doc   *parser.Document
	parts []mdPart
	opts  *markdown.Options
	dir   string // the folder of src's file, which opts loads images from
	theme markdown.Theme
	list  ui.ListState
	// anchor is the heading a link asked for, by its slug, which the next
	// build pass scrolls to as heading.
	anchor, heading string
}

// mdPart is a part of a preview's text, which a row of its list shows.
type mdPart struct {
	text string
	head int // the heading the part starts with, a block of doc, or 0
}

// previewPause is how long the text of a preview's tab stays as it is
// before the preview shows it, as in Zed: typing in a long file does not
// parse it all again at every key.
const previewPause = 200 * time.Millisecond

// A Markdown view builds and lays out all its blocks at every frame. A
// text longer than previewWhole shows in parts of about previewPart, each
// a row of a list, which builds only the rows in view. A shorter one
// shows whole, its footnotes linked and its text one selection.
const (
	previewWhole = 16 << 10
	previewPart  = 4 << 10
)

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
		d.kind, d.md = mdDoc, &mdPreview{src: src, seen: v}
		d.md.show(src.st.Buffer.String(), v)
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

// refresh takes the text of p's tab once it has stayed for previewPause
// since it changed.
func (p *mdPreview) refresh(c *ui.Context) {
	b := p.src.st.Buffer
	v := b.Version()
	if v == p.ver {
		return
	}
	if v != p.seen {
		p.seen, p.at = v, c.Now()
	}
	// A frame's After is for that frame: each one until then asks again.
	if wait := previewPause - c.Now().Sub(p.at); wait > 0 {
		c.After(wait)
		return
	}
	p.show(b.String(), v)
}

// show makes text, version ver of the source's, what p shows: whole, or
// in a part at each top-level heading, which a link to it scrolls to the
// top, and wherever a part grew to previewPart.
func (p *mdPreview) show(text string, ver uint64) {
	p.text, p.ver = text, ver
	p.parts = p.parts[:0]
	if len(text) <= previewWhole {
		p.parts = append(p.parts, mdPart{text: text})
		return
	}
	if p.doc == nil {
		p.doc = parser.New(parser.Default)
	}
	d := p.doc
	d.Set(text)
	// Link reference definitions are no block: they are between blocks
	// and after the last, with the ends of some (a table's last |, a
	// heading's closing #s), and go before each part for its links to
	// them.
	var defs strings.Builder
	addDefs := func(gap string) {
		if !strings.Contains(gap, "[") {
			return
		}
		for _, s := range strings.Split(gap, "\n\n") {
			if s = strings.TrimSpace(s); strings.HasPrefix(s, "[") && parser.Parse(s).Len() == 1 {
				defs.WriteString(s + "\n")
			}
		}
	}
	start, head, end := 0, 0, 0
	for i := range d.Children(0) {
		b := d.Block(i)
		addDefs(text[end:b.Start])
		end = b.End
		at := strings.LastIndexByte(text[:b.Start], '\n') + 1
		if at > start && (b.Kind == parser.Heading || at-start >= previewPart) {
			p.parts = append(p.parts, mdPart{text: text[start:at], head: head})
			start, head = at, 0
		}
		if at == start && b.Kind == parser.Heading {
			head = i
		}
	}
	addDefs(text[end:])
	p.parts = append(p.parts, mdPart{text: text[start:], head: head})
	if defs.Len() > 0 {
		defs.WriteString("\n")
		for k := range p.parts {
			p.parts[k].text = defs.String() + p.parts[k].text
		}
	}
}

// previewOptions are how p shows: in the colors of the scheme, its images
// loaded from beside its file, its links followed by followLink.
func (a *app) previewOptions(c *ui.Context, p *mdPreview) *markdown.Options {
	dir := ""
	if p.src.path != "" {
		dir = filepath.Dir(p.src.path)
	}
	// Images load once for the Options: a file moved elsewhere loads them
	// from its new folder.
	if p.opts == nil || dir != p.dir {
		p.dir = dir
		p.opts = &markdown.Options{}
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
	// The default theme's spacing spaces the parts.
	p.theme = *markdown.DefaultTheme(c.Theme())
	p.theme.CodeFont, p.theme.CodeColors = a.set.ContentFont, a.scheme.editorTheme()
	p.opts.Theme = &p.theme
	services := c.Services()
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

// isHeading reports whether block i of d is the heading of anchor slug.
func isHeading(d *parser.Document, i int, slug string) bool {
	return d.Block(i).Kind == parser.Heading && markdown.Slug(parser.PlainText(d.Inlines(i))) == strings.ToLower(slug)
}

// scrollToAnchor scrolls p.heading to the top of the view, as a link to
// it does in a browser, before the heading is built.
func (p *mdPreview) scrollToAnchor(c *ui.Context, d *parser.Document, i int) bool {
	if p.heading != "" && isHeading(d, i, p.heading) {
		// ScrollIntoView scrolls as little as shows a box: one as high as
		// the view, from the heading's top, shows from there.
		ui.Box(c).Absolute().Top(0).Size(1, p.list.Bounds(c).H).ScrollIntoView()
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
	p.refresh(c)
	opts := a.previewOptions(c, p)
	col := ui.Column(c.Key(key(d))).Grow(1).FillWidth().MinHeight(0).Background(a.scheme.bg)
	if col.FocusWithin() && a.current() != d {
		a.show(d)
	}
	col.Children(func() {
		// The pass after a link's builds what the click changed: the
		// heading comes into view in it, at the top of its part, or else
		// where its part's view finds it.
		p.heading, p.anchor = p.anchor, ""
		if p.heading != "" {
			if k := slices.IndexFunc(p.parts, func(pt mdPart) bool { return pt.head > 0 && isHeading(p.doc, pt.head, p.heading) }); k >= 0 {
				p.list.ScrollTo(k, ui.Start)
				p.heading = ""
			}
		}
		opts.Block = nil
		if p.heading != "" {
			opts.Block = p.scrollToAnchor
		}
		th := &p.theme
		ui.List(c, &p.list, len(p.parts), func(k int) {
			pt := p.parts[k]
			top, bottom := th.BlockSpacing*th.FontSize, float32(0)
			if pt.head > 0 {
				top = max(top, th.Headings[p.doc.Block(pt.head).Level-1].Top*th.FontSize)
			}
			if k == 0 {
				top = 24
			}
			if k == len(p.parts)-1 {
				bottom = 24
			}
			ui.Column(c).Padding(top, 32, bottom, 32).Children(func() { markdown.View(c, pt.text, opts) })
		}).Grow(1).FillWidth().MinHeight(0).Label(d.name())
	})
}

func (p *mdPreview) title() string { return locales.TabPreview(p.src.name()) }

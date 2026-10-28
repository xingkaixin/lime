package main

import (
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// lineEnding is one of Sublime Text's line endings, by the type its
// set_line_ending takes.
type lineEnding struct {
	typ, text string
	label     func(locales.Messages) string
}

var lineEndings = []lineEnding{
	{"windows", "\r\n", locales.Messages.MenuWindows},
	{"unix", "\n", locales.Messages.MenuUnix},
	{"cr", "\r", locales.Messages.MenuMacOS9},
}

func (e lineEnding) id() string { return "set_line_ending " + e.typ }

// endingType is the type of the line ending text, "" for none.
func endingType(text string) string {
	for _, e := range lineEndings {
		if e.text == text {
			return e.typ
		}
	}
	return ""
}

func lineEndingCommands() []command {
	var c []command
	for _, e := range lineEndings {
		caption := func(m locales.Messages) string { return m.CmdLineEndings(e.label(m)) }
		c = append(c, command{id: e.id(), caption: caption, run: withDoc(func(_ *app, d *doc) { d.setLineEnding(e.text) })})
	}
	return c
}

// lineEnding is what d's lines end in on disk: \n, \r\n or \r.
func (d *doc) lineEnding() string {
	if d.cr {
		return "\r"
	}
	return d.st.Buffer.LineEnding()
}

// setLineEnding has d's lines end in e as the file is saved. The buffer
// knows of \n and \r\n alone.
func (d *doc) setLineEnding(e string) {
	d.cr = e == "\r"
	d.st.Buffer.SetLineEnding(e)
}

// splitLines is text as the buffer takes it, and the line ending of its
// lines. Lines ending in \r alone, as classic Mac OS has them, end in \n,
// where the buffer would show them as one line; \r\n the buffer turns to
// \n itself.
func splitLines(text string) (string, string) {
	switch {
	case strings.Contains(text, "\r\n"):
		return text, "\r\n"
	case strings.Contains(text, "\r") && !strings.Contains(text, "\n"):
		return strings.ReplaceAll(text, "\r", "\n"), "\r"
	}
	return text, "\n"
}

// endingName is how the status bar shows the line ending e.
func endingName(e string) string {
	switch e {
	case "\r\n":
		return "CRLF"
	case "\r":
		return "CR"
	}
	return "LF"
}

// lineEndingButton shows d's line ending, and changes it from a menu.
func (a *app) lineEndingButton(c *ui.Context, d *doc) {
	cur := d.lineEnding()
	name := endingName(cur)
	a.statusMenu(c, name, locales.CmdLineEndings(name), func(m *ui.Menu) {
		for _, e := range lineEndings {
			if m.Item(e.label(locales.In(i18n.Locale()))).Checked(e.text == cur).Chosen() {
				a.run(e.id())
			}
		}
	})
}

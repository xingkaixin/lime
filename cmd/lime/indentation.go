package main

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

func tabWidth(n int) string { return "set_setting tab_size " + strconv.Itoa(n) }

// indentCommands are those of Sublime Text's View > Indentation menu.
func indentCommands() []command {
	c := []command{
		{id: "toggle_setting translate_tabs_to_spaces", caption: locales.Messages.CmdIndentUsingSpaces, run: withDoc(func(a *app, d *doc) {
			_, spaces := a.indentOf(d)
			d.indent = editor.IndentSpaces
			if spaces {
				d.indent = editor.IndentTabs
			}
		})},
		{id: "detect_indentation", caption: locales.Messages.CmdGuessIndentation, run: withDoc(func(_ *app, d *doc) { d.guessIndent() })},
		{id: "expand_tabs", caption: locales.Messages.CmdConvertToSpaces, run: withDoc(func(a *app, d *doc) { a.convertIndent(d, true) })},
		{id: "unexpand_tabs", caption: locales.Messages.CmdConvertToTabs, run: withDoc(func(a *app, d *doc) { a.convertIndent(d, false) })},
	}
	for n := 1; n <= 8; n++ {
		caption := func(m locales.Messages) string { return m.CmdTabWidth(n) }
		c = append(c, command{id: tabWidth(n), caption: caption, run: withDoc(func(_ *app, d *doc) { d.tabSize = n })})
	}
	return c
}

// applyIndent has d's editor indent as the file does, or as the settings
// say where the file has no indentation of its own.
func (a *app) applyIndent(d *doc) {
	// A tab_size of 0 in settings.json is the editor's default.
	d.st.TabSize, d.st.Indent = cmp.Or(d.tabSize, a.set.TabSize, 4), cmp.Or(d.indent, a.set.indent())
}

// indentOf is the width of a tab in d, and whether indenting inserts
// spaces.
func (a *app) indentOf(d *doc) (size int, spaces bool) {
	a.applyIndent(d)
	return d.st.TabSize, indentUnit(d.st) != "\t"
}

// indentButton shows how d indents, and changes it from the menu of
// View > Indentation, as Sublime Text's status bar does.
func (a *app) indentButton(c *ui.Context, d *doc) {
	size, spaces := a.indentOf(d)
	text := locales.StatusTabSize(size)
	if spaces {
		text = locales.StatusSpaces(size)
	}
	a.statusMenu(c, text, text, func(m *ui.Menu) {
		if m.Item(locales.MenuIndentUsingSpaces()).Checked(spaces).Chosen() {
			a.run("toggle_setting translate_tabs_to_spaces")
		}
		m.Separator()
		for n := 1; n <= 8; n++ {
			if m.Item(locales.MenuTabWidth(n)).Checked(n == size).Chosen() {
				a.run(tabWidth(n))
			}
		}
		m.Separator()
		if m.Item(locales.MenuGuessIndentation()).Chosen() {
			a.run("detect_indentation")
		}
		m.Separator()
		if m.Item(locales.MenuConvertToSpaces()).Chosen() {
			a.run("expand_tabs")
		}
		if m.Item(locales.MenuConvertToTabs()).Chosen() {
			a.run("unexpand_tabs")
		}
	})
}

// guessLines is how many lines guessIndent reads, which a large file
// would take long to read all of as it opens.
const guessLines = 10000

// guessIndent finds how text indents, as Sublime Text's
// detect_indentation does: with tabs when more lines start with one than
// with spaces, else with spaces as many as the step most lines indent by
// from the line before. The style is IndentAuto when no line is
// indented, and the size 0 when the text does not tell it.
func guessIndent(t editor.Snapshot) (size int, style editor.IndentStyle) {
	var steps [9]int
	tabs, spaced, prev := 0, 0, 0
	for i := range min(t.LineCount(), guessLines) {
		line := t.Line(i)
		body := strings.TrimLeft(line, " \t")
		if body == "" {
			continue
		}
		lead := line[:len(line)-len(body)]
		if strings.HasPrefix(lead, "\t") {
			tabs++
			continue
		}
		n := len(lead) - len(strings.TrimLeft(lead, " "))
		// One space is a block comment's " * " more often than indentation.
		if n > 1 {
			spaced++
		}
		if step := n - prev; step > 1 && step < len(steps) {
			steps[step]++
		}
		prev = n
	}
	switch {
	case tabs == 0 && spaced == 0:
		return 0, editor.IndentAuto
	case tabs >= spaced:
		return 0, editor.IndentTabs
	}
	for s := 2; s < len(steps); s++ {
		if steps[s] > steps[size] {
			size = s
		}
	}
	return size, editor.IndentSpaces
}

// guessIndent has d indent as its text does, where the text tells.
func (d *doc) guessIndent() {
	size, style := guessIndent(d.st.Buffer.Snapshot())
	if style == editor.IndentAuto {
		return
	}
	d.indent = style
	if size > 0 {
		d.tabSize = size
	}
}

// convertIndent rewrites the indentation of d's lines as spaces, or as
// tabs and the spaces short of a tab, as Sublime Text's expand_tabs and
// unexpand_tabs do, and has d indent so from then on.
func (a *app) convertIndent(d *doc, spaces bool) {
	size, _ := a.indentOf(d)
	t := d.st.Buffer.Snapshot()
	var edits []editor.Edit
	for i := range t.LineCount() {
		line := t.Line(i)
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		col := 0
		for _, c := range []byte(lead) {
			if c == '\t' {
				col += size - col%size
			} else {
				col++
			}
		}
		to := strings.Repeat(" ", col)
		if !spaces {
			to = strings.Repeat("\t", col/size) + strings.Repeat(" ", col%size)
		}
		if to != lead {
			start := t.LineStart(i)
			edits = append(edits, editor.Edit{Start: start, End: start + len(lead), Text: to})
		}
	}
	d.st.Buffer.Edit(edits...)
	d.indent = editor.IndentTabs
	if spaces {
		d.indent = editor.IndentSpaces
	}
}

// translateTabs is TranslateTabsToSpaces of style, and indentStyle the
// style of one.
func translateTabs(style editor.IndentStyle) *bool {
	if style == editor.IndentAuto {
		return nil
	}
	spaces := style == editor.IndentSpaces
	return &spaces
}

func indentStyle(spaces *bool) editor.IndentStyle {
	switch {
	case spaces == nil:
		return editor.IndentAuto
	case *spaces:
		return editor.IndentSpaces
	}
	return editor.IndentTabs
}

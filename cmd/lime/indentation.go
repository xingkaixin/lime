package main

import (
	"cmp"
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

// applyIndent has d's editor indent as the file does, or as the settings
// say where the file has no indentation of its own.
func (a *app) applyIndent(d *doc) {
	d.st.TabSize, d.st.Indent = cmp.Or(d.tabSize, a.set.TabSize), cmp.Or(d.indent, a.set.indent())
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

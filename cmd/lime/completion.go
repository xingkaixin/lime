package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// completeSizeLimit is Sublime Text's auto_complete_size_limit: a larger
// text has no words to complete, nor completes as it is typed.
const completeSizeLimit = 4 << 20

// wordList is the words of a doc's text at a version, each once, in the
// order they first come, and how many times each comes.
type wordList struct {
	version uint64
	words   []string
	count   map[string]int
}

// wordList returns the words of d's text, which it scans again only
// once the text changed.
func (d *doc) wordList() *wordList {
	s := d.st.Buffer.Snapshot()
	if w := d.words; w != nil && w.version == s.Version() {
		return w
	}
	w := &wordList{version: s.Version(), count: make(map[string]int)}
	d.words = w
	if s.Len() > completeSizeLimit {
		return w
	}
	chars := wordChars(d.st.Buffer)
	text := s.String()
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		if !isWordRune(r, chars) {
			i += n
			continue
		}
		e := wordEnd(text, i, chars)
		// A word of one character completes nothing, and numbers, which
		// typing would otherwise complete, take Enter from a new line.
		if word := text[i:e]; e > i+n && !unicode.IsDigit(r) {
			if _, ok := w.count[word]; !ok {
				word = strings.Clone(word)
				w.words = append(w.words, word)
			}
			w.count[word]++
		}
		i = e
	}
	return w
}

func wordChars(b *editor.Buffer) string {
	if l := b.Language(); l != nil {
		return l.WordChars
	}
	return ""
}

// complete answers d's request for completions with the words of the
// window's tabs, d's first, as Sublime Text's auto_complete does: not
// the word typed, unless it comes elsewhere too.
func (a *app) complete(d *doc, r editor.CompletionRequest) {
	b := d.st.Buffer
	if !r.Explicit && (!a.set.AutoComplete || r.Prefix == "" || !completesTyping(b)) {
		return
	}
	p := b.Pos(r.Offset)
	line := b.Line(p.Line)
	typed := r.Prefix + line[p.Col:wordEnd(line, p.Col, wordChars(b))]
	var items []editor.Completion
	seen := make(map[string]bool)
	add := func(w *wordList, skip string) {
		for _, word := range w.words {
			if word != skip && !seen[word] {
				seen[word] = true
				items = append(items, editor.Completion{Label: word})
			}
		}
	}
	own, skip := d.wordList(), ""
	if own.count[typed] == 1 {
		skip = typed
	}
	add(own, skip)
	for _, o := range a.allDocs() {
		if o != d && o.kind == textDoc {
			add(o.wordList(), "")
		}
	}
	d.st.ShowCompletions(r.Offset-len(r.Prefix), items)
}

// completesTyping reports whether typing in b shows completions, as
// Sublime Text's auto_complete_selector has it: in code, not prose.
func completesTyping(b *editor.Buffer) bool {
	l := b.Language()
	return l != nil && l.Name != "Markdown" && b.Len() <= completeSizeLimit
}

// renderCompletions leaves out a list of only the word typed, as Sublime
// Text does, which would take Enter from a new line.
func renderCompletions(c *ui.Context, panel ui.Element, m *editor.CompletionMenu) {
	if items := m.Items(); len(items) == 1 && items[0].Label == m.Prefix() {
		m.Close()
		return
	}
	editor.DefaultCompletions(c, panel, m)
}

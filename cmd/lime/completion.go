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
// order they first come, but for the word at caret, which is being typed
// (-1 for none).
type wordList struct {
	version uint64
	caret   int
	words   []string
	has     map[string]bool
}

func scanWords(s editor.Snapshot, chars string, caret int) *wordList {
	w := &wordList{version: s.Version(), caret: caret, has: make(map[string]bool)}
	if s.Len() > completeSizeLimit {
		return w
	}
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
		if word := text[i:e]; e > i+n && !unicode.IsDigit(r) && (caret < i || caret > e) && !w.has[word] {
			word = strings.Clone(word)
			w.words = append(w.words, word)
			w.has[word] = true
		}
		i = e
	}
	return w
}

// wordList returns the words of d's text but for the word at caret,
// scanning it again only once it changed.
func (a *app) wordList(d *doc, caret int) *wordList {
	s := d.st.Buffer.Snapshot()
	if w := d.words; w != nil && w.version == s.Version() && w.caret == caret {
		return w
	}
	d.words = scanWords(s, wordChars(d.st.Buffer), caret)
	return d.words
}

// tabWords is the words of the window's tabs but the current one, each
// once.
type tabWords struct {
	words []string
	has   map[string]bool
}

func (a *app) otherWords(d *doc) *tabWords {
	t := &tabWords{has: make(map[string]bool)}
	for _, o := range a.allDocs() {
		if o != d && o.kind == textDoc {
			for _, word := range a.wordList(o, -1).words {
				if !t.has[word] {
					t.has[word] = true
					t.words = append(t.words, word)
				}
			}
		}
	}
	a.others = t
	return t
}

func wordChars(b *editor.Buffer) string {
	if l := b.Language(); l != nil {
		return l.WordChars
	}
	return ""
}

func wordStart(s string, i int, chars string) int {
	for i > 0 {
		r, n := utf8.DecodeLastRuneInString(s[:i])
		if !isWordRune(r, chars) {
			break
		}
		i -= n
	}
	return i
}

// complete answers d's request for completions with the words of the
// window's tabs, d's first, as Sublime Text's auto_complete does.
func (a *app) complete(d *doc, r editor.CompletionRequest) {
	b := d.st.Buffer
	p := b.Pos(r.Offset)
	if !r.Explicit && (!a.set.AutoComplete || r.Prefix == "" || !completesTyping(b) || inComment(b, p) || !sameWords(d.st, r.Prefix)) {
		return
	}
	own, others := a.wordList(d, r.Offset), a.otherWords(d)
	// The word typed, a word itself, would be chosen first, and Enter
	// would complete it rather than break the line.
	if !r.Explicit && (own.has[r.Prefix] || others.has[r.Prefix]) {
		d.st.ShowCompletions(-1, nil)
		return
	}
	items := make([]editor.Completion, 0, len(own.words)+len(others.words))
	for _, w := range own.words {
		items = append(items, editor.Completion{Label: w})
	}
	for _, w := range others.words {
		if !own.has[w] {
			items = append(items, editor.Completion{Label: w})
		}
	}
	d.st.ShowCompletions(r.Offset-len(r.Prefix), items)
}

// typing closes the completions before text is typed that makes the word
// typed one of them, for complete to decide whether they show: the
// editor would otherwise narrow them down itself.
func (a *app) typing(d *doc, ev ui.InputEvent) {
	if ev.Kind != ui.InputText || d.words == nil || a.others == nil {
		return
	}
	b := d.st.Buffer
	chars := wordChars(b)
	if strings.ContainsFunc(ev.Text, func(r rune) bool { return !isWordRune(r, chars) }) {
		return
	}
	p := b.Pos(d.st.Cursor())
	line := b.Line(p.Line)
	if word := line[wordStart(line, p.Col, chars):p.Col] + ev.Text; d.words.has[word] || a.others.has[word] {
		d.st.ShowCompletions(-1, nil)
	}
}

// completesTyping reports whether typing in b shows completions, as
// Sublime Text's auto_complete_selector has it: in code, not prose.
func completesTyping(b *editor.Buffer) bool {
	l := b.Language()
	return l != nil && l.Name != "Markdown" && b.Len() <= completeSizeLimit
}

// inComment reports whether the text before p is a comment, as far as its
// line tells: a comment opened on a line above is not seen.
func inComment(b *editor.Buffer, p editor.Pos) bool {
	l := b.Language()
	if l == nil || l.Tokenizer == nil || p.Col == 0 {
		return false
	}
	toks, _ := l.Tokenizer.Tokenize(b.Line(p.Line)[:p.Col], 0, nil)
	for _, t := range toks {
		if t.End >= p.Col {
			return t.Kind == editor.TokenComment
		}
	}
	return false
}

// sameWords reports whether prefix is the word before each caret. The
// editor completes the word before the others only where they have the
// same, and puts the completion beside the others' words.
func sameWords(st *editor.State, prefix string) bool {
	b := st.Buffer
	chars := wordChars(b)
	for _, s := range st.Selections() {
		p := b.Pos(s.Head)
		line := b.Line(p.Line)
		if line[wordStart(line, p.Col, chars):p.Col] != prefix {
			return false
		}
	}
	return true
}

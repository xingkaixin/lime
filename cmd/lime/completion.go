package main

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// completeSizeLimit is Sublime Text's auto_complete_size_limit: a larger
// text has no words to complete, nor completes as it is typed.
const completeSizeLimit = 4 << 20

// syncScanLimit is the largest text scanned for words while completions
// wait, in about a millisecond. A larger one is scanned in the
// background, and its words as last found serve meanwhile.
const syncScanLimit = 128 << 10

type wordList struct {
	version uint64
	chars   string
	words   []string
	index   map[string]int
	counts  []int
}

func (w *wordList) has(word, exclude string) bool {
	i, ok := w.index[word]
	if !ok {
		return false
	}
	c := w.counts[i]
	if exclude != "" && word == exclude {
		return c > 1
	}
	return c > 0
}

var noWords wordList

func scanWords(s editor.Snapshot, chars string) *wordList {
	w := &wordList{version: s.Version(), chars: chars, index: make(map[string]int)}
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
		if e > i+n && !unicode.IsDigit(r) {
			word := text[i:e]
			if idx, ok := w.index[word]; ok {
				w.counts[idx]++
			} else {
				word = strings.Clone(word)
				w.index[word] = len(w.words)
				w.words = append(w.words, word)
				w.counts = append(w.counts, 1)
			}
		}
		i = e
	}
	return w
}

func (a *app) wordList(d *doc) *wordList {
	s := d.st.Buffer.Snapshot()
	chars := wordChars(d.st.Buffer)
	w := d.words
	switch {
	case w != nil && w.version == s.Version() && w.chars == chars || d.scanning:
	case s.Len() <= syncScanLimit || a.win == nil:
		w = scanWords(s, chars)
		d.words = w
	default:
		d.scanning = true
		go func() {
			found := scanWords(s, chars)
			a.win.Update(func() {
				d.words, d.scanning = found, false
				// The completions asked for meanwhile get the words found.
				if r := d.asked; (r.Explicit || r.Prefix != "") && d == a.current() && d.st.Buffer.Version() == r.Version && d.st.Cursor() == r.Offset {
					a.complete(d, r)
				}
			})
		}()
	}
	if w == nil {
		return &noWords
	}
	return w
}

type tabWords struct {
	lists []*wordList
	words []string
	has   map[string]bool
}

func (a *app) otherWords(d *doc) *tabWords {
	var lists []*wordList
	for _, o := range a.allDocs() {
		if o != d && o.kind == textDoc {
			lists = append(lists, a.wordList(o))
		}
	}
	if a.others == nil || !slices.Equal(lists, a.others.lists) {
		t := &tabWords{lists: lists, has: make(map[string]bool)}
		for _, w := range lists {
			for _, word := range w.words {
				if !t.has[word] {
					t.has[word] = true
					t.words = append(t.words, word)
				}
			}
		}
		a.others = t
	}
	return a.others
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

// matchesPrefix reports whether s contains all characters of prefix in order,
// ignoring case, matching editor.State's completion subsequence filter.
func matchesPrefix(prefix, s string) bool {
	if prefix == "" {
		return true
	}
	if isASCII(prefix) && isASCII(s) {
		pi := 0
		for i := 0; i < len(s); i++ {
			if lower(s[i]) == lower(prefix[pi]) {
				pi++
				if pi == len(prefix) {
					return true
				}
			}
		}
		return false
	}
	pi := 0
	pr, pn := utf8.DecodeRuneInString(prefix)
	pl := unicode.ToLower(pr)
	for _, r := range s {
		if unicode.ToLower(r) == pl {
			pi += pn
			if pi == len(prefix) {
				return true
			}
			pr, pn = utf8.DecodeRuneInString(prefix[pi:])
			pl = unicode.ToLower(pr)
		}
	}
	return false
}

// complete answers d's request for completions with the words of the
// window's tabs, d's first, as Sublime Text's auto_complete does.
func (a *app) complete(d *doc, r editor.CompletionRequest) {
	b := d.st.Buffer
	p := b.Pos(r.Offset)
	if !r.Explicit && (!a.set.AutoComplete || r.Prefix == "" || !completesTyping(b) || inCommentAt(b, p, &d.lex) || !sameWords(d.st, r.Prefix)) {
		return
	}
	d.asked = r
	own, others := a.wordList(d), a.otherWords(d)
	if !d.scanning {
		d.asked = editor.CompletionRequest{}
	}
	exclude := ""
	if own.version == b.Version() {
		chars := wordChars(b)
		line := b.Line(p.Line)
		exclude = line[wordStart(line, p.Col, chars):wordEnd(line, p.Col, chars)]
	}
	// The word typed, a word itself, would be chosen first, and Enter
	// would complete it rather than break the line.
	if !r.Explicit && (own.has(r.Prefix, exclude) || others.has[r.Prefix]) {
		d.st.ShowCompletions(-1, nil)
		return
	}
	var items []editor.Completion
	if r.Prefix == "" {
		items = make([]editor.Completion, 0, len(own.words)+len(others.words))
	}
	for _, w := range own.words {
		if !own.has(w, exclude) {
			continue
		}
		if r.Prefix == "" || matchesPrefix(r.Prefix, w) {
			items = append(items, editor.Completion{Label: w})
		}
	}
	for _, w := range others.words {
		if own.has(w, exclude) {
			continue
		}
		if r.Prefix == "" || matchesPrefix(r.Prefix, w) {
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
	start := wordStart(line, p.Col, chars)
	exclude := ""
	if d.words.version == b.Version() {
		exclude = line[start:wordEnd(line, p.Col, chars)]
	}
	if word := line[start:p.Col] + ev.Text; d.words.has(word, exclude) || a.others.has[word] {
		d.st.ShowCompletions(-1, nil)
	}
}

// completesTyping reports whether typing in b shows completions, as
// Sublime Text's auto_complete_selector has it: in code, not prose.
func completesTyping(b *editor.Buffer) bool {
	l := b.Language()
	return l != nil && l.Name != "Markdown" && b.Len() <= completeSizeLimit
}

// maxTokenizeLine matches MyGo's maxTokenize so minified lines do not stall completion.
const maxTokenizeLine = 10000

type lexCache struct {
	lang   *editor.Language
	snap   editor.Snapshot
	states []uint64
}

func (c *lexCache) stateAt(snap editor.Snapshot, l *editor.Language, line int) uint64 {
	if line <= 0 {
		return 0
	}
	if c == nil {
		_, state := propagateLineStates(nil, snap, l.Tokenizer, 0, line, false)
		return state
	}
	if c.lang != l || len(c.states) == 0 {
		c.lang = l
		c.snap = snap
		c.states = append(c.states[:0], 0)
	} else if c.snap.Version() != snap.Version() {
		limit := snap.LineStart(min(line, len(c.states)-1))
		valid := snap.LineOf(commonPrefix(c.snap, snap, limit)) + 1
		if valid < len(c.states) {
			c.states = c.states[:valid]
		}
		c.snap = snap
	}
	if from := len(c.states) - 1; from < line {
		c.states, _ = propagateLineStates(c.states, snap, l.Tokenizer, from, line, true)
	}
	return c.states[line]
}

func commonPrefix(a, b editor.Snapshot, limit int) int {
	end := min(limit, a.Len(), b.Len())
	off := 0
	a.Chunks(0, end, func(s string) bool {
		same := true
		b.Chunks(off, off+len(s), func(t string) bool {
			if s[:len(t)] == t {
				off += len(t)
				s = s[len(t):]
				return true
			}
			for i := 0; i < len(t) && s[i] == t[i]; i++ {
				off++
			}
			same = false
			return false
		})
		return same
	})
	return off
}

func propagateLineStates(states []uint64, snap editor.Snapshot, t editor.Tokenizer, from, to int, keep bool) ([]uint64, uint64) {
	var state uint64
	if len(states) > from {
		state = states[from]
	}
	if keep {
		states = slices.Grow(states, to-from)
	}
	var toks []editor.Token
	for i := from; i < to; {
		end := min(i+256, to)
		text := snap.Slice(snap.LineStart(i), snap.LineStart(end))
		for ; i < end; i++ {
			nl := strings.IndexByte(text, '\n')
			var ln string
			if nl >= 0 {
				ln, text = text[:nl], text[nl+1:]
			} else {
				ln, text = text, ""
			}
			if len(ln) <= maxTokenizeLine {
				toks, state = t.Tokenize(ln, state, toks[:0])
			}
			if keep {
				states = append(states, state)
			}
		}
	}
	return states, state
}

func inComment(b *editor.Buffer, p editor.Pos) bool {
	return inCommentAt(b, p, nil)
}

func inCommentAt(b *editor.Buffer, p editor.Pos, cache *lexCache) bool {
	l := b.Language()
	if l == nil || l.Tokenizer == nil {
		return false
	}
	snap := b.Snapshot()
	if p.Line < 0 || p.Line >= snap.LineCount() {
		return false
	}
	state := cache.stateAt(snap, l, p.Line)
	col := max(0, p.Col)
	if col == 0 && state == 0 {
		return false
	}
	line := snap.Line(p.Line)
	col = min(col, len(line))
	if col > maxTokenizeLine {
		return false
	}
	// Append a space so a caret right after a closing delimiter (e.g. */) is
	// classified outside the comment while col == 0 inside one stays a comment.
	toks, _ := l.Tokenizer.Tokenize(line[:col]+" ", state, nil)
	return len(toks) > 0 && toks[len(toks)-1].End >= col+1 && toks[len(toks)-1].Kind == editor.TokenComment
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

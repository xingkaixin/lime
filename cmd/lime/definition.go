package main

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// definers are the keywords, of any language, whose next name is defined
// there. MyGo's tokenizers mark keywords, but not the names they define.
var definers = map[string]bool{
	"actor": true, "class": true, "const": true, "def": true, "enum": true, "fn": true, "fun": true,
	"func": true, "function": true, "input": true, "interface": true, "message": true, "mixin": true,
	"mod": true, "module": true, "object": true, "protocol": true, "record": true, "rpc": true,
	"scalar": true, "service": true, "struct": true, "trait": true, "type": true, "typealias": true,
	"union": true,
}

// symbol is a name a definition defines, at its offset in the text.
type symbol struct {
	name string
	at   int
}

// symbols lists the definitions of text in language l: the name after a
// definer, or a function after a type or a name, as C's int f(.
func symbols(l *editor.Language, text string) []symbol {
	if l == nil || l.Tokenizer == nil {
		return nil
	}
	var syms []symbol
	var toks []editor.Token
	var state uint64
	at := 0
	for line := range strings.SplitSeq(text, "\n") {
		toks, state = l.Tokenizer.Tokenize(line, state, toks[:0])
		start, prev := 0, editor.TokenText
		for _, t := range toks {
			switch word := line[start:t.End]; {
			case t.Kind == editor.TokenKeyword && definers[word]:
				if name, i := definedAfter(line, t.End, l.WordChars); name != "" {
					syms = append(syms, symbol{name, at + i})
				}
			case t.Kind == editor.TokenFunction && (prev == editor.TokenType || prev == editor.TokenVariable):
				syms = append(syms, symbol{word, at + start})
			}
			if t.Kind != editor.TokenText {
				prev = t.Kind
			}
			start = t.End
		}
		at += len(line) + 1
	}
	return syms
}

// definedAfter is the name a definer ending at i in line defines, and
// where it is in line: past a Go method's receiver, and the last of a
// path, as Lua's function M.f or Ruby's def self.f.
func definedAfter(line string, i int, chars string) (string, int) {
	i = skipSpace(line, i)
	if strings.HasPrefix(line[i:], "(") {
		j := strings.IndexByte(line[i:], ')')
		if j < 0 {
			return "", 0
		}
		i = skipSpace(line, i+j+1)
	}
	name, at := "", 0
	for {
		e := wordEnd(line, i, chars)
		if e == i {
			return name, at
		}
		name, at = line[i:e], i
		if e == len(line) || line[e] != '.' && line[e] != ':' {
			return name, at
		}
		i = e + 1
	}
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func isWordRune(r rune, chars string) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(chars, r)
}

func wordEnd(s string, i int, chars string) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !isWordRune(r, chars) {
			break
		}
		i += n
	}
	return i
}

// wordAt is the word of text at offset i, before or after it.
func wordAt(text string, i int, chars string) string {
	start := i
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(text[:start])
		if !isWordRune(r, chars) {
			break
		}
		start -= n
	}
	return text[start:wordEnd(text, i, chars)]
}

// definition is where a symbol is defined.
type definition struct {
	d  *doc
	at int
}

// gotoDefinition goes to the definition of the word at offset off of d,
// as Sublime Text's goto_definition does, but from the open tabs rather
// than an index: d's definitions, else those of the window's other tabs,
// listed when there are more than one.
func (a *app) gotoDefinition(d *doc, off int) {
	var chars string
	if l := d.st.Buffer.Language(); l != nil {
		chars = l.WordChars
	}
	name := wordAt(d.st.Buffer.String(), off, chars)
	if name == "" {
		return
	}
	defs := definitionsIn(d, name)
	if len(defs) == 0 {
		for _, o := range a.allDocs() {
			if o != d && o.kind == textDoc {
				defs = append(defs, definitionsIn(o, name)...)
			}
		}
	}
	switch len(defs) {
	case 0:
		a.notify(locales.StatusNoDefinition(name))
	case 1:
		a.showDefinition(defs[0])
	default:
		items := make([]item, len(defs))
		for i, def := range defs {
			p := def.d.st.Buffer.Pos(def.at)
			items[i] = item{
				label:  strings.TrimSpace(def.d.st.Buffer.Line(p.Line)),
				detail: def.d.name() + ":" + strconv.Itoa(p.Line+1),
				pick:   func() { a.showDefinition(def) },
			}
		}
		a.openPanel(&panel{list: func(q string) []item { return ranked(items, byLabel(q)) }})
	}
}

func definitionsIn(d *doc, name string) []definition {
	var defs []definition
	for _, s := range symbols(d.st.Buffer.Language(), d.st.Buffer.String()) {
		if s.name == name {
			defs = append(defs, definition{d, s.at})
		}
	}
	return defs
}

func (a *app) showDefinition(def definition) {
	a.show(def.d)
	def.d.st.GoTo(def.d.st.Buffer.Pos(def.at))
}

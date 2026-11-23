package main

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"lime/locales"
)

const (
	fileIconsDefault = "file-icons"
	fileIconsPierre  = "pierre"
)

type fileIconSet struct {
	name  string
	label func(locales.Messages) string
}

func (f fileIconSet) id() string { return "set_file_icons " + f.name }

var fileIconSets = []fileIconSet{
	{fileIconsDefault, locales.Messages.FileIconsDefault},
	{fileIconsPierre, locales.Messages.FileIconsPierre},
}

type fileIconRule struct {
	icon, dark, light string
	pattern           string
	priority          float64
	matchPath         bool
}

type fileIconGlyph struct {
	font, char string
	size       float64 // in CSS pixels, of a 16 pixel row
}

type rowIcon struct {
	svg        *ui.SVG
	dual       *ui.SVG // two-colour SVG; falls back to monochrome svg when overridden by selection tint
	font, char string
	scale      float32
	color      ui.Color
}

func (ic rowIcon) tint(fallback ui.Color) ui.Color {
	if ic.color == (ui.Color{}) {
		return fallback
	}
	return ic.color
}

func (ic rowIcon) view(c *ui.Context, size float32, tint ui.Color) {
	if ic.dual != nil && ic.color.A > 0 && tint.R == ic.color.R && tint.G == ic.color.G && tint.B == ic.color.B {
		img := ui.Image(c, ic.dual).Size(size, size).Shrink(0).TextColor(ic.color)
		if tint.A < ic.color.A {
			img.Opacity(float32(tint.A) / float32(ic.color.A))
		}
		return
	}
	if ic.svg != nil {
		ui.Icon(c, ic.svg).Size(size, size).Shrink(0).TextColor(tint)
		return
	}
	ui.Box(c).Size(size, size).Shrink(0).Center().Children(func() {
		ui.Text(c, ic.char).Font(ic.font).FontSize(size * ic.scale).TextColor(tint).SingleLine()
	})
}

var (
	folderIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<path fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round" d="M4.5 4.5H12c.83 0 1.5.67 1.5 1.5v6c0 .83-.67 1.5-1.5 1.5H2A1.5 1.5 0 01.5 12V3.5a1 1 0 011-1h5a1 1 0 011 1v1" />
</svg>`))
	folderOpenIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<path fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round" d="m1.87 8 .7-2.74a1 1 0 01.96-.76h10.94a1 1 0 01.97 1.24l-1.75 7a1 1 0 01-.97.76H2A1.5 1.5 0 01.5 12V3.5a1 1 0 011-1h5a1 1 0 011 1v1" />
</svg>`))
)

// onIconFontLoaded redraws open windows when a lazily fetched icon font becomes ready.
var onIconFontLoaded func()

func fileIconFamily(font string) string {
	switch font {
	case "file-icons":
		return "fileicons-file-icons"
	case "fontawesome":
		return "fileicons-fontawesome"
	case "devopicons":
		return "fileicons-devopicons"
	case "octicons":
		return "fileicons-octicons"
	case "mfixx":
		return "fileicons-mfixx"
	default:
		return "fileicons-" + font
	}
}

type iconLitEntry struct {
	idx int
	cs  bool   // case-sensitive match against raw
	raw string // original literal (only checked when cs is true)
}

type iconRegexEntry struct {
	idx   int
	hints []string // lowercased ASCII substrings; at least one must appear in any match
}

type iconIndex struct {
	exact       map[string][]iconLitEntry
	ext         map[string][]iconLitEntry
	pathRules   []iconRegexEntry
	nameRules   []iconRegexEntry
	compiledMu  sync.RWMutex
	compiled    []*regexp.Regexp
}

func (ix *iconIndex) regex(i int) *regexp.Regexp {
	ix.compiledMu.RLock()
	re := ix.compiled[i]
	ix.compiledMu.RUnlock()
	if re != nil {
		return re
	}
	re = regexp.MustCompile(fileIconRules[i].pattern)
	ix.compiledMu.Lock()
	ix.compiled[i] = re
	ix.compiledMu.Unlock()
	return re
}

func buildIconIndex() *iconIndex {
	ix := &iconIndex{
		exact:    make(map[string][]iconLitEntry, 256),
		ext:      make(map[string][]iconLitEntry, 1600),
		compiled: make([]*regexp.Regexp, len(fileIconRules)),
	}
	for i, r := range fileIconRules {
		if r.matchPath {
			ix.pathRules = append(ix.pathRules, iconRegexEntry{idx: i, hints: requiredASCIIHints(r.pattern)})
			continue
		}
		if exacts, exts, ci, ok := classifyNameRule(r.pattern); ok {
			for _, e := range exacts {
				k := strings.ToLower(e)
				ix.exact[k] = append(ix.exact[k], iconLitEntry{idx: i, cs: !ci, raw: e})
			}
			for _, e := range exts {
				k := strings.ToLower(e)
				ix.ext[k] = append(ix.ext[k], iconLitEntry{idx: i, cs: !ci, raw: e})
			}
			continue
		}
		ix.nameRules = append(ix.nameRules, iconRegexEntry{idx: i, hints: requiredASCIIHints(r.pattern)})
	}
	return ix
}

var fileIconIdx = sync.OnceValue(buildIconIndex)

// classifyNameRule extracts pure exact-filename or pure dot-suffix alternatives
// from a file-icons regex pattern so common lookups avoid regex compilation.
func classifyNameRule(pat string) (exacts, exts []string, ci, ok bool) {
	p := pat
	if strings.HasPrefix(p, "(?i)") {
		ci = true
		p = p[4:]
	}
	branches := splitTopAlternation(p)
	if len(branches) == 0 {
		return nil, nil, false, false
	}
	for _, b := range branches {
		ex, sf, bok := parseSimpleBranch(b)
		if !bok {
			return nil, nil, false, false
		}
		exacts = append(exacts, ex...)
		exts = append(exts, sf...)
	}
	return exacts, exts, ci, len(exacts)+len(exts) > 0
}

func splitTopAlternation(p string) []string {
	var out []string
	depth, inClass, start := 0, false, 0
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' {
			i++
			continue
		}
		if inClass {
			if c == ']' {
				inClass = false
			}
			continue
		}
		switch c {
		case '[':
			inClass = true
		case '(':
			depth++
		case ')':
			depth--
		case '|':
			if depth == 0 {
				out = append(out, p[start:i])
				start = i + 1
			}
		}
	}
	if depth != 0 || inClass {
		return nil
	}
	out = append(out, p[start:])
	return out
}

func parseSimpleBranch(b string) (exacts, exts []string, ok bool) {
	if !strings.HasSuffix(b, "$") {
		return nil, nil, false
	}
	body := b[:len(b)-1]
	isExact := false
	if strings.HasPrefix(body, "^") {
		isExact = true
		body = body[1:]
	} else if !strings.HasPrefix(body, `\.`) {
		return nil, nil, false
	}
	lits, lok := expandSimpleLiterals(body)
	if !lok || len(lits) == 0 {
		return nil, nil, false
	}
	if isExact {
		return lits, nil, true
	}
	for _, s := range lits {
		if len(s) < 2 || s[0] != '.' {
			return nil, nil, false
		}
	}
	return nil, lits, true
}

// expandSimpleLiterals expands a pattern fragment containing only literal
// characters, escaped punctuation, optional single chars (x?), small literal
// char classes ([cm]), and flat alternations ((?:a|b) or (a|b)).
func expandSimpleLiterals(s string) ([]string, bool) {
	results := [][]byte{{}}
	appendByte := func(ch byte) {
		for i := range results {
			results[i] = append(results[i], ch)
		}
	}
	cloneWithByte := func(b []byte, ch byte) []byte {
		out := make([]byte, len(b)+1)
		copy(out, b)
		out[len(b)] = ch
		return out
	}
	for i := 0; i < len(s); i++ {
		if len(results) > 16 {
			return nil, false
		}
		c := s[i]
		if c == '\\' {
			if i+1 >= len(s) {
				return nil, false
			}
			next := s[i+1]
			if !isEscapableLit(next) {
				return nil, false
			}
			i++
			if i+1 < len(s) && s[i+1] == '?' {
				i++
				n := len(results)
				for j := range n {
					results = append(results, cloneWithByte(results[j], next))
				}
			} else {
				appendByte(next)
			}
			continue
		}
		if c == '[' {
			end := strings.IndexByte(s[i+1:], ']')
			if end < 0 {
				return nil, false
			}
			inner := s[i+1 : i+1+end]
			if len(inner) == 0 || len(inner) > 4 || inner[0] == '^' {
				return nil, false
			}
			var chars [4]byte
			nc := 0
			for j := 0; j < len(inner); j++ {
				ch := inner[j]
				if ch == '\\' || ch == '-' || ch >= 0x80 {
					return nil, false
				}
				chars[nc] = ch
				nc++
			}
			i += 1 + end
			optional := false
			if i+1 < len(s) && s[i+1] == '?' {
				optional = true
				i++
			}
			mult := nc
			if optional {
				mult++
			}
			if len(results)*mult > 16 {
				return nil, false
			}
			nextRes := make([][]byte, 0, len(results)*mult)
			if optional {
				nextRes = append(nextRes, results...)
			}
			for _, r := range results {
				for j := range nc {
					nextRes = append(nextRes, cloneWithByte(r, chars[j]))
				}
			}
			results = nextRes
			continue
		}
		if c == '(' {
			end := findMatchingParen(s, i)
			if end < 0 {
				return nil, false
			}
			inner := s[i+1 : end]
			if strings.HasPrefix(inner, "?:") {
				inner = inner[2:]
			} else if strings.HasPrefix(inner, "?") {
				return nil, false
			}
			alts := splitTopAlternation(inner)
			if len(alts) == 0 {
				return nil, false
			}
			var expanded []string
			for _, a := range alts {
				sub, ok := expandSimpleLiterals(a)
				if !ok {
					return nil, false
				}
				expanded = append(expanded, sub...)
				if len(expanded) > 16 {
					return nil, false
				}
			}
			i = end
			optional := false
			if i+1 < len(s) && s[i+1] == '?' {
				optional = true
				i++
			}
			if len(results)*len(expanded) > 16 {
				return nil, false
			}
			nextRes := make([][]byte, 0, len(results)*(len(expanded)+1))
			if optional {
				nextRes = append(nextRes, results...)
			}
			for _, r := range results {
				for _, e := range expanded {
					buf := make([]byte, len(r)+len(e))
					copy(buf, r)
					copy(buf[len(r):], e)
					nextRes = append(nextRes, buf)
				}
			}
			results = nextRes
			continue
		}
		if isRegexMeta(c) {
			return nil, false
		}
		if i+1 < len(s) && s[i+1] == '?' {
			i++
			n := len(results)
			for j := range n {
				results = append(results, cloneWithByte(results[j], c))
			}
		} else {
			appendByte(c)
		}
	}
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = string(r)
	}
	return out, true
}

func isEscapableLit(c byte) bool {
	switch c {
	case '.', '-', '_', '+', '$', '@', '#', '~', '/':
		return true
	}
	return false
}

func isRegexMeta(c byte) bool {
	switch c {
	case '.', '*', '+', '?', '[', ']', '(', ')', '{', '}', '^', '$', '|', '\\':
		return true
	}
	return false
}

func findMatchingParen(s string, start int) int {
	depth, inClass := 0, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if c == '\\' {
			i++
			continue
		}
		if inClass {
			if c == ']' {
				inClass = false
			}
			continue
		}
		switch c {
		case '[':
			inClass = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// requiredASCIIHints returns one required lowercase ASCII substring per
// top-level alternation branch when every branch has a provable literal hint.
func requiredASCIIHints(pat string) []string {
	p := pat
	if strings.HasPrefix(p, "(?i)") {
		p = p[4:]
	}
	branches := splitTopAlternation(p)
	if len(branches) == 0 {
		return nil
	}
	hints := make([]string, 0, len(branches))
	for _, b := range branches {
		h := branchASCIIHint(b)
		if h == "" {
			return nil
		}
		hints = append(hints, h)
	}
	return hints
}

func branchASCIIHint(p string) string {
	var best, cur []byte
	flush := func() {
		if len(cur) > len(best) {
			best = append(best[:0], cur...)
		}
		cur = cur[:0]
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' {
			if i+1 >= len(p) {
				flush()
				break
			}
			next := p[i+1]
			i++
			if !isEscapableLit(next) {
				flush()
				continue
			}
			if hasZeroQuantifier(p, i+1) {
				flush()
				i = skipQuantifier(p, i+1) - 1
				continue
			}
			cur = append(cur, lower(next))
			continue
		}
		if c == '(' {
			flush()
			end := findMatchingParen(p, i)
			if end < 0 {
				return ""
			}
			i = end
			continue
		}
		if c == '[' {
			flush()
			i++
			for i < len(p) && p[i] != ']' {
				if p[i] == '\\' {
					i++
				}
				i++
			}
			continue
		}
		if isRegexMeta(c) || c >= 0x80 {
			flush()
			continue
		}
		if hasZeroQuantifier(p, i+1) {
			flush()
			i = skipQuantifier(p, i+1) - 1
			continue
		}
		cur = append(cur, lower(c))
	}
	flush()
	if len(best) < 2 {
		return ""
	}
	return string(best)
}

func hasZeroQuantifier(p string, at int) bool {
	if at >= len(p) {
		return false
	}
	switch p[at] {
	case '?', '*':
		return true
	case '{':
		return at+1 < len(p) && p[at+1] == '0'
	}
	return false
}

func skipQuantifier(p string, at int) int {
	if at >= len(p) {
		return at
	}
	if p[at] == '?' || p[at] == '*' || p[at] == '+' {
		return at + 1
	}
	if p[at] == '{' {
		for j := at + 1; j < len(p); j++ {
			if p[j] == '}' {
				return j + 1
			}
		}
	}
	return at
}

// containsFoldASCII reports whether s contains lowerSub (already lowercase ASCII)
// using ASCII case-insensitive comparison, with zero allocations.
func containsFoldASCII(s, lowerSub string) bool {
	n := len(lowerSub)
	if n == 0 {
		return true
	}
	if len(s) < n {
		return false
	}
	first := lowerSub[0]
	upper := first
	if first >= 'a' && first <= 'z' {
		upper = first - ('a' - 'A')
	}
	limit := len(s) - n
	for i := 0; i <= limit; i++ {
		c := s[i]
		if c != first && c != upper {
			continue
		}
		ok := true
		for j := 1; j < n; j++ {
			if lower(s[i+j]) != lowerSub[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func lowerKey(s string, buf *[64]byte) string {
	if len(s) <= len(buf) {
		allASCII := true
		for i := range len(s) {
			c := s[i]
			if c >= 0x80 {
				allASCII = false
				break
			}
			buf[i] = lower(c)
		}
		if allASCII {
			return string(buf[:len(s)])
		}
	}
	return strings.ToLower(s)
}

func matchesAnyHint(s string, hints []string) bool {
	if len(hints) == 0 {
		return true
	}
	for _, h := range hints {
		if containsFoldASCII(s, h) {
			return true
		}
	}
	return false
}

func (ix *iconIndex) matchLinear(s string, byPath bool) int {
	for i, r := range fileIconRules {
		if r.matchPath == byPath && ix.regex(i).MatchString(s) {
			return i
		}
	}
	return -1
}

func (ix *iconIndex) matchPath(s string) int {
	if !isASCII(s) {
		return ix.matchLinear(s, true)
	}
	for _, r := range ix.pathRules {
		if !matchesAnyHint(s, r.hints) {
			continue
		}
		if ix.regex(r.idx).MatchString(s) {
			return r.idx
		}
	}
	return -1
}

func (ix *iconIndex) matchName(s string) int {
	if !isASCII(s) {
		return ix.matchLinear(s, false)
	}
	best := len(fileIconRules)
	var buf [64]byte
	if entries := ix.exact[lowerKey(s, &buf)]; len(entries) > 0 {
		for _, e := range entries {
			if e.idx >= best {
				break
			}
			if !e.cs || s == e.raw {
				best = e.idx
				break
			}
		}
	}
	for off := strings.IndexByte(s, '.'); off >= 0; {
		suf := s[off:]
		if entries := ix.ext[lowerKey(suf, &buf)]; len(entries) > 0 {
			for _, e := range entries {
				if e.idx >= best {
					break
				}
				if !e.cs || suf == e.raw {
					best = e.idx
					break
				}
			}
		}
		next := strings.IndexByte(s[off+1:], '.')
		if next < 0 {
			break
		}
		off += 1 + next
	}
	for _, r := range ix.nameRules {
		if r.idx >= best {
			break
		}
		if !matchesAnyHint(s, r.hints) {
			continue
		}
		if ix.regex(r.idx).MatchString(s) {
			best = r.idx
			break
		}
	}
	if best == len(fileIconRules) {
		return -1
	}
	return best
}

// stripFileIconSuffixes removes file-icons' backup (~orig, ~previous) and
// template (.in, .inc, .dist, .tmpl, .tpl, .temp, .tmp, .tidy, .tdy) suffixes
// without running regular expressions on the common case.
func stripFileIconSuffixes(name string) string {
	s := name
	if strings.HasSuffix(s, "~orig") {
		s = s[:len(s)-len("~orig")]
	} else if strings.HasSuffix(s, "~previous") {
		s = s[:len(s)-len("~previous")]
	}
	dot := strings.LastIndexByte(s, '.')
	if dot <= 0 {
		return s
	}
	prefix := s[:dot]
	// Matches ^([^.]*\.[^.]+)\.(?:inc?|dist|tm?pl|te?mp|ti?dy)$ case-insensitively:
	// prefix must contain exactly one dot, with at least one char after it.
	firstDot := strings.IndexByte(prefix, '.')
	if firstDot < 0 || firstDot == len(prefix)-1 || strings.IndexByte(prefix[firstDot+1:], '.') >= 0 {
		return s
	}
	var buf [8]byte
	ext := s[dot+1:]
	if len(ext) < 2 || len(ext) > 4 {
		return s
	}
	for i := range len(ext) {
		buf[i] = lower(ext[i])
	}
	switch string(buf[:len(ext)]) {
	case "in", "inc", "dist", "tmpl", "tpl", "temp", "tmp", "tidy", "tdy":
		return prefix
	}
	return s
}

var fileIconRuleOf = map[string]int{}

// matchFileIcon returns the rule of the file at p, as file-icons' path
// strategy finds it: by the path, then by the name without a backup's or
// a template's suffix, then by the whole name where that matched nothing
// of priority.
func matchFileIcon(p string) int {
	if i, ok := fileIconRuleOf[p]; ok {
		return i
	}
	ix := fileIconIdx()
	name := path.Base(p)
	filtered := stripFileIconSuffixes(name)
	pathStr := p
	if !strings.HasPrefix(pathStr, "/") {
		pathStr = "/" + pathStr
	}
	i := ix.matchPath(pathStr)
	if i < 0 {
		i = ix.matchName(filtered)
	}
	if filtered != name && (i < 0 || fileIconRules[i].priority < 1) {
		i = ix.matchName(name)
	}
	fileIconRuleOf[p] = i
	return i
}

func fileIcon(p string, dark bool) rowIcon {
	name, colour := "text", ""
	if p != "" {
		slash := p
		if filepath.Separator != '/' {
			slash = filepath.ToSlash(p)
		}
		if i := matchFileIcon(slash); i >= 0 {
			r := fileIconRules[i]
			name, colour = r.icon, r.light
			if dark {
				colour = r.dark
			}
		}
	}
	g := fileIconGlyphs[name]
	ensureIconFont(g.font)
	ic := rowIcon{font: fileIconFamily(g.font), char: g.char, scale: float32(g.size / 16)}
	if c, ok := fileIconColours[colour]; ok {
		if dark {
			ic.color = ui.Hex(c[0])
		} else {
			ic.color = ui.Hex(c[1])
		}
	}
	return ic
}

func (a *app) fileIcon(p string) rowIcon {
	if a.set.FileIcons == fileIconsPierre {
		return pierreFileIcon(p, a.scheme.dark)
	}
	return fileIcon(p, a.scheme.dark)
}

func (a *app) folderIcon(open bool) rowIcon {
	if a.set.FileIcons == fileIconsPierre {
		return pierreFolderIcon(open, a.scheme.accent)
	}
	if open {
		return rowIcon{svg: folderOpenIcon, color: a.scheme.accent}
	}
	return rowIcon{svg: folderIcon}
}

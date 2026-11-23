package main

import (
	"bytes"
	"crypto/rand"
	"io"
	"iter"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

type doc struct {
	keyAny       any
	tabHandle    ui.Handle
	editorHandle ui.Handle
	path         string
	st           *editor.State
	columns      bool
	saved        editor.Snapshot
	disk         stamp
	base         string
	baseGen      int
	// checked is the version modified was found for: dirty compares the
	// text with saved once a version.
	checked  uint64
	modified bool
	tabSize  int
	indent   editor.IndentStyle
	// savedEnding is the line ending on disk, which the buffer changes
	// without a new version. cr has the lines end in \r alone, which the
	// buffer has as \n.
	savedEnding string
	cr          bool
	enc         *fileEncoding

	kind      docKind
	img       *ui.Bitmap
	loading   bool
	cmp       *comparison
	commit    *commitView
	results   *results
	md        *mdPreview
	symbols   symCacheEntry
	lex       lexCache
	words     *wordList
	scanning  bool
	asked     editor.CompletionRequest
	blame     *blaming
	caret     caretFx
	rampage   rampageFx
	prompting bool

	selStatVer    uint64
	selStatAnchor int
	selStatHead   int
	selStatChars  int
	selStatLines  int
	selStatLocale string
	selStatText   string
	selStatValid  bool
}

func (d *doc) name() string {
	switch {
	case d.cmp != nil:
		return d.cmp.title()
	case d.commit != nil:
		return d.commit.title()
	case d.results != nil:
		return locales.FindResults()
	case d.md != nil:
		return d.md.title()
	case d.path == "":
		return locales.Untitled()
	}
	return filepath.Base(d.path)
}

// tabDirs returns the shortest parent folders that tell apart the files
// of docs with the same name, as Zed's and VS Code's tabs show them.
func tabDirs(docs []*doc) map[*doc]string {
	byName := make(map[string][]*doc)
	for _, d := range docs {
		if d.path != "" {
			byName[d.name()] = append(byName[d.name()], d)
		}
	}
	dirs := make(map[*doc]string)
	for _, same := range byName {
		if len(same) < 2 {
			continue
		}
		for _, d := range same {
			dirs[d] = shortestDir(d, same)
		}
	}
	return dirs
}

func shortestDir(d *doc, same []*doc) string {
	sep := string(filepath.Separator)
	parts := func(d *doc) []string { return strings.Split(filepath.Dir(d.path), sep) }
	mine := parts(d)
	for n := 1; n < len(mine); n++ {
		tail := mine[len(mine)-n:]
		if !slices.ContainsFunc(same, func(o *doc) bool {
			p := parts(o)
			return o != d && slices.Equal(p[max(len(p)-n, 0):], tail)
		}) {
			return strings.Join(tail, sep)
		}
	}
	return filepath.Dir(d.path)
}

func (d *doc) source() *doc {
	if d.cmp != nil {
		return d.cmp.src
	}
	return d
}

func (d *doc) plain() bool { return d.kind == textDoc && d.cmp == nil && d.commit == nil }

func (d *doc) dirty() bool {
	if d.lineEnding() != d.savedEnding {
		return true
	}
	s := d.st.Buffer.Snapshot()
	if s.Version() == d.saved.Version() {
		return false
	}
	if s.Version() != d.checked {
		d.checked, d.modified = s.Version(), !sameText(s, d.saved)
	}
	return d.modified
}

// sameText compares a and b a chunk at a time, without copying them.
func sameText(a, b editor.Snapshot) bool {
	if a.Len() != b.Len() {
		return false
	}
	next, stop := iter.Pull(func(yield func(string) bool) { b.Chunks(0, b.Len(), yield) })
	defer stop()
	same, rest := true, ""
	a.Chunks(0, a.Len(), func(s string) bool {
		for len(s) > 0 {
			for rest == "" {
				var ok bool
				if rest, ok = next(); !ok {
					same = false
					return false
				}
			}
			n := min(len(s), len(rest))
			if s[:n] != rest[:n] {
				same = false
				return false
			}
			s, rest = s[n:], rest[n:]
		}
		return true
	})
	return same
}

type group struct {
	keyAny any
	docs   []*doc
	active int
	bar    ui.Rect
	tabW   float32
	scroll float32
	shown  *doc
	at     int
	blank  *doc

	dirsCache map[*doc]string
	dirsDocs  []*doc
	dirsPaths []string
	docsBuf   []*doc
}

func (g *group) tabDirs() map[*doc]string {
	if len(g.docs) < 2 {
		clear(g.dirsDocs)
		g.dirsDocs = g.dirsDocs[:0]
		clear(g.dirsPaths)
		g.dirsPaths = g.dirsPaths[:0]
		g.dirsCache = nil
		return nil
	}
	same := len(g.dirsDocs) == len(g.docs)
	if same {
		for i, d := range g.docs {
			if g.dirsDocs[i] != d || g.dirsPaths[i] != d.path {
				same = false
				break
			}
		}
	}
	if same {
		return g.dirsCache
	}
	prevLen := len(g.dirsDocs)
	g.dirsDocs = append(g.dirsDocs[:0], g.docs...)
	if prevLen > len(g.dirsDocs) {
		clear(g.dirsDocs[len(g.dirsDocs):prevLen])
	}
	prevPaths := len(g.dirsPaths)
	g.dirsPaths = g.dirsPaths[:0]
	for _, d := range g.docs {
		g.dirsPaths = append(g.dirsPaths, d.path)
	}
	if prevPaths > len(g.dirsPaths) {
		clear(g.dirsPaths[len(g.dirsPaths):prevPaths])
	}
	g.dirsCache = tabDirs(g.docs)
	return g.dirsCache
}

func (g *group) current() *doc {
	if g.active < 0 || g.active >= len(g.docs) {
		return nil
	}
	return g.docs[g.active]
}

// placeholder is the editor an empty group shows, as Sublime Text's: what
// is typed in it opens a tab with the text (adopt).
func (g *group) placeholder() *doc {
	if g.blank == nil {
		g.blank = newDoc("", "")
		g.blank.st.NoFolding = true
	}
	return g.blank
}

func (a *app) adopt(g *group) {
	b := g.blank
	if b == nil || b.st.Buffer.Version() == 0 {
		return
	}
	g.blank = nil
	b.st.NoFolding = false
	g.docs = append(g.docs, b)
	a.show(b)
}

// place is where the keyboard focus is: an editor of a group. A doc moved
// to another group is in another place, which asks for the focus anew.
type place struct {
	g *group
	d *doc
}

// layout is one of Sublime Text's layouts: cols×rows groups of the same
// size.
type layout struct {
	name       string
	label      func(locales.Messages) string
	cols, rows int
}

func (l layout) id() string { return "layout " + l.name }

var layouts = []layout{
	{"Single", locales.Messages.LayoutSingle, 1, 1}, {"Columns: 2", locales.Messages.LayoutColumns2, 2, 1},
	{"Columns: 3", locales.Messages.LayoutColumns3, 3, 1}, {"Columns: 4", locales.Messages.LayoutColumns4, 4, 1},
	{"Grid: 4", locales.Messages.LayoutGrid4, 2, 2}, {"Rows: 2", locales.Messages.LayoutRows2, 1, 2},
	{"Rows: 3", locales.Messages.LayoutRows3, 1, 3},
}

// layoutStyle is how the groups sit in the window: compact ones meet at
// a line, comfortable ones are cards with room between them.
type layoutStyle struct {
	name  string
	label func(locales.Messages) string
}

func (s layoutStyle) id() string { return "layout_style " + s.name }

const (
	compactLayout     = "compact"
	comfortableLayout = "comfortable"
)

var layoutStyles = []layoutStyle{
	{compactLayout, locales.Messages.LayoutCompact}, {comfortableLayout, locales.Messages.LayoutComfortable},
}

func layoutOf(cols, rows int) layout {
	for _, l := range layouts {
		if l.cols == cols && l.rows == rows {
			return l
		}
	}
	return layouts[0]
}

type lime struct {
	dir    string // keeps the settings and the session; "" keeps none, as in tests
	set    settings
	wins   []*app
	last   *app // the window last focused, which the menu goes to
	menu   *mygo.Menu
	keys   *keymap
	recent recentList

	menuPaused bool

	// setErr is why settings.json does not parse: nothing writes over the
	// file until it does.
	setErr error

	themeSource   mygo.ThemeSource
	menuState     menuStateKey
	avatars       map[string]*ui.Bitmap
	fonts         *fontLists
	sessionMu     sync.Mutex
	sessionKey    string
	sessionKeyBuf []byte
	sessionKeyGen uint64
	sessionGen    uint64
	writing       sync.Mutex
	written       uint64
	quitting      bool
}

type app struct {
	l   *lime
	win *mygo.Window
	set *settings

	groups     []*group
	layout     layout
	cols       []float32
	rows       []float32
	focus      int
	focused    place
	mru         []*doc
	cycle       []*doc
	cycleAt     int
	cycleFrom   *doc
	cycleReveal bool
	closed      []string
	chordKeyUp ui.Key
	revealPath string
	chord      bool
	title      string
	scheme     *scheme
	panel      *panel
	scroll     *peek
	jumps      jumps
	find       finding
	fif        findInFiles
	proj       project
	marks      gitMarks
	git        gitRepo
	hist       history
	changes    changes
	gitTab     int
	remote     string
	side       sidePanel
	bounds     mygo.Rectangle
	drag       *tabDrag
	prefs      prefs
	notice     notice
	others     *tabWords

	reloading, reloadAgain bool
	closingPrompt          bool
	messageDialog          func(mygo.MessageOptions) (mygo.MessageResult, error)
	saveDialog             func(mygo.SaveDialogOptions) (string, error)

	uiThemeScheme *scheme
	uiThemeFont   string
	uiThemeBase   *ui.Theme
	uiThemeCached *ui.Theme

	boundKM        *keymap
	boundKeys      []boundShortcut
	boundChordKeys []boundShortcut

	titleDoc   *doc
	titlePath  string
	titleName  string
	titleDirty bool

	posLine   int
	posCol    int
	posLocale string
	posText   string
}

type boundShortcut struct {
	mods ui.Modifiers
	key  ui.Key
	fn   func()
}

var nextElemID atomic.Uint64

type (
	docElemKey   uint64
	groupElemKey uint64
)

func newLime(dir string) *lime {
	set, err := loadSettings(dir)
	l := &lime{dir: dir, set: set, setErr: err}
	l.keys = newKeymap(l.set.Keys)
	return l
}

func newApp(dir string) *app { return newLime(dir).newWindow() }

func (l *lime) newWindow() *app {
	a := &app{l: l, set: &l.set, layout: layouts[0], groups: []*group{{active: -1}}}
	a.scheme = l.set.scheme(true)
	if l.set.SideBar {
		a.side = filesPanel
	}
	l.wins = append(l.wins, a)
	if l.last == nil {
		l.last = a
	}
	return a
}

func (l *lime) at(w *mygo.Window) *app {
	for _, a := range l.wins {
		if w != nil && a.win == w {
			return a
		}
	}
	return l.last
}

func (l *lime) owner(d *doc) *app {
	for _, a := range l.wins {
		if gi, _ := a.locate(d); gi >= 0 {
			return a
		}
	}
	return nil
}

func (a *app) group() *group { return a.groups[a.focus] }
func (a *app) current() *doc { return a.group().current() }
func (a *app) docCount() int { return len(a.allDocs()) }
func (a *app) allDocs() []*doc {
	var docs []*doc
	for _, g := range a.groups {
		docs = append(docs, g.docs...)
	}
	return docs
}

func (a *app) locate(d *doc) (gi, i int) {
	for gi, g := range a.groups {
		if i := slices.Index(g.docs, d); i >= 0 {
			return gi, i
		}
	}
	return -1, -1
}

func (a *app) show(d *doc) {
	gi, i := a.locate(d)
	if gi < 0 {
		return
	}
	g := a.groups[gi]
	g.active, a.focus, a.focused = i, gi, place{g, d}
	if a.cycle == nil {
		a.touch(d)
	}
}

func (a *app) focusGroup(gi int) {
	if gi < 0 || gi >= len(a.groups) {
		return
	}
	g := a.groups[gi]
	if d := g.current(); d != nil {
		a.show(d)
		return
	}
	a.focus, a.focused = gi, place{g, g.placeholder()}
}

func (a *app) touch(d *doc) {
	a.mru = append([]*doc{d}, slices.DeleteFunc(a.mru, func(x *doc) bool { return x == d })...)
}

func (a *app) syncFocus(ctrlHeld bool) {
	if a.cycle != nil && !ctrlHeld {
		a.commitCycle()
	}
	p := a.focused
	if p.d == nil {
		return
	}
	if gi := slices.Index(a.groups, p.g); gi >= 0 {
		a.focus = gi
	}
	if a.cycle == nil && p.d != p.g.blank && (len(a.mru) == 0 || a.mru[0] != p.d) {
		a.touch(p.d)
	}
	// Only a text's editor binds the focus: a place that no element binds
	// would wait for one, and leave the focus the user moves unfollowed.
	if p.d.kind != textDoc && p.d.kind != findDoc {
		a.focused = place{}
	}
}

// cycleTabs is Sublime Text's next_view_in_stack: Ctrl+Tab walks the
// tabs in the order they were used for as long as Ctrl is held.
func (a *app) cycleTabs(step int) {
	if a.cycle == nil {
		if len(a.mru) < 2 {
			return
		}
		if a.panel != nil {
			a.cancelPanel()
		}
		a.cycle, a.cycleAt, a.cycleFrom = slices.Clone(a.mru), 0, a.current()
	}
	n := len(a.cycle)
	if n == 0 {
		return
	}
	a.cycleAt = ((a.cycleAt+step)%n + n) % n
	a.cycleReveal = true
	a.show(a.cycle[a.cycleAt])
}

func (a *app) commitCycle() {
	if a.cycle == nil {
		return
	}
	var d *doc
	if a.cycleAt >= 0 && a.cycleAt < len(a.cycle) {
		d = a.cycle[a.cycleAt]
	}
	a.cycle, a.cycleAt, a.cycleFrom, a.cycleReveal = nil, 0, nil, false
	if d != nil {
		a.show(d)
	}
}

func (a *app) cancelCycle() {
	if a.cycle == nil {
		return
	}
	from := a.cycleFrom
	a.cycle, a.cycleAt, a.cycleFrom, a.cycleReveal = nil, 0, nil, false
	if from != nil {
		a.show(from)
	}
}

func newDoc(path, text string) *doc {
	d := newDocWithStamp(path, text, stamp{})
	if path != "" {
		d.disk = stampOf(path)
	}
	return d
}

func newDocWithStamp(path, text string, disk stamp) *doc {
	text, ending := splitLines(text)
	buf := editor.NewBuffer(text)
	d := &doc{keyAny: docElemKey(nextElemID.Add(1)), path: path, st: &editor.State{Buffer: buf, Bindings: editorKeys, NoActiveLine: true}, saved: buf.Snapshot(), savedEnding: ending}
	d.setLineEnding(ending)
	if path != "" {
		buf.SetLanguage(editor.LanguageFor(path))
		if len(text) > largeFile && d.saved.Len() <= largeFile {
			d.diffDisk()
		} else {
			d.setDiffBase("", text)
		}
		d.disk = disk
	}
	return d
}

// stamp tells a file's versions apart, as Sublime Text does to reload a
// file another app changed.
type stamp struct {
	mod  time.Time
	size int64
}

func stampOf(path string) stamp {
	fi, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{fi.ModTime(), fi.Size()}
}

func (d *doc) changedOnDisk() bool {
	if d.path == "" {
		return false
	}
	now := stampOf(d.path)
	return now != (stamp{}) && now != d.disk
}

func (a *app) reloadFromDisk(d *doc) {
	if d.path == "" {
		return
	}
	raw, err := readText(d.path)
	if err != nil {
		go mygo.Dialog.Error(locales.DialogUnableToOpen(filepath.Base(d.path)), err.Error())
		return
	}
	text, enc := reopenText(raw, d.encoding(), a.set.FallbackEncoding)
	text, ending := splitLines(text)
	d.disk = stampOf(d.path)
	d.enc = enc
	d.st.Buffer.Set(text)
	d.setLineEnding(ending)
	d.savedEnding = ending
	d.st.Buffer.MarkSaved()
	d.saved = d.st.Buffer.Snapshot()
	if d.base == "" || d.saved.Len() > largeFile {
		d.diffDisk()
	}
	a.l.diskChanged(d)
	a.readBases(d)
}

type reload struct {
	d         *doc
	path      string
	was, disk stamp
	text      string
	enc       *fileEncoding
}

// changedOnDisk reads the files of the window's tabs that changed since
// they were opened or saved. It blocks, so call it off the main thread.
func changedOnDisk(docs []*doc, paths []string, stamps []stamp, encs []*fileEncoding, fallback string) []reload {
	var rs []reload
	for i, d := range docs {
		now := stampOf(paths[i])
		if now == stamps[i] || now == (stamp{}) {
			continue
		}
		if raw, err := readText(paths[i]); err == nil {
			text, enc := reopenText(raw, encs[i], fallback)
			rs = append(rs, reload{d, paths[i], stamps[i], now, text, enc})
		}
	}
	return rs
}

func (a *app) reloadChanged() {
	if a.reloading {
		a.reloadAgain = true
		return
	}
	var docs []*doc
	var paths []string
	var stamps []stamp
	var encs []*fileEncoding
	for _, d := range a.allDocs() {
		if d.path != "" && d.kind == textDoc && d.cmp == nil {
			docs, paths, stamps, encs = append(docs, d), append(paths, d.path), append(stamps, d.disk), append(encs, d.encoding())
		}
	}
	if len(docs) == 0 {
		return
	}
	a.readBases(docs...)
	a.reloading = true
	fallback := a.set.FallbackEncoding
	a.background(func() func() {
		rs := changedOnDisk(docs, paths, stamps, encs, fallback)
		return func() { a.applyReloads(rs) }
	})
}

func (a *app) applyReloads(rs []reload) {
	for _, r := range rs {
		d := r.d
		if d.path != r.path || d.disk != r.was || d.dirty() {
			continue
		}
		d.disk = r.disk
		text, ending := splitLines(r.text)
		d.enc = r.enc
		d.st.Buffer.Set(text)
		d.setLineEnding(ending)
		d.savedEnding = ending
		d.st.Buffer.MarkSaved()
		d.saved = d.st.Buffer.Snapshot()
		if d.base == "" || d.saved.Len() > largeFile {
			d.diffDisk()
		}
		a.l.diskChanged(d)
	}
	a.reloading = false
	if a.reloadAgain {
		a.reloadAgain = false
		a.reloadChanged()
	}
}

// largeFile is the size from which a file opens lighter. The gutter does
// not mark its changes: the DiffBase would hold a second copy of the
// text, which the editor compares line by line with the first as the
// file opens and is saved.
const largeFile = 16 << 20

func (a *app) add(path, text string) *doc {
	return a.addDoc(newDoc(path, text))
}

func (a *app) addDoc(d *doc) *doc {
	g := a.group()
	// An empty untitled tab that was never touched gives way to the file.
	if cur := g.current(); d.path != "" && cur != nil && cur.kind == textDoc && cur.path == "" && cur.st.Buffer.Len() == 0 && !cur.dirty() {
		g.docs[g.active] = d
		a.mru = slices.DeleteFunc(a.mru, func(x *doc) bool { return x == cur })
		a.jumps.forget(cur)
		a.closeViews(cur)
	} else {
		g.docs = append(g.docs, d)
	}
	a.show(d)
	return d
}

func (a *app) newFile() { a.add("", "") }

func (a *app) open(paths []string) {
	type prepared struct {
		path  string
		isDir bool
		exist *doc
		kind  docKind
		text  string
		enc   *fileEncoding
		disk  stamp
		err   error
	}
	items := make([]prepared, len(paths))
	var toRead []int
	for i, p := range paths {
		p, _ = filepath.Abs(p)
		items[i].path = p
		fi, err := os.Stat(p)
		if err == nil && fi.IsDir() {
			items[i].isDir = true
			continue
		}
		if err == nil {
			items[i].disk = stamp{fi.ModTime(), fi.Size()}
		}
		if d := a.docAt(p); d != nil {
			items[i].exist = d
			continue
		}
		toRead = append(toRead, i)
	}
	fallback := a.set.FallbackEncoding
	readOne := func(idx int) {
		p := items[idx].path
		k, raw, st, err := readOpenWithStamp(p, items[idx].disk)
		items[idx].kind, items[idx].disk, items[idx].err = k, st, err
		if err == nil && k == textDoc {
			items[idx].text, items[idx].enc = openText(raw, fallback)
		}
	}
	if len(toRead) >= 8 {
		var wg sync.WaitGroup
		slots := make(chan struct{}, min(len(toRead), 4))
		for _, idx := range toRead {
			wg.Go(func() {
				slots <- struct{}{}
				defer func() { <-slots }()
				readOne(idx)
			})
		}
		wg.Wait()
	} else {
		for _, idx := range toRead {
			readOne(idx)
		}
	}

	var files, folders []string
	var added []*doc
	seen := make(map[string]*doc, len(items))
	var seenCanon map[string]*doc
	if len(items) > 1 {
		seenCanon = make(map[string]*doc, len(items))
	}
	for _, it := range items {
		if it.isDir {
			a.addFolder(it.path)
			folders = append(folders, it.path)
			continue
		}
		if it.exist != nil {
			a.show(it.exist)
			continue
		}
		if d := seen[it.path]; d != nil {
			a.show(d)
			continue
		}
		var keyP string
		if seenCanon != nil {
			keyP = openedPathKey(it.path)
			if d := seenCanon[keyP]; d != nil {
				a.show(d)
				continue
			}
			var match *doc
			for k, d := range seenCanon {
				if sameOpenedKeys(k, keyP) {
					match = d
					break
				}
			}
			if match != nil {
				a.show(match)
				continue
			}
		}
		if it.err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToOpen(filepath.Base(it.path)), it.err.Error())
			continue
		}
		if it.kind != textDoc {
			d := a.addDoc(newDocWithStamp(it.path, "", it.disk))
			d.kind = it.kind
			seen[it.path] = d
			if seenCanon != nil {
				seenCanon[keyP] = d
			}
			files = append(files, it.path)
			continue
		}
		d := a.addDoc(newDocWithStamp(it.path, it.text, it.disk))
		d.enc = it.enc
		if a.set.DetectIndentation {
			d.guessIndent()
		}
		seen[it.path] = d
		if seenCanon != nil {
			seenCanon[keyP] = d
		}
		added = append(added, d)
		files = append(files, it.path)
	}
	// A window not made yet reads them as it comes to the front, rather
	// than hold up the launch.
	if a.win != nil {
		a.readBases(added...)
	}
	a.l.addRecent(files, folders)
}

func readOpen(path string) (docKind, string, error) {
	k, s, _, err := readOpenWithStamp(path, stamp{})
	return k, s, err
}

func readOpenWithStamp(path string, st stamp) (docKind, string, stamp, error) {
	if slices.Contains(imageExts, strings.ToLower(filepath.Ext(path))) {
		if st == (stamp{}) {
			fi, err := os.Stat(path)
			if err != nil {
				return textDoc, "", stamp{}, err
			}
			st = stamp{fi.ModTime(), fi.Size()}
		}
		return imageDoc, "", st, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return textDoc, "", stamp{}, err
	}
	defer f.Close()
	var head [8000]byte
	n, err := io.ReadFull(f, head[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return textDoc, "", stamp{}, err
	}
	prefix := head[:n]
	if !bytes.HasPrefix(prefix, []byte("\xff\xfe")) && !bytes.HasPrefix(prefix, []byte("\xfe\xff")) && bytes.IndexByte(prefix, 0) >= 0 {
		return binaryDoc, "", st, nil
	}
	var b strings.Builder
	if st.size > int64(n) {
		b.Grow(int(st.size))
	} else if fi, serr := f.Stat(); serr == nil {
		st = stamp{fi.ModTime(), fi.Size()}
		if fi.Size() > int64(n) {
			b.Grow(int(fi.Size()))
		} else {
			b.Grow(n)
		}
	} else {
		b.Grow(n)
	}
	b.Write(prefix)
	if err == nil {
		if _, err := io.Copy(&b, f); err != nil {
			return textDoc, "", st, err
		}
	}
	return textDoc, b.String(), st, nil
}

// readText reads the file at path as a string, without the copy that
// converting os.ReadFile's bytes makes, which a big file feels.
func readText(path string) (string, error) {
	s, _, err := readTextWithStamp(path)
	return s, err
}

func readTextWithStamp(path string) (string, stamp, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", stamp{}, err
	}
	defer f.Close()
	var b strings.Builder
	var st stamp
	if fi, err := f.Stat(); err == nil {
		st = stamp{fi.ModTime(), fi.Size()}
		b.Grow(int(fi.Size()))
	}
	if _, err := io.Copy(&b, f); err != nil {
		return "", st, err
	}
	return b.String(), st, nil
}

func (a *app) docAt(path string) *doc {
	if path == "" {
		return nil
	}
	hasNamed := false
	for _, g := range a.groups {
		for _, d := range g.docs {
			if d.path == path {
				return d
			}
			if d.path != "" {
				hasNamed = true
			}
		}
	}
	if !hasNamed {
		return nil
	}
	keyP := openedPathKey(path)
	for _, g := range a.groups {
		for _, d := range g.docs {
			if d.path != "" && sameOpenedKeys(openedPathKey(d.path), keyP) {
				return d
			}
		}
	}
	return nil
}

func sameOpenedPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return sameOpenedKeys(openedPathKey(a), openedPathKey(b))
}

func sameOpenedKeys(a, b string) bool {
	if a == b {
		return true
	}
	if !strings.EqualFold(a, b) {
		return false
	}
	fa, ea := os.Stat(a)
	fb, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(fa, fb)
	}
	return os.IsNotExist(ea) && os.IsNotExist(eb) && caseInsensitiveDir(filepath.Dir(a))
}

var caseInsensitiveDir = func(string) bool { return runtime.GOOS == "windows" }

func openedPathKey(path string) string {
	p := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	} else if dir, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		p = filepath.Join(dir, filepath.Base(p))
	}
	return p
}

func (a *app) promptOpen() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Multiple: true})
		if err != nil || len(paths) == 0 {
			return
		}
		a.win.Update(func() { a.open(paths) })
	}()
}

func (a *app) reopenClosed() {
	if n := len(a.closed); n > 0 {
		p := a.closed[n-1]
		a.closed = a.closed[:n-1]
		a.open([]string{p})
	}
}

// write saves d to path, on the main thread.
func (a *app) write(d *doc, path string) error { return a.writeIn(d, path, d.encoding()) }

func (a *app) writeIn(d *doc, path string, want *fileEncoding) error {
	a.tidy(d)
	snap := d.st.Buffer.Snapshot()
	var b bytes.Buffer
	if _, err := snap.WriteTo(&b); err != nil {
		return err
	}
	fi, _ := os.Stat(path)
	out := b.Bytes()
	if d.cr {
		out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r"))
	}
	// A character the encoding has none for saves the file as UTF-8, as
	// in Sublime Text.
	enc := want
	if data, err := enc.encode(out); err == nil {
		out = data
	} else {
		enc = utf8Encoding
	}
	if err := writeFileAtomic(path, out, fi); err != nil {
		go mygo.Dialog.Error(locales.DialogUnableToSave(filepath.Base(path)), err.Error())
		return err
	}
	if a.l.dir != "" && path == settingsPath(a.l.dir) {
		a.l.reloadSettings()
	}
	if path != d.path {
		d.path, d.base = path, ""
		d.st.Buffer.SetLanguage(editor.LanguageFor(path))
	}
	d.st.Buffer.MarkSaved()
	d.saved, d.savedEnding = snap, d.lineEnding()
	if enc != want {
		go mygo.Dialog.Error(locales.DialogSavedAsUTF8(filepath.Base(path)), locales.DialogNotRepresentable(want.title(locales.In(i18n.Locale()))))
	}
	d.enc = enc
	if d.base == "" || d.saved.Len() > largeFile {
		d.diffDisk()
	}
	a.l.diskChanged(d)
	d.disk = stampOf(path)
	a.readBases(d)
	if fi == nil && a.proj.holds(path) {
		a.refreshFolders()
	} else if a.proj.holds(path) {
		a.readMarks()
	}
	if a.git.root != "" {
		a.readWorktree()
	}
	return nil
}

// tidy makes the changes the settings make to a file as it is saved, as
// one step of undo.
func (a *app) tidy(d *doc) {
	trim, eol := a.set.TrimTrailingWhiteSpaceOnSave, a.set.EnsureNewlineAtEOFOnSave
	if !trim && !eol {
		return
	}
	b := d.st.Buffer
	b.Transact(func() {
		if trim {
			d.st.Run(editor.TrimTrailingWhitespace)
		}
		if n := b.Len(); eol && n > 0 && b.Slice(n-1, n) != "\n" {
			b.Edit(editor.Edit{Start: n, End: n, Text: "\n"})
		}
	})
}

var copySaveACL = func(src, dst *os.File) error { return nil }

func writeFileAtomic(path string, data []byte, existing os.FileInfo) error {
	// Resolve symlink targets so os.Rename updates the target instead of replacing the link.
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if target, err := filepath.EvalSymlinks(path); err == nil {
			path = target
		} else if link, err := os.Readlink(path); err == nil {
			if !filepath.IsAbs(link) {
				link = filepath.Join(filepath.Dir(path), link)
			}
			path = filepath.Clean(link)
		}
	}
	mode := os.FileMode(0o644)
	if existing != nil {
		if !existing.Mode().IsRegular() {
			return &os.PathError{Op: "save", Path: path, Err: os.ErrInvalid}
		}
		mode = existing.Mode().Perm()
	}
	tmp := filepath.Join(filepath.Dir(path), ".lime-"+rand.Text())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err = f.Write(data); err == nil && existing != nil {
		var target *os.File
		target, err = os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			err = f.Chmod(mode)
			if err == nil {
				err = copySaveACL(target, f)
			}
			target.Close()
		}
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}

func (a *app) pickPath(name, path string) string {
	opts := mygo.SaveDialogOptions{Parent: a.win, DefaultPath: name}
	if path != "" {
		opts.DefaultPath = path
	}
	var p string
	var err error
	if a.saveDialog != nil {
		p, err = a.saveDialog(opts)
	} else {
		p, err = mygo.Dialog.Save(opts)
	}
	if err != nil {
		return ""
	}
	return p
}

func (a *app) askMessage(opts mygo.MessageOptions) (mygo.MessageResult, error) {
	if a.messageDialog != nil {
		return a.messageDialog(opts)
	}
	return mygo.Dialog.Message(opts)
}

var askSaveConflict = (*app).askMessage

func (a *app) save(d *doc) {
	a.saveThen(d, d.encoding(), nil)
}

func (a *app) saveThen(d *doc, want *fileEncoding, done func(bool)) {
	if d.prompting {
		if done != nil {
			done(false)
		}
		return
	}
	if d.path == "" {
		a.saveAsThen(d, want, done)
		return
	}
	if !d.changedOnDisk() {
		ok := a.writeIn(d, d.path, want) == nil
		if done != nil {
			done(ok)
		}
		return
	}
	d.prompting = true
	name := d.name()
	a.background(func() func() {
		res, err := askSaveConflict(a, mygo.MessageOptions{
			Parent:             a.win,
			Type:               mygo.MessageWarning,
			Message:            locales.DialogChangedOnDisk(name),
			Detail:             locales.DialogChangedOnDiskDetail(),
			Buttons:            []string{locales.ButtonOverwrite(), locales.ButtonReload(), locales.ButtonCancel()},
			CancelButton:       2,
			DefaultButton:      2,
			DestructiveButtons: []int{0},
		})
		return func() {
			d.prompting = false
			owner := a.l.owner(d)
			ok := false
			if err == nil && owner != nil {
				switch res.Button {
				case 0:
					ok = owner.writeIn(d, d.path, want) == nil
				case 1:
					owner.reloadFromDisk(d)
				}
			}
			if done != nil {
				done(ok)
			}
		}
	})
}

func (a *app) saveAs(d *doc) { a.saveAsThen(d, d.encoding(), nil) }

func (a *app) saveAsThen(d *doc, want *fileEncoding, done func(bool)) {
	if d.prompting || (a.win == nil && a.saveDialog == nil) {
		if done != nil {
			done(false)
		}
		return
	}
	d.prompting = true
	name, path := d.name(), d.path
	a.background(func() func() {
		p := a.pickPath(name, path)
		return func() {
			d.prompting = false
			owner := a.l.owner(d)
			ok := p != "" && owner != nil && owner.writeIn(d, p, want) == nil
			if done != nil {
				done(ok)
			}
		}
	})
}

func (a *app) saveAll() { a.saveAllThen(nil) }

func (a *app) saveAllThen(done func(bool)) {
	docs := a.allDocs()
	savedAll := true
	var next func(int)
	next = func(i int) {
		for i < len(docs) {
			d := docs[i]
			i++
			if gi, _ := a.locate(d); gi < 0 || !d.dirty() {
				continue
			}
			if d.path == "" {
				a.show(d)
			}
			a.saveThen(d, d.encoding(), func(ok bool) {
				if !ok {
					savedAll = false
				}
				next(i)
			})
			return
		}
		if done != nil {
			done(savedAll)
		}
	}
	next(0)
}

func (a *app) closeDoc(d *doc) {
	if !d.dirty() {
		a.remove(d)
		return
	}
	if d.prompting {
		return
	}
	d.prompting = true
	name := d.name()
	a.background(func() func() {
		res, err := a.askMessage(mygo.MessageOptions{
			Parent:             a.win,
			Type:               mygo.MessageQuestion,
			Message:            locales.DialogSaveChanges(name),
			Buttons:            []string{locales.ButtonSave(), locales.ButtonDontSave(), locales.ButtonCancel()},
			CancelButton:       2,
			DestructiveButtons: []int{1},
		})
		return func() {
			d.prompting = false
			if gi, _ := a.locate(d); gi < 0 || err != nil {
				return
			}
			switch res.Button {
			case 0:
				a.saveThen(d, d.encoding(), func(ok bool) {
					if owner := a.l.owner(d); ok && owner != nil {
						owner.remove(d)
					}
				})
			case 1:
				a.remove(d)
			}
		}
	})
}

func (a *app) closeCurrent() {
	switch d := a.current(); {
	case d != nil:
		a.closeDoc(d)
	case a.docCount() == 0:
		a.win.Close()
	}
}

func (a *app) remove(d *doc) {
	if !a.take(d) {
		return
	}
	if d == a.fif.tab {
		a.fif.finding.stop()
	}
	if d.path != "" {
		a.closed = append(a.closed, d.path)
	}
	release(d)
	a.closeViews(d)
}

// closeViews closes the tabs of every window that show d, gone: a diff
// tab would go on editing a text no tab shows, and a preview showing it.
func (a *app) closeViews(d *doc) {
	for _, w := range a.l.wins {
		for _, x := range w.allDocs() {
			if x.cmp != nil && (x.cmp.src == d || x.cmp.old == d) || x.md != nil && x.md.src == d {
				w.remove(x)
			}
		}
	}
}

// release gives the memory of large docs just closed back to the system,
// once the frames that showed them are gone: the runtime would collect
// them only as the heap grows again, and give back the memory over
// minutes.
func release(docs ...*doc) {
	if slices.ContainsFunc(docs, func(d *doc) bool { return d.st.Buffer.Len() > largeFile }) {
		time.AfterFunc(time.Second, debug.FreeOSMemory)
	}
}

func (a *app) take(d *doc) bool {
	a.mru = slices.DeleteFunc(a.mru, func(x *doc) bool { return x == d })
	if a.cycleFrom == d {
		a.cycleFrom = nil
	}
	if i := slices.Index(a.cycle, d); i >= 0 {
		a.cycle = slices.Delete(a.cycle, i, i+1)
		switch {
		case len(a.cycle) < 2:
			a.cycle, a.cycleAt, a.cycleFrom, a.cycleReveal = nil, 0, nil, false
		case i < a.cycleAt || a.cycleAt >= len(a.cycle):
			a.cycleAt = max(a.cycleAt-1, 0)
		}
	}
	a.jumps.forget(d)
	return a.detach(d)
}

func (a *app) detach(d *doc) bool {
	gi, i := a.locate(d)
	if gi < 0 {
		return false
	}
	g := a.groups[gi]
	g.docs = slices.Delete(g.docs, i, i+1)
	if i < g.active || g.active >= len(g.docs) {
		g.active--
	}
	if a.focused.d == d {
		a.focused = place{}
		if gi == a.focus {
			a.focusGroup(gi)
		}
	}
	return true
}

func (a *app) selectTab(i int) {
	g := a.group()
	if n := len(g.docs); n > 0 {
		a.show(g.docs[(i%n+n)%n])
	}
}

func (a *app) setLayout(l layout) {
	cur := a.current()
	n := l.cols * l.rows
	for len(a.groups) > n {
		last := a.groups[len(a.groups)-1]
		a.groups = a.groups[:len(a.groups)-1]
		into := a.groups[len(a.groups)-1]
		into.docs = append(into.docs, last.docs...)
		if into.active < 0 && len(into.docs) > 0 {
			into.active = 0
		}
	}
	for len(a.groups) < n {
		a.groups = append(a.groups, &group{active: -1})
	}
	a.layout = l
	if cur != nil {
		a.show(cur)
	} else {
		a.focusGroup(min(a.focus, n-1))
	}
}

func (a *app) moveToGroup(gi int) {
	d := a.current()
	if d == nil || gi < 0 || gi >= len(a.groups) || gi == a.focus {
		return
	}
	a.detach(d)
	g := a.groups[gi]
	g.docs = append(g.docs, d)
	a.show(d)
}

func (a *app) neighbor(step int) int {
	n := len(a.groups)
	return ((a.focus+step)%n + n) % n
}

func (a *app) newPane() {
	l := a.layout
	switch {
	case l.rows == 1 && l.cols < 4:
		l.cols++
	case l.cols == 1 && l.rows < 3:
		l.rows++
	default:
		return
	}
	a.setLayout(layoutOf(l.cols, l.rows))
	a.moveToGroup(len(a.groups) - 1)
}

func (a *app) closePane() {
	l := a.layout
	if len(a.groups) == 1 || l.cols > 1 && l.rows > 1 {
		return
	}
	gone := a.groups[a.focus]
	a.groups = slices.Delete(a.groups, a.focus, a.focus+1)
	into := a.groups[max(a.focus-1, 0)]
	into.docs = append(into.docs, gone.docs...)
	if into.active < 0 && len(into.docs) > 0 {
		into.active = 0
	}
	if l.rows == 1 {
		l.cols--
	} else {
		l.rows--
	}
	a.layout = layoutOf(l.cols, l.rows)
	a.focusGroup(max(a.focus-1, 0))
}

func (a *app) moveTab(d *doc, gi, i int) {
	from := a.l.owner(d)
	if from == nil || gi >= len(a.groups) {
		return
	}
	if from == a {
		a.detach(d)
	} else {
		from.take(d)
	}
	g := a.groups[gi]
	g.docs = slices.Insert(g.docs, min(max(i, 0), len(g.docs)), d)
	a.show(d)
	if from != a && from.docCount() == 0 && from.win != nil {
		from.win.Close()
	}
}

func (a *app) openWindow() {
	b := a.l.newWindow()
	b.newFile()
	r := a.win.Bounds()
	b.makeWindow(mygo.Rectangle{X: r.X + 24, Y: r.Y + 24, Width: r.Width, Height: r.Height})
}

// closing asks to save the changes of a window closing while others stay,
// which would be lost. The last one keeps the session instead, as Sublime
// Text's hot exit does, and quits but on macOS, where the app stays in the
// Dock.
func (a *app) closing(e *mygo.CloseEvent) {
	l := a.l
	if l.quitting {
		return
	}
	if len(l.wins) == 1 {
		if mac {
			l.keep(e)
		} else {
			l.quit(e)
		}
		return
	}
	n := 0
	for _, d := range a.allDocs() {
		if d.dirty() {
			n++
		}
	}
	if n == 0 {
		return
	}
	e.PreventDefault()
	if a.closingPrompt {
		return
	}
	a.closingPrompt = true
	a.background(func() func() {
		res, err := a.askMessage(mygo.MessageOptions{
			Parent:             a.win,
			Type:               mygo.MessageQuestion,
			Message:            locales.DialogSaveWindowChanges(n),
			Buttons:            []string{locales.ButtonSave(), locales.ButtonDontSave(), locales.ButtonCancel()},
			CancelButton:       2,
			DestructiveButtons: []int{1},
		})
		return func() {
			a.closingPrompt = false
			if err != nil {
				return
			}
			switch res.Button {
			case 0:
				a.saveAllThen(func(savedAll bool) {
					if savedAll && !slices.ContainsFunc(a.allDocs(), (*doc).dirty) && a.win != nil {
						a.win.Close()
					}
				})
			case 1:
				for _, d := range a.allDocs() {
					a.remove(d)
				}
				if a.win != nil {
					a.win.Close()
				}
			}
		}
	})
}

// quit keeps the settings and the session of all windows, the unsaved
// text with it, for the next run to bring back: nothing asks to save. The
// app stays when the session cannot be kept.
func (l *lime) quit(e interface{ PreventDefault() }) {
	if !l.quitting && l.keep(e) {
		l.quitting = true
	}
}

// keep keeps the settings and the session, or cancels e and tells why.
// With no window left the session is the one the last window kept as it
// closed.
func (l *lime) keep(e interface{ PreventDefault() }) bool {
	l.saveSettings()
	if len(l.wins) == 0 {
		return true
	}
	if err := l.saveSession(); err != nil {
		e.PreventDefault()
		go mygo.Dialog.Error(locales.DialogUnableToKeepFiles(), err.Error())
		return false
	}
	return true
}

func (l *lime) closed(a *app) {
	release(a.allDocs()...)
	a.proj.unwatch()
	l.wins = slices.DeleteFunc(l.wins, func(w *app) bool { return w == a })
	if l.last == a {
		l.last = nil
		if len(l.wins) > 0 {
			l.last = l.wins[len(l.wins)-1]
			l.last.syncMenu()
		} else {
			l.pauseMenu(false)
		}
	}
}

// reopen brings back the windows the last one kept as it closed, on
// macOS, where the app stays without windows.
func (l *lime) reopen() {
	l.restoreSession()
	if len(l.wins) == 0 {
		l.newWindow()
	}
	for _, a := range l.wins {
		if a.docCount() == 0 {
			a.newFile()
		}
		a.makeWindow(a.bounds)
	}
}

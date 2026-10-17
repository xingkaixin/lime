package main

import (
	"bytes"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// doc is a file open in a tab.
type doc struct {
	path    string // "" while untitled
	st      *editor.State
	columns bool            // a column drag remapped from another press is going on
	saved   editor.Snapshot // the text on disk
	// checked is the version modified was found for: dirty compares the
	// text with saved once a version.
	checked  uint64
	modified bool
}

func (d *doc) name() string {
	if d.path == "" {
		return "untitled"
	}
	return filepath.Base(d.path)
}

// dirty reports whether the text differs from the saved one, as
// Buffer.Modified does, which copies both texts out at every call.
func (d *doc) dirty() bool {
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

// group is a pane of the layout, with its own tabs.
type group struct {
	docs   []*doc
	active int     // -1 while empty
	bar    ui.Rect // the tab bar as the last frame laid it out, in the window
	tabW   float32 // the width of its tabs, all the same
	blank  *doc    // the editor shown without tabs, which typing makes a tab
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
		g.blank.st.LineNumbers, g.blank.st.NoFolding = editor.LineNumbersOff, true
	}
	return g.blank
}

// adopt makes the placeholder of g a tab once it has been typed in.
func (a *app) adopt(g *group) {
	b := g.blank
	if b == nil || b.st.Buffer.Version() == 0 {
		return
	}
	g.blank = nil
	b.st.LineNumbers, b.st.NoFolding = editor.LineNumbersOn, false
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
	cols, rows int
}

func (l layout) id() string { return "layout " + l.name }

var layouts = []layout{
	{"Single", 1, 1}, {"Columns: 2", 2, 1}, {"Columns: 3", 3, 1}, {"Columns: 4", 4, 1},
	{"Grid: 4", 2, 2}, {"Rows: 2", 1, 2}, {"Rows: 3", 1, 3},
}

func layoutOf(cols, rows int) layout {
	for _, l := range layouts {
		if l.cols == cols && l.rows == rows {
			return l
		}
	}
	return layouts[0]
}

// lime is what the windows share: the settings, the menu and the session.
type lime struct {
	dir  string // keeps the settings and the session; "" keeps none, as in tests
	set  settings
	wins []*app
	last *app // the window last focused, which the menu goes to
	menu *mygo.Menu

	themeSource mygo.ThemeSource
	menuState   string
	sessionKey  string // of the session last kept
	sessionGen  uint64 // numbers the sessions taken to write
	writing     sync.Mutex
	written     uint64 // the number of the session last written
	quitting    bool   // the session is kept and the windows close
}

// app is a window: its groups of tabs, its project and its panels.
type app struct {
	l   *lime
	win *mygo.Window
	set *settings // the windows' own, l.set

	groups  []*group
	layout  layout
	cols    []float32 // where the dividers between the groups are, as SplitPercent has them
	rows    []float32
	focus   int   // the focused group
	focused place // bound with FocusBind
	mru     []*doc
	cycle   []*doc // mru as Ctrl+Tab began, while Ctrl is held
	cycleAt int
	closed  []string
	chord   bool // Cmd+K was pressed, waiting for the chord's second key
	title   string
	scheme  *scheme
	panel   *panel
	proj    project
	bounds  mygo.Rectangle // where the window opens, when restored
	drag    *tabDrag
}

func newLime(dir string) *lime { return &lime{dir: dir, set: loadSettings(dir)} }

// newApp makes the app with one window, without opening it.
func newApp(dir string) *app { return newLime(dir).newWindow() }

// newWindow adds a window, which makeWindow opens.
func (l *lime) newWindow() *app {
	a := &app{l: l, set: &l.set, layout: layouts[0], groups: []*group{{active: -1}}}
	a.scheme = resolveScheme(l.set.ColorScheme, true)
	l.wins = append(l.wins, a)
	if l.last == nil {
		l.last = a
	}
	return a
}

// at is the window of w, or the one last focused.
func (l *lime) at(w *mygo.Window) *app {
	for _, a := range l.wins {
		if w != nil && a.win == w {
			return a
		}
	}
	return l.last
}

// owner is the window with d in a tab.
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

// show makes d its group's tab and gives it the focus.
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

// focusGroup gives the focus to the group at gi.
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

// touch puts d first in the order tabs were last used.
func (a *app) touch(d *doc) {
	a.mru = append([]*doc{d}, slices.DeleteFunc(a.mru, func(x *doc) bool { return x == d })...)
}

// syncFocus follows the focus the user moved, and ends a Ctrl+Tab cycle
// as Ctrl is let go.
func (a *app) syncFocus(ctrlHeld bool) {
	if a.cycle != nil && !ctrlHeld {
		a.cycle = nil
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
}

// cycleTabs is Sublime Text's next_view_in_stack: Ctrl+Tab walks the
// tabs in the order they were used for as long as Ctrl is held.
func (a *app) cycleTabs(step int) {
	if a.cycle == nil {
		a.cycle, a.cycleAt = slices.Clone(a.mru), 0
	}
	n := len(a.cycle)
	if n == 0 {
		return
	}
	a.cycleAt = ((a.cycleAt+step)%n + n) % n
	a.show(a.cycle[a.cycleAt])
}

// newDoc makes a tab of the file at path holding text, its text on disk.
func newDoc(path, text string) *doc {
	buf := editor.NewBuffer(text)
	d := &doc{path: path, st: &editor.State{Buffer: buf, Bindings: editorKeys, NoActiveLine: true}, saved: buf.Snapshot()}
	if path != "" {
		buf.SetLanguage(editor.LanguageFor(path))
		d.setDiffBase(d.saved)
	}
	return d
}

// largeFile is the size from which a file opens lighter. The gutter does
// not mark its changes: the DiffBase would hold a second copy of the
// text, which the editor compares line by line with the first as the
// file opens and is saved.
const largeFile = 16 << 20

// setDiffBase has the gutter mark the changes against disk, the text on
// disk.
func (d *doc) setDiffBase(disk editor.Snapshot) {
	switch {
	case disk.Len() > largeFile:
		d.st.DiffBase = nil
	case d.st.DiffBase == nil:
		d.st.DiffBase = editor.NewBuffer(disk.String())
	default:
		d.st.DiffBase.Reset(disk.String())
	}
}

func (a *app) add(path, text string) *doc {
	d := newDoc(path, text)
	g := a.group()
	// An empty untitled tab that was never touched gives way to the file.
	if cur := g.current(); path != "" && cur != nil && cur.path == "" && cur.st.Buffer.Len() == 0 && !cur.dirty() {
		g.docs[g.active] = d
		a.mru = slices.DeleteFunc(a.mru, func(x *doc) bool { return x == cur })
	} else {
		g.docs = append(g.docs, d)
	}
	a.show(d)
	return d
}

func (a *app) newFile() { a.add("", "") }

// open shows files in tabs, or the tabs already showing them, and adds
// folders to the project.
func (a *app) open(paths []string) {
	for _, p := range paths {
		p, _ = filepath.Abs(p)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			a.addFolder(p)
			continue
		}
		if d := a.docAt(p); d != nil {
			a.show(d)
			continue
		}
		text, err := readText(p)
		if err != nil {
			go mygo.Dialog.Error("Unable to open "+filepath.Base(p), err.Error())
			continue
		}
		a.add(p, text)
	}
}

// readText reads the file at path as a string, without the copy that
// converting os.ReadFile's bytes makes, which a big file feels.
func readText(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var b strings.Builder
	if fi, err := f.Stat(); err == nil {
		b.Grow(int(fi.Size()))
	}
	if _, err := io.Copy(&b, f); err != nil {
		return "", err
	}
	return b.String(), nil
}

func (a *app) docAt(path string) *doc {
	for _, d := range a.allDocs() {
		if d.path == path {
			return d
		}
	}
	return nil
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
func (a *app) write(d *doc, path string) error {
	snap := d.st.Buffer.Snapshot()
	var b bytes.Buffer
	if _, err := snap.WriteTo(&b); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(path, b.Bytes(), mode); err != nil {
		go mygo.Dialog.Error("Unable to save "+filepath.Base(path), err.Error())
		return err
	}
	if path != d.path {
		d.path = path
		d.st.Buffer.SetLanguage(editor.LanguageFor(path))
	}
	d.st.Buffer.MarkSaved()
	d.saved = snap
	d.setDiffBase(snap)
	return nil
}

// pickPath asks where to save d; "" when canceled. It blocks, so call it
// off the main thread.
func (a *app) pickPath(name, path string) string {
	opts := mygo.SaveDialogOptions{Parent: a.win, DefaultPath: name}
	if path != "" {
		opts.DefaultPath = path
	}
	p, err := mygo.Dialog.Save(opts)
	if err != nil {
		return ""
	}
	return p
}

func (a *app) save(d *doc) {
	if d.path == "" {
		a.saveAs(d)
		return
	}
	a.write(d, d.path)
}

func (a *app) saveAs(d *doc) {
	name, path := d.name(), d.path
	go func() {
		if p := a.pickPath(name, path); p != "" {
			a.win.Update(func() { a.write(d, p) })
		}
	}()
}

func (a *app) saveAll() {
	docs := a.allDocs()
	for _, d := range docs {
		if d.dirty() && d.path != "" {
			a.write(d, d.path)
		}
	}
	for _, d := range docs {
		if d.dirty() && d.path == "" {
			a.saveAs(d)
			return
		}
	}
}

// closeDoc closes d's tab, asking first to save its changes.
func (a *app) closeDoc(d *doc) {
	if !d.dirty() {
		a.remove(d)
		return
	}
	name, path := d.name(), d.path
	go func() {
		res, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  a.win,
			Type:    mygo.MessageQuestion,
			Message: fmt.Sprintf("Save changes to %s before closing?", name),
			Buttons: []string{"Save", "Don't Save", "Cancel"},
		})
		switch res.Button {
		case 0:
			if path == "" {
				if path = a.pickPath(name, ""); path == "" {
					return
				}
			}
			a.win.Update(func() {
				if a.write(d, path) == nil {
					a.remove(d)
				}
			})
		case 1:
			a.win.Update(func() { a.remove(d) })
		}
	}()
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
	if d.path != "" {
		a.closed = append(a.closed, d.path)
	}
	release(d)
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

// take takes d out of the window.
func (a *app) take(d *doc) bool {
	a.mru = slices.DeleteFunc(a.mru, func(x *doc) bool { return x == d })
	return a.detach(d)
}

// detach takes d out of its group, the tab beside it taking its place.
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

// selectTab moves to the tab at i of the focused group, from the end when
// negative, wrapping.
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

// moveToGroup moves the focused tab to the group at gi.
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

// newPane adds a group after the others and moves the focused tab to it.
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

// closePane takes the focused group away, its tabs going to the group
// before it.
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

// moveTab puts d at i of the group at gi, from where it is, in this
// window or another. A window left without tabs closes.
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

// openWindow opens a window with an untitled tab, a little below and to
// the right of this one.
func (a *app) openWindow() {
	b := a.l.newWindow()
	b.newFile()
	r := a.win.Bounds()
	b.makeWindow(mygo.Rectangle{X: r.X + 24, Y: r.Y + 24, Width: r.Width, Height: r.Height})
}

// closing asks to save the changes of a window closing while others stay,
// which would be lost. The last one keeps the session instead, as Sublime
// Text's hot exit does.
func (a *app) closing(e *mygo.CloseEvent) {
	l := a.l
	if l.quitting {
		return
	}
	if len(l.wins) == 1 {
		l.quit(e)
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
	go func() {
		res, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  a.win,
			Type:    mygo.MessageQuestion,
			Message: fmt.Sprintf("Save changes to %d files before closing the window?", n),
			Buttons: []string{"Save", "Don't Save", "Cancel"},
		})
		switch res.Button {
		case 0:
			a.win.Update(func() {
				a.saveAll()
				if !slices.ContainsFunc(a.allDocs(), (*doc).dirty) {
					a.win.Close()
				}
			})
		case 1:
			a.win.Update(func() {
				for _, d := range a.allDocs() {
					a.remove(d)
				}
				a.win.Close()
			})
		}
	}()
}

// quit keeps the settings and the session of all windows, the unsaved
// text with it, for the next run to bring back: nothing asks to save. The
// app stays when the session cannot be kept.
func (l *lime) quit(e interface{ PreventDefault() }) {
	if l.quitting {
		return
	}
	l.saveSettings()
	if err := l.saveSession(); err != nil {
		e.PreventDefault()
		go mygo.Dialog.Error("Unable to keep the open files", err.Error())
		return
	}
	l.quitting = true
}

func (l *lime) closed(a *app) {
	release(a.allDocs()...)
	l.wins = slices.DeleteFunc(l.wins, func(w *app) bool { return w == a })
	if l.last == a && len(l.wins) > 0 {
		l.last = l.wins[len(l.wins)-1]
	}
}

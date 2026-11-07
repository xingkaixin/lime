package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// The editor's input goes through app.input, which reaches the editor's
// handler by linkname: Sublime's keys, chords and mouse work, and Zed's
// keys Sublime leaves alone do nothing.
func TestSublimeInput(t *testing.T) {
	if !mac {
		t.Skip("the test presses macOS keys")
	}
	a := newApp("")
	a.add("", "alpha\nbeta\n")
	tt := ui.NewTester(a.view, 800, 500)
	st := a.current().st

	tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	if got := st.Buffer.String(); got != "alpha\nalpha\nbeta\n" {
		t.Fatalf("Cmd+Shift+D: %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyL)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.CodeDown(ui.Cmd, ui.CodeSuperLeft)
	tt.Key(ui.Cmd, ui.KeyU)
	if got := st.Buffer.String(); got != "alpha\nALPHA\nbeta\n" {
		t.Fatalf("Cmd+K, Cmd again, Cmd+U: %q", got)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyDown)
	if n := len(st.Selections()); n != 2 {
		t.Fatalf("Ctrl+Shift+Down: %d carets, want 2", n)
	}
	tt.Key(0, ui.KeyEscape)
	r, _ := tt.Find("Code editor")
	tt.ClickAtWith(ui.Cmd, r.X+r.W/2, r.Y+8)
	if n := len(st.Selections()); n != 2 {
		t.Fatalf("Cmd+click: %d carets, want 2", n)
	}
	st.SetSelections(editor.Selection{Anchor: 0, Head: 5}, editor.Selection{Anchor: 6, Head: 11})
	tt.ClickAtWith(ui.Cmd|ui.Shift, r.X+r.W/2, r.Y+8)
	if got := st.Selections(); len(got) != 1 || got[0] != (editor.Selection{Anchor: 6, Head: 11}) {
		t.Fatalf("Cmd+Shift+click: %v, want the second line's selection", got)
	}
	tt.Key(ui.Alt, ui.KeyUp)
	if got := st.Buffer.String(); got != "alpha\nALPHA\nbeta\n" {
		t.Fatalf("Option+Up moved lines, as in Zed: %q", got)
	}
}

func TestDragPastSide(t *testing.T) {
	a := newApp("")
	a.set.WordWrap = false
	a.add("", strings.Repeat("x", 500))
	tt := ui.NewTester(a.view, 800, 500)
	d := a.current()
	r, _ := tt.Find("Code editor")
	tt.Press(r.X+r.W/2, r.Y+8)
	tt.Move(r.X+r.W+40, r.Y+8)
	head := d.st.Cursor()
	for range 5 {
		if !a.slideStep(d, unsafe.Add(unsafe.Pointer(d.st), editorOffset)) {
			t.Fatal("a drag past the right stopped scrolling")
		}
	}
	if got := d.st.Cursor(); got <= head {
		t.Fatalf("the selection stayed at %d, from %d", got, head)
	}
	tt.Release(r.X+r.W+40, r.Y+8)
	if a.slideStep(d, unsafe.Add(unsafe.Pointer(d.st), editorOffset)) {
		t.Fatal("scrolling went on after the release")
	}
}

func TestTabs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "main.go")
	if err := os.WriteFile(p, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp("")
	a.newFile()
	a.open([]string{p})
	tt := ui.NewTester(a.view, 800, 500)
	if n := a.docCount(); n != 1 || a.current().path != p {
		t.Fatalf("opening a file keeps the empty untitled tab: %d tabs", n)
	}
	a.newFile()
	tt.Frame()
	tt.Advance(time.Second)
	tt.Key(ui.Cmd, ui.Key1)
	if a.current().path != p {
		t.Fatalf("Cmd+1: tab %d", a.group().active)
	}
	if err := tt.Click("Close untitled"); err != nil {
		t.Fatal(err)
	}
	if n := a.docCount(); n != 1 {
		t.Fatalf("closing a tab: %d tabs", n)
	}
	a.remove(a.current())
	a.reopenClosed()
	if n := a.docCount(); n != 1 || a.current().path != p {
		t.Fatalf("reopen closed file: %d tabs", n)
	}

	tt.Frame()
	r, _ := tt.Find("Tab bar")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if n := a.docCount(); n != 2 || a.current().path != "" {
		t.Fatalf("double click on the tab bar: %d tabs", n)
	}
	a.remove(a.current())
	tt.Frame()
	tt.Advance(time.Second)
	if err := tt.Click("New File"); err != nil {
		t.Fatal(err)
	}
	if n := a.docCount(); n != 2 || a.current().path != "" {
		t.Fatalf("new tab button: %d tabs", n)
	}
	a.remove(a.current())

	// The status bar's syntax menu sets the syntax.
	tt.Frame()
	if err := tt.Click("Syntax: Go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Plain Text"); err != nil {
		t.Fatal(err)
	}
	if l := a.current().st.Buffer.Language(); l != nil {
		t.Fatalf("syntax menu: %s, want Plain Text", l.Name)
	}
}

// Typing in a group without tabs opens a tab with the text, as in
// Sublime Text.
func TestTypeWithoutTabs(t *testing.T) {
	a := newApp("")
	a.newFile()
	tt := ui.NewTester(a.view, 800, 500)
	if err := tt.Click("Close untitled"); err != nil {
		t.Fatal(err)
	}
	tt.Type("hi")
	tt.Type("!")
	if n := a.docCount(); n != 1 || a.current().st.Buffer.String() != "hi!" {
		t.Fatalf("typing without tabs: %d tabs", n)
	}
	if !a.current().dirty() || len(a.mru) != 1 {
		t.Fatalf("the tab typing opened: dirty %v, %d in the tab order", a.current().dirty(), len(a.mru))
	}
}

// A tab dragged along its tab bar moves as the pointer goes, the others
// making way, and stays where it is let go.
func TestTabDrag(t *testing.T) {
	a := newApp("")
	for _, s := range []string{"one", "two", "three"} {
		a.add("/tmp/"+s, s)
	}
	tt := ui.NewTester(a.view, 900, 300)
	tt.Frame()
	r, _ := tt.Find("one")
	tt.Press(r.X+20, r.Y+17)
	for x := float32(20); x <= 260; x += 20 {
		tt.Move(r.X+20+x, r.Y+17)
	}
	names := func() (s []string) {
		for _, d := range a.groups[0].docs {
			s = append(s, d.name())
		}
		return s
	}
	if got := names(); !slices.Equal(got, []string{"two", "one", "three"}) || a.current().name() != "one" {
		t.Fatalf("dragging one past two: %q", got)
	}
	tt.Release(r.X+280, r.Y+17)
	tt.Frame()
	if got := names(); !slices.Equal(got, []string{"two", "one", "three"}) || a.drag != nil {
		t.Fatalf("letting go: %q", got)
	}
}

// Tabs that do not fit scroll: to show the current one, by a touchpad or
// wheel either way, and along while a tab dragged is held at an end.
func TestTabOverflow(t *testing.T) {
	a := newApp("")
	for i := range 40 {
		a.add(fmt.Sprintf("/tmp/%02d", i), "")
	}
	tt := ui.NewTester(a.view, 900, 300)
	tt.Frame()
	g := a.groups[0]
	whole := func(name string) bool {
		r, _ := tt.Find(name)
		return r.W == tabMinW
	}
	if !whole("39") {
		t.Fatalf("the tab opened last is out of sight: scrolled %v", g.scroll)
	}
	tabs := ui.Cmd
	if !mac {
		tabs = ui.Alt
	}
	tt.Key(tabs, ui.Key1)
	tt.Frame()
	if !whole("00") {
		t.Fatalf("the first tab chosen is out of sight: scrolled %v", g.scroll)
	}
	tt.Scroll(g.bar.X+100, g.bar.Y+10, 0, 150)
	if g.scroll != 150 {
		t.Fatalf("scrolling down over the tabs: scrolled %v, want 150", g.scroll)
	}

	r, _ := tt.Find("02")
	x, y := r.X+20, r.Y+17
	tt.Press(x, y)
	for ; x < g.bar.X+g.bar.W-30; x += 40 {
		tt.Move(x, y)
	}
	tt.Advance(time.Second)
	i := slices.IndexFunc(g.docs, func(d *doc) bool { return d.name() == "02" })
	if g.scroll < 600 || i < 12 {
		t.Fatalf("holding a tab at the end: scrolled %v, the tab at %d", g.scroll, i)
	}
	tt.Move(x, y+200)
	tt.Advance(time.Second)
	s := g.scroll
	tt.Move(x, y)
	if g.scroll != s {
		t.Fatalf("a tab pulled out and back to the end: scrolled from %v to %v at once", s, g.scroll)
	}
	tt.Release(x, y)

	// The current tab moves as the tabs before it close.
	a.show(g.docs[30])
	tt.Frame()
	a.closeTabs(g.docs[:30], func(*doc) bool { return true })
	tt.Frame()
	tt.Advance(time.Second)
	if !whole(a.current().name()) {
		t.Fatalf("closing the tabs to the left: the current tab is out of sight: scrolled %v", g.scroll)
	}
}

// Tabs of files with the same name show the fewest parent folders that
// tell them apart.
func TestTabDirs(t *testing.T) {
	a := newApp("")
	want := map[string]string{
		"/w/a/src/index.ts": "a/src", "/w/b/src/index.ts": "b/src",
		"/w/cmd/lime/main.go": "lime", "/w/cmd/tool/main.go": "tool",
		"/src/x.go": "/src", "/q/src/x.go": "q/src",
		"/w/README.md": "",
	}
	for p := range want {
		a.add(filepath.FromSlash(p), "")
	}
	dirs := tabDirs(a.group().docs)
	for _, d := range a.group().docs {
		if w := filepath.FromSlash(want[filepath.ToSlash(d.path)]); dirs[d] != w {
			t.Errorf("%s: %q, want %q", d.path, dirs[d], w)
		}
	}
}

func TestGroups(t *testing.T) {
	if !mac {
		t.Skip("the test presses macOS keys")
	}
	a := newApp("")
	for _, s := range []string{"one", "two", "three"} {
		a.add("", s)
	}
	docs := a.allDocs()
	tt := ui.NewTester(a.view, 900, 500)

	tt.Key(ui.Cmd|ui.Alt, ui.Key2)
	tt.Key(ui.Ctrl|ui.Shift, ui.Key2)
	if len(a.groups) != 2 || a.focus != 1 || a.groups[1].current() != docs[2] {
		t.Fatalf("Columns: 2 and Ctrl+Shift+2: %d groups, focus %d", len(a.groups), a.focus)
	}
	tt.Type("x")
	if got := docs[2].st.Buffer.String(); got != "xthree" {
		t.Fatalf("the tab moved keeps the focus: %q", got)
	}

	// Ctrl+Tab walks the tabs by their last use while Ctrl is held.
	tt.HoldModifiers(ui.Ctrl)
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Key(ui.Ctrl, ui.KeyTab)
	if a.current() != docs[0] {
		t.Fatalf("Ctrl+Tab Tab: %q, want one", a.current().st.Buffer.String())
	}
	tt.HoldModifiers(0)
	tt.Frame()
	if !slices.Equal(a.mru, []*doc{docs[0], docs[2], docs[1]}) {
		t.Fatalf("letting Ctrl go does not make the tab the last used")
	}

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Key(ui.Cmd, ui.KeyDown)
	if len(a.groups) != 1 || a.docCount() != 3 {
		t.Fatalf("Cmd+K Cmd+Down: %d groups, %d tabs", len(a.groups), a.docCount())
	}
}

func TestPanels(t *testing.T) {
	if !mac {
		t.Skip("the test presses macOS keys")
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "src", "main.go")
	os.MkdirAll(filepath.Dir(main), 0o755)
	os.WriteFile(main, []byte("package main\n\nfunc main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Lime\n"), 0o644)
	a := newApp("")
	a.newFile()
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	if tt.HasText("FOLDERS") || !tt.HasText(filepath.Base(dir)) || !tt.HasText("README.md") {
		t.Fatalf("side bar: %q", tt.Texts())
	}

	tt.Key(ui.Cmd, ui.KeyP)
	tt.Type("mai:3")
	tt.Key(0, ui.KeyEnter)
	d := a.current()
	if d.path != main || d.st.CursorPos().Line != 2 {
		t.Fatalf("Goto Anything mai:3: %s line %d", d.path, d.st.CursorPos().Line+1)
	}

	tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	tt.Type("syntax markdown")
	tt.Key(0, ui.KeyEnter)
	if l := d.st.Buffer.Language(); l == nil || l.Name != "Markdown" {
		t.Fatalf("Set Syntax: Markdown: %v", l)
	}

	shown, before := a.scheme, *a.set
	a.showSchemes()
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	if next := schemes[(slices.Index(schemes, shown)+1)%len(schemes)]; a.scheme != next {
		t.Fatalf("moving in the color schemes shows %s, want %s", a.scheme.name, next.name)
	}
	tt.Key(0, ui.KeyEscape)
	if !reflect.DeepEqual(*a.set, before) || a.scheme != shown || a.panel != nil {
		t.Fatalf("Escape keeps %s", a.scheme.name)
	}

	proj := filepath.Join(dir, "lime.sublime-project")
	if err := a.saveProject(proj); err != nil {
		t.Fatal(err)
	}
	a.closeProject()
	if err := a.openProject(proj); err != nil || !slices.Equal(a.proj.folders, []string{dir}) {
		t.Fatalf("project: %v %q", err, a.proj.folders)
	}
}

// The find panel goes along the bottom of the editor, as Sublime Text's,
// and one opened in another tab starts with the last query, or the one
// Quick Add Next found.
func TestFindPanel(t *testing.T) {
	if !mac {
		t.Skip("the test presses macOS keys")
	}
	a := newApp("")
	one := a.add("", "alpha beta\nbeta alpha\n")
	two := a.add("", "one beta\n")
	tt := ui.NewTester(a.view, 900, 500)
	a.show(one)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Alt, ui.KeyF)
	tt.Type("beta")
	bar, _ := tt.Find("Find")
	ed, _ := tt.Find("Code editor")
	if bar.Y+bar.H != ed.Y+ed.H || bar.W != ed.W {
		t.Fatalf("find panel at %v, want along the bottom of the editor at %v", bar, ed)
	}
	if n, _ := one.st.FindMatches(); n != 2 || !tt.HasText("Replace All") {
		t.Fatalf("Cmd+Alt+F: %d matches, replace row %v", n, tt.HasText("Replace All"))
	}

	a.show(two)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	if got := two.st.SelectedText(); got != "beta" || tt.HasText("Replace All") {
		t.Fatalf("Cmd+F in another tab: selects %q, replace row %v", got, tt.HasText("Replace All"))
	}

	a.show(one)
	tt.Frame()
	one.st.Select(0, 0)
	tt.Key(ui.Cmd, ui.KeyD)
	tt.Key(ui.Cmd, ui.KeyD)
	if n := len(one.st.Selections()); n != 2 {
		t.Fatalf("Cmd+D twice: %d selections", n)
	}
	one.st.Select(5, 5)
	tt.Key(ui.Cmd, ui.KeyF)
	if got := one.st.SelectedText(); got != "alpha" {
		t.Fatalf("Cmd+F after Cmd+D: selects %q, want the next alpha", got)
	}
}

// A click anywhere on a folder's row in the side bar opens or closes it,
// and one on a file's opens the file.
func TestSideBar(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "src", "main.go")
	os.MkdirAll(filepath.Dir(main), 0o755)
	os.WriteFile(main, []byte("package main\n"), 0o644)
	a := newApp("")
	a.addFolder(dir)
	tt := ui.NewTester(a.view, 800, 500)
	if tt.HasText("main.go") {
		t.Fatal("src shows open")
	}
	if err := tt.Click("src"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second) // src's files grow in
	if err := tt.Click("main.go"); err != nil {
		t.Fatalf("a click on src: %v", err)
	}
	if d := a.current(); d == nil || d.path != main {
		t.Fatal("a click on main.go leaves it closed")
	}
	if err := tt.Click("src"); err != nil || *a.proj.isOpen(filepath.Dir(main), false) {
		t.Fatalf("a second click on src leaves it open: %v", err)
	}
	// A closed folder is no longer followed on disk, so it is read again
	// as it opens.
	tt.Advance(time.Second)
	os.WriteFile(filepath.Join(dir, "src", "util.go"), nil, 0o644)
	tt.Click("src")
	if tt.Advance(time.Second); !tt.HasText("util.go") {
		t.Fatal("src reopened leaves out a file made while it was closed")
	}

	files, git := locales.SideBarFilesPanel(), locales.SideBarGitPanel()
	for i, s := range []struct {
		click string // a panel's button, or "" for toggle_side_bar
		want  sidePanel
	}{
		{git, gitPanel}, {files, filesPanel}, {files, noPanel}, {git, gitPanel}, {git, noPanel}, {files, filesPanel},
		{"", noPanel}, {"", filesPanel}, {git, gitPanel}, {"", noPanel},
	} {
		if s.click == "" {
			a.run("toggle_side_bar")
		} else if err := tt.Click(s.click); err != nil {
			t.Fatal(err)
		}
		if tt.Frame(); a.side != s.want || a.set.SideBar != (s.want != noPanel) {
			t.Fatalf("step %d: side bar %d, setting %v, want %d", i, a.side, a.set.SideBar, s.want)
		}
	}
}

// Renaming a folder from its menu moves the tabs of its files, and it
// stays open.
func TestRenameFolder(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "src", "main.go")
	os.MkdirAll(filepath.Dir(main), 0o755)
	os.WriteFile(main, []byte("package main\n"), 0o644)
	a := newApp("")
	a.addFolder(dir)
	a.open([]string{main})
	*a.proj.isOpen(filepath.Dir(main), false) = true
	tt := ui.NewTester(a.view, 800, 500)
	if err := tt.RightClick("src"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename…"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("lib")
	tt.Key(0, ui.KeyEnter)
	lib := filepath.Join(dir, "lib")
	if _, err := os.Stat(filepath.Join(lib, "main.go")); err != nil {
		t.Fatal(err)
	}
	if d := a.current(); d.path != filepath.Join(lib, "main.go") {
		t.Fatalf("the tab shows %s", d.path)
	}
	if !*a.proj.isOpen(lib, false) {
		t.Fatal("lib shows closed")
	}
}

// New File and New Folder of a folder's menu name the item in the side
// bar. Escape or a press elsewhere makes nothing.
func TestCreateInSideBar(t *testing.T) {
	dir := t.TempDir()
	a := newApp("")
	a.addFolder(dir)
	tt := ui.NewTester(a.view, 800, 500)
	base := filepath.Base(dir)
	newItem := func(item string) {
		t.Helper()
		if err := tt.RightClick(base); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	newItem("New File")
	tt.Type("a.txt")
	tt.Key(0, ui.KeyEnter)
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if d := a.current(); d == nil || d.path != filepath.Join(dir, "a.txt") {
		t.Fatal("a.txt is not open")
	}
	tt.Frame()
	if !tt.HasText("a.txt") {
		t.Fatal("the side bar leaves a.txt out")
	}
	newItem("New Folder…")
	tt.Type("lib")
	tt.Key(0, ui.KeyEscape)
	if a.proj.create != nil {
		t.Fatal("Escape leaves the new folder's name")
	}
	newItem("New Folder…")
	tt.Type("lib")
	tt.ClickAt(400, 300)
	tt.Frame()
	if _, err := os.Stat(filepath.Join(dir, "lib")); err == nil || a.proj.create != nil {
		t.Fatal("a canceled folder was made")
	}
	d := a.add("", "x")
	a.write(d, filepath.Join(dir, "b.txt"))
	tt.Frame()
	if !tt.HasText("b.txt") {
		t.Fatal("the side bar leaves out a file saved in its folder")
	}
}

// Files take their icons as file-icons gives them, by name and by path.
func TestIcons(t *testing.T) {
	for p, want := range map[string]string{
		"main.go": "go", "go.mod": "config-go", ".gitignore": "git", "src/App.tsx": "tsx",
		"a.test.ts": "test-ts", "tests/foo_test.py": "test-python", "roles/web/tasks/main.yml": "ansible",
		"x.json.dist": "json", "x.unknownext": "",
	} {
		got := ""
		if i := matchFileIcon(p); i >= 0 {
			got = fileIconRules[i].icon
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", p, got, want)
		}
	}
	for _, r := range fileIconRules {
		if _, ok := fileIconGlyphs[r.icon]; !ok {
			t.Errorf("%s has no glyph", r.icon)
		}
	}
}

// Hot exit: the next run brings back the windows, their tabs, the unsaved
// text over the file on disk, and the layout.
func TestHotExit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	gone := filepath.Join(dir, "gone.txt")
	os.WriteFile(p, []byte("package a\n"), 0o644)
	os.WriteFile(gone, []byte("x\n"), 0o644)
	a := newApp(dir)
	a.open([]string{p})
	a.current().st.Buffer.Set("package b\n")
	a.open([]string{gone})
	a.setLayout(layouts[1])
	a.newFile()
	a.current().st.Buffer.Set("draft")
	a.moveToGroup(1)
	a.cols = []float32{30}
	// A tab dragged to another window leaves this one.
	w := a.l.newWindow()
	w.newFile()
	w.moveTab(a.docAt(gone), 0, 0)
	if a.docAt(gone) != nil || w.docCount() != 2 || w.current().path != gone {
		t.Fatalf("moving a tab between windows: %d tabs", w.docCount())
	}
	a.side, w.side = noPanel, gitPanel
	if err := a.l.saveSession(); err != nil {
		t.Fatal(err)
	}
	os.Remove(gone)

	l := newLime(dir)
	l.restoreSession()
	if len(l.wins) != 2 || l.wins[1].docCount() != 1 {
		t.Fatalf("windows: %d", len(l.wins))
	}
	b := l.wins[0]
	if b.side != noPanel || l.wins[1].side != gitPanel {
		t.Fatalf("side bars: %d, %d", b.side, l.wins[1].side)
	}
	if len(b.groups) != 2 || b.focus != 1 || b.cols[0] != 30 {
		t.Fatalf("layout: %d groups, focus %d", len(b.groups), b.focus)
	}
	g0, g1 := b.groups[0].docs, b.groups[1].docs
	if len(g0) != 1 || g0[0].path != p || g0[0].st.Buffer.String() != "package b\n" || !g0[0].dirty() {
		t.Fatalf("the unsaved file: %d tabs", len(g0))
	}
	if g0[0].st.Buffer.Undo(); g0[0].st.Buffer.String() != "package a\n" {
		t.Fatalf("Undo gives %q, not the file on disk", g0[0].st.Buffer.String())
	}
	if len(g1) != 1 || g1[0].path != "" || g1[0].st.Buffer.String() != "draft" || !g1[0].dirty() {
		t.Fatalf("the untitled tab: %d tabs", len(g1))
	}
}

// Hot exit keeps the bytes of unsaved text that isn't UTF-8, as of a GBK
// file.
func TestHotExitKeepsBytes(t *testing.T) {
	dir := t.TempDir()
	a := newApp(dir)
	a.newFile()
	text := "\xc4\xe3\xba\xc3\n"
	a.current().st.Buffer.Set(text)
	if err := a.l.saveSession(); err != nil {
		t.Fatal(err)
	}
	l := newLime(dir)
	l.restoreSession()
	if got := l.wins[0].current().st.Buffer.String(); got != text {
		t.Fatalf("restored %q, want %q", got, text)
	}
}

// Open Recent lists the files, folders and projects opened, newest first,
// which the session keeps. Quick Switch Project opens a folder in a window
// without folders or unsaved text.
func TestRecent(t *testing.T) {
	dir := t.TempDir()
	file, other := filepath.Join(dir, "a.txt"), filepath.Join(dir, "other")
	os.WriteFile(file, []byte("a\n"), 0o644)
	os.Mkdir(other, 0o755)
	proj := filepath.Join(dir, "a.sublime-project")
	a := newApp(dir)
	a.open([]string{dir, file})
	if err := a.saveProject(proj); err != nil {
		t.Fatal(err)
	}
	b := a.l.newWindow()
	b.switchProject(other)
	if !slices.Equal(b.proj.folders, []string{other}) {
		t.Fatalf("switching an empty window to %s: %q", other, b.proj.folders)
	}
	if err := a.l.saveSession(); err != nil {
		t.Fatal(err)
	}
	l := newLime(dir)
	l.restoreSession()
	if !slices.Equal(l.recent.Files, []string{file}) || !slices.Equal(l.recent.Folders, []string{other, proj, dir}) {
		t.Fatalf("recent: %+v", l.recent)
	}
}

// The color scheme of older settings becomes the appearance and the theme
// that show it.
func TestMigrateColorScheme(t *testing.T) {
	for _, tc := range []struct{ old, appearance, light, dark string }{
		{"Auto", followSystem, "GitHub Light", "GitHub Dark"},
		{"Monokai", darkMode, "GitHub Light", "Monokai"},
		{"Celeste", lightMode, "Celeste", "GitHub Dark"},
	} {
		dir := t.TempDir()
		os.WriteFile(settingsPath(dir), []byte(`{"color_scheme": "`+tc.old+`"}`), 0o644)
		s, _ := loadSettings(dir)
		if s.Appearance != tc.appearance || s.LightTheme != tc.light || s.DarkTheme != tc.dark || s.ColorScheme != "" {
			t.Errorf("%s: %+v", tc.old, s)
		}
	}
}

// A settings.json saved with an error leaves the settings as they were,
// and is not written over with them.
func TestInvalidSettings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(settingsPath(dir), []byte(`{"font_size": 20}`), 0o644)
	l := newLime(dir)
	bad := []byte(`{"font_size": 20,}`)
	os.WriteFile(settingsPath(dir), bad, 0o644)
	l.reloadSettings()
	if l.set.FontSize != 20 {
		t.Fatalf("font size %v after a file that does not parse, want 20", l.set.FontSize)
	}
	l.set.WordWrap = false
	l.saveSettings()
	if b, _ := os.ReadFile(settingsPath(dir)); string(b) != string(bad) {
		t.Fatalf("settings.json written over: %s", b)
	}
}

func TestDeleteBlankLines(t *testing.T) {
	for _, c := range []struct {
		id, text string
		sel      editor.Selection
		want     string
	}{
		{"delete_blank_lines", "a\n\n \t\nb\n\n", editor.Selection{}, "a\nb\n"},
		{"delete_blank_lines", "a\n\n  ", editor.Selection{}, "a\n"},
		{"delete_blank_lines", "a\n\nb\n\nc\n", editor.Selection{Anchor: 0, Head: 3}, "a\nb\n\nc\n"},
		{"delete_blank_lines surplus", "a\n\n\n\nb\n\n\n", editor.Selection{}, "a\n\nb\n\n"},
	} {
		a := newApp("")
		st := a.add("", c.text).st
		st.SetSelections(c.sel)
		a.run(c.id)
		if got := st.Buffer.String(); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.id, c.text, got, c.want)
		}
	}
}

// The case commands convert each selection, or the word at each caret, a
// word with several carets once, and select what they converted. The
// naming styles leave out the white space around a selection, and carets
// in no word stay where they are.
func TestConvertCase(t *testing.T) {
	sel := func(a, h int) editor.Selection { return editor.Selection{Anchor: a, Head: h} }
	for _, c := range []struct {
		id, text string
		sels     []editor.Selection
		want     string
		then     []editor.Selection
	}{
		{"swap_case", "Hello World", []editor.Selection{sel(11, 0)}, "hELLO wORLD", []editor.Selection{sel(0, 11)}},
		{"swap_case", "fooBar baz", []editor.Selection{sel(1, 1), sel(4, 4), sel(8, 8)}, "FOObAR BAZ", []editor.Selection{sel(0, 6), sel(7, 10)}},
		{"swap_case", "a  b", []editor.Selection{sel(2, 2)}, "a  b", []editor.Selection{sel(2, 2)}},
		{"swap_case", "ı x", []editor.Selection{sel(0, 0), sel(3, 3)}, "I X", []editor.Selection{sel(0, 1), sel(2, 3)}},
		{"upper_case", "fooBar", []editor.Selection{sel(1, 1), sel(4, 4)}, "FOOBAR", []editor.Selection{sel(0, 6)}},
		{"convert_to_snake", "  fooBar\nnext", []editor.Selection{sel(0, 9)}, "  foo_bar\nnext", []editor.Selection{sel(2, 9)}},
		{"convert_to_dash", "a -> b", []editor.Selection{sel(3, 3)}, "a -> b", []editor.Selection{sel(3, 3)}},
		{"convert_to_snake", "fooBar  a", []editor.Selection{sel(1, 1), sel(7, 7)}, "foo_bar  a", []editor.Selection{sel(0, 7), sel(8, 8)}},
	} {
		a := newApp("")
		st := a.add("", c.text).st
		st.SetSelections(c.sels...)
		a.run(c.id)
		if got := st.Buffer.String(); got != c.want || !slices.Equal(st.Selections(), c.then) {
			t.Errorf("%s %q: %q %v, want %q %v", c.id, c.text, got, st.Selections(), c.want, c.then)
		}
	}
}

func TestFormatJSON(t *testing.T) {
	a := newApp("")
	st := a.add("a.json", "{\"a\": [1, 2], \"b\": {}}\n").st
	st.Indent, st.TabSize = editor.IndentSpaces, 2
	a.run("format_json")
	if got, want := st.Buffer.String(), "{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": {}\n}\n"; got != want {
		t.Fatalf("format_json: %q, want %q", got, want)
	}
	a.run("minify_json")
	if got, want := st.Buffer.String(), "{\"a\":[1,2],\"b\":{}}\n"; got != want {
		t.Fatalf("minify_json: %q, want %q", got, want)
	}

	bad := "{\n  \"a\": 1,\n}\n"
	st.Buffer.Set(bad)
	st.Select(0, 0)
	a.run("format_json")
	if got := st.Buffer.String(); got != bad {
		t.Fatalf("format_json changed invalid JSON: %q", got)
	}
	if got := st.Selection(); got != (editor.Selection{Anchor: 12, Head: 12}) {
		t.Fatalf("caret at %v, want at the closing brace, 12", got)
	}
	if a.notice.text == "" {
		t.Fatal("no notice of the invalid JSON")
	}
}

func TestTidyOnSave(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	a := newApp("")
	a.set.TrimTrailingWhiteSpaceOnSave, a.set.EnsureNewlineAtEOFOnSave = true, true
	d := a.add(p, "a  \nb\t")
	if err := a.write(d, p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "a\nb\n" {
		t.Fatalf("saved %q, want %q", b, "a\nb\n")
	}
	d.st.Run(editor.Undo)
	if got := d.st.Buffer.String(); got != "a  \nb\t" {
		t.Fatalf("one undo: %q, want the text as typed", got)
	}
}

// A tab that shows no text has no editor to bind the focus to, which
// would leave a click in another group unfollowed.
func TestPreviewFocus(t *testing.T) {
	a := newApp("")
	a.add("/p/a.txt", "a\n")
	a.newPane()
	a.focusGroup(0)
	a.add("/p/x.bin", "").kind = binaryDoc
	tt := ui.NewTester(a.view, 1000, 500)
	tt.Frame()
	tt.ClickAt(800, 200)
	tt.Frame()
	if a.focus != 1 {
		t.Fatalf("a click in group 1's editor leaves the focus on group %d", a.focus)
	}
}

func TestCompare(t *testing.T) {
	a := newApp("")
	old := a.add("/p/old.txt", "one\ntwo\n")
	src := a.add("/p/new.txt", "one\n2\n")
	a.compareDocs(src, old)
	d := a.current()
	if d.cmp == nil || d.name() != "old.txt ↔ new.txt" || len(d.cmp.diff.Hunks()) != 1 {
		t.Fatalf("diff tab %q, want old.txt ↔ new.txt with one change", d.name())
	}
	tt := ui.NewTester(a.view, 900, 500)
	tt.Frame()
	if s := a.session(); len(s.Groups[0].Docs) != 2 {
		t.Fatalf("session keeps %d tabs, want the 2 files", len(s.Groups[0].Docs))
	}
	a.remove(src)
	if a.docCount() != 1 {
		t.Fatalf("%d tabs after closing new.txt, want its diff tab gone with it", a.docCount())
	}
	same := a.add("/p/same.txt", "one\ntwo\n")
	a.compareDocs(same, old)
	if a.current() != same || a.notice.text != locales.StatusNoDifferences() {
		t.Fatalf("same texts: notice %q, want no diff tab", a.notice.text)
	}
}

// F12 goes to the definition of the word at the caret, in its tab, else
// in another, and says when there is none.
func TestGotoDefinition(t *testing.T) {
	a := newApp("")
	other := a.add("/p/b.go", "package p\n\nfunc helper() {}\n")
	d := a.add("/p/a.go", "package p\n\nfunc main() {\n\trun()\n\thelper()\n\tmissing()\n}\n\nfunc (s *S) run() {}\n")
	text := d.st.Buffer.String()
	tt := ui.NewTester(a.view, 800, 500)

	d.st.Select(strings.Index(text, "run"), strings.Index(text, "run"))
	tt.Key(0, ui.KeyF12)
	if got, want := d.st.Cursor(), strings.LastIndex(text, "run"); got != want {
		t.Fatalf("F12 on run: caret at %d, want %d", got, want)
	}
	d.st.Select(strings.Index(text, "missing"), strings.Index(text, "missing"))
	tt.Key(0, ui.KeyF12)
	if a.notice.text != locales.StatusNoDefinition("missing") {
		t.Fatalf("F12 on missing: notice %q", a.notice.text)
	}
	d.st.Select(strings.Index(text, "helper"), strings.Index(text, "helper"))
	tt.Key(0, ui.KeyF12)
	if got, want := other.st.Cursor(), strings.Index(other.st.Buffer.String(), "helper"); a.current() != other || got != want {
		t.Fatalf("F12 on helper: tab %q, caret at %d, want b.go at %d", a.current().name(), got, want)
	}
}

// Goto Symbol lists the tab's definitions and Markdown's headings. One
// shows as it is selected; Escape, or another row picked, puts the caret
// and the scroll back. name@ lists another file's.
func TestGotoSymbol(t *testing.T) {
	a := newApp("")
	md := a.add("/p/README.md", "# Lime\n\nText\n\n## Install ##\n")
	d := a.add("/p/a.go", "package p\n\nfunc one() {}\n"+strings.Repeat("\n", 200)+"func two() {}\n")
	tt := ui.NewTester(a.view, 800, 500)
	selected := func(d *doc, name string) bool {
		at := strings.Index(d.st.Buffer.String(), name)
		return d.st.Selection() == editor.Selection{Anchor: at, Head: at + len(name)}
	}

	tt.Frame()
	d.st.SetScrollY(300)
	tt.Frame()
	a.run("goto_symbol")
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	if !selected(d, "two") {
		t.Fatalf("Down to two: selection %v", d.st.Selection())
	}
	a.panel.stale = true
	tt.Frame()
	if a.panel.sel != 1 {
		t.Fatalf("the items listed again select row %d, want two's", a.panel.sel)
	}
	tt.Key(0, ui.KeyEscape)
	if d.st.Cursor() != 0 || d.st.ScrollY() != 300 {
		t.Fatalf("Escape: caret at %d, scrolled to %v, want 0 and 300", d.st.Cursor(), d.st.ScrollY())
	}
	a.run("goto_symbol")
	tt.Frame()
	tt.Type("one")
	tt.Key(0, ui.KeyEnter)
	if !selected(d, "one") {
		t.Fatalf("@one: selection %v", d.st.Selection())
	}
	a.run("goto_symbol")
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyBackspace)
	tt.Type("READ@inst")
	tt.Key(0, ui.KeyEnter)
	if a.current() != md || !selected(md, "Install") || !selected(d, "one") {
		t.Fatalf("READ@inst: tab %q, selections %v and %v", a.current().name(), md.st.Selection(), d.st.Selection())
	}
	// A press outside a panel that showed nothing goes to the editor.
	a.run("show_goto")
	tt.Frame()
	tt.ClickAt(400, 450)
	if s := md.st.Selection(); a.panel != nil || s.Anchor != s.Head {
		t.Fatalf("a press outside Goto Anything: selection %v", s)
	}
	// A file of the project's, not open, with CRLFs, which its tab has not.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package p\r\n\r\nfunc three() {}\r\n"), 0o644)
	a.open([]string{dir})
	a.run("show_goto")
	tt.Frame()
	tt.Type("b.go@")
	tt.Key(0, ui.KeyEnter)
	if b := a.current(); b.path != filepath.Join(dir, "b.go") || !selected(b, "three") {
		t.Fatalf("b.go@: tab %q, selection %v", b.name(), b.st.Selection())
	}
}

// Jump Back and Jump Forward go through where the caret jumped from: 10
// lines or more at once, or to another tab. Nothing is a jump while the
// caret is being chosen: Goto Symbol's rows, the find panel's query typed,
// Ctrl+Tab's tabs. A closed tab's are forgotten.
func TestJumps(t *testing.T) {
	a := newApp("")
	b := a.add("/p/b.go", "package p\n")
	gap := strings.Repeat("\n", 30)
	d := a.add("/p/a.go", "package p\n\nfunc near() {}\n"+gap+"func far() {}\n"+gap+"func end() {}\n")
	tt := ui.NewTester(a.view, 800, 500)
	at := func(want *doc, line int) {
		t.Helper()
		if got := a.current(); got != want || got.st.CursorPos().Line != line {
			t.Fatalf("caret in %s on line %d, want %s on line %d", got.name(), got.st.CursorPos().Line, want.name(), line)
		}
	}
	// The editor shown takes the keyboard a frame after it shows.
	show := func(d *doc) {
		a.show(d)
		tt.Frame()
		tt.Frame()
	}
	end := d.st.Buffer.Pos(strings.Index(d.st.Buffer.String(), "end")).Line

	tt.Key(0, ui.KeyDown)
	a.run("goto_symbol")
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	at(d, end)
	a.run("jump_back")
	at(d, 1)
	a.run("jump_back")
	at(d, 1)
	a.run("jump_forward")
	at(d, end)
	a.run("goto_symbol")
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	a.run("jump_back")
	tt.Key(0, ui.KeyEscape)
	a.run("jump_forward")
	at(d, end)

	f := a.add("/p/f.txt", "start\n"+gap+"ab\n"+gap+"abc\n"+gap+"abcd\n")
	tt.Frame()
	tt.HoldModifiers(ui.Ctrl)
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.HoldModifiers(0)
	tt.Frame()
	at(b, 0)
	a.run("jump_back")
	at(f, 0)
	tt.Frame()
	a.run("show_panel find")
	tt.Frame()
	for _, r := range "abcd" {
		tt.Type(string(r))
		tt.Frame()
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	at(f, 93)
	a.run("jump_back")
	at(f, 0)

	show(b)
	show(f)
	a.remove(b)
	a.run("jump_back")
	at(d, end)
	tt.Key(0, ui.KeyForward)
	at(f, 0)
}

// A file dragged in the side bar onto a folder moves into it, its tab
// following. A copy leaves it and its tab where they are.
func TestSideBarDrag(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	file := filepath.Join(dir, "a.txt")
	os.WriteFile(file, []byte("a\n"), 0o644)
	a := newApp("")
	a.open([]string{dir})
	a.open([]string{file})
	tt := ui.NewTester(a.view, 900, 600)
	f, _ := tt.Find("a.txt")
	g, ok := tt.Find("sub")
	if !ok {
		t.Fatalf("side bar: %q", tt.Texts())
	}
	tt.Press(f.X+4, f.Y+4)
	for i := float32(1); i <= 8; i++ {
		tt.Move(f.X+4, f.Y+4+(g.Y-f.Y)*i/8)
	}
	tt.HoldModifiers(copyMods)
	if tt.Cursor() != ui.CursorCopy {
		t.Fatalf("holding copyMods shows cursor %v", tt.Cursor())
	}
	tt.HoldModifiers(0)
	if tt.Cursor() == ui.CursorCopy {
		t.Fatal("letting go of copyMods keeps the copy cursor")
	}
	tt.Release(g.X+4, g.Y+4)
	tt.Frame()
	moved := filepath.Join(dir, "sub", "a.txt")
	if _, err := os.Stat(moved); err != nil || a.current().path != moved {
		t.Fatalf("moving a.txt into sub: %v, tab at %s", err, a.current().path)
	}

	// The Tester lets go of the pointer without modifiers, so a copy is
	// asked for as the drop would.
	a.dropInto(moved, dir, true)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("copying a.txt back: %v", err)
	}
	if _, err := os.Stat(moved); err != nil || a.current().path != moved {
		t.Fatalf("copying keeps sub/a.txt and its tab: %v, tab at %s", err, a.current().path)
	}
	for name, want := range map[string]string{"a.txt": "a 2.txt", "sub": "sub 2", ".env": ".env 2"} {
		if got := keepBoth(filepath.Join(dir, name)); got != filepath.Join(dir, want) {
			t.Errorf("keeping both of %s: %s", name, filepath.Base(got))
		}
	}
}

// A file changed on disk shows as it is now, unless its tab has unsaved
// changes, which stay.
func TestReloadChanged(t *testing.T) {
	dir := t.TempDir()
	clean, edited := filepath.Join(dir, "clean.txt"), filepath.Join(dir, "edited.txt")
	os.WriteFile(clean, []byte("one\n"), 0o644)
	os.WriteFile(edited, []byte("one\n"), 0o644)
	a := newApp("")
	a.open([]string{clean, edited})
	a.docAt(edited).st.Buffer.Set("mine\n")
	os.WriteFile(clean, []byte("two two\n"), 0o644)
	os.WriteFile(edited, []byte("two two\n"), 0o644)
	a.reloadChanged()
	if d := a.docAt(clean); d.st.Buffer.String() != "two two\n" || d.dirty() {
		t.Fatalf("clean tab: %q, dirty %v", d.st.Buffer.String(), d.dirty())
	}
	if got := a.docAt(edited).st.Buffer.String(); got != "mine\n" {
		t.Fatalf("edited tab: %q", got)
	}
}

// The side bar shows the branch of a project's repository, whose panel
// switches it, and the tabs show the files as the branch has them.
func TestGitBar(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	git := func(args ...string) {
		t.Helper()
		if _, err := runGit(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(file, []byte("main\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "main")
	git("switch", "-q", "-c", "topic")
	os.WriteFile(file, []byte("topic branch\n"), 0o644)
	git("commit", "-q", "-am", "topic")
	git("switch", "-q", "main")

	a := newApp("")
	a.open([]string{dir, file})
	tt := ui.NewTester(a.view, 900, 600)
	tt.SetTitleBar(ui.TitleBar{Height: 38, Left: 78})
	if a.git.tree != "main" || a.git.branch != "main" || !tt.HasText(filepath.Base(dir)) {
		t.Fatalf("side bar: %q", tt.Texts())
	}
	if err := tt.Click(locales.GitSwitchBranch()); err != nil {
		t.Fatal(err)
	}
	if p := a.panel; p == nil || !slices.ContainsFunc(p.list(""), func(it item) bool { return it.label == "topic" }) {
		t.Fatal("the panel lists no topic branch")
	}
	a.cancelPanel()
	a.gitRun("switch", "topic")
	if a.git.branch != "topic" {
		t.Fatalf("branch: %q", a.git.branch)
	}
	if got := a.docAt(file).st.Buffer.String(); got != "topic branch\n" {
		t.Fatalf("tab: %q", got)
	}

	tree := filepath.Join(t.TempDir(), "side")
	git("worktree", "add", "-q", "-b", "side", tree)
	a.switchWorktree(tree)
	if a.git.tree != "side" || a.git.branch != "side" || a.proj.folders[0] != tree {
		t.Fatalf("work tree: %+v, folders %q", a.git, a.proj.folders)
	}
}

// The history lists the commits, each of which opens a tab of the diffs
// of its files, which neither side edits.
func TestHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	git := func(args ...string) {
		t.Helper()
		if _, err := runGit(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(file, []byte("one\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.bin"), []byte("x\x00y"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "first")
	os.WriteFile(file, []byte("one\ntwo\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.bin"), []byte("x\x00z"), 0o644)
	git("commit", "-q", "-am", "second", "-m", "The body.")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644)

	a := newApp("")
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	tt.Click(locales.SideBarGitPanel())
	tt.Click(locales.GitHistory())
	tt.Frame()
	if !tt.HasText("second") || !tt.HasText("first") {
		t.Fatalf("history: %q", tt.Texts())
	}

	tt.Click("second")
	tt.Frame()
	d := a.current()
	if d == nil || d.commit == nil || d.commit.body != "second\n\nThe body." || len(d.commit.files) != 2 {
		t.Fatalf("commit tab: %+v", d)
	}
	text, bin := d.commit.files[0], d.commit.files[1]
	if text.path != "a.txt" || text.added != 1 || text.deleted != 0 || len(text.hunks) != 1 || len(text.new.lines) != 2 || text.folded {
		t.Fatalf("a.txt: %+v", text)
	}
	if !bin.binary || !bin.folded {
		t.Fatalf("b.bin: %+v", bin)
	}
	if tt.HasText("The body.") || !tt.HasText("a.txt") {
		t.Fatalf("commit tab: %q", tt.Texts())
	}
	tt.Click(locales.CommitShowMessage())
	if !tt.HasText("The body.") {
		t.Fatalf("full message: %q", tt.Texts())
	}
	if !tt.HasText("two") {
		t.Fatalf("a.txt's lines: %q", tt.Texts())
	}
	tt.Click(locales.CommitCollapseAll())
	if !text.folded {
		t.Fatal("Collapse All left a.txt open")
	}
	n := a.docCount()
	tt.Click("second")
	if a.docCount() != n {
		t.Fatal("the commit opened a second tab")
	}
}

// newRepo makes a repository in a new folder, and a git to run in it.
func newRepo(t *testing.T) (string, func(...string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if _, err := runGit(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	return dir, git
}

// The Changes tab groups the work tree's changes as git status tells
// them, each of which shows the diffs of its section.
func TestChanges(t *testing.T) {
	dir, git := newRepo(t)
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a", "b", "c", "d", "e", "old", "both"} {
		write(name+".txt", name+"\n")
	}
	git("add", ".")
	git("commit", "-q", "-m", "base")
	git("switch", "-q", "-c", "other")
	write("both.txt", "other\n")
	git("commit", "-q", "-am", "other")
	git("switch", "-q", "main")
	write("both.txt", "main\n")
	git("commit", "-q", "-am", "main")
	runGit(dir, "merge", "-q", "other")
	write("a.txt", "a2\n")
	write("b.txt", "b2\n")
	git("add", "b.txt")
	write("c.txt", "c2\n")
	git("add", "c.txt")
	write("c.txt", "c3\n")
	write("new.txt", "new\n")
	git("add", "new.txt")
	os.Remove(filepath.Join(dir, "d.txt"))
	git("rm", "-q", "e.txt")
	git("mv", "old.txt", "renamed.txt")
	write("untracked.txt", "u\n")

	got := readStatus(dir)
	want := [3]string{
		"U both.txt ",
		"M b.txt M c.txt D e.txt A new.txt R old.txt>renamed.txt ",
		"M a.txt M c.txt D d.txt ? untracked.txt ",
	}
	for sec, files := range got {
		var b strings.Builder
		for _, f := range files {
			if f.oldPath != "" {
				fmt.Fprintf(&b, "%c %s>%s ", f.status, f.oldPath, f.path)
				continue
			}
			fmt.Fprintf(&b, "%c %s ", f.status, f.path)
		}
		if b.String() != want[sec] {
			t.Errorf("section %d:\n got %s\nwant %s", sec, b.String(), want[sec])
		}
	}

	a := newApp("")
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	tt.Click(locales.SideBarGitPanel())
	tt.Frame()
	for _, s := range []string{locales.ChangesConflicts(), locales.ChangesStaged(), locales.ChangesUnstaged(), "untracked.txt"} {
		if !tt.HasText(s) {
			t.Fatalf("no %q in %q", s, tt.Texts())
		}
	}
	tt.Click("new.txt")
	tt.Frame()
	if d := a.current(); d == nil || d.commit == nil || d.commit.hash != stagedChanges || len(d.commit.files) != 5 {
		t.Fatalf("staged tab: %+v", d)
	}
	tt.Click("untracked.txt")
	tt.Frame()
	d := a.current()
	if d == nil || d.commit == nil || d.commit.hash != unstagedChanges {
		t.Fatalf("unstaged tab: %+v", d)
	}
	if f := d.commit.files[3]; f.path != "untracked.txt" || f.added != 1 {
		t.Fatalf("untracked.txt: %+v", f)
	}
}

// The check boxes, Space and the menu of the rows chosen stage and
// unstage files.
func TestStage(t *testing.T) {
	dir, git := newRepo(t)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("1\n"), 0o644)
	}
	git("add", ".")
	git("commit", "-q", "-m", "base")
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("2\n"), 0o644)
	}
	staged := func() string {
		t.Helper()
		out, _ := runGit(dir, "diff", "--cached", "--name-only")
		return strings.Join(strings.Fields(out), " ")
	}

	a := newApp("")
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	tt.Click(locales.SideBarGitPanel())
	tt.Frame()
	box, _ := tt.Find(locales.ChangesStageAll())
	row, _ := tt.Find("a.txt")
	tt.ClickAt(box.X+box.W/2, row.Y+row.H/2)
	tt.Frame()
	if got := staged(); got != "a.txt" {
		t.Fatalf("the check box staged %q", got)
	}

	tt.Click("b.txt")
	tt.ClickWith(ui.Cmd, "c.txt")
	tt.RightClick("c.txt")
	if err := tt.ChooseMenuItem(locales.ChangesStage()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := staged(); got != "a.txt b.txt c.txt" {
		t.Fatalf("the menu staged %q", got)
	}

	tt.Click(locales.ChangesUnstageAll())
	tt.Frame()
	if got := staged(); got != "" {
		t.Fatalf("Unstage All left %q", got)
	}

	tt.Click("b.txt")
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if got := staged(); got != "b.txt" {
		t.Fatalf("Space staged %q", got)
	}
}

// The tree shows the changes in their folders, folders first, which open
// and close, and whose check boxes stage what they hold.
func TestChangesTree(t *testing.T) {
	dir, git := newRepo(t)
	names := []string{"top.txt", "src/b.txt", "src/a/x.txt", "src/a/y.txt"}
	for _, text := range []string{"1\n", "2\n"} {
		for _, name := range names {
			os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
			os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644)
		}
		if text == "1\n" {
			git("add", ".")
			git("commit", "-q", "-m", "base")
		}
	}
	a := newApp("")
	a.set.GitTreeView = true
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	tt.Click(locales.SideBarGitPanel())
	tt.Frame()
	rows := func() string {
		var b strings.Builder
		for _, r := range a.changes.rows {
			switch {
			case r.dir != "":
				fmt.Fprintf(&b, "%d:%s/ ", r.depth, path.Base(r.dir))
			case r.file != nil:
				fmt.Fprintf(&b, "%d:%s ", r.depth, path.Base(r.file.path))
			}
		}
		return b.String()
	}
	if got, want := rows(), "0:src/ 1:a/ 2:x.txt 2:y.txt 1:b.txt 0:top.txt "; got != want {
		t.Fatalf("tree:\n got %s\nwant %s", got, want)
	}
	tt.Click("a")
	tt.Frame()
	if got, want := rows(), "0:src/ 1:a/ 1:b.txt 0:top.txt "; got != want {
		t.Fatalf("a closed:\n got %s\nwant %s", got, want)
	}
	box, _ := tt.Find(locales.ChangesStageAll())
	row, _ := tt.Find("src")
	tt.ClickAt(box.X+box.W/2, row.Y+row.H/2)
	tt.Frame()
	if out, _ := runGit(dir, "diff", "--cached", "--name-only"); strings.Join(strings.Fields(out), " ") != "src/a/x.txt src/a/y.txt src/b.txt" {
		t.Fatalf("the folder's check box staged %q", out)
	}
}

// The Changes tab commits the changes staged, or with none, the tracked
// files' changes; a failing commit keeps its message.
func TestCommit(t *testing.T) {
	dir, git := newRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "base")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("2\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "u.txt"), []byte("u\n"), 0o644)
	last := func() string {
		t.Helper()
		out, _ := runGit(dir, "log", "-1", "--format=%B")
		return strings.TrimSpace(out)
	}

	a := newApp("")
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	tt.Click(locales.SideBarGitPanel())
	tt.Frame()
	tt.Click(locales.ChangesMessage())
	tt.Type("Track a\n\nThe body.")
	tt.Click(locales.ChangesCommitTracked())
	tt.Frame()
	if got := last(); got != "Track a\n\nThe body." {
		t.Fatalf("message: %q", got)
	}
	if out, _ := runGit(dir, "status", "--porcelain"); out != "?? u.txt\n" || a.changes.message != "" {
		t.Fatalf("after Commit Tracked: status %q, message %q", out, a.changes.message)
	}

	git("add", "u.txt")
	os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\necho no >&2\nexit 1\n"), 0o755)
	a.readChanges()
	tt.Frame()
	tt.Click(locales.ChangesMessage())
	tt.Type("Add u")
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if last() != "Track a\n\nThe body." || a.changes.message != "Add u" || !a.notice.err {
		t.Fatalf("a failing commit: last %q, message %q, error %v", last(), a.changes.message, a.notice.err)
	}
	os.Remove(filepath.Join(dir, ".git", "hooks", "pre-commit"))
	tt.Click(locales.ChangesCommit())
	tt.Frame()
	if last() != "Add u" {
		t.Fatalf("Commit: %q", last())
	}
}

// Amend loads HEAD's message, and its commit takes HEAD's place; the
// message typed before comes back.
func TestAmend(t *testing.T) {
	dir, git := newRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "Add a")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("2\n"), 0o644)
	git("add", ".")

	a := newApp("")
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	tt.Click(locales.SideBarGitPanel())
	tt.Frame()
	tt.Click(locales.ChangesMessage())
	tt.Type("Draft")
	tt.Click(locales.ChangesAmend())
	tt.Frame()
	if a.changes.message != "Add a" {
		t.Fatalf("amend loaded %q", a.changes.message)
	}
	a.changes.message = "Add a, as 2"
	tt.Click(locales.ChangesCommit())
	tt.Frame()
	if out, _ := runGit(dir, "log", "--format=%s"); out != "Add a, as 2\n" {
		t.Fatalf("log: %q", out)
	}
	if out, _ := runGit(dir, "show", "HEAD:a.txt"); out != "2\n" || a.changes.amend || a.changes.message != "Draft" {
		t.Fatalf("HEAD has %q; amend %v, message %q", out, a.changes.amend, a.changes.message)
	}
}

// A hunk of the unstaged changes stages alone, and unstages again from
// the staged changes.
func TestStageHunk(t *testing.T) {
	dir, git := newRepo(t)
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, strconv.Itoa(i))
	}
	file := filepath.Join(dir, "a.txt")
	os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "base")
	lines[1], lines[17] = "two", "eighteen"
	lines = slices.Insert(lines, 2, "added")
	os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	cached := func() string {
		t.Helper()
		out, _ := runGit(dir, "diff", "--cached", "-U0")
		var changed []string
		for l := range strings.Lines(out) {
			if (l[0] == '+' || l[0] == '-') && !strings.HasPrefix(l, "+++") && !strings.HasPrefix(l, "---") {
				changed = append(changed, strings.TrimSpace(l))
			}
		}
		return strings.Join(changed, " ")
	}

	a := newApp("")
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	cv := a.showCommit(commit{hash: unstagedChanges})
	tt.Frame()
	if f := cv.files; len(f) != 1 || len(f[0].hunks) != 2 {
		t.Fatalf("unstaged: %+v", f)
	}
	a.stageHunk("a.txt", cv.files[0].hunks[1], false)
	tt.Frame()
	if got := cached(); got != "-18 +eighteen" {
		t.Fatalf("staged the second hunk: %q", got)
	}
	if f := cv.files; len(f) != 1 || len(f[0].hunks) != 1 {
		t.Fatalf("unstaged after: %+v", f)
	}
	a.stageHunk("a.txt", cv.files[0].hunks[0], false)
	tt.Frame()
	if got := cached(); got != "-2 +two +added -18 +eighteen" {
		t.Fatalf("staged both hunks: %q", got)
	}

	staged := a.showCommit(commit{hash: stagedChanges})
	tt.Frame()
	if err := tt.Click(locales.ChangesUnstage()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := cached(); got != "-18 +eighteen" || len(staged.files[0].hunks) != 1 {
		t.Fatalf("unstaged the first hunk: %q", got)
	}
}

// Discarding a hunk or a file takes the work tree back to the index,
// whose staged changes stay, and the file's tab shows it. A textconv
// changes the diff shown, not the bytes it applies to.
func TestDiscard(t *testing.T) {
	dir, git := newRepo(t)
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, strconv.Itoa(i))
	}
	file := filepath.Join(dir, "a b.txt")
	write := func() { os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644) }
	write()
	git("add", ".")
	git("commit", "-q", "-m", "base")
	lines[17] = "eighteen"
	write()
	git("add", ".")
	lines[1], lines[9] = "two", "ten"
	write()
	git("config", "diff.upper.textconv", "tr a-z A-Z <")
	os.WriteFile(filepath.Join(dir, ".git", "info", "attributes"), []byte("*.txt diff=upper\n"), 0o644)

	a := newApp("")
	a.open([]string{dir, file})
	tt := ui.NewTester(a.view, 900, 600)
	cv := a.showCommit(commit{hash: unstagedChanges})
	tt.Frame()
	a.discardHunk("a b.txt", cv.files[0].hunks[0])
	tt.Frame()
	lines[1] = "2"
	if got := a.docAt(file).st.Buffer.String(); got != strings.Join(lines, "\n")+"\n" {
		t.Fatalf("after Discard Hunk: %q", got)
	}
	a.discardFiles([]change{{status: 'M', path: "a b.txt"}})
	tt.Frame()
	lines[9] = "10"
	if got := a.docAt(file).st.Buffer.String(); got != strings.Join(lines, "\n")+"\n" {
		t.Fatalf("after Discard Changes: %q", got)
	}
	if out, _ := runGit(dir, "status", "--porcelain"); out != "M  a b.txt\n" && out != "M  \"a b.txt\"\n" {
		t.Fatalf("status: %q", out)
	}
}

// Create Branch and Rename Branch take the name from the input panel,
// and a name git refuses shows what git says of it.
func TestBranchName(t *testing.T) {
	dir, git := newRepo(t)
	git("commit", "-q", "--allow-empty", "-m", "base")
	a := newApp("")
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	name := func(id, text string) {
		t.Helper()
		a.run(id)
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(text)
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
	}
	name("git_create_branch", "topic")
	if a.git.branch != "topic" {
		t.Fatalf("created %q", a.git.branch)
	}
	name("git_rename_branch", "feature")
	if a.git.branch != "feature" {
		t.Fatalf("renamed to %q", a.git.branch)
	}
	name("git_create_branch", "a..b")
	if !a.notice.err || !strings.Contains(a.notice.text, "a..b") || a.git.branch != "feature" {
		t.Fatalf("an invalid name: %q, branch %q", a.notice.text, a.git.branch)
	}
}

// Push publishes a branch without an upstream; the side bar counts the
// commits to push, and those Fetch finds to pull, which a click on each
// count takes.
func TestRemote(t *testing.T) {
	dir, git := newRepo(t)
	remote, other := t.TempDir(), t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		if _, err := runGit(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	run(remote, "init", "-q", "--bare", "-b", "main")
	git("remote", "add", "origin", remote)
	git("commit", "-q", "--allow-empty", "-m", "one")

	a := newApp("")
	a.open([]string{dir})
	tt := ui.NewTester(a.view, 900, 600)
	a.run("git_push")
	tt.Frame()
	if !a.git.upstream || a.git.ahead != 0 {
		t.Fatalf("published: %+v", a.git)
	}
	git("commit", "-q", "--allow-empty", "-m", "two")
	a.readGit()
	tt.Frame()
	if err := tt.Click(locales.CmdPush()); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	tt.Frame()
	if a.git.ahead != 0 {
		t.Fatalf("pushed: %+v", a.git)
	}

	run(other, "clone", "-q", remote, ".")
	run(other, "commit", "-q", "--allow-empty", "-m", "three")
	run(other, "push", "-q")
	a.run("git_fetch")
	tt.Frame()
	if err := tt.Click(locales.CmdPull()); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	tt.Frame()
	if out, _ := runGit(dir, "log", "-1", "--format=%s"); out != "three\n" || a.git.behind != 0 {
		t.Fatalf("pulled %q: %+v", out, a.git)
	}
}

func TestCommitRows(t *testing.T) {
	var old, cur []string
	for i := 1; i <= 10; i++ {
		old = append(old, strconv.Itoa(i))
	}
	cur = append(append(append(cur, old[:4]...), "five"), old[5:]...)
	ps, err := editor.ParsePatch("diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -2,7 +2,7 @@\n 2\n 3\n 4\n-5\n+five\n 6\n 7\n 8\n")
	if err != nil {
		t.Fatal(err)
	}
	f := &commitFile{hunks: ps[0].Hunks, old: fileText{lines: old}, new: fileText{lines: cur}}
	rows := func(unified bool) string {
		var b strings.Builder
		for _, r := range f.appendRows(nil, 0, unified, false) {
			fmt.Fprintf(&b, "%d:%d/%d ", r.kind, r.old, r.new)
			if r.kind == rowGap {
				fmt.Fprintf(&b, "(%d) ", r.count)
			}
		}
		return b.String()
	}
	if got, want := rows(false), "3:1/1 (1) 4:2/2 4:3/3 4:4/4 5:5/5 4:6/6 4:7/7 4:8/8 3:9/9 (2) "; got != want {
		t.Fatalf("side by side:\n got %s\nwant %s", got, want)
	}
	f.shown = map[int]bool{1: true}
	if got, want := rows(true), "3:1/1 (1) 4:2/2 4:3/3 4:4/4 5:5/0 5:0/5 4:6/6 4:7/7 4:8/8 4:9/9 4:10/10 "; got != want {
		t.Fatalf("unified:\n got %s\nwant %s", got, want)
	}
}

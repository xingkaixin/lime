package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
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
	tt.Key(ui.Cmd, ui.KeyU)
	if got := st.Buffer.String(); got != "alpha\nALPHA\nbeta\n" {
		t.Fatalf("Cmd+K Cmd+U: %q", got)
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
	if !tt.HasText("FOLDERS") || !tt.HasText("README.md") {
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

	a.showSchemes()
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	if a.set.ColorScheme != "GitHub Dark" {
		t.Fatalf("moving in the color schemes shows %s", a.set.ColorScheme)
	}
	tt.Key(0, ui.KeyEscape)
	if a.set.ColorScheme != autoScheme || a.panel != nil {
		t.Fatalf("Escape keeps %s", a.set.ColorScheme)
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
	if err := tt.Click("main.go"); err != nil {
		t.Fatalf("a click on src: %v", err)
	}
	if d := a.current(); d == nil || d.path != main {
		t.Fatal("a click on main.go leaves it closed")
	}
	if err := tt.Click("src"); err != nil || *a.proj.isOpen(filepath.Dir(main), false) {
		t.Fatalf("a second click on src leaves it open: %v", err)
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

// Files take their icons as Zed gives them, and every icon parses.
func TestIcons(t *testing.T) {
	for name, want := range map[string]string{
		"main.go": "go", "go.mod": "go-mod", ".gitignore": "git", "Gemfile.lock": "ruby-gem-lock",
		"Makefile": "makefile", "app.component.ts": "angular-component", "notes": "_file", "a.unknown": "_file",
	} {
		if fileIcon(name) != icon(want) {
			t.Errorf("%s: want %s", name, want)
		}
	}
	m := iconMap()
	for _, names := range []map[string]string{m.Stems, m.Suffixes} {
		for _, name := range names {
			b, err := iconFiles.ReadFile("icons/" + name + ".svg")
			if _, perr := ui.ParseSVG(b); err != nil || perr != nil {
				t.Errorf("%s: %v %v", name, err, perr)
			}
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

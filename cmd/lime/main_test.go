package main

import (
	"fmt"
	"os"
	"os/exec"
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
		for _, r := range f.appendRows(nil, 0, unified) {
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

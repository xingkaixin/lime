package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Cmd+K V opens a preview beside the Markdown tab, which keeps the
// keyboard; the preview follows the tab's edits once typing pauses,
// follows its links, and closes with the tab.
func TestMarkdownPreview(t *testing.T) {
	dir := t.TempDir()
	readme, guide := filepath.Join(dir, "README.md"), filepath.Join(dir, "docs", "my guide.md")
	os.MkdirAll(filepath.Dir(guide), 0o755)
	os.WriteFile(guide, []byte("# Guide\n"), 0o644)
	text := "# Title\n\nSee [the guide](docs/my%20guide.md), [the site](https://example.com) and [the end](#the-end).\n\n" +
		strings.Repeat("Text.\n\n", 100) + "## The End\n\n" + strings.Repeat("Text.\n\n", 50)
	os.WriteFile(readme, []byte(text), 0o644)
	a := newApp("")
	a.open([]string{readme})
	src := a.current()
	tt := ui.NewTester(a.view, 900, 600)

	tt.Key(ui.Cmd, ui.KeyK)
	// A window has V's text before the frame that runs the chord, which
	// the tester would run in between.
	tt.Type("v")
	tt.Key(0, ui.KeyV)
	if len(a.groups) != 2 || a.current() != src {
		t.Fatalf("Cmd+K V: %d groups, current %q", len(a.groups), a.current().name())
	}
	p := a.groups[1].current()
	if p == nil || p.md == nil || p.md.src != src || !tt.HasText("Preview: README.md") {
		t.Fatalf("Cmd+K V: no preview beside the tab in %q", tt.Texts())
	}
	if got := src.st.Buffer.String(); got[:1] != "#" {
		t.Fatalf("Cmd+K V typed into the tab: %q", got)
	}

	n := src.st.Buffer.Len()
	src.st.Buffer.Edit(editor.Edit{Start: n, End: n, Text: "\nMore text.\n"})
	tt.Frame()
	if tt.HasText("More text.") {
		t.Fatal("the preview parsed the text before typing paused")
	}
	time.Sleep(previewPause)
	tt.Frame()
	if !tt.HasText("More text.") {
		t.Fatalf("the preview did not follow the edit: %q", tt.Texts())
	}

	if err := tt.Click("the site"); err != nil {
		t.Fatal(err)
	}
	if got := tt.OpenedURLs(); !slices.Equal(got, []string{"https://example.com"}) {
		t.Fatalf("opened %q", got)
	}
	if err := tt.Click("the end"); err != nil {
		t.Fatal(err)
	}
	if r, _ := tt.Find("The End"); p.md.scroll.Y == 0 || r.Y > 100 {
		t.Fatalf("the end's link scrolled to %v, its heading at %v", p.md.scroll.Y, r.Y)
	}
	p.md.scroll.Y = 0
	tt.Frame()
	if err := tt.Click("the guide"); err != nil {
		t.Fatal(err)
	}
	if d := a.current(); d.path != guide {
		t.Fatalf("the guide's link opened %q", d.path)
	}
	if !slices.Contains(a.groups[1].docs, p) {
		t.Fatal("the guide took the preview's place")
	}

	a.remove(src)
	if slices.ContainsFunc(a.allDocs(), func(d *doc) bool { return d.md != nil }) {
		t.Fatal("the preview stayed open without its tab")
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// Cmd+K V opens a preview beside the Markdown tab, which keeps the
// keyboard; the preview follows the tab's edits once typing pauses,
// follows its links, and closes with the tab.
func TestMarkdownPreview(t *testing.T) {
	dir := t.TempDir()
	readme, guide := filepath.Join(dir, "README.md"), filepath.Join(dir, "docs", "my guide.md")
	os.MkdirAll(filepath.Dir(guide), 0o755)
	os.WriteFile(guide, []byte("# Guide\n"), 0o644)
	// Long enough to show in parts.
	text := "# Title\n\nSee [the guide](docs/my%20guide.md), [the site](https://example.com) and [the end](#the-end).\n\n" +
		strings.Repeat("Text.\n\n", 3000) + "## The End\n\n" + strings.Repeat("Text.\n\n", 50)
	os.WriteFile(readme, []byte(text), 0o644)
	a := newApp("")
	a.open([]string{readme})
	src := a.current()
	tt := ui.NewTester(a.view, 900, 600)

	tt.Key(ui.Cmd, ui.KeyK)
	if _, raw := tt.TextInputActive(); !raw {
		t.Fatal("Cmd+K left input methods on, which would take V")
	}
	// A window has V's text before the frame that runs the chord, which
	// the tester would run in between.
	tt.Type("v")
	tt.Key(0, ui.KeyV)
	if len(a.groups) != 2 || a.current() != src || !src.st.Focused() {
		t.Fatalf("Cmd+K V: %d groups, current %q, focused %v", len(a.groups), a.current().name(), src.st.Focused())
	}
	p := a.groups[1].current()
	if p == nil || p.md == nil || p.md.src != src || !tt.HasText("Preview: README.md") {
		t.Fatalf("Cmd+K V: no preview beside the tab in %q", tt.Texts())
	}
	if got := src.st.Buffer.String(); got[:1] != "#" {
		t.Fatalf("Cmd+K V typed into the tab: %q", got)
	}

	n := len("# Title\n\n")
	src.st.Buffer.Edit(editor.Edit{Start: n, End: n, Text: "More text.\n\n"})
	tt.Frame()
	if tt.HasText("More text.") {
		t.Fatal("the preview parsed the text before typing paused")
	}
	tt.Advance(previewPause)
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
	if r, ok := tt.Find("The End"); !ok || r.Y < 0 || r.Y > 100 {
		t.Fatalf("the end's link left its heading at %v, shown %v", r.Y, ok)
	}
	p.md.list.ScrollTo(0, ui.Start)
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

// A long text shows in parts that read as it does whole, its link
// reference definitions, between blocks and at the end, in every part.
func TestPreviewParts(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Parts\n\nSee [the start][mid] and [the end][end].\n\n[mid]: https://example.com/mid\n\n")
	for i := range 40 {
		fmt.Fprintf(&b, "Setext %d\n========\n\n## Closed %d ##\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- one\n- two\n\n```go\ncode()\n```\n\n> A quote.\n\n", i, i)
	}
	b.WriteString(strings.Repeat("A paragraph, long enough to fill a part without a heading.\n\n", 200))
	b.WriteString("[end]: https://example.com/end \"The end\"\n")
	text := b.String()
	p := &mdPreview{}
	p.show(text, 1)
	if len(p.parts) < 40 {
		t.Fatalf("%d parts", len(p.parts))
	}
	var got strings.Builder
	for _, pt := range p.parts {
		got.WriteString(parser.Parse(pt.text).HTML())
	}
	if want := parser.Parse(text).HTML(); got.String() != want {
		t.Fatalf("the parts read\n%s\nthe text\n%s", got.String(), want)
	}
}

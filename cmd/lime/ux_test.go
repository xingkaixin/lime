package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestChordCancelsOnEscapeAndNonTextFocus(t *testing.T) {
	a := newApp("")
	d1 := a.add("a.txt", "hello")
	d2 := a.add("b.txt", "world")
	a.compareDocs(d1, d2)
	if a.current().cmp == nil {
		t.Fatal("expected diff tab active")
	}

	tester := ui.NewTester(a.view, 900, 600)
	tester.Frame()

	tester.Key(ui.Cmd, ui.KeyK)
	if !a.chord {
		t.Fatal("expected Cmd+K to enter chord state")
	}
	if _, ok := tester.Find(shortcutText(ui.Cmd, ui.KeyK) + "…"); !ok {
		t.Fatal("expected status bar to display chord indicator")
	}

	tester.Key(0, ui.KeyEscape)
	if a.chord {
		t.Fatal("expected Escape to cancel chord state")
	}

	tester.Key(ui.Cmd, ui.KeyK)
	if !a.chord {
		t.Fatal("expected Cmd+K to enter chord state again")
	}
	tester.Key(0, ui.KeyA)
	if a.chord {
		t.Fatal("expected non-chord key on diff tab to cancel chord state")
	}
	tester.Key(ui.Cmd, ui.KeyK)
	tester.Key(ui.Cmd, ui.KeyW)
	if a.chord || a.docCount() != 3 {
		t.Fatalf("invalid chord closed a tab: %d", a.docCount())
	}

}

func TestChordWhileFocusChanges(t *testing.T) {
	a := newApp("")
	a.add("a.txt", "a")
	a.add("b.txt", "b")
	moveFocus := false
	tt := ui.NewTester(func(c *ui.Context) {
		a.view(c)
		target := ui.Box(c.Key("focus")).Focusable()
		if moveFocus {
			moveFocus = false
			target.Focus()
		}
	}, 900, 600)
	a.showFind(false)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK)
	if !a.chord {
		t.Fatal("chord did not start")
	}
	if a.l.menuKey("close") != "" {
		t.Fatal("native Close shortcut active during chord")
	}
	moveFocus = true
	tt.Key(ui.Cmd, ui.KeyW)
	if a.chord || a.docCount() != 2 {
		t.Fatalf("second key closed a tab: %d", a.docCount())
	}
	tt.Key(ui.Cmd, ui.KeyW)
	if a.docCount() != 1 {
		t.Fatal("next shortcut did not close tab")
	}
}

func TestChordEscapeInFindField(t *testing.T) {
	a := newApp("")
	a.add("doc.txt", "test")
	tt := ui.NewTester(a.view, 900, 600)
	a.showFind(false)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK)
	if !a.chord {
		t.Fatal("missing chord")
	}
	tt.Key(0, ui.KeyEscape)
	if a.chord {
		t.Fatal("find field kept chord after Escape")
	}
}

func TestCloseOthersKeepsPreviewSource(t *testing.T) {
	a := newApp("")
	d := a.add("doc.md", "# Markdown")
	a.add("extra.txt", "")
	a.openPreview(d, false)
	preview := a.current()
	a.closeOtherTabs(a.group(), preview)
	if a.current() != preview || a.docCount() != 2 || a.l.owner(d) != a {
		t.Fatal("lost preview or source")
	}
}

func TestFindNextSharesQueryAcrossTabs(t *testing.T) {
	a := newApp("")
	d1 := a.add("one.txt", "alpha beta alpha")
	d2 := a.add("two.txt", "one alpha two alpha")
	a.show(d1)

	tester := ui.NewTester(a.view, 900, 600)
	tester.Frame()

	a.find.query = "alpha"
	a.show(d2)
	d2.st.SetSelections(editor.Selection{Anchor: 0, Head: 0})
	tester.Frame()

	a.run("find_next")
	s := d2.st.Selection()
	got := d2.st.Buffer.Slice(min(s.Anchor, s.Head), max(s.Anchor, s.Head))
	if got != "alpha" {
		t.Fatalf("find_next across tabs selected %q, want %q", got, "alpha")
	}

	a.run("find_prev")
	s = d2.st.Selection()
	got = d2.st.Buffer.Slice(min(s.Anchor, s.Head), max(s.Anchor, s.Head))
	if got != "alpha" {
		t.Fatalf("find_prev across tabs selected %q, want %q", got, "alpha")
	}
}

func TestFormatJSONLeadingUnicodeWhitespace(t *testing.T) {
	a := newApp("")
	d := a.add("data.json", "\u00a0{\"a\":1}\n")
	a.formatJSON(d.st, false)
	want := "\u00a0{\n  \"a\": 1\n}\n"
	if got := d.st.Buffer.String(); got != want {
		t.Fatalf("formatJSON with NBSP = %q, want %q", got, want)
	}
}

func TestMarkdownPreviewEnabledOnlyOnMarkdown(t *testing.T) {
	a := newApp("")
	txt := a.add("notes.txt", "plain")
	md := a.add("readme.md", "# Title")

	preview := commandByID["open_markdown_preview"]
	side := commandByID["open_markdown_preview_to_the_side"]

	a.show(txt)
	if preview.enabled(a) || side.enabled(a) {
		t.Fatal("expected markdown preview commands disabled for plain text")
	}
	a.show(md)
	if !preview.enabled(a) || !side.enabled(a) {
		t.Fatal("expected markdown preview commands enabled for markdown")
	}
}

func TestCloseOtherTabsRevealAndRevert(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p1 := filepath.Join(sub, "one.go")
	p2 := filepath.Join(dir, "two.go")
	if err := os.WriteFile(p1, []byte("package sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newApp("")
	a.addFolder(dir)
	a.side = noPanel
	a.open([]string{p1, p2})
	d1 := a.docAt(p1)
	a.show(d1)

	a.run("reveal_in_side_bar")
	if a.side != filesPanel {
		t.Fatalf("reveal_in_side_bar side = %v, want filesPanel", a.side)
	}
	if !*a.proj.isOpen(sub, false) || !*a.proj.isOpen(filepath.Join(dir, "pkg"), false) {
		t.Fatal("expected ancestor folders opened by reveal_in_side_bar")
	}

	a.run("close_other_tabs")
	if len(a.group().docs) != 1 || a.current() != d1 {
		t.Fatalf("close_other_tabs left %d docs, want 1 (%s)", len(a.group().docs), d1.path)
	}

	d1.st.Buffer.Set("dirty content\n")
	if !d1.dirty() {
		t.Fatal("expected doc dirty after edit")
	}
	a.revertDoc(d1)
	if d1.dirty() || d1.st.Buffer.String() != "package sub\n" {
		t.Fatalf("revertDoc left dirty=%v text=%q", d1.dirty(), d1.st.Buffer.String())
	}
}

func TestSelectionStatusCounts(t *testing.T) {
	a := newApp("")
	d := a.add("sel.txt", "hello\nworld\nfoo")
	st := d.st

	if got := selectionStatus(st, st.Selections()); got != "" {
		t.Fatalf("empty selection status = %q, want empty", got)
	}

	st.SetSelections(editor.Selection{Anchor: 0, Head: 5})
	if got := selectionStatus(st, st.Selections()); !strings.Contains(got, "5") {
		t.Fatalf("single-line selection status = %q, want 5 chars", got)
	}

	st.SetSelections(editor.Selection{Anchor: 0, Head: 11})
	got := selectionStatus(st, st.Selections())
	if !strings.Contains(got, "2") || !strings.Contains(got, "11") {
		t.Fatalf("multi-line selection status = %q, want 2 lines and 11 chars", got)
	}
}

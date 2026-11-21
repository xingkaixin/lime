package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func editorInlays(st *editor.State) []editor.Inlay {
	v := reflect.ValueOf(st).Elem().FieldByName("ed").FieldByName("view").FieldByName("inlays")
	out := make([]editor.Inlay, v.Len())
	for i := range out {
		in := v.Index(i)
		out[i] = editor.Inlay{
			Offset:   int(in.FieldByName("at").Int()),
			Text:     in.FieldByName("text").String(),
			Trailing: in.FieldByName("kind").Int() == 3,
			Ellipsis: in.FieldByName("ellipsis").String(),
		}
	}
	return out
}

func TestBlame(t *testing.T) {
	dir, git := newRepo(t)
	file := filepath.Join(dir, "a.txt")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("one\ntwo\n")
	git("add", ".")
	git("commit", "-q", "-m", "first")
	write("one\ntwo\nthree\n")
	git("commit", "-q", "-am", "second")
	write("one\ntwo\nthree\nfour\n")

	a := newApp("")
	a.open([]string{file})
	a.reloadChanged()
	tt := ui.NewTester(a.view, 900, 600)
	d := a.current()
	subjects := func() string {
		t.Helper()
		tt.Frame()
		var s []string
		for i := range d.st.Buffer.LineCount() - 1 {
			switch bl := d.blame.lineOf(d, i); {
			case bl == nil:
				s = append(s, "")
			case bl.hash == uncommitted:
				s = append(s, "-")
			default:
				s = append(s, bl.subject)
			}
		}
		return strings.Join(s, ",")
	}
	if got := subjects(); got != "first,first,second,-" {
		t.Fatalf("blame: %q", got)
	}

	end := d.st.Buffer.Snapshot().LineEnd(2)
	d.st.Select(end, end)
	tt.Frame()
	if d.blame.inline || len(editorInlays(d.st)) != 0 {
		t.Fatal("the blame shows before the caret rests")
	}
	tt.Advance(blameDelay)
	tt.Frame()
	inlays := editorInlays(d.st)
	if !d.blame.inline || len(inlays) != 1 || inlays[0].Offset != end || !inlays[0].Trailing || inlays[0].Ellipsis != "…" || !strings.Contains(inlays[0].Text, "second") {
		t.Fatalf("inline blame inlay = %+v (inline=%v)", inlays, d.blame.inline)
	}
	tt.Key(0, ui.KeyLeft)
	if d.blame.inline || len(editorInlays(d.st)) != 0 {
		t.Fatalf("moving caret kept inline blame: %+v", editorInlays(d.st))
	}
	d.st.Select(end, end)
	tt.Frame()
	tt.Advance(blameDelay)
	tt.Frame()
	if !d.blame.inline || len(editorInlays(d.st)) != 1 {
		t.Fatal("no blame shows after caret rests again")
	}
	beforeTab := d.st.Buffer.String()
	tt.Key(0, ui.KeyTab)
	if text := d.st.Buffer.String(); text == beforeTab || strings.Contains(text, "second") || d.blame.inline || len(editorInlays(d.st)) != 0 {
		t.Fatalf("Tab: text=%q inline=%v inlays=%+v", text, d.blame.inline, editorInlays(d.st))
	}

	d.st.Buffer.Edit(editor.Edit{Start: 0, End: 0, Text: "zero\n"})
	if got := subjects(); got != ",first,first,,-" {
		t.Fatalf("blame after edits: %q", got)
	}
	a.save(d)
	if got := subjects(); got != "-,first,first,-,-" {
		t.Fatalf("blame after saving: %q", got)
	}

	a.showPanel(noPanel)
	a.run("toggle_blame")
	tt.Frame()
	tt.ClickAt(10, tabBarHeight+float32(d.st.LineTop(1)-d.st.ScrollY())+4)
	tt.Frame()
	if cur := a.current(); cur.commit == nil || cur.commit.subject != "first" || cur.commit.root != d.blame.root {
		t.Fatalf("the column opened %q", cur.name())
	}
}

func TestInlineBlameClearsOnEditBlurAndToggle(t *testing.T) {
	dir, git := newRepo(t)
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "initial commit")

	a := newApp("")
	a.open([]string{file})
	a.reloadChanged()
	tt := ui.NewTester(a.view, 900, 600)
	d := a.current()

	showBlame := func() {
		t.Helper()
		tt.Frame()
		tt.Advance(blameDelay)
		tt.Frame()
		if !d.blame.inline || len(editorInlays(d.st)) != 1 {
			t.Fatalf("expected inline blame inlay, inline=%v inlays=%+v", d.blame.inline, editorInlays(d.st))
		}
	}

	showBlame()

	d.st.Buffer.Edit(editor.Edit{Start: 0, End: 0, Text: "x"})
	tt.Frame()
	if d.blame.inline || len(editorInlays(d.st)) != 0 {
		t.Fatalf("edit kept inline blame: inline=%v inlays=%+v", d.blame.inline, editorInlays(d.st))
	}

	end1 := d.st.Buffer.Snapshot().LineEnd(1)
	d.st.Select(end1, end1)
	showBlame()
	a.run("show_panel find")
	tt.Frame()
	if d.blame.inline || len(editorInlays(d.st)) != 0 {
		t.Fatalf("opening find bar kept inline blame: inline=%v inlays=%+v", d.blame.inline, editorInlays(d.st))
	}
	tt.Key(0, ui.KeyEscape)
	showBlame()

	a.run("show_command_palette")
	tt.Frame()
	if d.blame.inline || len(editorInlays(d.st)) != 0 {
		t.Fatalf("opening command palette kept inline blame: inline=%v inlays=%+v", d.blame.inline, editorInlays(d.st))
	}
	tt.Key(0, ui.KeyEscape)
	a.focusGroup(0)
	showBlame()

	a.run("toggle_inline_blame")
	tt.Frame()
	if d.blame.inline || len(editorInlays(d.st)) != 0 {
		t.Fatalf("toggle off kept inline blame: inline=%v inlays=%+v", d.blame.inline, editorInlays(d.st))
	}
	a.run("toggle_inline_blame")
	showBlame()
}

func TestBlameFolds(t *testing.T) {
	dir, git := newRepo(t)
	file := filepath.Join(dir, "a.txt")
	commit := func(text, subject string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "-q", "-m", subject)
	}
	commit("a\n\tb\n\tc\nd\n", "one")
	commit("a\n\tB\n\tC\nd\n", "two")
	commit("a\n\tB\n\tC\nD\n", "three")

	a := newApp("")
	a.open([]string{file})
	a.reloadChanged()
	tt := ui.NewTester(a.view, 900, 600)
	d := a.current()
	d.st.Fold(0)
	tt.Frame()
	tt.Frame()
	st := d.st
	var got []string
	d.blame.eachBlock(d, func(top float64, bl *blamed) {
		got = append(got, bl.subject)
		if at := d.blame.lineOf(d, lineAt(st, top-st.ScrollY()+1)); at != bl {
			t.Errorf("%q shows where a click opens %v", bl.subject, at)
		}
	})
	if strings.Join(got, ",") != "one,three" {
		t.Fatalf("blocks: %q, folded %v", got, st.Folded())
	}
}

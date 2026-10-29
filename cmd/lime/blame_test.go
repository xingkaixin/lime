package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Blame tells the commit of each line of the file as saved: the lines
// edited since show none, and the others keep theirs where they moved.
// The caret's line shows its blame once the caret rests there, which Tab
// does not insert, and a click on the column opens the commit.
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
	if d.blame.inline {
		t.Fatal("the blame shows before the caret rests")
	}
	tt.Advance(blameDelay)
	tt.Frame()
	if !d.blame.inline {
		t.Fatal("no blame shows where the caret rests")
	}
	tt.Key(0, ui.KeyTab)
	if text := d.st.Buffer.String(); strings.Contains(text, "second") || d.blame.inline {
		t.Fatalf("Tab: %q", text)
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

// The column labels a fold's lines by the line after them, which a click
// there opens, not by the first line the fold hides.
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

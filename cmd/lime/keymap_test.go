package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Sublime Text's keys run one command each, and read back as
// settings.json writes them.
func TestDefaultKeys(t *testing.T) {
	km := newKeymap(nil)
	for s, users := range km.users {
		if len(users) > 1 {
			t.Errorf("%s runs %q", s, users)
		}
	}
	for _, keys := range defaultKeys {
		for _, k := range keys {
			if got, ok := parseKey(k.spec()); !ok || got != k.seq() {
				t.Errorf("%q reads as %v, want %v", k.spec(), got, k.seq())
			}
		}
	}
}

// A key given to a second command runs the command changed last, and a
// key taken away does nothing, though the editor binds it by default.
func TestChangedKeys(t *testing.T) {
	if !mac {
		t.Skip("the test presses macOS keys")
	}
	a := newApp("")
	a.add("", "alpha\n")
	tt := ui.NewTester(a.view, 800, 500)
	st := a.current().st

	cmdD := appKey{mods: ui.Cmd, key: ui.KeyD, id: "duplicate_line"}
	a.l.setKeys("duplicate_line", []appKey{cmdD})
	if km := a.l.keys; !km.conflicted(cmdD) || km.text("find_under_expand") != "" || a.l.menuKey("find_under_expand") != "" {
		t.Fatalf("Quick Add Next keeps ⌘D: %q", km.text("find_under_expand"))
	}
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	tt.Key(ui.Cmd, ui.KeyD)
	if got := st.Buffer.String(); got != "alpha\nalpha\n" {
		t.Fatalf("⌘D, then ⇧⌘D taken away: %q", got)
	}

	a.l.set.Keys = append(a.l.set.Keys, keyChange{Command: "find_under_expand", Keys: []string{"super+d"}})
	a.l.keys = newKeymap(a.l.set.Keys)
	if got := a.l.keys.text("find_under_expand"); got != "⌘D" {
		t.Fatalf("Quick Add Next changed last: %q, want ⌘D", got)
	}

	a.l.resetKeys()
	if len(a.l.set.Keys) != 0 || a.l.keys.conflicted(cmdD) || a.l.keys.text("duplicate_line") != "⇧⌘D" {
		t.Fatalf("restoring the defaults: %v", a.l.set.Keys)
	}
}

// A key pressed while recording runs nothing, and Replace takes it from
// the command that had it.
func TestRecordKey(t *testing.T) {
	if !mac {
		t.Skip("the test presses macOS keys")
	}
	a := newApp("")
	a.add("", "alpha\n")
	tt := ui.NewTester(a.view, 900, 600)
	st := a.current().st
	a.showSettings()
	a.prefs.page, a.prefs.query = pageShortcuts, "Duplicate Line"
	tt.Frame()
	r, _ := tt.Find("Duplicate Line")
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	if err := tt.Click("Add Shortcut"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyD)
	if !tt.HasText("Replace removes ⌘D from “Quick Add Next”") {
		t.Fatalf("⌘D shows no conflict: %q", tt.Texts())
	}
	if err := tt.Click("Replace"); err != nil {
		t.Fatal(err)
	}
	km := a.l.keys
	if got := km.keys["duplicate_line"]; len(got) != 2 || got[1].String() != "⌘D" || len(km.keys["find_under_expand"]) != 0 {
		t.Fatalf("Duplicate Line has %v, Quick Add Next %v", got, km.keys["find_under_expand"])
	}
	if a.prefs.rec.id != "" || st.Buffer.String() != "alpha\n" {
		t.Fatalf("still recording %q, or ⌘D ran: %q", a.prefs.rec.id, st.Buffer.String())
	}
}

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

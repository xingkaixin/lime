package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

type fakeTermSession struct {
	opts       termSessionOpts
	cleared    int
	closed     bool
	searched   string
	nextCount  int
	prevCount  int
	endCount   int
	syncCount  int
	lastScheme *scheme
}

func (f *fakeTermSession) view(c *ui.Context) ui.Element {
	return ui.Box(c).Focusable().Label("FakeTerminal")
}

func (f *fakeTermSession) syncAppearance(s *scheme, _ *settings) {
	f.syncCount++
	f.lastScheme = s
}

func (f *fakeTermSession) dir() string         { return f.opts.dir }
func (f *fakeTermSession) clearScreen() bool   { f.cleared++; return true }
func (f *fakeTermSession) search(q string)     { f.searched = q }
func (f *fakeTermSession) searchNext()         { f.nextCount++ }
func (f *fakeTermSession) searchPrev()         { f.prevCount++ }
func (f *fakeTermSession) endSearch()          { f.endCount++ }
func (f *fakeTermSession) close() error        { f.closed = true; return nil }

func installFakeTerminals(a *app) *[]*fakeTermSession {
	var created []*fakeTermSession
	a.term.newSession = func(o termSessionOpts) (termSession, error) {
		f := &fakeTermSession{opts: o}
		created = append(created, f)
		return f, nil
	}
	return &created
}

func TestTerminalDockShortcutsAndFocus(t *testing.T) {
	a := newApp("")
	a.add("", "hello\n")
	created := installFakeTerminals(a)
	tt := ui.NewTester(a.view, 900, 600)
	tt.Frame()

	// Ctrl+` opens the terminal dock and focuses the terminal.
	tt.Key(ui.Ctrl, ui.KeyBackquote)
	tt.Frame()
	if !a.term.open || len(*created) != 1 || a.term.active != 0 {
		t.Fatalf("after Ctrl+`: open=%v tabs=%d active=%d", a.term.open, len(*created), a.term.active)
	}
	if !a.termFocused {
		t.Fatalf("expected terminal to be focused after Ctrl+`")
	}

	// Ctrl+Shift+` creates a second terminal tab.
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyBackquote)
	tt.Frame()
	if len(a.term.tabs) != 2 || a.term.active != 1 {
		t.Fatalf("after Ctrl+Shift+`: tabs=%d active=%d", len(a.term.tabs), a.term.active)
	}

	// Shift+Escape toggles terminal zoom.
	tt.Key(ui.Shift, ui.KeyEscape)
	tt.Frame()
	if !a.term.zoomed {
		t.Fatalf("expected Shift+Escape to zoom terminal")
	}
	tt.Key(ui.Shift, ui.KeyEscape)
	tt.Frame()
	if a.term.zoomed {
		t.Fatalf("expected second Shift+Escape to restore terminal")
	}

	// Clicking back into the editor leaves the dock open, and Ctrl+` refocuses it instead of closing it.
	a.focusGroup(0)
	tt.Frame()
	if a.termPanelFocused {
		t.Fatalf("expected editor focus after focusGroup(0)")
	}
	tt.Key(ui.Ctrl, ui.KeyBackquote)
	tt.Frame()
	if !a.term.open || !a.termFocused {
		t.Fatalf("Ctrl+` from editor should focus open terminal: open=%v focused=%v", a.term.open, a.termFocused)
	}

	// Pressing Ctrl+` while terminal is focused hides the dock.
	tt.Key(ui.Ctrl, ui.KeyBackquote)
	tt.Frame()
	if a.term.open {
		t.Fatalf("Ctrl+` while terminal focused should hide dock")
	}

	// Cmd+J / Ctrl+J (toggle_bottom_dock) opens and hides the dock regardless of focus.
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Frame()
	if !a.term.open {
		t.Fatalf("toggle_bottom_dock should open dock")
	}
	a.focusGroup(0)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Frame()
	if a.term.open {
		t.Fatalf("toggle_bottom_dock should hide dock even when editor is focused")
	}

	// Closing remaining tabs closes the dock and closes underlying sessions.
	a.showTerminal()
	tt.Frame()
	a.closeTerminal()
	a.closeTerminal()
	tt.Frame()
	if a.term.open || len(a.term.tabs) != 0 {
		t.Fatalf("closing all terminal tabs should close dock: open=%v tabs=%d", a.term.open, len(a.term.tabs))
	}
	for i, f := range *created {
		if !f.closed {
			t.Errorf("session %d was not closed", i)
		}
	}
}

func TestTerminalShortcutCustomization(t *testing.T) {
	// Every terminal command must be listed in shortcutGroups for user customization.
	wantIDs := []string{
		"toggle_terminal", "toggle_bottom_dock", "new_terminal",
		"close_terminal", "clear_terminal", "toggle_terminal_zoom",
		"next_terminal", "prev_terminal", "find_in_terminal",
	}
	listed := make(map[string]bool)
	for _, g := range shortcutGroups {
		for _, s := range g.items {
			listed[s.id] = true
		}
	}
	for _, id := range wantIDs {
		if !listed[id] {
			t.Errorf("command %q missing from shortcutGroups", id)
		}
	}

	if msg := refusal(appKey{mods: ui.Shift, key: ui.KeyEscape}); msg != "" {
		t.Errorf("Shift+Escape should be recordable, got refusal %q", msg)
	}

	a := newApp("")
	a.add("", "hello\n")
	created := installFakeTerminals(a)
	tt := ui.NewTester(a.view, 900, 600)

	// Rebind toggle_terminal to Alt+T and clear_terminal to Alt+K.
	a.l.setKeys("toggle_terminal", []appKey{{mods: ui.Alt, key: ui.KeyT, id: "toggle_terminal"}})
	a.l.setKeys("clear_terminal", []appKey{{mods: ui.Alt, key: ui.KeyK, id: "clear_terminal"}})
	tt.Frame()

	// Default Ctrl+` should no longer open the terminal.
	tt.Key(ui.Ctrl, ui.KeyBackquote)
	tt.Frame()
	if a.term.open {
		t.Fatalf("old default Ctrl+` should not open terminal after rebinding")
	}

	// Custom Alt+T opens and focuses the terminal.
	tt.Key(ui.Alt, ui.KeyT)
	tt.Frame()
	if !a.term.open || !a.termFocused {
		t.Fatalf("custom Alt+T should open and focus terminal: open=%v focused=%v", a.term.open, a.termFocused)
	}

	// Custom Alt+K runs clear_terminal while terminal has focus.
	tt.Key(ui.Alt, ui.KeyK)
	tt.Frame()
	if (*created)[0].cleared != 1 {
		t.Fatalf("custom Alt+K cleared=%d, want 1", (*created)[0].cleared)
	}

	// Custom Alt+T hides the terminal again.
	tt.Key(ui.Alt, ui.KeyT)
	tt.Frame()
	if a.term.open {
		t.Fatalf("custom Alt+T should hide terminal")
	}
}

func TestTerminalShellKeyIsolation(t *testing.T) {
	// Terminal commands are always allowed regardless of modifier.
	if !terminalAllowsShortcut("toggle_terminal", ui.Ctrl, ui.KeyBackquote) {
		t.Error("toggle_terminal should be allowed when terminal focused")
	}
	if !terminalAllowsShortcut("clear_terminal", ui.Ctrl, ui.KeyL) {
		t.Error("custom clear_terminal binding should be allowed when terminal focused")
	}
	// Bare Ctrl+G (goto_line) must reach the shell on every OS.
	if terminalAllowsShortcut("goto_line", ui.Ctrl, ui.KeyG) {
		t.Error("Ctrl+G should pass through to the shell when terminal focused")
	}
	// Bare Ctrl+D (find_under_expand) must reach the shell on every OS.
	if terminalAllowsShortcut("find_under_expand", ui.Ctrl, ui.KeyD) {
		t.Error("Ctrl+D should pass through to the shell when terminal focused")
	}
	// Platform-specific checks.
	if mac {
		if !terminalAllowsShortcut("show_command_palette", ui.Super|ui.Shift, ui.KeyP) {
			t.Error("Cmd+Shift+P should work on macOS when terminal focused")
		}
	} else {
		if terminalAllowsShortcut("close", ui.Ctrl, ui.KeyW) {
			t.Error("Ctrl+W should pass through to the shell on non-macOS")
		}
		if !terminalAllowsShortcut("show_command_palette", ui.Ctrl|ui.Shift, ui.KeyP) {
			t.Error("Ctrl+Shift+P should work on non-macOS when terminal focused")
		}
	}
}

func TestTerminalLinkResolution(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, line, col, ok := resolveTerminalLink("main.go:3:6", dir)
	if !ok || got != file || line != 3 || col != 6 {
		t.Fatalf("resolveTerminalLink(main.go:3:6) = (%q, %d, %d, %v), want (%q, 3, 6, true)", got, line, col, ok, file)
	}
	if _, _, _, ok := resolveTerminalLink("missing.go:1", dir); ok {
		t.Fatal("missing file should not resolve")
	}
	if _, _, _, ok := resolveTerminalLink(dir, dir); ok {
		t.Fatal("directory should not resolve as file link")
	}

	a := newApp("")
	tab := &termTab{initDir: dir}
	if a.openTerminalLink(tab, "https://example.com") {
		t.Fatal("HTTP URL should not be hijacked as local file")
	}
	if !a.openTerminalLink(tab, "main.go:3") {
		t.Fatal("existing local file link should open in editor")
	}
	if cur := a.current(); cur == nil || cur.path != file {
		t.Fatalf("expected %q to open, got %v", file, cur)
	}
}

func TestTerminalRealSession(t *testing.T) {
	if !terminalSupported {
		t.Skip("terminal unsupported on this target")
	}
	a := newApp("")
	tab := a.openTerminalInDir(t.TempDir())
	if tab == nil || tab.sess == nil {
		t.Fatalf("failed to start real terminal session: notice=%q", a.notice.text)
	}
	defer a.closeAllTerminals()

	tt := ui.NewTester(a.view, 900, 600)
	tt.Frame()
	if !a.term.open {
		t.Fatal("expected terminal dock to be open")
	}
	a.clearTerminal()
	a.closeAllTerminals()
	if a.term.open || len(a.term.tabs) != 0 {
		t.Fatalf("expected closeAllTerminals to clean up dock")
	}
}

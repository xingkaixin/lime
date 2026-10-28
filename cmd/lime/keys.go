package main

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

var mac = runtime.GOOS == "darwin"

// editorKeys are the editor's keys, as Sublime Text's keymap of this
// platform has them; every other key the editor would bind by default
// is taken away from it.
var editorKeys = sublimeOnly(func() []editor.Binding {
	if mac {
		return macEditorKeys()
	}
	return pcEditorKeys(runtime.GOOS == "windows")
}())

func bind(mods ui.Modifiers, key ui.Key, cmd editor.Command) editor.Binding {
	return editor.Binding{Mods: mods, Key: key, Command: cmd}
}

func macEditorKeys() []editor.Binding {
	cmd, alt, ctrl, sh := ui.Super, ui.Alt, ui.Ctrl, ui.Shift
	return []editor.Binding{
		// The text keys of macOS, which Sublime Text leaves to the system.
		bind(0, ui.KeyLeft, editor.MoveLeft), bind(0, ui.KeyRight, editor.MoveRight),
		bind(0, ui.KeyUp, editor.MoveUp), bind(0, ui.KeyDown, editor.MoveDown),
		bind(sh, ui.KeyLeft, editor.SelectLeft), bind(sh, ui.KeyRight, editor.SelectRight),
		bind(sh, ui.KeyUp, editor.SelectUp), bind(sh, ui.KeyDown, editor.SelectDown),
		bind(ctrl, ui.KeyB, editor.MoveLeft), bind(ctrl, ui.KeyF, editor.MoveRight),
		bind(ctrl, ui.KeyP, editor.MoveUp), bind(ctrl, ui.KeyN, editor.MoveDown),
		bind(alt, ui.KeyLeft, editor.MoveWordLeft), bind(alt, ui.KeyRight, editor.MoveWordRight),
		bind(alt|sh, ui.KeyLeft, editor.SelectWordLeft), bind(alt|sh, ui.KeyRight, editor.SelectWordRight),
		bind(alt, ui.KeyUp, editor.MoveParagraphUp), bind(alt, ui.KeyDown, editor.MoveParagraphDown),
		bind(alt|sh, ui.KeyUp, editor.SelectParagraphUp), bind(alt|sh, ui.KeyDown, editor.SelectParagraphDown),
		bind(cmd, ui.KeyLeft, editor.MoveLineStart), bind(cmd, ui.KeyRight, editor.MoveLineEnd),
		bind(cmd|sh, ui.KeyLeft, editor.SelectLineStart), bind(cmd|sh, ui.KeyRight, editor.SelectLineEnd),
		bind(ctrl, ui.KeyA, editor.MoveLineStart), bind(ctrl, ui.KeyE, editor.MoveLineEnd),
		bind(cmd, ui.KeyUp, editor.MoveDocStart), bind(cmd, ui.KeyDown, editor.MoveDocEnd),
		bind(cmd|sh, ui.KeyUp, editor.SelectDocStart), bind(cmd|sh, ui.KeyDown, editor.SelectDocEnd),
		bind(0, ui.KeyHome, editor.MoveDocStart), bind(0, ui.KeyEnd, editor.MoveDocEnd),
		bind(sh, ui.KeyHome, editor.SelectDocStart), bind(sh, ui.KeyEnd, editor.SelectDocEnd),
		bind(0, ui.KeyPageUp, editor.MovePageUp), bind(0, ui.KeyPageDown, editor.MovePageDown),
		bind(sh, ui.KeyPageUp, editor.SelectPageUp), bind(sh, ui.KeyPageDown, editor.SelectPageDown),
		bind(0, ui.KeyBackspace, editor.Backspace), bind(sh, ui.KeyBackspace, editor.Backspace),
		bind(ctrl, ui.KeyH, editor.Backspace), bind(0, ui.KeyDelete, editor.Delete), bind(ctrl, ui.KeyD, editor.Delete),
		bind(alt, ui.KeyBackspace, editor.DeleteWordLeft), bind(alt, ui.KeyDelete, editor.DeleteWordRight),
		bind(0, ui.KeyEnter, editor.Newline), bind(sh, ui.KeyEnter, editor.Newline),
		bind(0, ui.KeyTab, editor.Tab), bind(sh, ui.KeyTab, editor.Backtab), bind(0, ui.KeyEscape, editor.Cancel),

		// Sublime Text's own.
		bind(ctrl, ui.KeyLeft, editor.MoveSubwordLeft), bind(ctrl, ui.KeyRight, editor.MoveSubwordRight),
		bind(ctrl|sh, ui.KeyLeft, editor.SelectSubwordLeft), bind(ctrl|sh, ui.KeyRight, editor.SelectSubwordRight),
		bind(ctrl|alt, ui.KeyLeft, editor.MoveSubwordLeft), bind(ctrl|alt, ui.KeyRight, editor.MoveSubwordRight),
		bind(ctrl|alt|sh, ui.KeyLeft, editor.SelectSubwordLeft), bind(ctrl|alt|sh, ui.KeyRight, editor.SelectSubwordRight),
		bind(ctrl|alt, ui.KeyUp, editor.ScrollLineUp), bind(ctrl|alt, ui.KeyDown, editor.ScrollLineDown),
		bind(ctrl|sh, ui.KeyUp, editor.AddCursorAbove), bind(ctrl|sh, ui.KeyDown, editor.AddCursorBelow),
		bind(cmd, ui.KeyZ, editor.Undo), bind(cmd|sh, ui.KeyZ, editor.Redo), bind(cmd, ui.KeyY, editor.Redo),
		bind(cmd, ui.KeyU, editor.UndoSelection), bind(cmd|sh, ui.KeyU, editor.RedoSelection),
		bind(cmd, ui.KeyX, editor.Cut), bind(cmd, ui.KeyC, editor.Copy),
		bind(cmd, ui.KeyV, editor.Paste), bind(cmd|sh, ui.KeyV, editor.Paste),
		bind(cmd, ui.KeyA, editor.SelectAll), bind(cmd|sh, ui.KeyL, editor.SplitSelectionIntoLines),
		bind(cmd, ui.KeyL, editor.SelectLine),
		bind(cmd|sh, ui.KeySpace, editor.SelectLarger), bind(cmd|sh, ui.KeyA, editor.SelectLarger),
		bind(ctrl|sh, ui.KeyM, editor.SelectLarger), bind(ctrl, ui.KeyM, editor.MoveToBracket),
		bind(cmd, ui.KeyBracketRight, editor.Indent), bind(cmd, ui.KeyBracketLeft, editor.Outdent),
		bind(cmd, ui.KeyEnter, editor.NewlineBelow), bind(cmd|sh, ui.KeyEnter, editor.NewlineAbove),
		bind(0, ui.KeyF12, editor.GoToDefinition), bind(cmd|alt, ui.KeyDown, editor.GoToDefinition),
		bind(cmd, ui.KeyG, editor.FindNext), bind(cmd|sh, ui.KeyG, editor.FindPrevious),
		bind(ctrl, ui.KeyPeriod, editor.NextHunk), bind(ctrl, ui.KeyComma, editor.PreviousHunk),
		bind(ctrl|cmd, ui.KeyUp, editor.MoveLinesUp), bind(ctrl|cmd, ui.KeyDown, editor.MoveLinesDown),
		bind(ctrl, ui.KeyBackspace, editor.DeleteSubwordLeft), bind(ctrl, ui.KeyDelete, editor.DeleteSubwordRight),
		bind(cmd, ui.KeyBackspace, editor.DeleteToLineStart), bind(cmd, ui.KeyDelete, editor.DeleteToLineEnd),
		bind(ctrl, ui.KeyK, editor.DeleteToLineEnd), bind(ctrl|sh, ui.KeyK, editor.DeleteLine),
		bind(cmd, ui.KeySlash, editor.ToggleComment), bind(cmd|alt, ui.KeySlash, editor.ToggleBlockComment),
		bind(cmd|sh, ui.KeyJ, editor.JoinLines), bind(cmd|sh, ui.KeyD, editor.DuplicateSelection),
		bind(ctrl, ui.KeyT, editor.Transpose), bind(ctrl, ui.KeyL, editor.ScrollCursorCenter),
		bind(0, ui.KeyF5, editor.SortLinesCaseInsensitive), bind(ctrl, ui.KeyF5, editor.SortLines),
		bind(cmd|alt, ui.KeyBracketLeft, editor.Fold), bind(cmd|alt, ui.KeyBracketRight, editor.Unfold),
	}
}

func pcEditorKeys(windows bool) []editor.Binding {
	alt, ctrl, sh := ui.Alt, ui.Ctrl, ui.Shift
	// Linux leaves Ctrl+Alt+arrows to the desktop's workspaces.
	addCursor := alt | sh
	if windows {
		addCursor = ctrl | alt
	}
	return []editor.Binding{
		bind(0, ui.KeyLeft, editor.MoveLeft), bind(0, ui.KeyRight, editor.MoveRight),
		bind(0, ui.KeyUp, editor.MoveUp), bind(0, ui.KeyDown, editor.MoveDown),
		bind(sh, ui.KeyLeft, editor.SelectLeft), bind(sh, ui.KeyRight, editor.SelectRight),
		bind(sh, ui.KeyUp, editor.SelectUp), bind(sh, ui.KeyDown, editor.SelectDown),
		bind(ctrl, ui.KeyLeft, editor.MoveWordLeft), bind(ctrl, ui.KeyRight, editor.MoveWordRight),
		bind(ctrl|sh, ui.KeyLeft, editor.SelectWordLeft), bind(ctrl|sh, ui.KeyRight, editor.SelectWordRight),
		bind(alt, ui.KeyLeft, editor.MoveSubwordLeft), bind(alt, ui.KeyRight, editor.MoveSubwordRight),
		bind(alt|sh, ui.KeyLeft, editor.SelectSubwordLeft), bind(alt|sh, ui.KeyRight, editor.SelectSubwordRight),
		bind(addCursor, ui.KeyUp, editor.AddCursorAbove), bind(addCursor, ui.KeyDown, editor.AddCursorBelow),
		bind(0, ui.KeyPageUp, editor.MovePageUp), bind(0, ui.KeyPageDown, editor.MovePageDown),
		bind(sh, ui.KeyPageUp, editor.SelectPageUp), bind(sh, ui.KeyPageDown, editor.SelectPageDown),
		bind(0, ui.KeyHome, editor.MoveLineStart), bind(0, ui.KeyEnd, editor.MoveLineEnd),
		bind(sh, ui.KeyHome, editor.SelectLineStart), bind(sh, ui.KeyEnd, editor.SelectLineEnd),
		bind(ctrl, ui.KeyHome, editor.MoveDocStart), bind(ctrl, ui.KeyEnd, editor.MoveDocEnd),
		bind(ctrl|sh, ui.KeyHome, editor.SelectDocStart), bind(ctrl|sh, ui.KeyEnd, editor.SelectDocEnd),
		bind(ctrl, ui.KeyUp, editor.ScrollLineUp), bind(ctrl, ui.KeyDown, editor.ScrollLineDown),
		bind(0, ui.KeyBackspace, editor.Backspace), bind(sh, ui.KeyBackspace, editor.Backspace),
		bind(0, ui.KeyDelete, editor.Delete),
		bind(0, ui.KeyEnter, editor.Newline), bind(sh, ui.KeyEnter, editor.Newline),
		bind(ctrl, ui.KeyEnter, editor.NewlineBelow), bind(ctrl|sh, ui.KeyEnter, editor.NewlineAbove),
		bind(ctrl, ui.KeyZ, editor.Undo), bind(ctrl|sh, ui.KeyZ, editor.Redo), bind(ctrl, ui.KeyY, editor.Redo),
		bind(ctrl, ui.KeyU, editor.UndoSelection), bind(ctrl|sh, ui.KeyU, editor.RedoSelection),
		bind(sh, ui.KeyDelete, editor.Cut), bind(ctrl, ui.KeyInsert, editor.Copy), bind(sh, ui.KeyInsert, editor.Paste),
		bind(ctrl, ui.KeyX, editor.Cut), bind(ctrl, ui.KeyC, editor.Copy),
		bind(ctrl, ui.KeyV, editor.Paste), bind(ctrl|sh, ui.KeyV, editor.Paste),
		bind(ctrl, ui.KeyA, editor.SelectAll), bind(ctrl|sh, ui.KeyL, editor.SplitSelectionIntoLines),
		bind(0, ui.KeyEscape, editor.Cancel), bind(0, ui.KeyTab, editor.Tab), bind(sh, ui.KeyTab, editor.Backtab),
		bind(ctrl, ui.KeyBracketRight, editor.Indent), bind(ctrl, ui.KeyBracketLeft, editor.Outdent),
		bind(ctrl, ui.KeyL, editor.SelectLine),
		bind(ctrl|sh, ui.KeySpace, editor.SelectLarger), bind(ctrl|sh, ui.KeyA, editor.SelectLarger),
		bind(ctrl|sh, ui.KeyM, editor.SelectLarger), bind(ctrl, ui.KeyM, editor.MoveToBracket),
		bind(0, ui.KeyF12, editor.GoToDefinition),
		bind(0, ui.KeyF3, editor.FindNext), bind(sh, ui.KeyF3, editor.FindPrevious),
		bind(ctrl, ui.KeyPeriod, editor.NextHunk), bind(ctrl, ui.KeyComma, editor.PreviousHunk),
		bind(ctrl|sh, ui.KeyUp, editor.MoveLinesUp), bind(ctrl|sh, ui.KeyDown, editor.MoveLinesDown),
		bind(ctrl, ui.KeyBackspace, editor.DeleteWordLeft), bind(ctrl, ui.KeyDelete, editor.DeleteWordRight),
		bind(ctrl|sh, ui.KeyBackspace, editor.DeleteToLineStart), bind(ctrl|sh, ui.KeyDelete, editor.DeleteToLineEnd),
		bind(ctrl|sh, ui.KeyK, editor.DeleteLine),
		bind(ctrl, ui.KeySlash, editor.ToggleComment), bind(ctrl|sh, ui.KeySlash, editor.ToggleBlockComment),
		bind(ctrl|sh, ui.KeyJ, editor.JoinLines), bind(ctrl|sh, ui.KeyD, editor.DuplicateSelection),
		bind(ctrl, ui.KeyT, editor.Transpose),
		bind(0, ui.KeyF9, editor.SortLinesCaseInsensitive), bind(ctrl, ui.KeyF9, editor.SortLines),
		bind(ctrl|sh, ui.KeyBracketLeft, editor.Fold), bind(ctrl|sh, ui.KeyBracketRight, editor.Unfold),
	}
}

// sublimeOnly adds to keys a binding to NoCommand for each key the editor
// binds by default and keys leave out. The chords are left: their first
// key, Cmd+K, goes to the window.
func sublimeOnly(keys []editor.Binding) []editor.Binding {
	type key struct {
		mods ui.Modifiers
		key  ui.Key
	}
	have := make(map[key]bool)
	for _, b := range keys {
		have[key{b.Mods, b.Key}] = true
	}
	for _, b := range editor.DefaultBindings() {
		if k := (key{b.Mods, b.Key}); b.ThenKey == 0 && !have[k] {
			have[k] = true
			keys = append(keys, bind(b.Mods, b.Key, editor.NoCommand))
		}
	}
	return keys
}

// appKey is a key of the command named id: mods+key, or the chord Cmd+K
// then mods+key when chord is set.
type appKey struct {
	mods  ui.Modifiers
	key   ui.Key
	chord bool
	id    string
}

// appKeys are the window's keys as Sublime Text has them.
var appKeys = func() []appKey {
	cmd, alt, ctrl, sh := ui.Cmd, ui.Alt, ui.Ctrl, ui.Shift
	k := func(mods ui.Modifiers, key ui.Key, id string) appKey { return appKey{mods, key, false, id} }
	ch := func(mods ui.Modifiers, key ui.Key, id string) appKey { return appKey{cmd | mods, key, true, id} }
	keys := []appKey{
		k(cmd, ui.KeyN, "new_file"), k(cmd|sh, ui.KeyN, "new_window"), k(cmd, ui.KeyO, "prompt_open"), k(cmd|sh, ui.KeyT, "reopen_last_file"),
		k(cmd, ui.KeyS, "save"), k(cmd|sh, ui.KeyS, "prompt_save_as"),
		k(cmd, ui.KeyW, "close"), k(cmd|sh, ui.KeyW, "close_window"),
		k(cmd, ui.KeyP, "show_goto"), k(cmd|sh, ui.KeyP, "show_command_palette"), k(ctrl, ui.KeyG, "goto_line"),
		k(ctrl, ui.KeyPageUp, "prev_view"), k(ctrl, ui.KeyPageDown, "next_view"),
		k(ctrl, ui.KeyTab, "next_view_in_stack"), k(ctrl|sh, ui.KeyTab, "prev_view_in_stack"),
		k(cmd, ui.KeyEqual, "increase_font_size"), k(cmd|sh, ui.KeyEqual, "increase_font_size"),
		k(cmd, ui.KeyMinus, "decrease_font_size"),
		k(cmd, ui.KeyF, "show_panel find"), k(cmd, ui.KeyI, "show_panel find"), k(cmd|sh, ui.KeyI, "show_panel find"),
		k(cmd, ui.KeyE, "slurp_find_string"), k(cmd, ui.KeyD, "find_under_expand"),
		k(cmd, ui.KeyComma, "show_settings"),
		k(cmd, ui.KeyB, "toggle_side_bar"),
		// Zed's keys, which Sublime Text has no panels for.
		k(cmd|sh, ui.KeyE, "toggle_files_panel"), k(ctrl|sh, ui.KeyG, "toggle_git_panel"),

		ch(0, ui.KeyB, "toggle_side_bar"),
		ch(0, ui.KeyU, "upper_case"), ch(0, ui.KeyL, "lower_case"), ch(0, ui.KeyD, "find_under_expand_skip"),
		ch(0, ui.KeyK, "delete_to_eol"), ch(0, ui.KeyBackspace, "delete_to_bol"), ch(0, ui.KeyC, "show_at_center"),
		ch(0, ui.Key0, "unfold_all"), ch(0, ui.KeyJ, "unfold_all"), ch(0, ui.KeyZ, "revert_hunk"),
		ch(0, ui.KeyUp, "new_pane"), ch(0, ui.KeyDown, "close_pane"),
		ch(0, ui.KeyLeft, "focus_prev_group"), ch(0, ui.KeyRight, "focus_next_group"),
		ch(sh, ui.KeyLeft, "move_to_prev_group"), ch(sh, ui.KeyRight, "move_to_next_group"),
	}
	for i := range 9 {
		keys = append(keys, ch(0, ui.Key1+ui.Key(i), foldLevel(i+1)))
	}
	for i := range 4 {
		keys = append(keys, k(ctrl, ui.Key1+ui.Key(i), focusGroup(i)), k(ctrl|sh, ui.Key1+ui.Key(i), moveToGroup(i)))
	}
	tabs, layout := cmd, cmd|alt
	if !mac {
		tabs, layout = alt, alt|sh
	}
	for i := range 8 {
		keys = append(keys, k(tabs, ui.Key1+ui.Key(i), selectTab(i)))
	}
	keys = append(keys, k(tabs, ui.Key9, "select_last_tab"))
	for i, l := range layouts {
		key := ui.Key1 + ui.Key(i)
		if l.rows > 1 && l.cols == 1 {
			key = ui.Key2 + ui.Key(l.rows-2)
			if mac {
				keys = append(keys, k(layout|sh, key, l.id()))
				continue
			}
			key = ui.Key8 + ui.Key(l.rows-2)
		}
		keys = append(keys, k(layout, key, l.id()))
	}
	if mac {
		keys = append(keys, k(cmd|alt, ui.KeyF, "show_panel replace"), k(ctrl|cmd, ui.KeyG, "find_all_under"), k(cmd, ui.KeyT, "show_goto"), k(cmd|alt, ui.KeyS, "save_all"),
			k(cmd|sh, ui.KeyBracketLeft, "prev_view"), k(cmd|sh, ui.KeyBracketRight, "next_view"),
			k(cmd|alt, ui.KeyLeft, "prev_view"), k(cmd|alt, ui.KeyRight, "next_view"),
			k(ctrl|cmd, ui.KeyP, "prompt_select_workspace"))
	} else {
		keys = append(keys, k(ctrl, ui.KeyH, "show_panel replace"), k(alt, ui.KeyF3, "find_all_under"), k(ctrl, ui.KeyF4, "close"),
			k(ctrl|alt, ui.KeyP, "prompt_select_workspace"))
	}
	return keys
}()

// keyNames names keys for menus' accelerators and, on macOS, for showing.
var keyNames = map[ui.Key][2]string{
	ui.KeyLeft: {"Left", "←"}, ui.KeyRight: {"Right", "→"}, ui.KeyUp: {"Up", "↑"}, ui.KeyDown: {"Down", "↓"},
	ui.KeyPageUp: {"PageUp", "⇞"}, ui.KeyPageDown: {"PageDown", "⇟"}, ui.KeyHome: {"Home", "↖"}, ui.KeyEnd: {"End", "↘"},
	ui.KeyBackspace: {"Backspace", "⌫"}, ui.KeyDelete: {"Delete", "⌦"}, ui.KeyEnter: {"Enter", "↩"},
	ui.KeyTab: {"Tab", "⇥"}, ui.KeyEscape: {"Escape", "⎋"}, ui.KeySpace: {"Space", "Space"}, ui.KeyInsert: {"Insert", "Insert"},
	ui.KeyMinus: {"-", "-"}, ui.KeyEqual: {"=", "="}, ui.KeyComma: {",", ","}, ui.KeyPeriod: {".", "."},
	ui.KeySlash: {"/", "/"}, ui.KeySemicolon: {";", ";"}, ui.KeyQuote: {"'", "'"}, ui.KeyBackquote: {"`", "`"},
	ui.KeyBracketLeft: {"[", "["}, ui.KeyBracketRight: {"]", "]"}, ui.KeyBackslash: {"\\", "\\"},
}

func keyName(k ui.Key, symbol bool) string {
	switch {
	case k >= ui.KeyA && k <= ui.KeyZ:
		return string(rune('A' + k - ui.KeyA))
	case k >= ui.Key0 && k <= ui.Key9:
		return string(rune('0' + k - ui.Key0))
	case k >= ui.KeyF1 && k <= ui.KeyF12:
		return "F" + strconv.Itoa(int(k-ui.KeyF1)+1)
	}
	n := keyNames[k]
	if symbol {
		return n[1]
	}
	return n[0]
}

// accelerator writes a key as a menu item's Accelerator.
func accelerator(mods ui.Modifiers, key ui.Key) string {
	var b strings.Builder
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{{ui.Super, "Super+"}, {ui.Ctrl, "Ctrl+"}, {ui.Alt, "Alt+"}, {ui.Shift, "Shift+"}} {
		if mods&m.mod != 0 {
			b.WriteString(m.name)
		}
	}
	return b.String() + keyName(key, false)
}

// shortcutText shows a key as the platform does: ⌃⌥⇧⌘K on macOS,
// Ctrl+Shift+K elsewhere.
func shortcutText(mods ui.Modifiers, key ui.Key) string {
	if !mac {
		return strings.ReplaceAll(accelerator(mods, key), "Super+", "Win+")
	}
	var b strings.Builder
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{{ui.Ctrl, "⌃"}, {ui.Alt, "⌥"}, {ui.Shift, "⇧"}, {ui.Super, "⌘"}} {
		if mods&m.mod != 0 {
			b.WriteString(m.name)
		}
	}
	return b.String() + keyName(key, true)
}

func (k appKey) String() string {
	s := shortcutText(k.mods, k.key)
	if k.chord {
		s = shortcutText(ui.Cmd, ui.KeyK) + " " + s
	}
	return s
}

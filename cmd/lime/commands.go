package main

import (
	"strconv"

	"github.com/egoist/mygo/ui/editor"
)

// command is something keys, menus and the command palette run, by the
// name of Sublime Text's command where it has one.
type command struct {
	id      string
	caption string // "" keeps it out of the command palette
	run     func(a *app)
	ed      editor.Command // the editor's command it runs, for its key
}

func foldLevel(n int) string   { return "fold_by_level " + strconv.Itoa(n) }
func focusGroup(i int) string  { return "focus_group " + strconv.Itoa(i) }
func moveToGroup(i int) string { return "move_to_group " + strconv.Itoa(i) }
func selectTab(i int) string   { return "select_by_index " + strconv.Itoa(i) }

func withDoc(fn func(a *app, d *doc)) func(a *app) {
	return func(a *app) {
		if d := a.current(); d != nil {
			fn(a, d)
		}
	}
}

func edit(id, caption string, cmd editor.Command) command {
	return command{id, caption, withDoc(func(_ *app, d *doc) { d.st.Run(cmd) }), cmd}
}

func (a *app) setFontSize(size float32) {
	a.set.FontSize = max(8, min(size, 48))
	a.l.saveSettings()
}

// commands and commandByID are set by init: the commands' functions read
// them, which their initializers could not.
var (
	commands    []command
	commandByID map[string]*command
)

func init() {
	commands = makeCommands()
	commandByID = make(map[string]*command, len(commands))
	for i := range commands {
		commandByID[commands[i].id] = &commands[i]
	}
}

func makeCommands() []command {
	c := []command{
		{id: "new_file", caption: "File: New File", run: (*app).newFile},
		{id: "prompt_open", caption: "File: Open…", run: (*app).promptOpen},
		{id: "prompt_open_folder", caption: "File: Open Folder…", run: (*app).promptAddFolder},
		{id: "reopen_last_file", caption: "File: Reopen Closed File", run: (*app).reopenClosed},
		{id: "save", caption: "File: Save", run: withDoc((*app).save)},
		{id: "prompt_save_as", caption: "File: Save As…", run: withDoc((*app).saveAs)},
		{id: "save_all", caption: "File: Save All", run: (*app).saveAll},
		{id: "close", caption: "File: Close", run: (*app).closeCurrent},
		{id: "new_window", caption: "Window: New Window", run: (*app).openWindow},
		{id: "close_window", caption: "Window: Close", run: func(a *app) { a.win.Close() }},

		{id: "show_goto", caption: "Goto Anything", run: func(a *app) { a.showGoto("") }},
		{id: "show_command_palette", run: (*app).showCommandPalette},
		{id: "goto_line", caption: "Goto Line", run: func(a *app) { a.showGoto(":") }},
		{id: "show_panel find", run: func(a *app) { a.showFind(false) }},
		{id: "show_panel replace", run: func(a *app) { a.showFind(true) }},
		{id: "next_view", run: func(a *app) { a.selectTab(a.group().active + 1) }},
		{id: "prev_view", run: func(a *app) { a.selectTab(a.group().active - 1) }},
		{id: "next_view_in_stack", run: func(a *app) { a.cycleTabs(1) }},
		{id: "prev_view_in_stack", run: func(a *app) { a.cycleTabs(-1) }},
		{id: "select_last_tab", run: func(a *app) { a.selectTab(-1) }},

		{id: "toggle_side_bar", caption: "View: Toggle Side Bar", run: func(a *app) {
			a.set.SideBar = !a.set.SideBar
			a.l.saveSettings()
		}},
		{id: "toggle_word_wrap", caption: "Word Wrap: Toggle", run: func(a *app) {
			a.set.WordWrap = !a.set.WordWrap
			a.l.saveSettings()
		}},
		{id: "increase_font_size", caption: "Font: Increase", run: func(a *app) { a.setFontSize(a.set.FontSize + 1) }},
		{id: "decrease_font_size", caption: "Font: Decrease", run: func(a *app) { a.setFontSize(a.set.FontSize - 1) }},
		{id: "reset_font_size", caption: "Font: Reset", run: func(a *app) { a.setFontSize(13) }},
		{id: "select_color_scheme", caption: "UI: Select Color Scheme", run: (*app).showSchemes},
		{id: "select_syntax", caption: "Set Syntax…", run: (*app).showSyntaxes},
		{id: "new_pane", caption: "Layout: New Pane", run: (*app).newPane},
		{id: "close_pane", caption: "Layout: Close Pane", run: (*app).closePane},
		{id: "focus_next_group", run: func(a *app) { a.focusGroup(a.neighbor(1)) }},
		{id: "focus_prev_group", run: func(a *app) { a.focusGroup(a.neighbor(-1)) }},
		{id: "move_to_next_group", run: func(a *app) { a.moveToGroup(a.neighbor(1)) }},
		{id: "move_to_prev_group", run: func(a *app) { a.moveToGroup(a.neighbor(-1)) }},

		{id: "prompt_add_folder", caption: "Project: Add Folder…", run: (*app).promptAddFolder},
		{id: "prompt_open_project", caption: "Project: Open Project…", run: (*app).promptOpenProject},
		{id: "save_project_as", caption: "Project: Save As…", run: (*app).promptSaveProject},
		{id: "close_project", caption: "Project: Close Project", run: (*app).closeProject},
		{id: "refresh_folders", caption: "Project: Refresh Folders", run: (*app).refreshFolders},

		edit("upper_case", "Convert Case: Upper Case", editor.Uppercase),
		edit("lower_case", "Convert Case: Lower Case", editor.Lowercase),
		edit("title_case", "Convert Case: Title Case", editor.TitleCase),
		edit("sort_lines", "Sort Lines", editor.SortLinesCaseInsensitive),
		edit("sort_lines_case_sensitive", "Sort Lines (Case Sensitive)", editor.SortLines),
		edit("reverse_lines", "Permute Lines: Reverse", editor.ReverseLines),
		edit("unique_lines", "Permute Lines: Unique", editor.UniqueLines),
		edit("toggle_comment", "Toggle Comment", editor.ToggleComment),
		edit("toggle_block_comment", "Toggle Block Comment", editor.ToggleBlockComment),
		edit("split_selection_into_lines", "Selection: Split into Lines", editor.SplitSelectionIntoLines),
		edit("join_lines", "Join Lines", editor.JoinLines),
		edit("duplicate_line", "Duplicate Line", editor.DuplicateSelection),
		edit("delete_line", "Delete Line", editor.DeleteLine),
		edit("swap_line_up", "Swap Line Up", editor.MoveLinesUp),
		edit("swap_line_down", "Swap Line Down", editor.MoveLinesDown),
		edit("trim_trailing_white_space", "Trim Trailing White Space", editor.TrimTrailingWhitespace),
		edit("fold_all", "Code Folding: Fold All", editor.FoldAll),
		edit("unfold_all", "Code Folding: Unfold All", editor.UnfoldAll),
		edit("next_modification", "Goto Next Modification", editor.NextHunk),
		edit("prev_modification", "Goto Previous Modification", editor.PreviousHunk),
		edit("revert_hunk", "Revert Diff Hunk", editor.RevertHunk),
		edit("delete_to_eol", "", editor.DeleteToLineEnd),
		edit("delete_to_bol", "", editor.DeleteToLineStart),
		edit("show_at_center", "", editor.ScrollCursorCenter),
		edit("find_next", "", editor.FindNext),
		edit("find_prev", "", editor.FindPrevious),
		{id: "find_under_expand", run: quickFind(func(st *editor.State) { st.Run(editor.SelectNext) })},
		{id: "find_all_under", run: quickFind(func(st *editor.State) { st.Run(editor.SelectAllMatches) })},
		{id: "find_under_expand_skip", run: quickFind(skipNext)},
		{id: "slurp_find_string", run: quickFind(func(st *editor.State) { st.Run(editor.UseSelectionForFind) })},
	}
	for i := range 9 {
		c = append(c, edit(foldLevel(i+1), "", editor.FoldLevel1+editor.Command(i)))
	}
	for i := range 4 {
		c = append(c,
			command{id: focusGroup(i), run: func(a *app) { a.focusGroup(i) }},
			command{id: moveToGroup(i), run: func(a *app) { a.moveToGroup(i) }})
	}
	for i := range 8 {
		c = append(c, command{id: selectTab(i), run: func(a *app) {
			if g := a.group(); i < len(g.docs) {
				a.show(g.docs[i])
			}
		}})
	}
	for _, l := range layouts {
		c = append(c, command{id: l.id(), caption: "Layout: " + l.name, run: func(a *app) { a.setLayout(l) }})
	}
	return c
}

func (a *app) run(id string) {
	if c := commandByID[id]; c != nil {
		c.run(a)
	}
}

// key returns the first key of the command: the window's, else the
// editor's.
func (c *command) key() (appKey, bool) {
	if k, ok := keyFor(c.id); ok {
		return k, true
	}
	if c.ed != editor.NoCommand {
		for _, b := range editorKeys {
			if b.Command == c.ed {
				return appKey{mods: b.Mods, key: b.Key}, true
			}
		}
	}
	return appKey{}, false
}

func (c *command) keyText() string {
	if k, ok := c.key(); ok {
		return k.String()
	}
	return ""
}

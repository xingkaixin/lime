package main

import (
	"strconv"

	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// command is something keys, menus and the command palette run, by the
// name of Sublime Text's command where it has one.
type command struct {
	id      string
	caption func(locales.Messages) string // nil keeps it out of the command palette
	run     func(a *app)
	ed      editor.Command // the editor's command it runs, for its key
}

func foldLevel(n int) string   { return "fold_by_level " + strconv.Itoa(n) }
func focusGroup(i int) string  { return "focus_group " + strconv.Itoa(i) }
func moveToGroup(i int) string { return "move_to_group " + strconv.Itoa(i) }
func selectTab(i int) string   { return "select_by_index " + strconv.Itoa(i) }

// withDoc runs fn on the current doc, when it shows text.
func withDoc(fn func(a *app, d *doc)) func(a *app) {
	return func(a *app) {
		if d := a.current(); d != nil && d.kind == textDoc {
			fn(a, d)
		}
	}
}

func edit(id string, caption func(locales.Messages) string, cmd editor.Command) command {
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
		{id: "new_file", caption: locales.Messages.CmdNewFile, run: (*app).newFile},
		{id: "prompt_open", caption: locales.Messages.CmdOpen, run: (*app).promptOpen},
		{id: "prompt_open_folder", caption: locales.Messages.CmdOpenFolder, run: (*app).promptAddFolder},
		{id: "reopen_last_file", caption: locales.Messages.CmdReopenClosedFile, run: (*app).reopenClosed},
		{id: "save", caption: locales.Messages.CmdSave, run: withDoc((*app).save)},
		{id: "prompt_save_as", caption: locales.Messages.CmdSaveAs, run: withDoc((*app).saveAs)},
		{id: "save_all", caption: locales.Messages.CmdSaveAll, run: (*app).saveAll},
		{id: "close", caption: locales.Messages.CmdClose, run: (*app).closeCurrent},
		{id: "new_window", caption: locales.Messages.CmdNewWindow, run: (*app).openWindow},
		{id: "close_window", caption: locales.Messages.CmdCloseWindow, run: func(a *app) { a.win.Close() }},

		{id: "show_goto", caption: locales.Messages.CmdGotoAnything, run: func(a *app) { a.showGoto("") }},
		{id: "show_command_palette", run: (*app).showCommandPalette},
		{id: "goto_line", caption: locales.Messages.CmdGotoLine, run: func(a *app) { a.showGoto(":") }},
		{id: "show_panel find", run: func(a *app) { a.showFind(false) }},
		{id: "show_panel replace", run: func(a *app) { a.showFind(true) }},
		{id: "next_view", run: func(a *app) { a.selectTab(a.group().active + 1) }},
		{id: "prev_view", run: func(a *app) { a.selectTab(a.group().active - 1) }},
		{id: "next_view_in_stack", run: func(a *app) { a.cycleTabs(1) }},
		{id: "prev_view_in_stack", run: func(a *app) { a.cycleTabs(-1) }},
		{id: "select_last_tab", run: func(a *app) { a.selectTab(-1) }},

		{id: "toggle_side_bar", caption: locales.Messages.CmdToggleSideBar, run: func(a *app) {
			a.set.SideBar = !a.set.SideBar
			a.l.saveSettings()
		}},
		{id: "toggle_word_wrap", caption: locales.Messages.CmdToggleWordWrap, run: func(a *app) {
			a.set.WordWrap = !a.set.WordWrap
			a.l.saveSettings()
		}},
		{id: "increase_font_size", caption: locales.Messages.CmdIncreaseFontSize, run: func(a *app) { a.setFontSize(a.set.FontSize + 1) }},
		{id: "decrease_font_size", caption: locales.Messages.CmdDecreaseFontSize, run: func(a *app) { a.setFontSize(a.set.FontSize - 1) }},
		{id: "reset_font_size", caption: locales.Messages.CmdResetFontSize, run: func(a *app) { a.setFontSize(defaultFontSize) }},
		{id: "show_settings", caption: locales.Messages.CmdSettings, run: (*app).showSettings},
		{id: "select_color_scheme", caption: locales.Messages.CmdSelectColorScheme, run: (*app).showSchemes},
		{id: "select_syntax", caption: locales.Messages.CmdSetSyntax, run: (*app).showSyntaxes},
		{id: "new_pane", caption: locales.Messages.CmdNewPane, run: (*app).newPane},
		{id: "close_pane", caption: locales.Messages.CmdClosePane, run: (*app).closePane},
		{id: "focus_next_group", run: func(a *app) { a.focusGroup(a.neighbor(1)) }},
		{id: "focus_prev_group", run: func(a *app) { a.focusGroup(a.neighbor(-1)) }},
		{id: "move_to_next_group", run: func(a *app) { a.moveToGroup(a.neighbor(1)) }},
		{id: "move_to_prev_group", run: func(a *app) { a.moveToGroup(a.neighbor(-1)) }},

		{id: "prompt_add_folder", caption: locales.Messages.CmdAddFolder, run: (*app).promptAddFolder},
		{id: "prompt_open_project", caption: locales.Messages.CmdOpenProject, run: (*app).promptOpenProject},
		{id: "save_project_as", caption: locales.Messages.CmdSaveProjectAs, run: (*app).promptSaveProject},
		{id: "close_project", caption: locales.Messages.CmdCloseProject, run: (*app).closeProject},
		{id: "refresh_folders", caption: locales.Messages.CmdRefreshFolders, run: (*app).refreshFolders},

		edit("upper_case", locales.Messages.CmdUpperCase, editor.Uppercase),
		edit("lower_case", locales.Messages.CmdLowerCase, editor.Lowercase),
		edit("title_case", locales.Messages.CmdTitleCase, editor.TitleCase),
		edit("sort_lines", locales.Messages.CmdSortLines, editor.SortLinesCaseInsensitive),
		edit("sort_lines_case_sensitive", locales.Messages.CmdSortLinesCaseSensitive, editor.SortLines),
		edit("reverse_lines", locales.Messages.CmdReverseLines, editor.ReverseLines),
		edit("unique_lines", locales.Messages.CmdUniqueLines, editor.UniqueLines),
		edit("toggle_comment", locales.Messages.CmdToggleComment, editor.ToggleComment),
		edit("toggle_block_comment", locales.Messages.CmdToggleBlockComment, editor.ToggleBlockComment),
		edit("split_selection_into_lines", locales.Messages.CmdSplitSelection, editor.SplitSelectionIntoLines),
		edit("join_lines", locales.Messages.CmdJoinLines, editor.JoinLines),
		edit("duplicate_line", locales.Messages.CmdDuplicateLine, editor.DuplicateSelection),
		edit("delete_line", locales.Messages.CmdDeleteLine, editor.DeleteLine),
		edit("swap_line_up", locales.Messages.CmdSwapLineUp, editor.MoveLinesUp),
		edit("swap_line_down", locales.Messages.CmdSwapLineDown, editor.MoveLinesDown),
		edit("trim_trailing_white_space", locales.Messages.CmdTrimTrailingWhiteSpace, editor.TrimTrailingWhitespace),
		edit("fold_all", locales.Messages.CmdFoldAll, editor.FoldAll),
		edit("unfold_all", locales.Messages.CmdUnfoldAll, editor.UnfoldAll),
		edit("next_modification", locales.Messages.CmdNextModification, editor.NextHunk),
		edit("prev_modification", locales.Messages.CmdPreviousModification, editor.PreviousHunk),
		edit("revert_hunk", locales.Messages.CmdRevertHunk, editor.RevertHunk),
		edit("delete_to_eol", nil, editor.DeleteToLineEnd),
		edit("delete_to_bol", nil, editor.DeleteToLineStart),
		edit("show_at_center", nil, editor.ScrollCursorCenter),
		edit("find_next", nil, editor.FindNext),
		edit("find_prev", nil, editor.FindPrevious),
		{id: "find_under_expand", run: quickFind(func(st *editor.State) { st.Run(editor.SelectNext) })},
		{id: "find_all_under", run: quickFind(func(st *editor.State) { st.Run(editor.SelectAllMatches) })},
		{id: "find_under_expand_skip", run: quickFind(skipNext)},
		{id: "slurp_find_string", run: quickFind(func(st *editor.State) { st.Run(editor.UseSelectionForFind) })},
	}
	for i := range 9 {
		c = append(c, edit(foldLevel(i+1), nil, editor.FoldLevel1+editor.Command(i)))
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
		caption := func(m locales.Messages) string { return m.CmdLayout(l.label(m)) }
		c = append(c, command{id: l.id(), caption: caption, run: func(a *app) { a.setLayout(l) }})
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

package main

import (
	"math"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// shortcut is a command of the shortcuts settings, by its name there.
type shortcut struct {
	id   string
	name func(locales.Messages) string
}

type shortcutGroup struct {
	title func(locales.Messages) string
	items []shortcut
}

// shortcutGroups list the commands by the menus they are in. The keys of
// the editor's other commands, as the arrows', and of the system's menus,
// as Copy's, are not the user's to change.
var shortcutGroups = func() []shortcutGroup {
	type m = locales.Messages
	s := func(id string, name func(m) string) shortcut { return shortcut{id, name} }
	numbered := func(count int, id func(int) string, name func(m, int) string) []shortcut {
		var items []shortcut
		for i := range count {
			items = append(items, shortcut{id(i), func(msgs m) string { return name(msgs, i+1) }})
		}
		return items
	}
	var layoutItems, endingItems, encodingItems, cursorItems []shortcut
	for _, e := range encodings {
		encodingItems = append(encodingItems,
			shortcut{e.reopenID(), func(msgs m) string { return msgs.CmdReopenWithEncoding(e.title(msgs)) }},
			shortcut{e.saveID(), func(msgs m) string { return msgs.CmdSaveWithEncoding(e.title(msgs)) }})
	}
	for _, e := range lineEndings {
		endingItems = append(endingItems, shortcut{e.id(), e.label})
	}
	for _, l := range layouts {
		layoutItems = append(layoutItems, shortcut{l.id(), func(msgs m) string { return msgs.CmdLayout(l.label(msgs)) }})
	}
	for _, ls := range layoutStyles {
		layoutItems = append(layoutItems, shortcut{ls.id(), func(msgs m) string { return msgs.CmdLayout(ls.label(msgs)) }})
	}
	for _, cs := range cursorStyles {
		cursorItems = append(cursorItems, shortcut{cs.id(), func(msgs m) string { return msgs.CmdCursorStyle(cs.label(msgs)) }})
	}
	return []shortcutGroup{
		{m.MenuFile, append([]shortcut{
			s("new_file", m.MenuNewFile), s("new_window", m.MenuNewWindow), s("prompt_open", m.MenuOpen),
			s("prompt_open_folder", m.MenuOpenFolder), s("reopen_last_file", m.MenuReopenClosedFile),
			s("save", m.MenuSave), s("prompt_save_as", m.MenuSaveAs), s("save_all", m.MenuSaveAll),
			s("close", m.MenuCloseFile), s("close_window", m.MenuCloseWindow), s("show_settings", m.MenuSettings),
			s("compare_with", m.CmdCompareWith), s("compare_with_clipboard", m.CmdCompareWithClipboard),
			s("diff_changes", m.CmdShowUnsavedChanges),
		}, encodingItems...)},
		{m.ShortcutGroupEdit, []shortcut{
			s("soft_undo", m.ShortcutSoftUndo), s("soft_redo", m.ShortcutSoftRedo),
			s("toggle_comment", m.CmdToggleComment), s("toggle_block_comment", m.CmdToggleBlockComment),
			s("duplicate_line", m.CmdDuplicateLine), s("delete_line", m.CmdDeleteLine),
			s("swap_line_up", m.CmdSwapLineUp), s("swap_line_down", m.CmdSwapLineDown), s("join_lines", m.CmdJoinLines),
			s("indent", m.ShortcutIndent), s("unindent", m.ShortcutUnindent),
			s("insert_line_after", m.ShortcutInsertLineAfter), s("insert_line_before", m.ShortcutInsertLineBefore),
			s("transpose", m.ShortcutTranspose), s("delete_to_eol", m.ShortcutDeleteToEnd),
			s("delete_to_bol", m.ShortcutDeleteToStart), s("trim_trailing_white_space", m.CmdTrimTrailingWhiteSpace),
			s("auto_complete", m.ShortcutShowCompletions),
		}},
		{m.ShortcutGroupText, []shortcut{
			s("upper_case", m.CmdUpperCase), s("lower_case", m.CmdLowerCase), s("title_case", m.CmdTitleCase),
			s("swap_case", m.CmdSwapCase), s("convert_to_snake", m.CmdSnakeCase), s("convert_to_dash", m.CmdKebabCase),
			s("convert_to_camel", m.CmdCamelCase), s("convert_to_pascal", m.CmdPascalCase),
			s("sort_lines", m.CmdSortLines), s("sort_lines_case_sensitive", m.CmdSortLinesCaseSensitive),
			s("reverse_lines", m.CmdReverseLines), s("unique_lines", m.CmdUniqueLines),
			s("delete_blank_lines", m.CmdDeleteBlankLines), s("delete_blank_lines surplus", m.CmdDeleteSurplusBlankLines),
			s("format_json", m.CmdFormatJSON), s("minify_json", m.CmdMinifyJSON),
		}},
		{m.ShortcutGroupSelection, []shortcut{
			s("select_line", m.ShortcutSelectLine), s("expand_selection", m.ShortcutExpandSelection),
			s("split_selection_into_lines", m.CmdSplitSelection),
			s("add_cursor_above", m.ShortcutAddCursorAbove), s("add_cursor_below", m.ShortcutAddCursorBelow),
			s("move_to_bracket", m.ShortcutMoveToBracket),
		}},
		{m.MenuFind, []shortcut{
			s("show_panel find", m.MenuFindEllipsis), s("find_next", m.MenuFindNext), s("find_prev", m.MenuFindPrevious),
			s("show_panel replace", m.MenuReplace), s("find_all_under", m.MenuQuickFindAll),
			s("find_under_expand", m.MenuQuickAddNext), s("find_under_expand_skip", m.MenuQuickSkipNext),
			s("slurp_find_string", m.MenuUseSelectionForFind),
			s("show_panel find_in_files", m.MenuFindInFiles), s("next_result", m.ShortcutNextResult),
			s("prev_result", m.ShortcutPrevResult),
		}},
		{m.MenuView, append([]shortcut{
			s("toggle_side_bar", m.StatusSideBar), s("toggle_files_panel", m.SideBarFilesPanel), s("toggle_git_panel", m.SideBarGitPanel),
			s("toggle_word_wrap", m.MenuWordWrap),
			s("increase_font_size", m.MenuLargerFont), s("decrease_font_size", m.MenuSmallerFont),
			s("reset_font_size", m.MenuResetFontSize), s("select_color_scheme", m.ShortcutSelectColorScheme),
			s("select_syntax", m.CmdSetSyntax), s("show_at_center", m.ShortcutScrollToCursor),
			s("open_markdown_preview", m.CmdOpenPreview), s("open_markdown_preview_to_the_side", m.CmdOpenPreviewToTheSide),
		}, cursorItems...)},
		{m.MenuIndentation, slices.Concat([]shortcut{
			s("toggle_setting translate_tabs_to_spaces", m.MenuIndentUsingSpaces),
		}, numbered(8, func(i int) string { return tabWidth(i + 1) }, m.MenuTabWidth), []shortcut{
			s("detect_indentation", m.MenuGuessIndentation),
			s("expand_tabs", m.MenuConvertToSpaces), s("unexpand_tabs", m.MenuConvertToTabs),
		})},
		{m.MenuLineEndings, endingItems},
		{m.ShortcutGroupFolding, append([]shortcut{
			s("fold", m.ShortcutFold), s("unfold", m.ShortcutUnfold), s("fold_all", m.ShortcutFoldAll), s("unfold_all", m.ShortcutUnfoldAll),
		}, numbered(9, func(i int) string { return foldLevel(i + 1) }, m.ShortcutFoldLevel)...)},
		{m.MenuGoto, []shortcut{
			s("show_goto", m.MenuGotoAnything), s("goto_symbol", m.MenuGotoSymbol), s("goto_line", m.MenuGotoLine), s("show_command_palette", m.MenuCommandPalette),
			s("goto_definition", m.ShortcutGotoDefinition), s("next_modification", m.MenuNextModification),
			s("prev_modification", m.MenuPreviousModification), s("revert_hunk", m.CmdRevertHunk),
			s("jump_back", m.CmdJumpBack), s("jump_forward", m.CmdJumpForward),
		}},
		{m.ShortcutGroupTabs, slices.Concat([]shortcut{
			s("next_view", m.ShortcutNextTab), s("prev_view", m.ShortcutPrevTab),
			s("next_view_in_stack", m.ShortcutNextTabInStack), s("prev_view_in_stack", m.ShortcutPrevTabInStack),
		}, numbered(8, selectTab, m.ShortcutSelectTab), []shortcut{s("select_last_tab", m.ShortcutSelectLastTab)})},
		{m.ShortcutGroupLayout, slices.Concat(layoutItems, []shortcut{
			s("new_pane", m.MenuNewGroup), s("close_pane", m.MenuCloseGroup),
			s("focus_next_group", m.ShortcutFocusNextGroup), s("focus_prev_group", m.ShortcutFocusPrevGroup),
			s("move_to_next_group", m.ShortcutMoveToNextGroup), s("move_to_prev_group", m.ShortcutMoveToPrevGroup),
		}, numbered(4, focusGroup, m.ShortcutFocusGroup), numbered(4, moveToGroup, m.ShortcutMoveToGroup))},
		{m.MenuProject, []shortcut{
			s("prompt_open_project", m.MenuOpenProject), s("prompt_select_workspace", m.MenuQuickSwitchProject),
			s("save_project_as", m.MenuSaveProjectAs),
			s("close_project", m.MenuCloseProject), s("prompt_add_folder", m.MenuAddFolderToProject),
			s("refresh_folders", m.MenuRefreshFolders),
		}},
		{m.ShortcutGroupGit, []shortcut{
			s("git_create_branch", m.CmdCreateBranch), s("git_delete_branch", m.CmdDeleteBranch),
			s("git_rename_branch", m.CmdRenameBranch), s("git_uncommit", m.CmdUncommit),
			s("git_push", m.CmdPush), s("git_pull", m.CmdPull), s("git_fetch", m.CmdFetch),
			s("sublime_merge_file_history", m.MenuFileHistory),
			s("toggle_blame", m.ShortcutToggleBlame), s("toggle_inline_blame", m.ShortcutToggleInlineBlame),
			s("copy_permalink_to_line", m.MenuCopyPermalink), s("open_permalink_to_line", m.MenuOpenOnGitHub),
		}},
	}
}()

// shortcutNamed returns the command named id of the settings, and its
// group.
func shortcutNamed(id string) (shortcut, shortcutGroup) {
	for _, g := range shortcutGroups {
		for _, s := range g.items {
			if s.id == id {
				return s, g
			}
		}
	}
	unnamed := func(locales.Messages) string { return id }
	return shortcut{id, unnamed}, shortcutGroup{title: unnamed}
}

// recording is the key the shortcuts settings record for a command.
type recording struct {
	id      string
	chord   bool   // Cmd+K was pressed, before the chord's second key
	pending appKey // pressed, and other commands' key too; zero for none
	notice  string // why the key pressed cannot be a shortcut
	focused bool   // the recorder took the focus: losing it ends the recording
	seen    bool   // the recorder showed in the build
}

var (
	shortcutsIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="1.75" y="3.75" width="12.5" height="8.5" rx="1.75"/><path d="M4.5 6.5h.01M6.83 6.5h.01M9.17 6.5h.01M11.5 6.5h.01M5.25 9.5h5.5"/></svg>`))
	warningIcon   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M8 2.25 14.25 13.25H1.75z"/><path d="M8 6.5v3M8 11.4h.01"/></svg>`))
	resetIcon     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.25 8a4.75 4.75 0 1 0 1.4-3.36"/><path d="M3.25 2.75v2.5h2.5"/></svg>`))
)

// shortcutsBar is the top of the shortcuts settings, which stays as the
// commands scroll.
func (a *app) shortcutsBar(c *ui.Context) {
	p := &a.prefs
	conflicts := a.conflictKeys()
	if len(conflicts) == 0 {
		p.conflicts = false
	}
	ui.Column(c).FillWidth().Shrink(0).Padding(4, 24, 12).Gap(12).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.SearchField(c, &p.query).Grow(1).MinWidth(0).Label(locales.SettingsShortcutsSearch()).Changed() {
				p.conflicts = false
			}
			reset := a.button(c, locales.SettingsShortcutsResetAll(), a.confirmResetKeys)
			if len(a.set.Keys) == 0 {
				reset.Disabled(true).Opacity(0.5)
			}
		})
		if len(conflicts) == 0 {
			return
		}
		danger := a.scheme.danger()
		label := locales.SettingsShortcutsOnlyConflicts()
		if p.conflicts {
			label = locales.SettingsShortcutsShowAll()
		}
		ui.Row(c).FillWidth().Gap(8).Padding(6, 8, 6, 12).Radius(8).Border(1, danger.Alpha(0.3)).Background(danger.Alpha(0.06)).
			AlignItems(ui.Center).Children(func() {
			ui.Icon(c, warningIcon).Size(14, 14).TextColor(danger).Shrink(0)
			ui.Text(c, locales.SettingsShortcutsConflicts(len(conflicts))).FontSize(13).Grow(1).MinWidth(0)
			a.smallButton(c, label, false, func() { p.conflicts = !p.conflicts })
		})
	})
}

func (a *app) shortcutsPage(c *ui.Context) {
	r := &a.prefs.rec
	r.seen = false
	if a.prefs.conflicts {
		a.conflictList(c)
	} else {
		a.shortcutList(c)
	}
	// The command being recorded went out of the list, as a search left it.
	if r.id != "" && !r.seen {
		a.stopRecording()
		c.Invalidate()
	}
}

// shortcutList lists the commands matching the search by their groups.
func (a *app) shortcutList(c *ui.Context) {
	tx, en := locales.In(i18n.Locale()), locales.In("en")
	q := strings.ToLower(strings.TrimSpace(a.prefs.query))
	found := false
	for _, g := range shortcutGroups {
		var items []shortcut
		for _, s := range g.items {
			if q == "" || strings.Contains(strings.ToLower(s.name(tx)), q) || strings.Contains(strings.ToLower(s.name(en)), q) {
				items = append(items, s)
			}
		}
		if len(items) == 0 {
			continue
		}
		found = true
		ui.Column(c.Key(g.title(en))).FillWidth().Gap(10).Children(func() {
			a.heading(c, g.title(tx), false)
			a.settingsGroup(c, func() {
				for _, s := range items {
					a.shortcutRow(c, s.id, s.name(tx), "")
				}
			})
		})
	}
	if !found {
		ui.Text(c, locales.SettingsShortcutsNone()).FontSize(13).TextColor(a.scheme.chrome().muted)
	}
}

// conflictList lists the keys of two commands or more, with their
// commands.
func (a *app) conflictList(c *ui.Context) {
	ch := a.scheme.chrome()
	km := a.l.keys
	tx := locales.In(i18n.Locale())
	for _, k := range a.conflictKeys() {
		ui.Column(c.Key(k.spec())).FillWidth().Gap(10).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				a.keyChip(c, "", k, false)
				ui.Text(c, locales.SettingsShortcutsCommands(len(km.users[k]))).FontSize(12).TextColor(ch.muted)
			})
			a.settingsGroup(c, func() {
				for _, id := range km.users[k] {
					s, g := shortcutNamed(id)
					a.shortcutRow(c, id, s.name(tx), g.title(tx))
				}
			})
		})
	}
	ui.Text(c, locales.SettingsShortcutsResolve()).FontSize(12).TextColor(ch.muted)
}

// conflictKeys are the keys of two commands or more, in the order the
// settings list the commands.
func (a *app) conflictKeys() []appKey {
	km := a.l.keys
	var keys []appKey
	for _, g := range shortcutGroups {
		for _, s := range g.items {
			for _, k := range km.keys[s.id] {
				if k := k.seq(); km.conflicted(k) && !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
		}
	}
	return keys
}

// shortcutRow shows the keys of the command named id, which the pointer
// over it changes. group names the command's group where the list does
// not.
func (a *app) shortcutRow(c *ui.Context, id, name, group string) {
	ch := a.scheme.chrome()
	km := a.l.keys
	r := &a.prefs.rec
	// A command in two conflicts records in its first row.
	recording := r.id == id && !r.seen
	if recording {
		r.seen = true
	}
	row := ui.Column(c.Key(id)).FillWidth().MinHeight(36).Padding(7, 10, 7, 14).Gap(8).Transition(colorFade)
	hovered := row.Hovered() && !recording
	switch {
	case recording:
		row.Background(a.scheme.accent.Alpha(0.06))
	case hovered:
		row.Background(ch.panel.Mix(ch.text, 0.04))
	}
	row.Children(func() {
		ui.Row(c).FillWidth().MinHeight(22).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).FontSize(13).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				if group != "" {
					ui.Text(c, group).FontSize(12).TextColor(ch.muted).SingleLine().Shrink(0)
				}
				if km.changed[id] {
					a.tag(c, locales.SettingsShortcutModified())
				}
			})
			ui.Row(c).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
				for _, k := range km.keys[id] {
					a.keyChip(c, id, k, hovered)
				}
				switch {
				case recording:
					a.recorder(c, id, name)
				case hovered:
					a.iconButton(c, plusIcon, locales.SettingsShortcutAdd(), func() { a.startRecording(id) })
					if km.changed[id] {
						a.iconButton(c, resetIcon, locales.SettingsShortcutReset(), func() { a.l.setKeys(id, nil) })
					}
				}
			})
		})
		if recording {
			a.recordingDetail(c, id)
		}
	})
}

// keyChip shows k, a key of the command named id, in the color of a
// conflict when another command has it too, and a button taking it away
// when removable.
func (a *app) keyChip(c *ui.Context, id string, k appKey, removable bool) {
	ch := a.scheme.chrome()
	fg, line, bg := ch.text, ch.line, ch.panel
	conflict := a.l.keys.conflicted(k)
	if conflict {
		d := a.scheme.danger()
		fg, line, bg = d, d, d.Alpha(0.08)
	}
	end := float32(7)
	if removable {
		end = 3
	}
	ui.Row(c.Key(k.spec())).Height(22).Padding(0, end, 0, 7).Gap(4).Radius(5).Border(1, line).Background(bg).
		AlignItems(ui.Center).Shrink(0).Children(func() {
		if conflict {
			ui.Icon(c, warningIcon).Size(11, 11).TextColor(fg)
		}
		ui.Text(c, k.String()).FontSize(12).TextColor(fg).SingleLine()
		if !removable {
			return
		}
		btn := ui.ButtonBase(c).Width(14).Height(14).Radius(3).Center().Transition(colorFade).
			Label(locales.SettingsShortcutRemove(k.String())).OnClick(func() { a.l.setKeys(id, a.l.keys.without(id, k)) })
		if btn.Hovered() {
			btn.Background(fg.Alpha(0.12))
		}
		btn.Children(func() { ui.Icon(c, closeIcon).Size(10, 10).TextColor(fg.Mix(bg, 0.3)) })
	})
}

// recorder takes the next key pressed for the command named id.
func (a *app) recorder(c *ui.Context, id, name string) {
	ch := a.scheme.chrome()
	r := &a.prefs.rec
	text, fg, line := locales.SettingsShortcutPress(), ch.muted, a.scheme.accent
	conflict := r.pending != appKey{}
	switch {
	case conflict:
		d := a.scheme.danger()
		text, fg, line = r.pending.String(), d, d
	case r.chord:
		text, fg = shortcutText(ui.Cmd, ui.KeyK)+" …", ch.text
	}
	field := ui.Row(c.Key("recorder")).Height(22).MinWidth(112).Padding(0, 8).Gap(4).Radius(5).Border(1, line).
		Background(ch.panel).Shadow(0, 0, 0, 3, line.Alpha(0.2)).AlignItems(ui.Center).Focusable().FocusRing(false).AutoFocus().
		Label(locales.SettingsShortcutRecord(name)).HandleInput(func(ev ui.InputEvent) bool { return a.record(id, ev) })
	if field.Focused() {
		r.focused = true
	} else if r.focused {
		a.stopRecording()
		c.Invalidate()
	}
	field.Children(func() {
		if conflict {
			ui.Icon(c, warningIcon).Size(11, 11).TextColor(fg)
		}
		ui.Text(c, text).FontSize(12).TextColor(fg).SingleLine()
	})
}

// recordingDetail says how to record a key, or why the key pressed is
// not one, or which commands have it.
func (a *app) recordingDetail(c *ui.Context, id string) {
	r := &a.prefs.rec
	muted := a.scheme.chrome().muted
	switch {
	case r.pending != appKey{}:
		a.conflictPanel(c, id)
	case r.notice != "":
		ui.Text(c, r.notice).FontSize(12).TextColor(a.scheme.danger())
	case r.chord:
		ui.Text(c, locales.SettingsShortcutChordHint()).FontSize(12).TextColor(muted)
	default:
		ui.Text(c, locales.SettingsShortcutRecordHint(shortcutText(ui.Cmd, ui.KeyK))).FontSize(12).TextColor(muted)
	}
}

// conflictPanel lists the commands that have the key pressed for the
// command named id, which it keeps or takes from them.
func (a *app) conflictPanel(c *ui.Context, id string) {
	ch := a.scheme.chrome()
	tx := locales.In(i18n.Locale())
	k := a.prefs.rec.pending
	others := a.l.keys.others(k, id)
	hint := locales.SettingsShortcutReplaceMany(k.String())
	if len(others) == 1 {
		s, _ := shortcutNamed(others[0])
		hint = locales.SettingsShortcutReplaceOne(k.String(), s.name(tx))
	}
	d := a.scheme.danger()
	ui.Column(c).FillWidth().Gap(8).Padding(10, 12).Radius(6).Border(1, d.Alpha(0.3)).Background(d.Alpha(0.05)).Children(func() {
		ui.Text(c, locales.SettingsShortcutTaken(k.String())).FontSize(12).Bold().TextColor(d)
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			for _, o := range others {
				s, g := shortcutNamed(o)
				ui.Row(c.Key(o)).FillWidth().Gap(12).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
					ui.Text(c, s.name(tx)).FontSize(13)
					ui.Text(c, g.title(tx)).FontSize(12).TextColor(ch.muted)
				})
			}
		})
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, hint).FontSize(12).TextColor(ch.muted).Grow(1).MinWidth(0)
			a.smallButton(c, locales.ButtonCancel(), false, a.stopRecording)
			a.smallButton(c, locales.SettingsShortcutKeepBoth(), false, func() { a.addKey(id, k, nil) })
			a.smallButton(c, locales.SettingsShortcutReplace(), true, func() { a.addKey(id, k, others) })
		})
	})
}

func (a *app) startRecording(id string) {
	a.prefs.rec = recording{id: id}
	a.l.pauseMenu(true)
}

func (a *app) stopRecording() {
	a.prefs.rec = recording{}
	a.l.pauseMenu(false)
}

// record takes ev, the recorder's input, for the command named id: all
// of the keys, which are the recorder's while it records.
func (a *app) record(id string, ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputKeyDown:
	case ui.InputText, ui.InputCompose, ui.InputCommand:
		return true
	default:
		return false
	}
	r := &a.prefs.rec
	key := ev.Key
	if key == ui.KeyUnknown {
		key = ev.PhysicalKey
	}
	switch {
	case key == ui.KeyUnknown:
		return true
	case ev.Mods == 0 && key == ui.KeyEscape:
		a.stopRecording()
		return true
	case !r.chord && ev.Mods == ui.Cmd && key == ui.KeyK:
		r.chord, r.pending, r.notice = true, appKey{}, ""
		return true
	}
	k := appKey{mods: ev.Mods, key: key, chord: r.chord, id: id}
	r.chord, r.pending, r.notice = false, appKey{}, refusal(k)
	km := a.l.keys
	switch {
	case r.notice != "":
	case slices.Contains(km.keys[id], k):
		a.stopRecording()
	case len(km.others(k, id)) > 0:
		r.pending = k
	default:
		a.addKey(id, k, nil)
	}
	return true
}

// refusal says why k cannot be a key of a command, "" when it can: a
// key without Cmd, Ctrl or Alt would be typed, and others are the
// system's or the editor's.
func refusal(k appKey) string {
	switch {
	case k.chord:
		return ""
	case k.mods&(ui.Super|ui.Ctrl|ui.Alt) == 0 && (k.key < ui.KeyF1 || k.key > ui.KeyF12):
		return locales.SettingsShortcutNeedsModifier()
	case reserved[k.seq()]:
		return locales.SettingsShortcutReserved(k.String())
	}
	return ""
}

// addKey gives the command named id the key k, taking it from the
// commands from.
func (a *app) addKey(id string, k appKey, from []string) {
	km := a.l.keys
	for _, o := range from {
		a.l.setKeys(o, km.without(o, k))
	}
	a.l.setKeys(id, append(slices.Clone(km.keys[id]), k))
	a.stopRecording()
}

func (a *app) confirmResetKeys() {
	go func() {
		res, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:       a.win,
			Type:         mygo.MessageQuestion,
			Message:      locales.SettingsShortcutsResetAllConfirm(),
			Buttons:      []string{locales.SettingsShortcutsResetAll(), locales.ButtonCancel()},
			CancelButton: 1,
		})
		if res.Button == 0 {
			a.win.Update(func() { a.l.resetKeys() })
		}
	}()
}

// smallButton is a button that leaves the keyboard where it is, with
// the key being recorded.
func (a *app) smallButton(c *ui.Context, label string, primary bool, click func()) {
	ch := a.scheme.chrome()
	bg, fg, line := ch.panel, ch.text, ch.line
	if primary {
		bg, fg, line = a.scheme.accent, textOn(a.scheme.accent), a.scheme.accent
	}
	btn := ui.ButtonBase(c).Height(24).Padding(0, 10).Shrink(0).Radius(6).Border(1, line).Background(bg).KeepFocus().
		Transition(colorFade).Label(label).OnClick(click)
	if btn.Hovered() {
		btn.Background(bg.Mix(fg, 0.08))
	}
	btn.Children(func() { ui.Text(c, label).FontSize(12).TextColor(fg) })
}

func (a *app) iconButton(c *ui.Context, icon *ui.SVG, label string, click func()) {
	ch := a.scheme.chrome()
	btn := ui.ButtonBase(c).Width(22).Height(22).Radius(5).Center().Transition(colorFade).Label(label).Tooltip(label).OnClick(click)
	if btn.Hovered() {
		btn.Background(ch.panel.Mix(ch.text, 0.08))
	}
	btn.Children(func() { ui.Icon(c, icon).Size(14, 14).TextColor(ch.muted) })
}

// textOn is white or black, whichever is the more legible on bg.
func textOn(bg ui.Color) ui.Color {
	lin := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	// White and black have the same contrast with a color of this
	// relative luminance, as WCAG measures it.
	if 0.2126*lin(bg.R)+0.7152*lin(bg.G)+0.0722*lin(bg.B) < 0.179 {
		return ui.RGB(255, 255, 255)
	}
	return black
}

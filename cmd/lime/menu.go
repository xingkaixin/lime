package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"

	"lime/locales"
)

// menuItem runs the command named id in the focused window, showing its
// key. With no window, as on macOS after the last closes, the windows come
// back first, and new_window has its window in them.
func (l *lime) menuItem(label, id string) *mygo.MenuItem {
	return &mygo.MenuItem{Label: label, Accelerator: l.menuKey(id), Click: func(_ *mygo.MenuItem, w *mygo.Window) {
		if len(l.wins) == 0 {
			l.reopen()
			if id == "new_window" {
				return
			}
		}
		if a := l.at(w); a != nil {
			a.win.Update(func() { a.run(id) })
		}
	}}
}

// check is a checkbox or radio item that runs the command named id,
// checked as syncMenu has it.
func (l *lime) check(label, id string, typ mygo.MenuItemType) *mygo.MenuItem {
	it := l.menuItem(label, id)
	it.ID, it.Type = id, typ
	return it
}

func (l *lime) newMenu() *mygo.Menu {
	var layoutItems, schemeItems []*mygo.MenuItem
	for _, lo := range layouts {
		layoutItems = append(layoutItems, l.check(lo.label(locales.In(i18n.Locale())), lo.id(), mygo.MenuItemRadio))
	}
	for _, sc := range schemes {
		it := l.check(sc.name, "scheme "+sc.name, mygo.MenuItemRadio)
		it.Click = func(_ *mygo.MenuItem, w *mygo.Window) {
			if a := l.at(w); a != nil {
				a.win.Update(func() { a.setScheme(sc) })
			}
		}
		schemeItems = append(schemeItems, it)
	}
	// Settings go in the app's menu on macOS, and in File elsewhere.
	settingsItem := l.menuItem(locales.MenuSettings(), "show_settings")
	appMenu := &mygo.MenuItem{Role: mygo.RoleAppMenu}
	var fileSettings []*mygo.MenuItem
	if mac {
		appMenu.Submenu = []*mygo.MenuItem{
			{Role: mygo.RoleAbout}, mygo.Separator(), settingsItem, mygo.Separator(),
			{Role: mygo.RoleServices}, mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide}, mygo.Separator(),
			{Role: mygo.RoleQuit},
		}
	} else {
		fileSettings = []*mygo.MenuItem{mygo.Separator(), settingsItem}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		appMenu,
		{Label: locales.MenuFile(), Submenu: append([]*mygo.MenuItem{
			l.menuItem(locales.MenuNewFile(), "new_file"),
			l.menuItem(locales.MenuNewWindow(), "new_window"),
			l.menuItem(locales.MenuOpen(), "prompt_open"),
			l.menuItem(locales.MenuOpenFolder(), "prompt_open_folder"),
			l.recentMenu(),
			mygo.Separator(),
			l.menuItem(locales.MenuSave(), "save"),
			l.menuItem(locales.MenuSaveAs(), "prompt_save_as"),
			l.menuItem(locales.MenuSaveAll(), "save_all"),
			mygo.Separator(),
			l.menuItem(locales.MenuCloseFile(), "close"),
			l.menuItem(locales.MenuCloseWindow(), "close_window"),
		}, fileSettings...)},
		{Role: mygo.RoleEditMenu},
		{Label: locales.MenuFind(), Submenu: []*mygo.MenuItem{
			l.menuItem(locales.MenuFindEllipsis(), "show_panel find"),
			l.menuItem(locales.MenuFindNext(), "find_next"),
			l.menuItem(locales.MenuFindPrevious(), "find_prev"),
			mygo.Separator(),
			l.menuItem(locales.MenuReplace(), "show_panel replace"),
			mygo.Separator(),
			l.menuItem(locales.MenuQuickFindAll(), "find_all_under"),
			l.menuItem(locales.MenuQuickAddNext(), "find_under_expand"),
			l.menuItem(locales.MenuQuickSkipNext(), "find_under_expand_skip"),
			mygo.Separator(),
			l.menuItem(locales.MenuUseSelectionForFind(), "slurp_find_string"),
		}},
		{Label: locales.MenuView(), Submenu: []*mygo.MenuItem{
			l.check(locales.MenuSideBar(), "toggle_side_bar", mygo.MenuItemCheckbox),
			mygo.Separator(),
			{Label: locales.MenuLayout(), Submenu: layoutItems},
			{Label: locales.MenuGroups(), Submenu: []*mygo.MenuItem{
				l.menuItem(locales.MenuNewGroup(), "new_pane"),
				l.menuItem(locales.MenuCloseGroup(), "close_pane"),
			}},
			{Label: locales.MenuColorScheme(), Submenu: schemeItems},
			l.menuItem(locales.MenuSyntax(), "select_syntax"),
			mygo.Separator(),
			l.check(locales.MenuWordWrap(), "toggle_word_wrap", mygo.MenuItemCheckbox),
			mygo.Separator(),
			l.menuItem(locales.MenuLargerFont(), "increase_font_size"),
			l.menuItem(locales.MenuSmallerFont(), "decrease_font_size"),
			l.menuItem(locales.MenuResetFontSize(), "reset_font_size"),
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Label: locales.MenuGoto(), Submenu: []*mygo.MenuItem{
			l.menuItem(locales.MenuGotoAnything(), "show_goto"),
			l.menuItem(locales.MenuGotoLine(), "goto_line"),
			l.menuItem(locales.MenuCommandPalette(), "show_command_palette"),
			mygo.Separator(),
			l.menuItem(locales.MenuNextModification(), "next_modification"),
			l.menuItem(locales.MenuPreviousModification(), "prev_modification"),
		}},
		{Label: locales.MenuProject(), Submenu: []*mygo.MenuItem{
			l.menuItem(locales.MenuOpenProject(), "prompt_open_project"),
			l.menuItem(locales.MenuSaveProjectAs(), "save_project_as"),
			l.menuItem(locales.MenuCloseProject(), "close_project"),
			mygo.Separator(),
			l.menuItem(locales.MenuAddFolderToProject(), "prompt_add_folder"),
			l.menuItem(locales.MenuRefreshFolders(), "refresh_folders"),
		}},
		{Role: mygo.RoleWindowMenu},
	})
}

// syncMenu checks the menu's checkboxes and radio items as the state of
// the window last focused has them, when that changed.
func (a *app) syncMenu() {
	l := a.l
	if l.menu == nil || l.last != a {
		return
	}
	state := fmt.Sprint(a.side, a.set.WordWrap, a.layout.name, a.scheme.name)
	if state == l.menuState {
		return
	}
	l.menuState = state
	check := func(id string, on bool) {
		if it := l.menu.ItemByID(id); it != nil {
			it.SetChecked(on)
		}
	}
	check("toggle_side_bar", a.side != noPanel)
	check("toggle_word_wrap", a.set.WordWrap)
	check(a.layout.id(), true)
	check("scheme "+a.scheme.name, true)
}

// pauseMenu takes the keys away from the menu while a key is recorded,
// as the menu would take them first.
func (l *lime) pauseMenu(paused bool) {
	if l.menuPaused != paused {
		l.menuPaused = paused
		l.refreshMenu()
	}
}

// refreshMenu makes the menu anew, with its keys as they are.
func (l *lime) refreshMenu() {
	if l.menu == nil {
		return
	}
	l.menu, l.menuState = l.newMenu(), ""
	mygo.App.SetMenu(l.menu)
	if l.last != nil {
		l.last.syncMenu()
	}
}

// menuKey is the accelerator of the menu item running the command named
// id: its first key that is not a chord, when it runs it.
func (l *lime) menuKey(id string) string {
	if l.menuPaused {
		return ""
	}
	for _, k := range l.keys.keys[id] {
		if !k.chord && l.keys.runs(k) {
			return accelerator(k.mods, k.key)
		}
	}
	return ""
}

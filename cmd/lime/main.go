// Lime is a plain text editor in the manner of Sublime Text, without
// packages: tabs, groups, Goto Anything, the command palette, a side bar
// of folders, syntax highlighting, word wrap, several carets and column
// selection, on Sublime Text's keys.
package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// menuItem runs the command named id in the focused window, showing its
// key.
func (l *lime) menuItem(label, id string) *mygo.MenuItem {
	return &mygo.MenuItem{Label: label, Accelerator: l.menuKey(id), Click: func(_ *mygo.MenuItem, w *mygo.Window) {
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
			l.menuItem(locales.MenuReopenClosedFile(), "reopen_last_file"),
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
	state := fmt.Sprint(a.set.SideBar, a.set.WordWrap, a.layout.name, a.scheme.name)
	if state == l.menuState {
		return
	}
	l.menuState = state
	check := func(id string, on bool) {
		if it := l.menu.ItemByID(id); it != nil {
			it.SetChecked(on)
		}
	}
	check("toggle_side_bar", a.set.SideBar)
	check("toggle_word_wrap", a.set.WordWrap)
	check(a.layout.id(), true)
	check("scheme "+a.scheme.name, true)
}

// makeWindow opens the window of a, at r unless r is empty, the first
// window where it was last.
func (a *app) makeWindow(r mygo.Rectangle) {
	o := mygo.WindowOptions{Title: "Lime", Width: 1100, Height: 760, MinWidth: 480, MinHeight: 320, Content: ui.View(a.view)}
	switch {
	case a == a.l.wins[0]:
		o.StateKey = "main"
	case r.Width > 0:
		o.X, o.Y, o.Width, o.Height = r.X, r.Y, r.Width, r.Height
	}
	if runtime.GOOS == "darwin" {
		o.Vibrancy, o.TitleBarStyle = mygo.VibrancySidebar, mygo.TitleBarHidden
	}
	a.win = mygo.NewWindow(o)
	a.win.OnFocus(func() {
		a.l.last, a.l.menuState = a, ""
		a.syncMenu()
	})
	a.win.OnClose(a.closing)
	a.win.OnClosed(func() { a.l.closed(a) })
}

func main() {
	l := newLime(dataDir())
	l.restoreSession()
	if len(l.wins) == 0 {
		l.newWindow()
	}
	l.themeSource = themeSource(l.set.Appearance)
	mygo.Theme.SetSource(l.themeSource)
	mygo.App.SetLocale(l.set.locale())
	var paths []string
	for _, arg := range os.Args[1:] {
		if !strings.HasPrefix(arg, "-") {
			paths = append(paths, arg)
		}
	}
	l.last.open(paths)
	for _, a := range l.wins {
		if a.docCount() == 0 {
			a.newFile()
		}
	}
	mygo.App.WhenReady(func() {
		l.menu = l.newMenu()
		mygo.App.SetMenu(l.menu)
		// The menu's labels are said once, in the language shown as it is
		// made.
		mygo.App.OnLocaleChanged(func(string) { l.refreshMenu() })
		for _, a := range l.wins {
			a.makeWindow(a.bounds)
		}
		mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) { l.quit(e) })
		go func() {
			for range time.Tick(3 * time.Second) {
				mygo.RunOnMain(l.autosave)
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

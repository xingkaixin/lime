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
	"github.com/egoist/mygo/ui"
)

// menuItem runs the command named id in the focused window, showing its
// key.
func (l *lime) menuItem(label, id string) *mygo.MenuItem {
	return &mygo.MenuItem{Label: label, Accelerator: menuAccelerator(id), Click: func(_ *mygo.MenuItem, w *mygo.Window) {
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

func menuAccelerator(id string) string {
	if c := commandByID[id]; c != nil {
		if k, ok := c.key(); ok && !k.chord {
			return accelerator(k.mods, k.key)
		}
	}
	return ""
}

func (l *lime) newMenu() *mygo.Menu {
	var layoutItems, schemeItems []*mygo.MenuItem
	for _, lo := range layouts {
		layoutItems = append(layoutItems, l.check(lo.name, lo.id(), mygo.MenuItemRadio))
	}
	for _, name := range schemeNames() {
		it := l.check(name, "scheme "+name, mygo.MenuItemRadio)
		it.Click = func(_ *mygo.MenuItem, w *mygo.Window) {
			if a := l.at(w); a != nil {
				a.win.Update(func() { a.setScheme(name) })
			}
		}
		schemeItems = append(schemeItems, it)
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			l.menuItem("New File", "new_file"),
			l.menuItem("New Window", "new_window"),
			l.menuItem("Open…", "prompt_open"),
			l.menuItem("Open Folder…", "prompt_open_folder"),
			l.menuItem("Reopen Closed File", "reopen_last_file"),
			mygo.Separator(),
			l.menuItem("Save", "save"),
			l.menuItem("Save As…", "prompt_save_as"),
			l.menuItem("Save All", "save_all"),
			mygo.Separator(),
			l.menuItem("Close File", "close"),
			l.menuItem("Close Window", "close_window"),
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "Find", Submenu: []*mygo.MenuItem{
			l.menuItem("Find…", "show_panel find"),
			l.menuItem("Find Next", "find_next"),
			l.menuItem("Find Previous", "find_prev"),
			mygo.Separator(),
			l.menuItem("Replace…", "show_panel replace"),
			mygo.Separator(),
			l.menuItem("Quick Find All", "find_all_under"),
			l.menuItem("Quick Add Next", "find_under_expand"),
			l.menuItem("Quick Skip Next", "find_under_expand_skip"),
			mygo.Separator(),
			l.menuItem("Use Selection for Find", "slurp_find_string"),
		}},
		{Label: "View", Submenu: []*mygo.MenuItem{
			l.check("Side Bar", "toggle_side_bar", mygo.MenuItemCheckbox),
			mygo.Separator(),
			{Label: "Layout", Submenu: layoutItems},
			{Label: "Groups", Submenu: []*mygo.MenuItem{
				l.menuItem("New Group", "new_pane"),
				l.menuItem("Close Group", "close_pane"),
			}},
			{Label: "Color Scheme", Submenu: schemeItems},
			l.menuItem("Syntax…", "select_syntax"),
			mygo.Separator(),
			l.check("Word Wrap", "toggle_word_wrap", mygo.MenuItemCheckbox),
			mygo.Separator(),
			l.menuItem("Larger Font", "increase_font_size"),
			l.menuItem("Smaller Font", "decrease_font_size"),
			l.menuItem("Reset Font Size", "reset_font_size"),
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Label: "Goto", Submenu: []*mygo.MenuItem{
			l.menuItem("Goto Anything…", "show_goto"),
			l.menuItem("Goto Line…", "goto_line"),
			l.menuItem("Command Palette…", "show_command_palette"),
			mygo.Separator(),
			l.menuItem("Next Modification", "next_modification"),
			l.menuItem("Previous Modification", "prev_modification"),
		}},
		{Label: "Project", Submenu: []*mygo.MenuItem{
			l.menuItem("Open Project…", "prompt_open_project"),
			l.menuItem("Save Project As…", "save_project_as"),
			l.menuItem("Close Project", "close_project"),
			mygo.Separator(),
			l.menuItem("Add Folder to Project…", "prompt_add_folder"),
			l.menuItem("Refresh Folders", "refresh_folders"),
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
	state := fmt.Sprint(a.set.SideBar, a.set.WordWrap, a.layout.name, a.set.ColorScheme)
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
	check("scheme "+a.set.ColorScheme, true)
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
	l.themeSource = themeSource(l.set.ColorScheme)
	mygo.Theme.SetSource(l.themeSource)
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

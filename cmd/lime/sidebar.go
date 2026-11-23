package main

import (
	"path/filepath"
	"runtime"
	"slices"

	"github.com/egoist/mygo/ui"

	"lime/locales"
)

func (a *app) sideBar(c *ui.Context) {
	if a.proj.jump {
		c.DisableAnimations()
		a.proj.jump = false
	}
	ch := a.shell(c)
	cur := a.current()
	ui.Column(c).Fill().Background(ch.bg).ContextMenu(a.projectMenu).Children(func() {
		a.sideBarTitle(c)
		switch {
		case a.side == gitPanel && a.git.root == "":
			ui.Column(c).Grow(1).FillWidth().Padding(16).Center().Children(func() {
				ui.Text(c, locales.SideBarNoRepository()).FontSize(12).TextColor(ch.muted).TextAlign(ui.Center)
			})
		case a.side == gitPanel:
			a.gitTabs(c)
			if a.gitTab == historyTab {
				if a.hist.root != a.git.root {
					a.readHistory()
				}
				a.historyView(c)
				break
			}
			if a.changes.root != a.git.root {
				a.readChanges()
			}
			a.changesView(c)
		case len(a.proj.folders) == 0:
			a.sideBarEmpty(c)
		default:
			if !slices.Equal(a.marks.folders, a.proj.folders) {
				a.readMarks()
			}
			a.filesTree(c, cur)
		}
	})
	a.proj.forgetHidden()
}

type sidePanel int

const (
	noPanel sidePanel = iota
	filesPanel
	gitPanel
)

// togglePanel shows p, or hides the side bar while it shows p, as the
// panel buttons of Zed's status bar do.
func (a *app) togglePanel(p sidePanel) {
	if a.side == p {
		p = noPanel
	}
	a.showPanel(p)
}

// showPanel shows p in the window's side bar, and new windows open with
// the side bar as it is now.
func (a *app) showPanel(p sidePanel) {
	if p == gitPanel && a.side != gitPanel {
		a.readHistory()
		a.readChanges()
	}
	a.side = p
	a.set.SideBar = p != noPanel
	a.l.saveSettings()
}

// sideBarTitle is the side bar's part of the window's top, level with the
// tab bars: the work tree and the branch of the project's repository,
// after the window controls, as Zed's title bar shows them.
func (a *app) sideBarTitle(c *ui.Context) {
	band := titleBand(c)
	if a.git.root == "" {
		if band > 0 {
			ui.Box(c).FillWidth().Height(band).Shrink(0).DragWindow()
		}
		return
	}
	ui.Row(c).FillWidth().Height(max(band, tabBarHeight)).Shrink(0).Gap(2).AlignItems(ui.Stretch).Children(func() {
		lead := ui.Box(c).Width(c.TitleBar().Left + controlsGap/2).Shrink(0)
		// The branch gives way first: the work tree's name is most often main.
		a.gitButton(c, worktreeIcon, a.git.tree, locales.GitSwitchWorktree(), a.showWorktrees).Shrink(0).MaxWidth(120)
		a.gitButton(c, branchIcon, a.git.branch, locales.GitSwitchBranch(), a.showBranches)
		a.syncButton(c)
		if band > 0 {
			lead.DragWindow()
			ui.Box(c).Grow(1).DragWindow()
		}
	})
}

func (a *app) sideBarEmpty(c *ui.Context) {
	ch := a.shell(c)
	area := ui.Column(c).Grow(1).FillWidth().Margin(8).Padding(16).Gap(12).Center().Radius(c.Theme().Radius).
		Transition(colorFade)
	if files := area.DroppedFiles(); files != nil {
		a.open(files)
	}
	if area.FileDragOver() {
		area.Border(1, a.scheme.accent).Background(a.scheme.accent.Alpha(0.08))
	}
	area.Children(func() {
		ui.Text(c, locales.SideBarDropFolder()).FontSize(12).TextColor(ch.muted).TextAlign(ui.Center)
		a.button(c, locales.MenuOpenFolder(), a.promptAddFolder)
	})
}

var filesIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<g fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round">
		<path d="M2 2.5v8A1.5 1.5 0 003.5 12H7M2 5.5A1.5 1.5 0 003.5 7H7M5 2.5h9M9.5 7H14M9.5 12H14" />
	</g>
</svg>`))

func (a *app) panelButtons(c *ui.Context) {
	ui.Row(c).Gap(2).Children(func() {
		a.panelButton(c, filesPanel, filesIcon, locales.SideBarFilesPanel(), "toggle_files_panel")
		a.panelButton(c, gitPanel, branchIcon, locales.SideBarGitPanel(), "toggle_git_panel")
	})
}

func (a *app) panelButton(c *ui.Context, p sidePanel, icon *ui.SVG, label, id string) {
	ch := a.shell(c)
	on := a.side == p
	tint := ch.muted
	if on {
		tint = ch.text
	}
	tip := label
	if k := a.l.keys.text(id); k != "" {
		tip += "  " + k
	}
	btn := ui.ToggleBase(c.Key(id), &on).Width(22).Height(20).Radius(4).Center().Transition(colorFade).
		Label(label).Tooltip(tip).OnClick(func() { a.run(id) })
	if btn.Hovered() {
		btn.Background(ch.hover)
	}
	btn.Children(func() {
		ui.Icon(c, icon).Size(treeIcon, treeIcon).TextColor(tint)
	})
}

var (
	worktreeIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<g fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round">
		<path d="M4.5 4.5H12c.83 0 1.5.67 1.5 1.5v6c0 .83-.67 1.5-1.5 1.5H2A1.5 1.5 0 01.5 12V3.5a1 1 0 011-1h5a1 1 0 011 1v1" />
		<circle cx="7" cy="9" r="1.5" />
		<path d="M8.5 9h3" />
	</g>
</svg>`))
	branchIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<g fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round">
		<circle cx="4.5" cy="3.5" r="1.5" />
		<circle cx="4.5" cy="12.5" r="1.5" />
		<circle cx="11.5" cy="5.5" r="1.5" />
		<path d="M4.5 5v6M11.5 7c0 2.5-2 3-7 4" />
	</g>
</svg>`))
	commitIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<g fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round">
		<circle cx="8" cy="8" r="2.5" />
		<path d="M.5 8h5M10.5 8h5" />
	</g>
</svg>`))
)

func (a *app) gitButton(c *ui.Context, icon *ui.SVG, text, label string, onClick func()) ui.Element {
	ch := a.shell(c)
	btn := ui.ButtonBase(c.Key(label)).Height(22).AlignSelf(ui.Center).Padding(0, 6).Gap(5).Radius(4).AlignItems(ui.Center).
		Shrink(1).MinWidth(0).ClipX().Transition(colorFade).Label(label).Tooltip(label).OnClick(onClick)
	if btn.Hovered() {
		btn.Background(ch.hover)
	}
	btn.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14).Shrink(0).TextColor(ch.muted)
		}
		ui.Text(c, text).FontSize(12).TextColor(ch.muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	})
	return btn
}

// treeIcon and treeGap size a row's icon and the room after it, which
// each level indents by, as Sublime Text's side bar does.
const treeIcon, treeGap = 16, 6

var treeMotion = ui.ElementTransition{Position: true, Colors: true, Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}}

type treeDrag string

// copyMods held as a drag is let go copy rather than move, as Finder's
// Option and Explorer's Ctrl do.
var copyMods = func() ui.Modifiers {
	if runtime.GOOS == "darwin" {
		return ui.Alt
	}
	return ui.Ctrl
}()

func (a *app) flattenTree() []treeRowItem {
	rows := a.proj.treeRows[:0]
	var walk func(root int, dir, name string, depth int)
	walk = func(root int, dir, name string, depth int) {
		open := a.proj.isOpen(dir, depth == 0)
		rows = append(rows, treeRowItem{
			root: root, path: dir, name: name, parent: dir, depth: depth, isDir: true, open: open,
		})
		if !*open {
			return
		}
		if cr := a.proj.create; cr != nil && cr.dir == dir {
			rows = append(rows, treeRowItem{
				root: root, path: dir + "\x00create", parent: dir, depth: depth + 1, create: cr,
			})
		}
		for _, e := range a.shownDirItems(dir) {
			if e.isDir {
				walk(root, e.path, e.name, depth+1)
				continue
			}
			rows = append(rows, treeRowItem{
				root: root, path: e.path, name: e.name, parent: dir, depth: depth + 1,
			})
		}
	}
	for i, f := range a.proj.folders {
		walk(i, f, filepath.Base(f), 0)
	}
	a.proj.treeRows = rows
	return rows
}

func (a *app) filesTree(c *ui.Context, cur *doc) {
	rows := a.flattenTree()
	if a.revealPath != "" {
		for i := range rows {
			if rows[i].create == nil && rows[i].path == a.revealPath {
				a.proj.listState.ScrollIntoView(i)
				break
			}
		}
		a.revealPath = ""
	}
	curPath := ""
	if cur != nil {
		curPath = cur.path
	}
	list := ui.List(c.Key("files"), &a.proj.listState, len(rows)).
		ItemKey(func(i int) any {
			if rows[i].create != nil {
				return rows[i].create
			}
			return treeRowKey{rows[i].root, rows[i].path}
		}).
		Rows(func(r ui.ListRow) {
			it := rows[r.Index]
			if it.create != nil {
				a.treeInput(r.Context, it.create, it.depth)
				return
			}
			if it.isDir {
				open := it.open
				row := a.treeRow(r.Context, it.path, it.name, it.depth, a.folderIcon(*open), false, r.Hovered()).
					Expanded(*open).
					OnClick(func() { *open = !*open }).
					ContextMenu(func(m *ui.Menu) { a.folderMenu(r.Context, m, it.path, it.depth == 0) })
				if p, ok := ui.Drop[treeDrag](row); ok && canDrop(string(p), it.path) {
					a.dropInto(string(p), it.path, r.Context.Modifiers()&copyMods != 0)
				}
				if p, ok := ui.DragOver[treeDrag](row); ok && canDrop(string(p), it.path) {
					row.Background(a.scheme.accent.Alpha(0.08))
				}
				if it.depth > 0 {
					treeSource(r.Context, row, it.path)
				}
				return
			}
			row := a.treeRow(r.Context, it.path, it.name, it.depth, a.fileIcon(it.path), curPath != "" && curPath == it.path, r.Hovered()).
				OnClick(func() { a.open([]string{it.path}) }).
				ContextMenu(func(m *ui.Menu) { a.fileMenu(r.Context, m, it.path) })
			if p, ok := ui.Drop[treeDrag](row); ok && canDrop(string(p), it.parent) {
				a.dropInto(string(p), it.parent, r.Context.Modifiers()&copyMods != 0)
			}
			if p, ok := ui.DragOver[treeDrag](row); ok && canDrop(string(p), it.parent) {
				row.Background(a.scheme.accent.Alpha(0.08))
			}
			treeSource(r.Context, row, it.path)
		})
	list.Grow(1).FillWidth().Role(ui.RoleTree)
}

// treeSource makes row drag path, showing the copy cursor while copyMods
// are held. The pointer takes the cursor of the row it presses.
func treeSource(c *ui.Context, row ui.Element, path string) {
	row.Drag(treeDrag(path))
	if row.Dragging() && c.Modifiers()&copyMods != 0 {
		row.Cursor(ui.CursorCopy)
	}
}

func (a *app) treeInput(c *ui.Context, cr *creation, depth int) {
	t := c.Theme()
	ic := a.fileIcon(filepath.Join(cr.dir, cr.name))
	if cr.folder {
		ic = a.folderIcon(false)
	}
	tint := ic.tint(a.scheme.chrome().muted)
	ui.Row(c.Key(cr)).FillWidth().Height(t.Space(7)).Gap(treeGap).AlignItems(ui.Center).
		Padding(0, t.Space(2)).PaddingStart(t.Space(2) + (treeIcon+treeGap)*float32(depth)).Transition(treeMotion).
		Children(func() {
			ic.view(c, treeIcon, tint)
			pad := t.Space(0.5)
			in := ui.TextInputBase(c, &cr.name).AutoFocus().Grow(1).MinWidth(0).Padding(pad).
				Margin(0, -pad-1).Radius(t.Space(1)).Background(t.Background).Border(1, t.Accent).TextColor(t.Text).
				OnSubmit(a.finishCreate).OnShortcut(0, ui.KeyEscape, func() { a.proj.create = nil })
			if cr.folder {
				in.Label(locales.MenuNewFolder())
			} else {
				in.Label(locales.MenuNewFile())
			}
			switch {
			case in.PressedOutside() || cr.focused && !in.FocusWithin():
				a.proj.create = nil
			case in.FocusWithin():
				cr.focused = true
			}
		})
}

func (a *app) treeRow(c *ui.Context, path, name string, depth int, ic rowIcon, selected, hovered bool) ui.Element {
	t := c.Theme()
	row := ui.Row(c).FillWidth().Height(t.Space(7)).Gap(treeGap).AlignItems(ui.Center).
		Padding(0, t.Space(2)).PaddingStart(t.Space(2) + (treeIcon+treeGap)*float32(depth)).Radius(t.Radius).
		Role(ui.RoleTreeItem).Level(depth + 1).Label(name).Selected(selected)
	tint, text := ic.tint(a.scheme.chrome().muted), t.Text
	if selected {
		tint, text = t.AccentText, t.AccentText
	} else {
		if hovered {
			hover := t.SurfaceHover
			if c.Vibrancy() {
				hover = a.shell(c).hover
			}
			row.Background(hover)
		}
		switch m := a.marks.mark(path); m {
		case gitClean:
		case gitIgnored:
			tint, text = tint.Alpha(0.5), text.Alpha(0.5)
		default:
			text = a.scheme.gitTint(m)
		}
	}
	row.Children(func() {
		ic.view(c, treeIcon, tint)
		ui.Text(c, name).TextColor(text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	})
	return row
}

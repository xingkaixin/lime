package main

import (
	"path/filepath"
	"runtime"

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
		if band := titleBand(c); band > 0 {
			ui.Box(c).FillWidth().Height(band).Shrink(0).DragWindow()
		}
		ui.Text(c, locales.SideBarFolders()).FontSize(11).TextColor(ch.muted).Padding(10, 14, 6)
		if len(a.proj.folders) == 0 {
			a.sideBarEmpty(c)
			return
		}
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Role(ui.RoleTree).Children(func() {
				for _, f := range a.proj.folders {
					a.treeDir(c, f, 0, cur)
				}
			})
		})
	})
}

// sideBarEmpty asks for a folder, opened from its button or dropped on it.
func (a *app) sideBarEmpty(c *ui.Context) {
	ch := a.shell(c)
	area := ui.Column(c).Grow(1).FillWidth().Margin(0, 8, 8).Padding(16).Gap(12).Center().Radius(c.Theme().Radius).
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

// sideBarIcon is the status bar's button for the side bar, drawn as the
// folder icons are.
var sideBarIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<g fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round">
		<rect x="1.5" y="2.5" width="13" height="11" rx="1.5" />
		<path d="M6 2.5v11" />
	</g>
</svg>`))

// sideBarButton shows and hides the side bar, as Zed's status bar does.
func (a *app) sideBarButton(c *ui.Context) {
	ch := a.shell(c)
	btn := ui.ButtonBase(c).Width(22).Height(20).Radius(4).Center().Transition(colorFade).
		Label(locales.StatusSideBar()).Tooltip(locales.StatusSideBar()).OnClick(func() { a.run("toggle_side_bar") })
	if btn.Hovered() {
		btn.Background(ch.hover)
	}
	tint := ch.muted
	if a.set.SideBar {
		tint = ch.text
	}
	btn.Children(func() {
		ui.Icon(c, sideBarIcon).Size(treeIcon, treeIcon).TextColor(tint)
	})
}

// treeIcon and treeGap size a row's icon and the room after it, which
// each level indents by, as Sublime Text's side bar does.
const treeIcon, treeGap = 16, 6

// treeMotion slides the rows of the side bar as folders above them open
// and close, and fades their hover. What a folder holds grows in and
// collapses as one block, which the rows below follow.
var treeMotion = ui.ElementTransition{Position: true, Colors: true, Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}}

// treeDrag is a file or folder of the side bar dragged to another folder.
type treeDrag string

// treeBox keys the box of a folder's row and what it holds, apart from
// both.
type treeBox string

// copyMods held as a drag is let go copy rather than move, as Finder's
// Option and Explorer's Ctrl do.
var copyMods = func() ui.Modifiers {
	if runtime.GOOS == "darwin" {
		return ui.Alt
	}
	return ui.Ctrl
}()

// treeDir shows dir, and what it holds while open. A click anywhere on its
// row opens or closes it. Files and folders dragged anywhere on it go into
// it.
func (a *app) treeDir(c *ui.Context, dir string, depth int, cur *doc) {
	open := a.proj.isOpen(dir, depth == 0)
	ic := rowIcon{svg: folderIcon}
	if *open {
		ic = rowIcon{svg: folderOpenIcon, color: a.scheme.accent}
	}
	box := ui.Column(c.Key(treeBox(dir))).FillWidth().Radius(c.Theme().Radius).Transition(colorFade)
	if p, ok := ui.Drop[treeDrag](box); ok && canDrop(string(p), dir) {
		a.dropInto(string(p), dir, c.Modifiers()&copyMods != 0)
	}
	if p, ok := ui.DragOver[treeDrag](box); ok && canDrop(string(p), dir) {
		box.Background(a.scheme.accent.Alpha(0.08))
	}
	box.Children(func() {
		row := a.treeRow(c, dir, depth, ic, false).Expanded(*open).OnClick(func() { *open = !*open }).
			ContextMenu(func(m *ui.Menu) { a.folderMenu(c, m, dir, depth == 0) })
		if depth > 0 {
			treeSource(c, row, dir)
		}
		if !*open {
			return
		}
		// Keyed dir/, apart from dir's row.
		ui.Column(c.Key(dir + string(filepath.Separator))).FillWidth().Transition(treeMotion).Children(func() {
			if cr := a.proj.create; cr != nil && cr.dir == dir {
				a.treeInput(c, cr, depth+1)
			}
			for _, e := range a.proj.list(dir) {
				p := filepath.Join(dir, e.Name())
				if e.IsDir() {
					a.treeDir(c, p, depth+1, cur)
					continue
				}
				row := a.treeRow(c, p, depth+1, fileIcon(p, a.scheme.dark), cur != nil && cur.path == p).
					OnClick(func() { a.open([]string{p}) }).
					ContextMenu(func(m *ui.Menu) { a.fileMenu(c, m, p) })
				treeSource(c, row, p)
			}
		})
	})
}

// treeSource makes row drag path, showing the copy cursor while copyMods
// are held. The pointer takes the cursor of the row it presses.
func treeSource(c *ui.Context, row ui.Element, path string) {
	row.Drag(treeDrag(path))
	if row.Dragging() && c.Modifiers()&copyMods != 0 {
		row.Cursor(ui.CursorCopy)
	}
}

// treeInput names a new file or folder in a row of the tree. Enter makes
// it; Escape, a press elsewhere or the focus leaving cancels it.
func (a *app) treeInput(c *ui.Context, cr *creation, depth int) {
	t := c.Theme()
	ic := fileIcon(filepath.Join(cr.dir, cr.name), a.scheme.dark)
	if cr.folder {
		ic = rowIcon{svg: folderIcon}
	}
	tint := ic.color
	if tint == (ui.Color{}) {
		tint = a.scheme.chrome().muted
	}
	ui.Row(c.Key(cr)).FillWidth().Height(t.Space(7)).Gap(treeGap).AlignItems(ui.Center).
		Padding(0, t.Space(2)).PaddingStart(t.Space(2) + (treeIcon+treeGap)*float32(depth)).Transition(treeMotion).
		Children(func() {
			ic.view(c, tint)
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

func (a *app) treeRow(c *ui.Context, path string, depth int, ic rowIcon, selected bool) ui.Element {
	t := c.Theme()
	name := filepath.Base(path)
	motion := treeMotion
	// The selection shows at once, as its text turns AccentText at once.
	motion.Colors = !selected
	row := ui.Row(c.Key(path)).FillWidth().Height(t.Space(7)).Gap(treeGap).AlignItems(ui.Center).
		Padding(0, t.Space(2)).PaddingStart(t.Space(2) + (treeIcon+treeGap)*float32(depth)).Radius(t.Radius).
		Role(ui.RoleTreeItem).Level(depth + 1).Label(name).Selected(selected).Transition(motion)
	tint := ic.color
	if tint == (ui.Color{}) {
		tint = a.scheme.chrome().muted
	}
	if selected {
		tint = t.AccentText
	} else if row.Hovered() {
		hover := t.SurfaceHover
		if c.Vibrancy() {
			hover = a.shell(c).hover
		}
		row.Background(hover)
	}
	row.Children(func() {
		ic.view(c, tint)
		ui.Text(c, name).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	})
	return row
}

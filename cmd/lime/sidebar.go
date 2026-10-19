package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// Sublime Text's default patterns of what folders leave out: of the side
// bar, and binaryFiles of Goto Anything too.
var (
	folderExclude = []string{".svn", ".git", ".hg", "CVS", ".Trash", ".Trash-*"}
	fileExclude   = []string{"*.pyc", "*.pyo", "*.exe", "*.dll", "*.obj", "*.o", "*.a", "*.lib", "*.so", "*.dylib",
		"*.ncb", "*.sdf", "*.suo", "*.pdb", "*.idb", ".DS_Store", ".directory", "desktop.ini", "*.class", "*.psd", "*.db",
		"*.sublime-workspace"}
	binaryFiles = []string{"*.jpg", "*.jpeg", "*.png", "*.gif", "*.ttf", "*.tga", "*.dds", "*.ico", "*.eot", "*.pdf",
		"*.swf", "*.jar", "*.zip"}
)

// maxIndexed bounds the files Goto Anything lists, for a folder as large
// as a home directory.
const maxIndexed = 50000

func excluded(name string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

// project is the folders of the window, kept in a .sublime-project file
// once saved.
type project struct {
	path    string
	folders []string
	open    map[string]*bool         // folders of the side bar shown open
	dirs    map[string][]os.DirEntry // folders read, for the side bar
	files   []string                 // the folders' files, for Goto Anything
	gen     int                      // counts the indexings, for the last to win
	// jump has the side bar show the next frame without motion: another
	// project or a rename replaced the rows' paths, not their places.
	jump bool
}

// projectFile is a .sublime-project file, of which Lime reads the folders.
type projectFile struct {
	Folders []struct {
		Path string `json:"path"`
	} `json:"folders"`
}

// rel shows path from its folder, as Goto Anything does.
func (p *project) rel(path string) string {
	for _, f := range p.folders {
		if r, err := filepath.Rel(filepath.Dir(f), path); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return tildePath(path)
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

func (p *project) isOpen(dir string, open bool) *bool {
	if p.open == nil {
		p.open = make(map[string]*bool)
	}
	o := p.open[dir]
	if o == nil {
		o = &open
		p.open[dir] = o
	}
	return o
}

// list reads dir as the side bar shows it: folders first, then files, by
// name.
func (p *project) list(dir string) []os.DirEntry {
	if es, ok := p.dirs[dir]; ok {
		return es
	}
	es, _ := os.ReadDir(dir)
	es = slices.DeleteFunc(es, func(e os.DirEntry) bool {
		if e.IsDir() {
			return excluded(e.Name(), folderExclude)
		}
		return excluded(e.Name(), fileExclude)
	})
	slices.SortStableFunc(es, func(x, y os.DirEntry) int {
		if x.IsDir() != y.IsDir() {
			if x.IsDir() {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(x.Name()), strings.ToLower(y.Name()))
	})
	if p.dirs == nil {
		p.dirs = make(map[string][]os.DirEntry)
	}
	p.dirs[dir] = es
	return es
}

func walkFolders(folders []string) []string {
	var files []string
	for _, f := range folders {
		filepath.WalkDir(f, func(path string, e fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return nil
			case len(files) >= maxIndexed:
				return filepath.SkipAll
			case e.IsDir():
				if path != f && excluded(e.Name(), folderExclude) {
					return filepath.SkipDir
				}
			case !excluded(e.Name(), fileExclude) && !excluded(e.Name(), binaryFiles):
				files = append(files, path)
			}
			return nil
		})
	}
	return files
}

// indexFolders lists the folders' files for Goto Anything, off the main
// thread in the app.
func (a *app) indexFolders() {
	a.proj.gen++
	gen, folders := a.proj.gen, slices.Clone(a.proj.folders)
	if a.win == nil {
		a.proj.files = walkFolders(folders)
		return
	}
	go func() {
		files := walkFolders(folders)
		a.win.Update(func() {
			if gen == a.proj.gen {
				a.proj.files = files
				if a.panel != nil {
					a.panel.stale = true
				}
			}
		})
	}()
}

func (a *app) addFolder(dir string) {
	if slices.Contains(a.proj.folders, dir) {
		return
	}
	a.proj.folders = append(a.proj.folders, dir)
	a.set.SideBar = true
	a.indexFolders()
}

func (a *app) promptAddFolder() {
	go func() {
		dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Directory: true, Multiple: true})
		if err != nil || len(dirs) == 0 {
			return
		}
		a.win.Update(func() {
			for _, d := range dirs {
				a.addFolder(d)
			}
		})
	}()
}

func (a *app) refreshFolders() {
	a.proj.dirs = nil
	a.indexFolders()
}

func (a *app) closeProject() {
	a.proj = project{gen: a.proj.gen + 1}
}

func (a *app) openProject(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var pf projectFile
	if err := json.Unmarshal(b, &pf); err != nil {
		return err
	}
	a.closeProject()
	a.proj.path, a.proj.jump = path, true
	for _, f := range pf.Folders {
		p := filepath.FromSlash(f.Path)
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(path), p)
		}
		a.proj.folders = append(a.proj.folders, filepath.Clean(p))
	}
	a.set.SideBar = true
	a.indexFolders()
	return nil
}

// saveProject writes the folders to path, relative to it as Sublime Text
// writes them.
func (a *app) saveProject(path string) error {
	var pf projectFile
	for _, f := range a.proj.folders {
		p := f
		if r, err := filepath.Rel(filepath.Dir(path), f); err == nil {
			p = r
		}
		pf.Folders = append(pf.Folders, struct {
			Path string `json:"path"`
		}{filepath.ToSlash(p)})
	}
	b, err := json.MarshalIndent(pf, "", "\t")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return err
	}
	a.proj.path = path
	return nil
}

func projectFilter() []mygo.FileFilter {
	return []mygo.FileFilter{{Name: locales.FilterProject(), Extensions: []string{"sublime-project"}}}
}

func (a *app) promptOpenProject() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Filters: projectFilter()})
		if err != nil || len(paths) == 0 {
			return
		}
		a.win.Update(func() {
			if err := a.openProject(paths[0]); err != nil {
				go mygo.Dialog.Error(locales.DialogUnableToOpenProject(), err.Error())
			}
		})
	}()
}

func (a *app) promptSaveProject() {
	name := "untitled.sublime-project"
	if len(a.proj.folders) > 0 {
		name = filepath.Join(a.proj.folders[0], filepath.Base(a.proj.folders[0])+".sublime-project")
	}
	if a.proj.path != "" {
		name = a.proj.path
	}
	go func() {
		p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, DefaultPath: name, Filters: projectFilter()})
		if err != nil || p == "" {
			return
		}
		a.win.Update(func() {
			if err := a.saveProject(p); err != nil {
				go mygo.Dialog.Error(locales.DialogUnableToSaveProject(), err.Error())
			}
		})
	}()
}

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

// treeIcon and treeGap size a row's icon and the room after it, which
// each level indents by, as Sublime Text's side bar does.
const treeIcon, treeGap = 16, 6

// treeMotion slides the rows of the side bar as folders above them open
// and close, and fades their hover. What a folder holds grows in and
// collapses as one block, which the rows below follow.
var treeMotion = ui.ElementTransition{Position: true, Colors: true, Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}}

// treeDir shows dir, and what it holds while open. A click anywhere on its
// row opens or closes it.
func (a *app) treeDir(c *ui.Context, dir string, depth int, cur *doc) {
	open := a.proj.isOpen(dir, depth == 0)
	ic, tint := icon("_folder"), a.scheme.chrome().muted
	if *open {
		ic, tint = icon("_folder_open"), a.scheme.accent
	}
	a.treeRow(c, dir, depth, ic, tint, false).Expanded(*open).OnClick(func() { *open = !*open }).
		ContextMenu(func(m *ui.Menu) { a.folderMenu(c, m, dir, depth == 0) })
	if !*open {
		return
	}
	// Keyed dir/, apart from dir's row.
	ui.Column(c.Key(dir + string(filepath.Separator))).FillWidth().Transition(treeMotion).Children(func() {
		for _, e := range a.proj.list(dir) {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				a.treeDir(c, p, depth+1, cur)
				continue
			}
			a.treeRow(c, p, depth+1, fileIcon(e.Name()), a.scheme.chrome().muted, cur != nil && cur.path == p).
				OnClick(func() { a.open([]string{p}) }).
				ContextMenu(func(m *ui.Menu) { a.fileMenu(c, m, p) })
		}
	})
}

func (a *app) treeRow(c *ui.Context, path string, depth int, ic *ui.SVG, tint ui.Color, selected bool) ui.Element {
	t := c.Theme()
	name := filepath.Base(path)
	motion := treeMotion
	// The selection shows at once, as its text turns AccentText at once.
	motion.Colors = !selected
	row := ui.Row(c.Key(path)).FillWidth().Height(t.Space(7)).Gap(treeGap).AlignItems(ui.Center).
		Padding(0, t.Space(2)).PaddingStart(t.Space(2) + (treeIcon+treeGap)*float32(depth)).Radius(t.Radius).
		Role(ui.RoleTreeItem).Level(depth + 1).Label(name).Selected(selected).Transition(motion)
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
		ui.Icon(c, ic).Size(treeIcon, treeIcon).Shrink(0).TextColor(tint)
		ui.Text(c, name).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	})
	return row
}

package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"

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

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// moved returns p once from is renamed to.
func moved(p, from, to string) string {
	if within(p, from) {
		return to + p[len(from):]
	}
	return p
}

func (a *app) promptRename(path string) {
	a.openPanel(&panel{prompt: locales.PromptNewName(), query: filepath.Base(path), submit: func(name string) {
		if name == "" || name == filepath.Base(path) {
			return
		}
		to := filepath.Join(filepath.Dir(path), name)
		if err := renameNew(path, to); err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToRename(filepath.Base(path)), err.Error())
			return
		}
		a.renamed(path, to)
	}})
}

// renameNew renames from to to, but not over another file as os.Rename
// does. A to that is from differs from it by case only, on a file system
// that ignores case.
func renameNew(from, to string) error {
	if ti, err := os.Lstat(to); err == nil {
		if fi, err := os.Lstat(from); err != nil || !os.SameFile(ti, fi) {
			return &os.PathError{Op: "rename", Path: to, Err: fs.ErrExist}
		}
	}
	return os.Rename(from, to)
}

// renamed has the tabs of the files in from, its folders shown open and
// the project's folders follow it to to.
func (a *app) renamed(from, to string) {
	for _, d := range a.allDocs() {
		if p := moved(d.path, from, to); p != d.path {
			d.path = p
			d.st.Buffer.SetLanguage(editor.LanguageFor(p))
		}
		d.dir = moved(d.dir, from, to)
	}
	open := make(map[string]*bool, len(a.proj.open))
	for dir, o := range a.proj.open {
		open[moved(dir, from, to)] = o
	}
	a.proj.open = open
	for i, f := range a.proj.folders {
		a.proj.folders[i] = moved(f, from, to)
	}
	a.proj.jump = true
	a.refreshFolders()
}

func (a *app) promptNewFolder(dir string) {
	a.openPanel(&panel{prompt: locales.PromptFolderName(), submit: func(name string) {
		if name == "" {
			return
		}
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToCreate(name), err.Error())
			return
		}
		*a.proj.isOpen(dir, true) = true
		a.refreshFolders()
	}})
}

// trash moves path to the Trash, with the tabs of its files. It asks first
// for a folder, or when a tab has changes.
func (a *app) trash(path string, folder bool) {
	var docs []*doc
	for _, d := range a.allDocs() {
		if d.path != "" && within(d.path, path) {
			docs = append(docs, d)
		}
	}
	del := func() {
		if err := mygo.Shell.TrashItem(path); err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToDelete(filepath.Base(path)), err.Error())
			return
		}
		for _, d := range docs {
			a.take(d)
		}
		release(docs...)
		a.proj.folders = slices.DeleteFunc(a.proj.folders, func(f string) bool { return within(f, path) })
		a.refreshFolders()
	}
	var detail []string
	if folder {
		detail = append(detail, locales.DialogDeleteFolder())
	}
	if slices.ContainsFunc(docs, (*doc).dirty) {
		detail = append(detail, locales.DialogUnsavedLost())
	}
	if detail == nil {
		del()
		return
	}
	go func() {
		res, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  a.win,
			Type:    mygo.MessageWarning,
			Message: locales.DialogDelete(filepath.Base(path)),
			Detail:  strings.Join(detail, " "),
			Buttons: []string{locales.ButtonDelete(), locales.ButtonCancel()},
		})
		if res.Button == 0 {
			a.win.Update(del)
		}
	}()
}

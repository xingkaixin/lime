package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// leftOut reports whether the side bar leaves out the file or folder
// named name.
func leftOut(name string, dir bool) bool {
	if dir {
		return excluded(name, folderExclude)
	}
	return excluded(name, fileExclude)
}

// project is the folders of the window, kept in a .sublime-project file
// once saved.
type project struct {
	path    string
	folders []string
	open    map[string]*bool              // folders of the side bar shown open
	dirs    map[string][]os.DirEntry      // folders read, for the side bar
	watches map[string]context.CancelFunc // folders followed on disk while shown
	shown   map[string]bool               // folders the side bar listed as it built
	files   []string                      // the folders' files, for Goto Anything
	gen     int                           // counts the indexings, for the last to win
	create  *creation
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
	es = slices.DeleteFunc(es, func(e os.DirEntry) bool { return leftOut(e.Name(), e.IsDir()) })
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

// watchDirs says whether the side bar follows its open folders on disk.
// A watch on Windows holds a handle on each folder it covers, and Windows
// doesn't let a folder be renamed or moved while a handle is open beneath
// it. There the open folders are read again as the window takes the focus
// instead.
const watchDirs = runtime.GOOS != "windows"

// shownDir is what dir, which the side bar shows open, holds. While it
// shows, dir is followed on disk and read again as what it holds changes.
func (a *app) shownDir(dir string) []os.DirEntry {
	if a.proj.shown == nil {
		a.proj.shown = make(map[string]bool)
	}
	a.proj.shown[dir] = true
	if watchDirs && a.win != nil && a.proj.watches[dir] == nil {
		a.watchDir(dir)
	}
	return a.proj.list(dir)
}

// watchDir follows dir, dropping what was read of it as entries appear,
// go away or are renamed. A watch that can't start stays, so that dir
// isn't tried again while it shows. One that ends, as when dir goes away,
// is dropped with what was read, for dir to be read and followed anew.
func (a *app) watchDir(dir string) {
	if a.proj.watches == nil {
		a.proj.watches = make(map[string]context.CancelFunc)
	}
	ctx, stop := context.WithCancel(context.Background())
	a.proj.watches[dir] = stop
	changed := func(ended bool) {
		a.win.Update(func() {
			// Stopped: another watch, or none, follows dir now.
			if ctx.Err() != nil {
				return
			}
			delete(a.proj.dirs, dir)
			if ended {
				stop()
				delete(a.proj.watches, dir)
			}
		})
	}
	go func() {
		w, err := mygo.WatchFiles(ctx, dir, mygo.FileWatchOptions{Ignore: leftOut})
		if err != nil {
			return
		}
		defer w.Close()
		for {
			events, err := w.Next(ctx)
			if err != nil {
				changed(true)
				return
			}
			if slices.ContainsFunc(events, func(e mygo.FileEvent) bool { return e.Op != mygo.FileWrite }) {
				changed(false)
			}
		}
	}()
}

// forgetHidden stops following the folders the side bar no longer shows
// open, and drops what was read of them, which would go stale.
func (p *project) forgetHidden() {
	for dir, stop := range p.watches {
		if !p.shown[dir] {
			stop()
			delete(p.watches, dir)
		}
	}
	for dir := range p.dirs {
		if !p.shown[dir] {
			delete(p.dirs, dir)
		}
	}
	clear(p.shown)
}

// unwatch stops following the folders on disk.
func (p *project) unwatch() {
	for _, stop := range p.watches {
		stop()
	}
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
	if a.side == noPanel {
		a.side = filesPanel
	}
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
	a.proj.unwatch()
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
	if a.side == noPanel {
		a.side = filesPanel
	}
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

// canDrop reports whether path dragged in the side bar can go into dir:
// not where it is, nor into itself.
func canDrop(path, dir string) bool {
	return filepath.Dir(path) != dir && !within(dir, path)
}

// dropInto moves path into dir, or copies it. What dir holds by its name
// it asks first to replace, or to keep both, as Finder does.
func (a *app) dropInto(path, dir string, copy bool) {
	*a.proj.isOpen(dir, true) = true
	name := filepath.Base(path)
	to := filepath.Join(dir, name)
	if _, err := os.Lstat(to); err != nil {
		a.transfer(path, to, copy)
		return
	}
	detail := []string{locales.DialogReplaceDetail()}
	if slices.ContainsFunc(a.docsIn(to), (*doc).dirty) {
		detail = append(detail, locales.DialogUnsavedLost())
	}
	labels := []string{locales.ButtonReplace(), locales.ButtonKeepBoth()}
	choices := []func(){
		func() { a.replace(path, to, copy) },
		func() { a.transfer(path, keepBoth(to), copy) },
	}
	// Replacing the folder path is in would trash path too.
	if within(path, to) {
		labels, choices, detail = labels[1:], choices[1:], nil
	}
	labels = append(labels, locales.ButtonCancel())
	go func() {
		res, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:       a.win,
			Type:         mygo.MessageWarning,
			Message:      locales.DialogItemExists(name, filepath.Base(dir)),
			Detail:       strings.Join(detail, " "),
			Buttons:      labels,
			CancelButton: len(labels) - 1,
		})
		if res.Button < len(choices) {
			a.win.Update(choices[res.Button])
		}
	}()
}

// replace moves the item at to to the Trash, then moves or copies path
// there.
func (a *app) replace(path, to string, copy bool) {
	if err := a.discard(to); err != nil {
		a.transferError(path, copy, err)
		a.refreshFolders()
		return
	}
	a.transfer(path, to, copy)
}

// transfer moves path to to, or copies it off the main thread in the app.
func (a *app) transfer(path, to string, copy bool) {
	if !copy {
		if err := renameNew(path, to); err != nil {
			a.transferError(path, copy, err)
			return
		}
		a.renamed(path, to)
		return
	}
	done := func(err error) {
		if err != nil {
			a.transferError(path, copy, err)
		}
		a.refreshFolders()
	}
	if a.win == nil {
		done(copyNew(path, to))
		return
	}
	go func() {
		err := copyNew(path, to)
		a.win.Update(func() { done(err) })
	}()
}

func (a *app) transferError(path string, copy bool, err error) {
	title := locales.DialogUnableToMove(filepath.Base(path))
	if copy {
		title = locales.DialogUnableToCopy(filepath.Base(path))
	}
	go mygo.Dialog.Error(title, err.Error())
}

// keepBoth returns to with the first number from 2 after its name that
// nothing has, as Finder's Keep Both names a copy.
func keepBoth(to string) string {
	dir, name := filepath.Split(to)
	ext := filepath.Ext(name)
	if ext == name {
		ext = ""
	}
	stem := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		p := filepath.Join(dir, fmt.Sprintf("%s %d%s", stem, n, ext))
		if _, err := os.Lstat(p); err != nil {
			return p
		}
	}
}

// copyNew copies the file or folder from to to, which must not exist.
func copyNew(from, to string) error {
	fi, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		return &os.PathError{Op: "copy", Path: to, Err: fs.ErrExist}
	}
	if fi.IsDir() {
		return os.CopyFS(to, os.DirFS(from))
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// holds reports whether path is in one of the folders.
func (p *project) holds(path string) bool {
	return slices.ContainsFunc(p.folders, func(f string) bool { return within(path, f) })
}

// creation is a file or folder being named in the side bar, at the top of
// the folder it goes in.
type creation struct {
	dir     string
	folder  bool
	name    string
	focused bool // the input took the focus, which leaving cancels it
}

func (a *app) startCreate(dir string, folder bool) {
	*a.proj.isOpen(dir, true) = true
	a.proj.create = &creation{dir: dir, folder: folder}
}

// finishCreate makes the file or folder named, opening a file in a tab.
func (a *app) finishCreate() {
	cr := a.proj.create
	a.proj.create = nil
	name := strings.TrimSpace(cr.name)
	if name == "" {
		return
	}
	p := filepath.Join(cr.dir, name)
	if err := createNew(p, cr.folder); err != nil {
		go mygo.Dialog.Error(locales.DialogUnableToCreate(name), err.Error())
		return
	}
	a.refreshFolders()
	if !cr.folder {
		a.open([]string{p})
	}
}

func createNew(path string, folder bool) error {
	if folder {
		return os.Mkdir(path, 0o755)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// docsIn returns the tabs of the files in path.
func (a *app) docsIn(path string) []*doc {
	var docs []*doc
	for _, d := range a.allDocs() {
		if d.path != "" && within(d.path, path) {
			docs = append(docs, d)
		}
	}
	return docs
}

// discard moves path to the Trash, with the tabs of its files and the
// project's folders in it.
func (a *app) discard(path string) error {
	if err := mygo.Shell.TrashItem(path); err != nil {
		return err
	}
	docs := a.docsIn(path)
	for _, d := range docs {
		a.take(d)
	}
	release(docs...)
	a.proj.folders = slices.DeleteFunc(a.proj.folders, func(f string) bool { return within(f, path) })
	return nil
}

// trash moves path to the Trash, with the tabs of its files. It asks first
// for a folder, or when a tab has changes.
func (a *app) trash(path string, folder bool) {
	docs := a.docsIn(path)
	del := func() {
		if err := a.discard(path); err != nil {
			go mygo.Dialog.Error(locales.DialogUnableToDelete(filepath.Base(path)), err.Error())
			return
		}
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

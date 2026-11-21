package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

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

func leftOut(name string, dir bool) bool {
	return projLeftOut(name, dir, folderExclude, fileExclude)
}

func projLeftOut(name string, dir bool, folderExcl, fileExcl []string) bool {
	if dir {
		return excluded(name, folderExcl)
	}
	return excluded(name, fileExcl)
}

const watchDebounce = 150 * time.Millisecond

// projWatchBatch coalesces bursts of file system watch events across a
// window's watched folders before refreshing git marks, re-indexing, or
// reloading externally modified tabs.
type projWatchBatch struct {
	mu      sync.Mutex
	timer   *time.Timer
	dirs    map[string]bool
	paths   map[string]bool
	reindex bool
	stopped bool
}

type project struct {
	path       string
	folders    []string
	meta       map[string]json.RawMessage
	folderMeta map[string]map[string]json.RawMessage
	open       map[string]*bool
	dirs       map[string][]os.DirEntry
	watches    map[string]context.CancelFunc
	watchBatch *projWatchBatch
	shown      map[string]bool
	files      []string
	gen        int
	create     *creation
	// jump has the side bar show the next frame without motion: another
	// project or a rename replaced the rows' paths, not their places.
	jump bool
}

type projectFolder struct {
	Path  string
	extra map[string]json.RawMessage
}

// projectFile is a .sublime-project file, of which Lime reads the folders
// while preserving any other fields for round-trip saves.
type projectFile struct {
	Folders []projectFolder
	extra   map[string]json.RawMessage
}

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

func (p *project) list(dir string, folderExcl, fileExcl []string) []os.DirEntry {
	if es, ok := p.dirs[dir]; ok {
		return es
	}
	es, _ := os.ReadDir(dir)
	es = slices.DeleteFunc(es, func(e os.DirEntry) bool { return projLeftOut(e.Name(), e.IsDir(), folderExcl, fileExcl) })
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

func (a *app) shownDir(dir string) []os.DirEntry {
	if a.proj.shown == nil {
		a.proj.shown = make(map[string]bool)
	}
	a.proj.shown[dir] = true
	if watchDirs && a.win != nil && a.proj.watches[dir] == nil {
		a.watchDir(dir)
	}
	return a.proj.list(dir, a.set.folderExcludes(), a.set.fileExcludes())
}

// watchDir follows dir, dropping what was read of it as entries appear,
// go away or are renamed. A watch that can't start stays, so that dir
// isn't tried again while it shows. One that ends, as when dir goes away,
// is dropped with what was read, for dir to be read and followed anew.
func (a *app) watchDir(dir string) {
	if a.proj.watches == nil {
		a.proj.watches = make(map[string]context.CancelFunc)
	}
	if a.proj.watchBatch == nil {
		a.proj.watchBatch = &projWatchBatch{}
	}
	batch := a.proj.watchBatch
	folderExcl := slices.Clone(a.set.folderExcludes())
	fileExcl := slices.Clone(a.set.fileExcludes())
	ctx, stop := context.WithCancel(context.Background())
	a.proj.watches[dir] = stop
	ended := func() {
		a.win.Update(func() {
			// Stopped: another watch, or none, follows dir now.
			if ctx.Err() != nil {
				return
			}
			delete(a.proj.dirs, dir)
			stop()
			delete(a.proj.watches, dir)
			a.queueWatchEvents(batch, dir, true, nil)
		})
	}
	go func() {
		w, err := mygo.WatchFiles(ctx, dir, mygo.FileWatchOptions{
			Ignore: func(name string, isDir bool) bool {
				return filepath.Base(name) != ".gitignore" && projLeftOut(filepath.Base(name), isDir, folderExcl, fileExcl)
			},
		})
		if err != nil {
			return
		}
		defer w.Close()
		for {
			events, err := w.Next(ctx)
			if err != nil {
				ended()
				return
			}
			var structural bool
			var paths []string
			for _, e := range events {
				if e.Op != mygo.FileWrite || filepath.Base(e.Path) == ".gitignore" {
					structural = true
				}
				if !e.IsDir && e.Path != "" && e.Path != "." {
					paths = append(paths, filepath.Join(dir, filepath.FromSlash(e.Path)))
				}
			}
			if structural || len(paths) > 0 {
				a.queueWatchEvents(batch, dir, structural, paths)
			}
		}
	}()
}

func (a *app) queueWatchEvents(batch *projWatchBatch, dir string, structural bool, paths []string) {
	batch.mu.Lock()
	defer batch.mu.Unlock()
	if batch.stopped {
		return
	}
	if structural {
		batch.reindex = true
		if batch.dirs == nil {
			batch.dirs = make(map[string]bool)
		}
		batch.dirs[dir] = true
	}
	if len(paths) > 0 {
		if batch.paths == nil {
			batch.paths = make(map[string]bool, len(paths))
		}
		for _, p := range paths {
			batch.paths[p] = true
		}
	}
	if !batch.reindex && len(batch.paths) == 0 {
		return
	}
	if batch.timer == nil {
		batch.timer = time.AfterFunc(watchDebounce, func() { a.flushWatchBatch(batch) })
	}
}

func (a *app) flushWatchBatch(batch *projWatchBatch) {
	batch.mu.Lock()
	if batch.stopped {
		batch.mu.Unlock()
		return
	}
	batch.timer = nil
	dirs, paths, reindex := batch.dirs, batch.paths, batch.reindex
	batch.dirs, batch.paths, batch.reindex = nil, nil, false
	batch.mu.Unlock()
	if !reindex && len(paths) == 0 {
		return
	}
	if a.win == nil {
		return
	}
	a.win.Update(func() {
		a.applyWatchBatch(batch, dirs, paths, reindex)
	})
}

func (a *app) applyWatchBatch(batch *projWatchBatch, dirs, paths map[string]bool, reindex bool) {
	if a.proj.watchBatch != batch {
		return
	}
	if reindex {
		for dir := range dirs {
			delete(a.proj.dirs, dir)
		}
		a.readMarks()
		a.indexFolders()
	}
	if len(paths) > 0 {
		for _, w := range a.l.wins {
			if w.hasOpenPath(paths) {
				w.reloadChanged()
			}
		}
	}
}

func (a *app) hasOpenPath(paths map[string]bool) bool {
	for _, d := range a.allDocs() {
		if d.path != "" && paths[d.path] {
			return true
		}
	}
	return false
}

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

func (p *project) unwatch() {
	for _, stop := range p.watches {
		stop()
	}
	if p.watchBatch != nil {
		p.watchBatch.mu.Lock()
		p.watchBatch.stopped = true
		if p.watchBatch.timer != nil {
			p.watchBatch.timer.Stop()
		}
		p.watchBatch.mu.Unlock()
		p.watchBatch = nil
	}
}

type projIgnoreRule struct {
	pattern  string
	negated  bool
	dirOnly  bool
	anchored bool
}

type projIgnoreLayer struct {
	dir   string
	rules []projIgnoreRule
}

type projIgnore struct {
	layers []projIgnoreLayer
}

func projNewIgnore(folder string) *projIgnore {
	ig := &projIgnore{}
	// Load ancestor .gitignore files up to the enclosing git repository root,
	// if any, so opening a subdirectory still respects repo-level ignores.
	var ancestors []string
	var repoRoot string
	for cur := filepath.Dir(folder); ; {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			repoRoot = cur
			ancestors = append(ancestors, cur)
			break
		}
		ancestors = append(ancestors, cur)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	if repoRoot != "" {
		if b, err := os.ReadFile(filepath.Join(repoRoot, ".git", "info", "exclude")); err == nil {
			if rules := projParseIgnore(b); len(rules) > 0 {
				ig.layers = append(ig.layers, projIgnoreLayer{dir: repoRoot, rules: rules})
			}
		}
		for i := len(ancestors) - 1; i >= 0; i-- {
			ig.pushDir(ancestors[i])
		}
	}
	return ig
}

func (ig *projIgnore) pushDir(dir string) {
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return
	}
	if rules := projParseIgnore(b); len(rules) > 0 {
		ig.layers = append(ig.layers, projIgnoreLayer{dir: dir, rules: rules})
	}
}

func (ig *projIgnore) ignored(p, name string, isDir bool) bool {
	ignored := false
	for _, layer := range ig.layers {
		rel, err := filepath.Rel(layer.dir, p)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, r := range layer.rules {
			if r.dirOnly && !isDir {
				continue
			}
			var matched bool
			if r.anchored {
				matched = projMatchGlob(r.pattern, rel)
			} else {
				matched = projMatchGlob(r.pattern, name)
			}
			if matched {
				ignored = !r.negated
			}
		}
	}
	return ignored
}

func projParseIgnore(data []byte) []projIgnoreRule {
	var rules []projIgnoreRule
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = projTrimTrailingSpaces(line)
		if line == "" {
			continue
		}
		negated := false
		if strings.HasPrefix(line, "!") {
			negated = true
			line = line[1:]
		} else if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
			line = line[1:]
		}
		dirOnly := false
		if strings.HasSuffix(line, "/") {
			dirOnly = true
			line = strings.TrimRight(line, "/")
		}
		if line == "" {
			continue
		}
		anchored := false
		if strings.HasPrefix(line, "/") {
			anchored = true
			line = strings.TrimLeft(line, "/")
		}
		if strings.Contains(line, "/") {
			anchored = true
		}
		if line == "" {
			continue
		}
		rules = append(rules, projIgnoreRule{
			pattern:  line,
			negated:  negated,
			dirOnly:  dirOnly,
			anchored: anchored,
		})
	}
	return rules
}

func projTrimTrailingSpaces(s string) string {
	end := len(s)
	for end > 0 && s[end-1] == ' ' {
		bs := 0
		for i := end - 2; i >= 0 && s[i] == '\\'; i-- {
			bs++
		}
		if bs%2 == 1 {
			// Escaped trailing space; keep it and drop the escape backslash.
			return s[:end-2] + " "
		}
		end--
	}
	return s[:end]
}

func projMatchGlob(pattern, text string) bool {
	if !strings.Contains(pattern, "**") {
		ok, _ := path.Match(pattern, text)
		return ok
	}
	return projMatchSegs(strings.Split(pattern, "/"), strings.Split(text, "/"))
}

func projMatchSegs(pSegs, tSegs []string) bool {
	m, n := len(pSegs), len(tSegs)
	dp := make([]bool, n+1)
	dp[0] = true
	for i := 1; i <= m; i++ {
		next := make([]bool, n+1)
		p := pSegs[i-1]
		if p == "**" {
			// Trailing "/**" matches inside the prefix, not the prefix itself.
			trailing := i == m && m > 1
			if !trailing {
				next[0] = dp[0]
			}
			for j := 1; j <= n; j++ {
				if trailing {
					next[j] = dp[j-1] || next[j-1]
				} else {
					next[j] = dp[j] || next[j-1]
				}
			}
		} else {
			for j := 1; j <= n; j++ {
				if dp[j-1] {
					if ok, _ := path.Match(p, tSegs[j-1]); ok {
						next[j] = true
					}
				}
			}
		}
		dp = next
	}
	return dp[n]
}

func walkFolders(folders []string) []string {
	return walkFoldersWith(folders, folderExclude, fileExclude)
}

func walkFoldersWith(folders, folderExcl, fileExcl []string) []string {
	var files []string
	pending := slices.Clone(folders)
	for len(pending) > 0 {
		f := pending[0]
		pending = pending[1:]
		if len(files) >= maxIndexed {
			break
		}
		if candidates, err := runGit(f, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--deduplicate", "--", "."); err == nil {
			for rel := range strings.SplitSeq(candidates, "\x00") {
				if rel == "" {
					continue
				}
				rel = strings.TrimSuffix(rel, "/")
				dir, name := path.Split(rel)
				if slices.ContainsFunc(strings.Split(strings.TrimSuffix(dir, "/"), "/"), func(d string) bool { return d != "" && excluded(d, folderExcl) }) {
					continue
				}
				file := filepath.Join(f, filepath.FromSlash(rel))
				fi, err := os.Lstat(file)
				if err != nil {
					continue
				}
				if fi.IsDir() {
					// Git lists a checked-out submodule or embedded repository as one entry.
					if _, err := os.Stat(filepath.Join(file, ".git")); err == nil && !excluded(name, folderExcl) {
						pending = append(pending, file)
					}
					continue
				}
				if !excluded(name, fileExcl) && !excluded(name, binaryFiles) && regular(file, fi.Mode().Type()) {
					files = append(files, file)
					if len(files) >= maxIndexed {
						break
					}
				}
			}
			continue
		}
		ig := projNewIgnore(f)
		baseLayers := len(ig.layers)
		filepath.WalkDir(f, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if len(files) >= maxIndexed {
				return filepath.SkipAll
			}
			for len(ig.layers) > baseLayers && !within(p, ig.layers[len(ig.layers)-1].dir) {
				ig.layers = ig.layers[:len(ig.layers)-1]
			}
			if e.IsDir() {
				if p != f && (excluded(e.Name(), folderExcl) || ig.ignored(p, e.Name(), true)) {
					return filepath.SkipDir
				}
				ig.pushDir(p)
				return nil
			}
			if !excluded(e.Name(), fileExcl) && !excluded(e.Name(), binaryFiles) && !ig.ignored(p, e.Name(), false) {
				files = append(files, p)
			}
			return nil
		})
	}
	return files
}

func (a *app) indexFolders() {
	a.proj.gen++
	gen, folders := a.proj.gen, slices.Clone(a.proj.folders)
	folderExcl, fileExcl := slices.Clone(a.set.folderExcludes()), slices.Clone(a.set.fileExcludes())
	if a.win == nil {
		a.proj.files = walkFoldersWith(folders, folderExcl, fileExcl)
		return
	}
	go func() {
		files := walkFoldersWith(folders, folderExcl, fileExcl)
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
			a.l.addRecent(nil, dirs)
		})
	}()
}

func (a *app) refreshFolders() {
	a.proj.dirs = nil
	a.indexFolders()
	a.readMarks()
}

func (a *app) closeProject() {
	a.proj.unwatch()
	a.proj = project{gen: a.proj.gen + 1}
}

// projStripJSONC strips UTF-8 BOM, // line comments, /* */ block comments, and
// trailing commas from Sublime Text's JSONC project files while leaving string
// literals untouched.
func projStripJSONC(src []byte) []byte {
	src = bytes.TrimPrefix(src, []byte{0xef, 0xbb, 0xbf})
	noComments := make([]byte, 0, len(src))
	inString := false
	for i := 0; i < len(src); {
		b := src[i]
		if inString {
			if b == '\\' && i+1 < len(src) {
				noComments = append(noComments, b, src[i+1])
				i += 2
				continue
			}
			if b == '"' {
				inString = false
			}
			noComments = append(noComments, b)
			i++
			continue
		}
		if b == '"' {
			inString = true
			noComments = append(noComments, b)
			i++
			continue
		}
		if b == '/' && i+1 < len(src) {
			if src[i+1] == '/' {
				i += 2
				for i < len(src) && src[i] != '\n' {
					i++
				}
				continue
			}
			if src[i+1] == '*' {
				i += 2
				for i < len(src) {
					if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
						i += 2
						break
					}
					i++
				}
				noComments = append(noComments, ' ')
				continue
			}
		}
		noComments = append(noComments, b)
		i++
	}

	out := make([]byte, 0, len(noComments))
	inString = false
	for i := 0; i < len(noComments); {
		b := noComments[i]
		if inString {
			if b == '\\' && i+1 < len(noComments) {
				out = append(out, b, noComments[i+1])
				i += 2
				continue
			}
			if b == '"' {
				inString = false
			}
			out = append(out, b)
			i++
			continue
		}
		if b == '"' {
			inString = true
			out = append(out, b)
			i++
			continue
		}
		if b == ',' {
			j := i + 1
			for j < len(noComments) && (noComments[j] == ' ' || noComments[j] == '\t' || noComments[j] == '\r' || noComments[j] == '\n') {
				j++
			}
			if j < len(noComments) && (noComments[j] == '}' || noComments[j] == ']') {
				i++
				continue
			}
		}
		out = append(out, b)
		i++
	}
	return out
}

func readProject(path string) (projectFile, error) {
	var pf projectFile
	b, err := os.ReadFile(path)
	if err != nil {
		return pf, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(projStripJSONC(b), &raw); err != nil {
		return pf, err
	}
	pf.extra = raw
	if fb, ok := raw["folders"]; ok {
		var rawFolders []map[string]json.RawMessage
		if err := json.Unmarshal(fb, &rawFolders); err != nil {
			return pf, err
		}
		for _, fm := range rawFolders {
			var p string
			if pb, ok := fm["path"]; ok {
				if err := json.Unmarshal(pb, &p); err != nil {
					return pf, err
				}
			}
			pf.Folders = append(pf.Folders, projectFolder{Path: p, extra: fm})
		}
	}
	return pf, nil
}

func (a *app) openProject(path string) error {
	pf, err := readProject(path)
	if err != nil {
		return err
	}
	a.useProject(path, pf)
	return nil
}

func (a *app) useProject(path string, pf projectFile) {
	a.closeProject()
	a.proj.path, a.proj.jump = path, true
	a.proj.loadProjectFields(path, pf)
	if a.side == noPanel {
		a.side = filesPanel
	}
	a.indexFolders()
	a.l.addRecent(nil, []string{path})
}

func (p *project) loadProjectFields(path string, pf projectFile) {
	p.meta = pf.extra
	p.folderMeta = make(map[string]map[string]json.RawMessage)
	for _, f := range pf.Folders {
		dir := filepath.FromSlash(f.Path)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(path), dir)
		}
		dir = filepath.Clean(dir)
		p.folders = append(p.folders, dir)
		p.folderMeta[dir] = f.extra
	}
}

// saveProject writes the folders to path, relative to it as Sublime Text
// writes them, preserving any unknown top-level and per-folder fields.
func (a *app) saveProject(path string) error {
	out := make(map[string]any, len(a.proj.meta)+1)
	for k, v := range a.proj.meta {
		out[k] = v
	}
	folders := make([]map[string]any, 0, len(a.proj.folders))
	for _, f := range a.proj.folders {
		p := f
		if r, err := filepath.Rel(filepath.Dir(path), f); err == nil {
			p = r
		}
		entry := make(map[string]any, len(a.proj.folderMeta[f])+1)
		for k, v := range a.proj.folderMeta[f] {
			entry[k] = v
		}
		entry["path"] = filepath.ToSlash(p)
		folders = append(folders, entry)
	}
	out["folders"] = folders
	b, err := json.MarshalIndent(out, "", "\t")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return err
	}
	a.proj.path = path
	a.l.addRecent(nil, []string{path})
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

func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

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

func (a *app) renamed(from, to string) {
	a.l.renameRecent(from, to)
	for _, w := range a.l.wins {
		for _, d := range w.allDocs() {
			if p := moved(d.path, from, to); p != d.path {
				d.path = p
				d.st.Buffer.SetLanguage(editor.LanguageFor(p))
				if w.reloading {
					w.reloadAgain = true
				}
			}
		}
		open := make(map[string]*bool, len(w.proj.open))
		for dir, o := range w.proj.open {
			open[moved(dir, from, to)] = o
		}
		w.proj.open = open
		if len(w.proj.folderMeta) > 0 {
			meta := make(map[string]map[string]json.RawMessage, len(w.proj.folderMeta))
			for dir, m := range w.proj.folderMeta {
				meta[moved(dir, from, to)] = m
			}
			w.proj.folderMeta = meta
		}
		w.proj.path = moved(w.proj.path, from, to)
		rootsMoved := false
		for i, f := range w.proj.folders {
			w.proj.folders[i] = moved(f, from, to)
			rootsMoved = rootsMoved || w.proj.folders[i] != f
		}
		if rootsMoved || w == a || w.proj.holds(from) || w.proj.holds(to) {
			w.proj.jump = true
			w.refreshFolders()
		}
	}
	a.l.redraw()
}

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
	if slices.ContainsFunc(a.l.docsIn(to), (*doc).dirty) {
		detail = append(detail, locales.DialogUnsavedLost())
	}
	labels := []string{locales.ButtonReplace(), locales.ButtonKeepBoth()}
	choices := []func(){
		func() { a.replace(path, to, copy) },
		func() { a.transfer(path, keepBoth(to), copy) },
	}
	destructive := []int{0}
	// Replacing the folder path is in would trash path too.
	if within(path, to) {
		labels, choices, detail, destructive = labels[1:], choices[1:], nil, nil
	}
	labels = append(labels, locales.ButtonCancel())
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             a.win,
			Type:               mygo.MessageWarning,
			Message:            locales.DialogItemExists(name, filepath.Base(dir)),
			Detail:             strings.Join(detail, " "),
			Buttons:            labels,
			CancelButton:       len(labels) - 1,
			DestructiveButtons: destructive,
		})
		if err != nil {
			return
		}
		if res.Button >= 0 && res.Button < len(choices) {
			a.win.Update(choices[res.Button])
		}
	}()
}

func (a *app) replace(path, to string, copy bool) {
	if err := a.discard(to); err != nil {
		a.transferError(path, copy, err)
		a.refreshFolders()
		return
	}
	a.transfer(path, to, copy)
}

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

func (p *project) holds(path string) bool {
	return slices.ContainsFunc(p.folders, func(f string) bool { return within(path, f) })
}

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

func projResolveCreate(dir, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return "", &os.PathError{Op: "create", Path: name, Err: fs.ErrInvalid}
	}
	rel := filepath.FromSlash(name)
	if filepath.IsAbs(rel) {
		return "", &os.PathError{Op: "create", Path: name, Err: fs.ErrInvalid}
	}
	base := filepath.Clean(dir)
	p := filepath.Join(base, rel)
	r, err := filepath.Rel(base, p)
	if err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", &os.PathError{Op: "create", Path: name, Err: fs.ErrPermission}
	}
	return p, nil
}

func (a *app) finishCreate() {
	cr := a.proj.create
	a.proj.create = nil
	if cr == nil {
		return
	}
	name := strings.TrimSpace(cr.name)
	if name == "" {
		return
	}
	if !cr.folder && (strings.HasSuffix(name, "/") || strings.HasSuffix(name, "\\")) {
		go mygo.Dialog.Error(locales.DialogUnableToCreate(name), fs.ErrInvalid.Error())
		return
	}
	p, err := projResolveCreate(cr.dir, name)
	if err != nil {
		go mygo.Dialog.Error(locales.DialogUnableToCreate(name), err.Error())
		return
	}
	if err := createNew(cr.dir, p, cr.folder); err != nil {
		go mygo.Dialog.Error(locales.DialogUnableToCreate(name), err.Error())
		return
	}
	base := filepath.Clean(cr.dir)
	for d := filepath.Dir(p); within(d, base) && d != base; {
		*a.proj.isOpen(d, true) = true
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	a.refreshFolders()
	if !cr.folder {
		a.open([]string{p})
	}
}

func createNew(dir, p string, folder bool) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		return err
	}
	if folder {
		return root.Mkdir(rel, 0755)
	}
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

func (a *app) docsIn(path string) []*doc {
	var docs []*doc
	for _, d := range a.allDocs() {
		if d.path != "" && within(d.path, path) {
			docs = append(docs, d)
		}
	}
	return docs
}

func (l *lime) docsIn(path string) []*doc {
	var docs []*doc
	for _, w := range l.wins {
		docs = append(docs, w.docsIn(path)...)
	}
	return docs
}

var projTrashItem = func(p string) error { return mygo.Shell.TrashItem(p) }

func (a *app) discard(path string) error {
	if err := projTrashItem(path); err != nil {
		return err
	}
	var removed []*doc
	for _, w := range a.l.wins {
		docs := w.docsIn(path)
		for _, d := range docs {
			w.closeViews(d)
			w.take(d)
		}
		removed = append(removed, docs...)
		before := len(w.proj.folders)
		w.proj.folders = slices.DeleteFunc(w.proj.folders, func(f string) bool { return within(f, path) })
		for dir := range w.proj.folderMeta {
			if within(dir, path) {
				delete(w.proj.folderMeta, dir)
			}
		}
		if w != a && (len(w.proj.folders) != before || w.proj.holds(path)) {
			w.refreshFolders()
		}
	}
	release(removed...)
	a.l.redraw()
	return nil
}

func (a *app) trash(path string, folder bool) {
	docs := a.l.docsIn(path)
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
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             a.win,
			Type:               mygo.MessageWarning,
			Message:            locales.DialogDelete(filepath.Base(path)),
			Detail:             strings.Join(detail, " "),
			Buttons:            []string{locales.ButtonDelete(), locales.ButtonCancel()},
			CancelButton:       1,
			DestructiveButtons: []int{0},
		})
		if err != nil {
			return
		}
		if res.Button == 0 {
			a.win.Update(del)
		}
	}()
}

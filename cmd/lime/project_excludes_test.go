package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestWalkFoldersGitignoreAndSidebarVisibility(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(rel, body string) string {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	writeFile(".gitignore", strings.Join([]string{
		"# dependencies",
		"node_modules/",
		"*.log",
		"!important.log",
		"/dist/",
		"**/generated/*.go",
	}, "\n")+"\n")

	mainGo := writeFile("main.go", "package main\n")
	writeFile("node_modules/pkg/index.js", "module.exports = {}\n")
	writeFile("debug.log", "debug\n")
	importantLog := writeFile("important.log", "keep\n")
	writeFile("dist/bundle.js", "bundled\n")
	nestedDist := writeFile("src/dist/keep.js", "ok\n")
	writeFile("pkg/generated/out.go", "package generated\n")

	a := newApp("")
	a.addFolder(dir)

	wantIndexed := []string{filepath.Join(dir, ".gitignore"), importantLog, mainGo, nestedDist}
	gotIndexed := slices.Clone(a.proj.files)
	slices.Sort(wantIndexed)
	slices.Sort(gotIndexed)
	if !slices.Equal(gotIndexed, wantIndexed) {
		t.Fatalf("indexed files = %v, want %v", gotIndexed, wantIndexed)
	}

	// Sidebar still shows gitignored folders and files (dimmed by gitmarks).
	entries := a.shownDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	for _, want := range []string{"node_modules", "dist", "debug.log", "main.go"} {
		if !slices.Contains(names, want) {
			t.Fatalf("sidebar missing gitignored entry %q; got %v", want, names)
		}
	}
}

func TestCustomExcludePatterns(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{
		".git/HEAD",
		"vendor/lib.go",
		"build/out.txt",
		"src/main.go",
		"notes.bak",
		"secret.txt",
		"old.pyc",
	} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Backward compatibility: omitted patterns keep defaults (.git, *.pyc).
	cfgDir := t.TempDir()
	if err := os.WriteFile(settingsPath(cfgDir), []byte(`{"font_size": 14}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp(cfgDir)
	a.addFolder(dir)
	if slices.Contains(a.proj.files, filepath.Join(dir, ".git", "HEAD")) ||
		slices.Contains(a.proj.files, filepath.Join(dir, "old.pyc")) {
		t.Fatalf("default excludes did not apply: %v", a.proj.files)
	}
	if !slices.Contains(a.proj.files, filepath.Join(dir, "vendor", "lib.go")) {
		t.Fatalf("vendor/lib.go should be indexed under defaults: %v", a.proj.files)
	}

	// Custom folder_exclude_patterns and file_exclude_patterns apply to both
	// sidebar listing and indexing.
	a.set.FolderExcludePatterns = []string{"vendor", "build", ".git"}
	a.set.FileExcludePatterns = []string{"*.bak", "secret.txt"}
	a.refreshFolders()

	for _, excludedPath := range []string{
		filepath.Join(dir, "vendor", "lib.go"),
		filepath.Join(dir, "build", "out.txt"),
		filepath.Join(dir, "notes.bak"),
		filepath.Join(dir, "secret.txt"),
	} {
		if slices.Contains(a.proj.files, excludedPath) {
			t.Fatalf("custom excluded path %q still indexed: %v", excludedPath, a.proj.files)
		}
	}
	if !slices.Contains(a.proj.files, filepath.Join(dir, "src", "main.go")) ||
		!slices.Contains(a.proj.files, filepath.Join(dir, "old.pyc")) {
		t.Fatalf("expected non-excluded files in index: %v", a.proj.files)
	}

	var sidebarNames []string
	for _, e := range a.shownDir(dir) {
		sidebarNames = append(sidebarNames, e.Name())
	}
	for _, hidden := range []string{"vendor", "build", ".git", "notes.bak", "secret.txt"} {
		if slices.Contains(sidebarNames, hidden) {
			t.Fatalf("sidebar should exclude %q; got %v", hidden, sidebarNames)
		}
	}
	if !slices.Contains(sidebarNames, "src") || !slices.Contains(sidebarNames, "old.pyc") {
		t.Fatalf("sidebar missing expected entries: %v", sidebarNames)
	}
}

func TestSublimeProjectJSONCAndPreservation(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	extraDir := filepath.Join(root, "extra")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatal(err)
	}

	projPath := filepath.Join(root, "demo.sublime-project")
	jsonc := "\xef\xbb\xbf{\n" +
		"\t// Leading comment\n" +
		"\t\"folders\": [\n" +
		"\t\t{\n" +
		"\t\t\t\"name\": \"Main App\",\n" +
		"\t\t\t\"path\": \"app\",\n" +
		"\t\t\t\"folder_exclude_patterns\": [\"dist\",],\n" +
		"\t\t\t\"file_exclude_patterns\": [\"*.bak\",],\n" +
		"\t\t},\n" +
		"\t],\n" +
		"\t/* Block comment with // inside */\n" +
		"\t\"settings\": {\n" +
		"\t\t\"tab_size\": 2,\n" +
		"\t\t\"homepage\": \"https://example.com//not-a-comment/*ok*/\",\n" +
		"\t},\n" +
		"\t\"build_systems\": [\n" +
		"\t\t{\"name\": \"Go Build\", \"cmd\": [\"go\", \"build\"],},\n" +
		"\t],\n" +
		"}\n"
	if err := os.WriteFile(projPath, []byte(jsonc), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newApp("")
	if err := a.openProject(projPath); err != nil {
		t.Fatalf("openProject JSONC: %v", err)
	}
	if !slices.Equal(a.proj.folders, []string{appDir}) {
		t.Fatalf("folders = %v, want [%s]", a.proj.folders, appDir)
	}

	a.addFolder(extraDir)
	snapshot := a.session()
	legacy := snapshot
	legacy.ProjectMeta, legacy.FolderMeta = nil, nil
	for _, ss := range []sessionWindow{legacy, snapshot} {
		a = newApp("")
		a.restore(ss)
		if len(a.proj.meta["settings"]) == 0 || len(a.proj.folderMeta[appDir]["name"]) == 0 || !slices.Contains(a.proj.folders, extraDir) {
			t.Fatal("session lost project metadata or folders")
		}
	}
	if err := a.saveProject(projPath); err != nil {
		t.Fatalf("saveProject: %v", err)
	}

	saved, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Folders      []map[string]any `json:"folders"`
		Settings     map[string]any   `json:"settings"`
		BuildSystems []map[string]any `json:"build_systems"`
	}
	if err := json.Unmarshal(saved, &raw); err != nil {
		t.Fatalf("saved project is not strict JSON: %v\n%s", err, saved)
	}
	if len(raw.Folders) != 2 {
		t.Fatalf("folders len = %d, want 2", len(raw.Folders))
	}
	if raw.Folders[0]["name"] != "Main App" || raw.Folders[0]["path"] != "app" {
		t.Fatalf("first folder metadata lost: %#v", raw.Folders[0])
	}
	if _, ok := raw.Folders[0]["folder_exclude_patterns"]; !ok {
		t.Fatalf("folder_exclude_patterns lost: %#v", raw.Folders[0])
	}
	if _, ok := raw.Folders[0]["file_exclude_patterns"]; !ok {
		t.Fatalf("file_exclude_patterns lost: %#v", raw.Folders[0])
	}
	if raw.Folders[1]["path"] != "extra" {
		t.Fatalf("second folder path = %v, want extra", raw.Folders[1]["path"])
	}
	if raw.Settings["tab_size"] != float64(2) || raw.Settings["homepage"] != "https://example.com//not-a-comment/*ok*/" {
		t.Fatalf("top-level settings lost: %#v", raw.Settings)
	}
	if len(raw.BuildSystems) != 1 || raw.BuildSystems[0]["name"] != "Go Build" {
		t.Fatalf("build_systems lost: %#v", raw.BuildSystems)
	}
}

func TestNestedCreateAndEscapeGuard(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := newApp("")
	a.addFolder(dir)

	// Nested file creation creates parent directories and opens the file.
	a.startCreate(dir, false)
	a.proj.create.name = "a/b/c.go"
	a.finishCreate()

	target := filepath.Join(dir, "a", "b", "c.go")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("nested file not created: %v", err)
	}
	if d := a.current(); d == nil || d.path != target {
		t.Fatalf("created file not opened: %v", d)
	}
	if !*a.proj.isOpen(filepath.Join(dir, "a"), false) || !*a.proj.isOpen(filepath.Join(dir, "a", "b"), false) {
		t.Fatal("intermediate created folders should be expanded in sidebar")
	}

	if err := os.Symlink(parent, filepath.Join(dir, "link")); err == nil {
		if err := createNew(dir, filepath.Join(dir, "link", "outside"), false); err == nil {
			t.Fatal("created through an escaping symlink")
		}
	}
	// Nested folder creation creates parent directories too.
	a.startCreate(dir, true)
	a.proj.create.name = "x/y/z"
	a.finishCreate()
	if fi, err := os.Stat(filepath.Join(dir, "x", "y", "z")); err != nil || !fi.IsDir() {
		t.Fatalf("nested folder not created: %v", err)
	}

	// Reject paths escaping cr.dir or absolute paths.
	for _, bad := range []string{"../escape.go", "a/../../escape.go", "..", ".", "/abs/escape.go"} {
		if _, err := projResolveCreate(dir, bad); err == nil {
			t.Fatalf("projResolveCreate(%q) succeeded, want error", bad)
		}
		a.startCreate(dir, false)
		a.proj.create.name = bad
		a.finishCreate()
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.go")); err == nil {
		t.Fatal("escape path created a file outside project directory")
	}
}

func TestMultiWindowRenamedAndDiscardViews(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "doc.md")
	newPath := filepath.Join(dir, "guide.md")
	otherPath := filepath.Join(dir, "other.md")
	if err := os.WriteFile(oldPath, []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("# Other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origTrash := projTrashItem
	projTrashItem = os.RemoveAll
	t.Cleanup(func() { projTrashItem = origTrash })

	l := newLime("")
	w1 := l.newWindow()
	w2 := l.newWindow()
	w1.addFolder(dir)
	w2.addFolder(dir)

	w1.open([]string{oldPath})
	w2.open([]string{oldPath, otherPath})
	d1 := w1.docAt(oldPath)
	d2 := w2.docAt(oldPath)
	otherDoc := w2.docAt(otherPath)

	// Attach a Markdown preview in w1 and a diff tab in w2 referencing the doc.
	w1.openPreview(d1, false)
	w2.compareDocs(d2, otherDoc)
	if w1.docCount() != 2 || w2.docCount() != 3 {
		t.Fatalf("unexpected initial tab counts: w1=%d w2=%d", w1.docCount(), w2.docCount())
	}

	// Rename updates tabs across both windows.
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	w1.renamed(oldPath, newPath)
	if d1.path != newPath || d2.path != newPath {
		t.Fatalf("multi-window rename: d1=%q d2=%q, want %q", d1.path, d2.path, newPath)
	}

	// Discard removes the file, closes matching tabs across all windows, and
	// closes dependent preview and diff views so none remain orphaned.
	if err := w1.discard(newPath); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("discard did not remove file: %v", err)
	}
	if w1.docCount() != 0 {
		t.Fatalf("w1 still has %d tabs after discard (orphan preview?)", w1.docCount())
	}
	if w2.docCount() != 1 || w2.allDocs()[0] != otherDoc {
		t.Fatalf("w2 tabs after discard = %d, want only otherDoc (orphan diff?)", w2.docCount())
	}
}

func TestRenamedProjectInQuickSwitch(t *testing.T) {
	a := newApp("")
	dir := t.TempDir()
	from, to := filepath.Join(dir, "old.sublime-project"), filepath.Join(dir, "new.sublime-project")
	a.proj.path = from
	a.l.addRecent(nil, []string{from})
	a.renamed(from, to)
	a.showWorkspaces()
	items := a.panel.list("")
	if a.proj.path != to || len(items) != 1 || items[0].label != "new" {
		t.Fatalf("project %q, items %+v", a.proj.path, items)
	}
}

func TestWatchBatchReindexAndReload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(filePath, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newApp("")
	a.addFolder(dir)
	a.open([]string{filePath})
	d := a.docAt(filePath)
	a.proj.watchBatch = &projWatchBatch{}
	batch := a.proj.watchBatch

	// Prime sidebar cache, then add a new file externally and apply a
	// structural watch batch.
	_ = a.shownDir(dir)
	addedPath := filepath.Join(dir, "added.go")
	if err := os.WriteFile(addedPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.applyWatchBatch(batch, map[string]bool{dir: true}, nil, true)
	if !slices.Contains(a.proj.files, addedPath) {
		t.Fatalf("watch reindex missed new file %q: %v", addedPath, a.proj.files)
	}
	if _, cached := a.proj.dirs[dir]; cached {
		t.Fatal("watch structural batch did not invalidate cached dir entries")
	}

	// Modify open file externally (advance mtime/size so stamp differs) and
	// apply a write batch to trigger reloadChanged.
	if err := os.WriteFile(filePath, []byte("v2 updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(filePath, future, future)
	a.applyWatchBatch(batch, nil, map[string]bool{filePath: true}, false)
	if got := d.st.Buffer.String(); got != "v2 updated\n" {
		t.Fatalf("watch write did not reload open tab: got %q", got)
	}
}

func TestSettingsAtomicSaveAndUIControls(t *testing.T) {
	cfgDir := t.TempDir()
	if err := writeSettingsAtomic(cfgDir, []byte("{\"font_size\":15}\n")); err != nil {
		t.Fatalf("writeSettingsAtomic: %v", err)
	}
	entries, err := os.ReadDir(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Fatalf("unexpected files after atomic write: %v", entries)
	}

	// Writing into a path blocked by a regular file returns an error.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeSettingsAtomic(blocker, []byte("{}\n")); err == nil {
		t.Fatal("expected error when settings dir is a file")
	}

	// Verify newly exposed controls render and toggle settings properly.
	saveDir := t.TempDir()
	a := newApp(saveDir)
	a.showSettings()
	tt := ui.NewTester(a.view, 900, 650)

	// General page has Fallback Encoding.
	if !tt.HasText("Fallback Encoding") {
		t.Fatal("General settings page missing Fallback Encoding")
	}

	// Switch to Editor page and toggle exposed controls.
	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{
		"Word Wrap",
		"Indent Guides",
		"Scroll Past End",
		"Auto Complete",
		"Inline Blame",
		"Diff Target",
	} {
		if !tt.HasText(label) {
			t.Fatalf("Editor settings page missing %q", label)
		}
	}

	// Verify toggling bound switches (including pastEnd bool conversion) updates settings.
	clickRole := func(role ui.Role, name string) {
		t.Helper()
		info, ok := tt.AccessByRole(role, name)
		if !ok {
			t.Fatalf("missing control %q with role %v", name, role)
		}
		tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	}
	if !bool(a.set.ScrollPastEnd) || !a.set.WordWrap {
		t.Fatalf("unexpected defaults: word_wrap=%v scroll_past_end=%v", a.set.WordWrap, a.set.ScrollPastEnd)
	}
	clickRole(ui.RoleSwitch, "Word Wrap")
	clickRole(ui.RoleSwitch, "Scroll Past End")
	tt.Frame()
	if a.set.WordWrap || bool(a.set.ScrollPastEnd) {
		t.Fatalf("switches did not update settings: word_wrap=%v scroll_past_end=%v", a.set.WordWrap, a.set.ScrollPastEnd)
	}

	// Verify Rulers does not rewrite settings.json when parsed rulers don't change.
	a.set.Rulers = rulers{80}
	a.l.saveSettings()
	tt.Frame()
	beforeInfo, err := os.Stat(settingsPath(saveDir))
	if err != nil {
		t.Fatal(err)
	}
	past := beforeInfo.ModTime().Add(-5 * time.Second)
	_ = os.Chtimes(settingsPath(saveDir), past, past)

	clickRole(ui.RoleTextField, "Rulers")
	tt.Type(",")
	tt.Frame()
	afterComma, err := os.Stat(settingsPath(saveDir))
	if err != nil {
		t.Fatal(err)
	}
	if !afterComma.ModTime().Equal(past) {
		t.Fatal("typing a trailing comma in Rulers rewrote settings.json even though parsed rulers did not change")
	}

	// Typing a new column value updates rulers and writes settings.json.
	tt.Type(" 100")
	tt.Frame()
	if !slices.Equal(a.set.Rulers, rulers{80, 100}) {
		t.Fatalf("rulers = %v, want [80 100]", a.set.Rulers)
	}
	afterNew, err := os.Stat(settingsPath(saveDir))
	if err != nil {
		t.Fatal(err)
	}
	if afterNew.ModTime().Equal(past) {
		t.Fatal("changing Rulers value did not save settings.json")
	}
}

func TestIndexGitTrackedAndExcluded(t *testing.T) {
	dir, git := newRepo(t)
	for _, rel := range []string{"tracked/keep.go", "local.txt", "untracked.go"} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "tracked/keep.go")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("tracked/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("local.txt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(nested, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(nested, "child.go")
	if err := os.WriteFile(child, nil, 0600); err != nil {
		t.Fatal(err)
	}
	files := walkFolders([]string{dir})
	if !slices.Contains(files, child) || !slices.Contains(files, filepath.Join(dir, "tracked", "keep.go")) || slices.Contains(files, filepath.Join(dir, "local.txt")) {
		t.Fatal(files)
	}
}

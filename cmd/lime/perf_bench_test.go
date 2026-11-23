package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestMatchFileIconParity(t *testing.T) {
	compiled := make([]*regexp.Regexp, len(fileIconRules))
	for i, r := range fileIconRules {
		compiled[i] = regexp.MustCompile(r.pattern)
	}
	backupRe := regexp.MustCompile(`~(?:orig|previous)$`)
	tmplRe := regexp.MustCompile(`(?i)^([^.]*\.[^.]+)\.(?:inc?|dist|tm?pl|te?mp|ti?dy)$`)
	refMatch := func(p string) int {
		name := filepath.Base(p)
		if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
			name = p[idx+1:]
		}
		filtered := tmplRe.ReplaceAllString(backupRe.ReplaceAllString(name, ""), "$1")
		find := func(s string, byPath bool) int {
			for i, r := range fileIconRules {
				if r.matchPath == byPath && compiled[i].MatchString(s) {
					return i
				}
			}
			return -1
		}
		i := find("/"+strings.TrimPrefix(p, "/"), true)
		if i < 0 {
			i = find(filtered, false)
		}
		if filtered != name && (i < 0 || fileIconRules[i].priority < 1) {
			i = find(name, false)
		}
		return i
	}

	samples := []string{
		"/repo/cmd/lime/app.go",
		"/repo/cmd/lime/main_test.go",
		"/repo/go.mod",
		"/repo/go.sum",
		"/repo/go.work",
		"/repo/AGENTS.md",
		"/repo/README.md",
		"/repo/LICENSE",
		"/repo/Dockerfile",
		"/repo/docker-compose.yml",
		"/repo/docker-compose.yaml",
		"/repo/.gitignore",
		"/repo/.eslintrc.json",
		"/repo/eslint.config.mjs",
		"/repo/Makefile",
		"/repo/Makefile.boot",
		"/repo/package.json",
		"/repo/tsconfig.json",
		"/repo/Cargo.toml",
		"/repo/Gemfile",
		"/repo/Gemfile.lock",
		"/repo/index.html",
		"/repo/app.d.ts",
		"/repo/component.test.tsx",
		"/repo/style.min.css",
		"/repo/bootstrap.min.js",
		"/repo/custom.bootstrap.scss",
		"/repo/config.yaml.tmpl",
		"/repo/config.yaml~orig",
		"/repo/config.yaml~previous",
		"/repo/foo.bar.baz.dist",
		"/repo/a.b.DIST",
		"/repo/a.b.TMPL",
		"/repo/roles/web/tasks/main.yml",
		"/repo/roles/web/vars/main.yaml",
		"/repo/group_vars/all.yml",
		"/repo/.git/HEAD",
		"/repo/.ssh/authorized_keys",
		"/repo/vault.yml",
		"/repo/my-vault.yaml",
		"/repo/test/1-foo.js",
		"/repo/tests/test_app.py",
		"/repo/foo.swp",
		"/repo/UPPER.GO",
		"/repo/Mixed.TsX",
		"/repo/Makefile.IN",
		"/repo/noext",
		"/repo/unknown.xyz123",
		"/repo/Über.txt",
		"/repo/example.ſwp",
		"/repo/.env.local",
		"/repo/.travis.yml",
		"/repo/webpack.config.js",
		"/repo/vite.config.ts",
		"/repo/babel.config.json",
		"/repo/renovate.json",
		"/repo/renovate",
		"/repo/PKGBUILD",
		"/repo/pacman.conf",
		"/repo/text.enc",
	}
	// Also test every single simple extension and exact filename across all 2,088 rules.
	for _, ext := range []string{
		".c", ".h", ".cpp", ".hpp", ".rs", ".py", ".rb", ".php", ".java", ".kt", ".swift",
		".js", ".mjs", ".cjs", ".jsx", ".ts", ".mts", ".cts", ".tsx", ".vue", ".svelte",
		".json", ".jsonc", ".json5", ".toml", ".ini", ".cfg", ".conf", ".xml", ".svg",
		".png", ".jpg", ".webp", ".gif", ".ico", ".pdf", ".zip", ".tar.gz", ".sql",
		".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat", ".lua", ".zig", ".nim", ".ex", ".exs",
		".erl", ".hs", ".ml", ".clj", ".scala", ".dart", ".r", ".jl", ".proto", ".graphql",
	} {
		samples = append(samples, "/repo/src/sample"+ext, "/repo/src/SAMPLE"+strings.ToUpper(ext))
	}

	clear(fileIconRuleOf)
	for _, p := range samples {
		want := refMatch(p)
		got := matchFileIcon(p)
		if got != want {
			t.Errorf("matchFileIcon(%q) = %d, want %d", p, got, want)
		}
	}
}

func TestClosedTabClearsGroupCaches(t *testing.T) {
	a := newApp("")
	d1 := a.add("/tmp/dir1/shared.go", "package a\n")
	d2 := a.add("/tmp/dir2/shared.go", "package b\n")
	tt := ui.NewTester(a.view, 900, 600)
	tt.Frame()

	a.remove(d2)
	tt.Frame()

	g := a.group()
	if g.dirsCache != nil {
		t.Fatal("dirsCache retained entries after dropping below 2 tabs")
	}
	for _, d := range g.docsBuf[:cap(g.docsBuf)] {
		if d != nil {
			t.Fatal("docsBuf tail retained pointer to closed doc")
		}
	}
	for _, d := range g.dirsDocs[:cap(g.dirsDocs)] {
		if d != nil {
			t.Fatal("dirsDocs tail retained pointer to closed doc")
		}
	}
	if a.current() != d1 {
		t.Fatalf("current tab = %v, want d1", a.current())
	}
}

func TestSelectionStatusUpdatesOnLocaleChange(t *testing.T) {
	loc := "en"
	i18n.SetLocaleFunc(func() string { return loc })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })

	a := newApp("")
	d := a.add("sel.txt", "hello world")
	d.st.SetSelections(editor.Selection{Anchor: 0, Head: 5})

	en := d.selectionStatus(d.st, d.st.Selections())
	loc = "zh-Hans"
	zh := d.selectionStatus(d.st, d.st.Selections())
	if en == "" || zh == "" || en == zh {
		t.Fatalf("selectionStatus did not update across locale switch: en=%q zh=%q", en, zh)
	}
}

func BenchmarkFileIconFirstInit(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = buildIconIndex()
	}
}

func BenchmarkMatchFileIconCold(b *testing.B) {
	paths := make([]string, 0, 200)
	exts := []string{
		".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".md", ".json", ".yaml",
		".yml", ".toml", ".html", ".css", ".scss", ".sql", ".sh", ".c", ".h", ".cpp",
		".java", ".rb", ".php", ".swift", ".kt", ".vue", ".svelte", ".xml", ".svg", ".txt",
		".lock", ".env", ".gitignore", ".dockerignore", ".Makefile", ".unknownext",
	}
	specials := []string{
		"Makefile", "Dockerfile", "README.md", "LICENSE", "go.mod", "go.sum",
		"package.json", "tsconfig.json", "Cargo.toml", ".eslintrc.json",
		"docker-compose.yml", "AGENTS.md", "index.html", "main_test.go",
	}
	for _, s := range specials {
		paths = append(paths, "/repo/pkg/sub/"+s)
	}
	for i := 0; len(paths) < 200; i++ {
		ext := exts[i%len(exts)]
		paths = append(paths, fmt.Sprintf("/repo/src/dir%d/file_%d%s", i%10, i, ext))
	}
	// Warm up one-time init before timing uncached matching.
	matchFileIcon("/repo/warm.go")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		clear(fileIconRuleOf)
		for _, p := range paths {
			_ = matchFileIcon(p)
		}
	}
}

func BenchmarkMatchFileIconHot(b *testing.B) {
	paths := []string{
		"/repo/cmd/lime/app.go",
		"/repo/cmd/lime/view.go",
		"/repo/website/index.html",
		"/repo/go.mod",
		"/repo/README.md",
		"/repo/locales/en.json",
	}
	for _, p := range paths {
		_ = matchFileIcon(p)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, p := range paths {
			_ = matchFileIcon(p)
		}
	}
}

func setupBenchWorkspace(tb testing.TB, nDirs, filesPerDir int) (string, []string) {
	tb.Helper()
	root := tb.TempDir()
	var files []string
	content := []byte("package demo\n\n// Sample source file for benchmarking.\nfunc Compute(x int) int {\n\treturn x * 2\n}\n")
	for d := range nDirs {
		dir := filepath.Join(root, fmt.Sprintf("pkg%02d", d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatal(err)
		}
		for f := range filesPerDir {
			p := filepath.Join(dir, fmt.Sprintf("file_%03d.go", f))
			if err := os.WriteFile(p, content, 0o644); err != nil {
				tb.Fatal(err)
			}
			files = append(files, p)
		}
	}
	return root, files
}

func BenchmarkViewSteadyFrame(b *testing.B) {
	root, files := setupBenchWorkspace(b, 5, 20)
	a := newApp("")
	a.addFolder(root)
	// Expand all folders so sidebar renders 100 rows.
	for d := range 5 {
		*a.proj.isOpen(filepath.Join(root, fmt.Sprintf("pkg%02d", d)), false) = true
	}
	a.open(files[:8])
	a.marks = gitMarks{
		folders: []string{root},
		bases: map[string]gitBase{
			root: {
				marks: map[string]gitMark{
					"pkg00/file_000.go": gitModified,
					"pkg00":             gitModified,
					"pkg01/file_002.go": gitAdded,
					"pkg01":             gitAdded,
					"pkg02":             gitIgnored,
				},
			},
		},
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		tt.Frame()
	}
}

func BenchmarkViewLargeSidebarFrame(b *testing.B) {
	root, files := setupBenchWorkspace(b, 10, 50)
	a := newApp("")
	a.addFolder(root)
	for d := range 10 {
		*a.proj.isOpen(filepath.Join(root, fmt.Sprintf("pkg%02d", d)), false) = true
	}
	a.open(files[:6])
	a.marks = gitMarks{
		folders: []string{root},
		bases: map[string]gitBase{
			root: {
				marks: map[string]gitMark{
					"pkg00/file_000.go": gitModified,
					"pkg00":             gitModified,
					"pkg05":             gitIgnored,
				},
			},
		},
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		tt.Frame()
	}
}

func BenchmarkGitMarksLookup(b *testing.B) {
	root := "/repo/workspace/project"
	gm := gitMarks{
		folders: []string{root},
		bases: map[string]gitBase{
			root: {
				marks: map[string]gitMark{
					"cmd/lime/app.go":   gitModified,
					"cmd/lime":          gitModified,
					"cmd":               gitModified,
					"vendor":            gitIgnored,
					"website/index.html": gitAdded,
					"website":           gitAdded,
				},
			},
		},
	}
	paths := make([]string, 0, 100)
	for i := range 100 {
		switch i % 4 {
		case 0:
			paths = append(paths, fmt.Sprintf("%s/cmd/lime/file_%d.go", root, i))
		case 1:
			paths = append(paths, fmt.Sprintf("%s/vendor/pkg/dep_%d.go", root, i))
		case 2:
			paths = append(paths, fmt.Sprintf("%s/website/page_%d.html", root, i))
		default:
			paths = append(paths, fmt.Sprintf("%s/internal/sub/mod_%d.go", root, i))
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var acc gitMark
		for _, p := range paths {
			acc ^= gm.mark(p)
		}
		_ = acc
	}
}

func BenchmarkSessionRestore(b *testing.B) {
	root, files := setupBenchWorkspace(b, 3, 4)
	dir := b.TempDir()
	var docs []sessionDoc
	for _, p := range files {
		docs = append(docs, sessionDoc{Path: p, Syntax: "Go", Sels: [][2]int{{10, 10}}})
	}
	s := session{
		Windows: []sessionWindow{{
			Layout:  "Single",
			Focus:   0,
			Folders: []string{root},
			SideBar: "files",
			Groups:  []sessionGroup{{Active: 0, Docs: docs}},
		}},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), raw, 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l := newLime(dir)
		l.restoreSession()
	}
}

func BenchmarkSessionChanged(b *testing.B) {
	_, files := setupBenchWorkspace(b, 2, 10)
	a := newApp("")
	a.open(files)
	a.l.sessionChanged()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = a.l.sessionChanged()
	}
}

func BenchmarkOpenFiles(b *testing.B) {
	_, files := setupBenchWorkspace(b, 2, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		a := newApp("")
		a.open(files)
	}
}

func BenchmarkDirList(b *testing.B) {
	root, _ := setupBenchWorkspace(b, 1, 300)
	dir := filepath.Join(root, "pkg00")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var p project
		_ = p.list(dir, folderExclude, fileExclude)
	}
}

func BenchmarkTypingFrame(b *testing.B) {
	root, files := setupBenchWorkspace(b, 3, 20)
	a := newApp("")
	a.addFolder(root)
	a.open(files[:5])
	d := a.current()
	d.st.Select(0, 0)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		pos := d.st.Buffer.Len()
		d.st.Buffer.Edit(editor.Edit{Start: pos, End: pos, Text: "a"})
		if i%64 == 63 {
			d.st.Buffer.Set(strings.Repeat("func f() { return 1 }\n", 20))
		}
		tt.Frame()
	}
}

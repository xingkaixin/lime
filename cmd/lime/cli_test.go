package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCLIRequestAndRelativePaths(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "main.go")
	if err := os.WriteFile(file, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	colon := filepath.Join(sub, "foo:2")
	if err := os.WriteFile(colon, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := parseCLIRequest([]string{"-n", "main.go:2:1", "foo:2", "--", "-n"}, sub)
	if !req.newWindow {
		t.Fatal("expected newWindow=true")
	}
	if len(req.args) != 3 {
		t.Fatalf("got %d args, want 3: %+v", len(req.args), req.args)
	}
	if req.args[0] != (openArg{path: "main.go", line: 2, col: 1}) {
		t.Fatalf("arg[0] = %+v", req.args[0])
	}
	if req.args[1] != (openArg{path: "foo:2"}) {
		t.Fatalf("arg[1] = %+v, want literal colon file", req.args[1])
	}
	if req.args[2] != (openArg{path: "-n"}) {
		t.Fatalf("arg[2] = %+v, want literal -n after --", req.args[2])
	}
}

func TestOpenCLILaunchAndSecondInstance(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "project")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(f1, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Cold launch with -n and no restored session reuses the single initial window.
	l := newLime("")
	w0 := l.newWindow()
	l.openCLILaunch([]string{"-n", "a.txt"}, dir, false)
	if len(l.wins) != 1 {
		t.Fatalf("wins = %d, want 1", len(l.wins))
	}
	if w0.current() == nil || w0.current().path != f1 {
		t.Fatalf("w0 current = %v, want %s", w0.current(), f1)
	}

	// Cold launch with -n and an existing restored session opens a second window.
	l.openCLILaunch([]string{"--new", "b.txt"}, dir, true)
	if len(l.wins) != 2 {
		t.Fatalf("wins = %d, want 2", len(l.wins))
	}
	if l.last != l.wins[1] || l.last.current() == nil || l.last.current().path != f2 {
		t.Fatalf("new window current = %v, want %s", l.last.current(), f2)
	}

	// Second instance without -n opens project folder in the current window.
	l.openCLISecondInstance([]string{"project"}, dir)
	if len(l.wins) != 2 {
		t.Fatalf("wins = %d, want 2", len(l.wins))
	}
	if len(l.last.proj.folders) != 1 || l.last.proj.folders[0] != projDir {
		t.Fatalf("folders = %v, want [%s]", l.last.proj.folders, projDir)
	}

	// Second instance with -n opens file in a new window.
	l.openCLISecondInstance([]string{"-n", "a.txt"}, dir)
	if len(l.wins) != 3 {
		t.Fatalf("wins = %d, want 3", len(l.wins))
	}
	if l.last != l.wins[2] || l.last.current() == nil || l.last.current().path != f1 {
		t.Fatalf("third window current = %v, want %s", l.last.current(), f1)
	}

	// Bare "lime" does not open extra windows or tabs.
	docsBefore := l.last.docCount()
	l.openCLISecondInstance(nil, dir)
	if len(l.wins) != 3 || l.last.docCount() != docsBefore {
		t.Fatalf("bare invoke changed wins=%d docs=%d", len(l.wins), l.last.docCount())
	}
}

func TestOpenCLISecondInstanceNewWindowPreservesClosedSession(t *testing.T) {
	sessionDir := t.TempDir()
	workDir := t.TempDir()
	opened := filepath.Join(workDir, "cli.txt")
	if err := os.WriteFile(opened, []byte("from cli\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := newLime(sessionDir)
	w := l.newWindow()
	d := w.add("", "")
	d.st.Buffer.Set("unsaved work")
	if err := l.saveSession(); err != nil {
		t.Fatal(err)
	}
	l.closed(w)
	if len(l.wins) != 0 {
		t.Fatalf("expected 0 windows after closing last window, got %d", len(l.wins))
	}

	l.openCLISecondInstance([]string{"-n", "cli.txt"}, workDir)
	if len(l.wins) != 2 {
		t.Fatalf("wins = %d, want 2 (restored session + new CLI window)", len(l.wins))
	}
	if got := l.wins[0].current().st.Buffer.String(); got != "unsaved work" {
		t.Fatalf("restored window text = %q, want %q", got, "unsaved work")
	}
	if got := l.wins[1].current().path; got != opened {
		t.Fatalf("new CLI window path = %q, want %q", got, opened)
	}
}

func TestResolveCLIPathWindowsDriveRoot(t *testing.T) {
	if got := resolveCLIPathOS(`\notes.txt`, `C:\work\sub`, true); got != `C:\notes.txt` {
		t.Fatalf("got %q, want %q", got, `C:\notes.txt`)
	}
	if got := resolveCLIPathOS(`/dir/../notes.txt`, `D:\proj`, true); got != `D:\notes.txt` {
		t.Fatalf("got %q, want %q", got, `D:\notes.txt`)
	}
	if got := resolveCLIPathOS(`c:notes.txt`, `C:\work\sub`, true); got != `c:\work\sub\notes.txt` {
		t.Fatalf("got %q, want %q", got, `c:\work\sub\notes.txt`)
	}
}

func TestAppendWindowsPath(t *testing.T) {
	bin := `C:\Users\Alice\AppData\Local\Programs\Lime\bin`
	tests := []struct {
		name    string
		current string
		dir     string
		want    string
		changed bool
	}{
		{"empty", "", bin, bin, true},
		{"append", `C:\Windows\System32;`, bin, `C:\Windows\System32;` + bin, true},
		{"already present case insensitive", `c:\users\alice\appdata\local\programs\lime\bin\`, bin, `c:\users\alice\appdata\local\programs\lime\bin\`, false},
		{"already present quoted", `C:\Tools;"C:/Users/Alice/AppData/Local/Programs/Lime/bin"`, bin, `C:\Tools;"C:/Users/Alice/AppData/Local/Programs/Lime/bin"`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := appendWindowsPath(tc.current, tc.dir)
			if got != tc.want || changed != tc.changed {
				t.Fatalf("appendWindowsPath(%q, %q) = (%q, %v), want (%q, %v)", tc.current, tc.dir, got, changed, tc.want, tc.changed)
			}
		})
	}
}

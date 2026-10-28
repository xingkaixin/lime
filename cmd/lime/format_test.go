package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Changing a file's line endings leaves its tab unsaved, which the buffer
// alone would not tell, and saving writes them. Lines ending in \r alone
// show as lines.
func TestLineEndings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	os.WriteFile(p, []byte("a\r\nb\r\n"), 0o644)
	a := newApp("")
	a.open([]string{p})
	d := a.current()
	if d.lineEnding() != "\r\n" || d.dirty() {
		t.Fatalf("opened with %q, dirty %v", d.lineEnding(), d.dirty())
	}
	a.run(lineEndings[2].id())
	if !d.dirty() {
		t.Fatal("not dirty after changing the line endings")
	}
	a.save(d)
	if b, _ := os.ReadFile(p); string(b) != "a\rb\r" || d.dirty() {
		t.Fatalf("saved %q, dirty %v", b, d.dirty())
	}
	a.remove(d)
	a.open([]string{p})
	if d := a.current(); d.st.Buffer.LineCount() != 3 || d.lineEnding() != "\r" {
		t.Fatalf("reopened %q with %q", d.st.Buffer.String(), d.lineEnding())
	}
}

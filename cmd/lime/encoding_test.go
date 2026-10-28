package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// A file that isn't UTF-8 opens in the fallback encoding, reopens in
// another and saves in it, after hot exit too. Text the encoding has no
// characters for saves as UTF-8.
func TestEncoding(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	os.WriteFile(p, []byte("\xc4\xe3\xba\xc3\n"), 0o644)
	a := newApp(dir)
	a.open([]string{p})
	d := a.current()
	if d.encoding().name != defaultFallback {
		t.Fatalf("opened in %s", d.encoding().name)
	}
	a.run(encodingNamed("GBK").reopenID())
	if got := d.st.Buffer.String(); got != "你好\n" || d.dirty() {
		t.Fatalf("reopened as %q, dirty %v", got, d.dirty())
	}
	d.st.Buffer.Set("你好吗\n")
	if err := a.l.saveSession(); err != nil {
		t.Fatal(err)
	}
	l := newLime(dir)
	l.restoreSession()
	b, d := l.wins[0], l.wins[0].current()
	b.save(d)
	want, _ := simplifiedchinese.GBK.NewEncoder().String("你好吗\n")
	if got, _ := os.ReadFile(p); string(got) != want {
		t.Fatalf("saved %q, want %q", got, want)
	}
	d.st.Buffer.Set("😀\n")
	b.save(d)
	if got, _ := os.ReadFile(p); string(got) != "😀\n" || d.encoding() != utf8Encoding {
		t.Fatalf("saved %q in %s", got, d.encoding().name)
	}
}

// UTF-16 with a BOM opens as text rather than binary, and saves as it was.
func TestUTF16(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	raw := "\xff\xfeh\x00i\x00\n\x00"
	os.WriteFile(p, []byte(raw), 0o644)
	a := newApp("")
	a.open([]string{p})
	d := a.current()
	if d.kind != textDoc || d.st.Buffer.String() != "hi\n" {
		t.Fatalf("opened %q as kind %d", d.st.Buffer.String(), d.kind)
	}
	a.save(d)
	if got, _ := os.ReadFile(p); string(got) != raw {
		t.Fatalf("saved %q", got)
	}
}

// A file that its encoding would not save back as it was keeps its bytes,
// which saving writes as they were, and does not reopen in an encoding
// that would change them.
func TestEncodingKeepsBytes(t *testing.T) {
	for _, raw := range []string{
		"\x81@ \xc4\xe3\n",     // GBK's 丂, which Windows 1252 has no character for
		"\xff\xfeh\x00i\x00\n", // UTF-16 cut short
	} {
		p := filepath.Join(t.TempDir(), "a.txt")
		os.WriteFile(p, []byte(raw), 0o644)
		a := newApp("")
		a.open([]string{p})
		d := a.current()
		a.run(encodingNamed("Big5").reopenID())
		a.save(d)
		if got, _ := os.ReadFile(p); string(got) != raw || d.encoding() != utf8Encoding {
			t.Errorf("saved %q in %s, want %q", got, d.encoding().name, raw)
		}
	}
}

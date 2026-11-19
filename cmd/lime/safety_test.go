package main

import (
	"errors"
	"lime/locales"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestWriteFileAtomicAndSymlink(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(realPath, []byte("before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(realPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(realPath, []byte("after\n"), fi); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(realPath)
	if err != nil || string(got) != "after\n" {
		t.Fatalf("read %q, err %v", got, err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(realPath)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o640 {
			t.Fatalf("mode %o, want 0640", fi.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".lime-") {
			t.Fatalf("leftover temp file %q", e.Name())
		}
	}

	linkPath := filepath.Join(dir, "link.txt")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	a := newApp("")
	a.open([]string{linkPath})
	d := a.current()
	d.st.Buffer.Set("via symlink\n")
	if err := a.write(d, linkPath); err != nil {
		t.Fatal(err)
	}
	lfi, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if lfi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced by regular file: mode %v", lfi.Mode())
	}
	if got, _ := os.ReadFile(realPath); string(got) != "via symlink\n" {
		t.Fatalf("target content %q, want %q", got, "via symlink\n")
	}
}

func TestNewFilePermissions(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "new")
	reference := filepath.Join(dir, "reference")
	if err := os.WriteFile(reference, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(p, nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(reference)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode().Perm() != want.Mode().Perm() {
		t.Fatalf("mode %o, want %o", got.Mode().Perm(), want.Mode().Perm())
	}
}

func TestExternalChangeConflict(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(p, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newApp("")
	a.open([]string{p})
	d := a.current()
	if d.changedOnDisk() {
		t.Fatal("freshly opened doc reported changedOnDisk")
	}

	d.st.Buffer.Set("local edit\n")
	if err := os.WriteFile(p, []byte("external change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ensure mtime differs even on coarse-grained filesystems.
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, later, later)
	if !d.changedOnDisk() {
		t.Fatal("externally modified doc not detected")
	}

	var gotOpts mygo.MessageOptions
	a.messageDialog = func(opts mygo.MessageOptions) (mygo.MessageResult, error) {
		gotOpts = opts
		return mygo.MessageResult{Button: 0}, errors.New("dialog failed")
	}
	a.save(d)
	if gotOpts.CancelButton != 2 || gotOpts.DefaultButton != 2 || !slices.Equal(gotOpts.DestructiveButtons, []int{0}) {
		t.Fatalf("overwrite dialog options: cancel=%d destructive=%v", gotOpts.CancelButton, gotOpts.DestructiveButtons)
	}
	if got, _ := os.ReadFile(p); string(got) != "external change\n" {
		t.Fatalf("dialog error overwrote file: %q", got)
	}
	if !d.dirty() {
		t.Fatal("doc should stay dirty after dialog error")
	}

	// Cancel keeps both disk and buffer untouched.
	a.messageDialog = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 2}, nil
	}
	a.save(d)
	if got, _ := os.ReadFile(p); string(got) != "external change\n" || d.st.Buffer.String() != "local edit\n" {
		t.Fatalf("cancel changed disk=%q buf=%q", got, d.st.Buffer.String())
	}

	// Reload replaces buffer with external content and clears dirty.
	a.messageDialog = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 1}, nil
	}
	a.save(d)
	if got := d.st.Buffer.String(); got != "external change\n" || d.dirty() || d.changedOnDisk() {
		t.Fatalf("after reload: buf=%q dirty=%v changed=%v", got, d.dirty(), d.changedOnDisk())
	}

	// Overwrite forces buffer content onto disk.
	d.st.Buffer.Set("override\n")
	if err := os.WriteFile(p, []byte("external 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later = later.Add(2 * time.Second)
	_ = os.Chtimes(p, later, later)
	a.messageDialog = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 0}, nil
	}
	a.save(d)
	if got, _ := os.ReadFile(p); string(got) != "override\n" || d.dirty() || d.changedOnDisk() {
		t.Fatalf("after overwrite: disk=%q dirty=%v changed=%v", got, d.dirty(), d.changedOnDisk())
	}
}

func TestSaveCommandsRespectNewDiskFile(t *testing.T) {
	for _, command := range []string{"save", "save_all", "close", "close_window", "encoding"} {
		t.Run(command, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "new.txt")
			a := newApp("")
			a.l.newWindow()
			a.openCLI([]string{p})
			d := a.current()
			d.st.Buffer.Set("mine")
			if err := os.WriteFile(p, []byte("theirs"), 0600); err != nil {
				t.Fatal(err)
			}
			conflicts := 0
			a.messageDialog = func(o mygo.MessageOptions) (mygo.MessageResult, error) {
				if o.Message == locales.DialogChangedOnDisk(d.name()) {
					conflicts++
					return mygo.MessageResult{Button: 2}, nil
				}
				return mygo.MessageResult{Button: 0}, nil
			}
			switch command {
			case "save":
				a.save(d)
			case "save_all":
				a.saveAll()
			case "close":
				a.closeDoc(d)
			case "close_window":
				a.closing(&mygo.CloseEvent{})
			case "encoding":
				a.run(encodingNamed("GBK").saveID())
			}
			data, err := os.ReadFile(p)
			if err != nil || string(data) != "theirs" || conflicts != 1 || !d.dirty() || a.l.owner(d) != a {
				t.Fatalf("disk=%q err=%v conflicts=%d dirty=%v", data, err, conflicts, d.dirty())
			}
		})
	}
}

func TestDialogErrorAndStackedGuard(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("init\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := newLime("")
	a := l.newWindow()
	_ = l.newWindow() // second window so closing() prompts
	a.open([]string{p})
	d := a.current()
	d.st.Buffer.Set("dirty\n")

	var closeOpts mygo.MessageOptions
	calls := 0
	a.messageDialog = func(opts mygo.MessageOptions) (mygo.MessageResult, error) {
		calls++
		closeOpts = opts
		// Simulate re-entrant click while dialog is open.
		a.closeDoc(d)
		return mygo.MessageResult{Button: 0}, errors.New("cancelled by system")
	}
	a.closeDoc(d)
	if calls != 1 {
		t.Fatalf("expected stacked closeDoc to be ignored, got %d calls", calls)
	}
	if closeOpts.CancelButton != 2 || !slices.Equal(closeOpts.DestructiveButtons, []int{1}) {
		t.Fatalf("closeDoc options: cancel=%d destructive=%v", closeOpts.CancelButton, closeOpts.DestructiveButtons)
	}
	if a.docCount() != 1 {
		t.Fatalf("closeDoc error closed tab: docCount=%d", a.docCount())
	}
	if got, _ := os.ReadFile(p); string(got) != "init\n" {
		t.Fatalf("closeDoc error saved file: %q", got)
	}
	if d.prompting {
		t.Fatal("d.prompting remained true after dialog returned")
	}

	var winOpts mygo.MessageOptions
	a.messageDialog = func(opts mygo.MessageOptions) (mygo.MessageResult, error) {
		winOpts = opts
		return mygo.MessageResult{Button: 0}, errors.New("dialog error")
	}
	ev := &mygo.CloseEvent{}
	a.closing(ev)
	if winOpts.CancelButton != 2 || !slices.Equal(winOpts.DestructiveButtons, []int{1}) {
		t.Fatalf("closing options: cancel=%d destructive=%v", winOpts.CancelButton, winOpts.DestructiveButtons)
	}
	if got, _ := os.ReadFile(p); string(got) != "init\n" || a.docCount() != 1 {
		t.Fatalf("closing error saved or removed doc: disk=%q count=%d", got, a.docCount())
	}
}

func TestSymlinkDedupAndSinglePassRead(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "AGENTS.md")
	bigText := strings.Repeat("hello world\n", 1000) // > 8000 bytes
	if err := os.WriteFile(realPath, []byte(bigText), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "CLAUDE.md")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	a := newApp("")
	a.open([]string{realPath})
	otherPath := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(otherPath, []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.open([]string{otherPath})
	if a.current().path != otherPath {
		t.Fatalf("active tab %q, want %q", a.current().path, otherPath)
	}

	// Opening the symlink activates the existing AGENTS.md tab without duplicating it.
	a.open([]string{linkPath})
	if a.docCount() != 2 {
		t.Fatalf("docCount=%d, want 2 after opening symlink", a.docCount())
	}
	if a.current().path != realPath {
		t.Fatalf("active tab %q, want %q", a.current().path, realPath)
	}
	if a.current().st.Buffer.String() != bigText {
		t.Fatalf("single-pass read length=%d, want %d", a.current().st.Buffer.Len(), len(bigText))
	}

	binPath := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(binPath, []byte("abc\x00def"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.open([]string{binPath})
	if a.current().kind != binaryDoc {
		t.Fatalf("binary file opened as kind %d, want binaryDoc", a.current().kind)
	}
}

func TestSaveAllChaining(t *testing.T) {
	dir := t.TempDir()
	namedPath := filepath.Join(dir, "named.txt")
	if err := os.WriteFile(namedPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	u1Path := filepath.Join(dir, "u1.txt")
	u2Path := filepath.Join(dir, "u2.txt")

	a := newApp("")
	a.open([]string{namedPath})
	named := a.current()
	named.st.Buffer.Set("new named\n")

	u1 := a.add("", "")
	u1.st.Buffer.Set("untitled 1\n")
	u2 := a.add("", "")
	u2.st.Buffer.Set("untitled 2\n")

	// Cancel first untitled save, accept second.
	picks := []string{"", u2Path}
	i := 0
	a.saveDialog = func(mygo.SaveDialogOptions) (string, error) {
		p := picks[i]
		i++
		return p, nil
	}
	var allSaved bool
	a.saveAllThen(func(ok bool) { allSaved = ok })
	if i != 2 {
		t.Fatalf("prompted %d times, want 2", i)
	}
	if allSaved {
		t.Fatal("saveAllThen reported true despite canceled untitled doc")
	}
	if got, _ := os.ReadFile(namedPath); string(got) != "new named\n" {
		t.Fatalf("named doc content %q", got)
	}
	if !u1.dirty() || u1.path != "" {
		t.Fatalf("u1 should remain dirty untitled, got path=%q dirty=%v", u1.path, u1.dirty())
	}
	if u2.dirty() || u2.path != u2Path {
		t.Fatalf("u2 should be saved at %q, got path=%q dirty=%v", u2Path, u2.path, u2.dirty())
	}

	// Now save remaining dirty untitled doc; allSaved should be true.
	a.saveDialog = func(mygo.SaveDialogOptions) (string, error) {
		return u1Path, nil
	}
	a.saveAllThen(func(ok bool) { allSaved = ok })
	if !allSaved || u1.dirty() || u1.path != u1Path {
		t.Fatalf("second saveAllThen: allSaved=%v path=%q dirty=%v", allSaved, u1.path, u1.dirty())
	}
	if got, _ := os.ReadFile(u1Path); string(got) != "untitled 1\n" {
		t.Fatalf("u1 content %q", got)
	}
}

func TestSaveWithEncodingCancelPreservesEncoding(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "gbk.txt")

	a := newApp("")
	d := a.add("", "")
	d.st.Buffer.Set("你好\n")

	gbk := encodingNamed("GBK")
	a.saveDialog = func(mygo.SaveDialogOptions) (string, error) {
		return "", nil
	}
	a.run(gbk.saveID())
	if d.enc != nil || d.encoding() != utf8Encoding {
		t.Fatalf("canceling Save with Encoding mutated d.enc to %v", d.enc)
	}

	a.saveDialog = func(mygo.SaveDialogOptions) (string, error) {
		return outPath, nil
	}
	a.run(gbk.saveID())
	if d.encoding() != gbk || d.path != outPath {
		t.Fatalf("after saving: enc=%v path=%q", d.encoding().name, d.path)
	}
	want, _ := simplifiedchinese.GBK.NewEncoder().String("你好\n")
	if got, _ := os.ReadFile(outPath); string(got) != want {
		t.Fatalf("saved %q, want %q", got, want)
	}
}

func TestKeymapDuplicateKeyNoSelfConflict(t *testing.T) {
	km := newKeymap([]keyChange{
		{Command: "duplicate_line", Keys: []string{"super+alt+f12", "super+alt+f12"}},
	})
	keys := km.keys["duplicate_line"]
	if len(keys) != 1 {
		t.Fatalf("duplicate_line has %d keys (%v), want 1", len(keys), keys)
	}
	if km.conflicted(keys[0]) {
		t.Fatalf("command conflicted with itself: users=%v", km.users[keys[0].seq()])
	}
}

func TestCLIArgsAndNonExistentFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(src, []byte("first\nhéllo world\nthird\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dash := filepath.Join(dir, "-dash.txt")
	if err := os.WriteFile(dash, []byte("dashed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	literalColon := filepath.Join(dir, "colon:2")
	if err := os.WriteFile(literalColon, []byte("literal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "brand_new.go")

	a := newApp("")
	// "--" allows filenames starting with "-", while flags before "--" are skipped.
	parsed := parseCLIArgs([]string{"-ignored", "--", "-flaglike.txt"})
	if len(parsed) != 1 || parsed[0].path != "-flaglike.txt" {
		t.Fatalf("parseCLIArgs with --: %+v", parsed)
	}

	a.openCLI([]string{src + ":2:3"})
	d := a.current()
	if d.path != src {
		t.Fatalf("opened %q, want %q", d.path, src)
	}
	// Line 2 is "héllo world": column 3 (1-based rune) is byte offset 3 ('h' + 2-byte 'é').
	if got := d.st.CursorPos(); got != (editor.Pos{Line: 1, Col: 3}) {
		t.Fatalf("cursor at %+v, want {Line:1 Col:3}", got)
	}

	a.openCLI([]string{literalColon})
	if a.current().path != literalColon || a.current().st.Buffer.String() != "literal\n" {
		t.Fatalf("literal colon file: path=%q text=%q", a.current().path, a.current().st.Buffer.String())
	}

	a.openCLI([]string{"--", dash})
	if a.current().path != dash || a.current().st.Buffer.String() != "dashed\n" {
		t.Fatalf("dash file: path=%q text=%q", a.current().path, a.current().st.Buffer.String())
	}

	a.openCLI([]string{missing})
	nd := a.current()
	if nd.path != missing || nd.disk != (stamp{}) {
		t.Fatalf("non-existent CLI file: path=%q disk=%+v", nd.path, nd.disk)
	}
	nd.st.Buffer.Set("package main\n")
	a.save(nd)
	if got, err := os.ReadFile(missing); err != nil || string(got) != "package main\n" {
		t.Fatalf("saving new CLI file: got=%q err=%v", got, err)
	}
}

func TestSessionSideBarAndAutosaveRetry(t *testing.T) {
	dir := t.TempDir()
	l := newLime(dir)
	a := l.newWindow()
	a.newFile()

	// Prime sessionKey.
	if !l.sessionChanged() {
		t.Fatal("initial sessionChanged should be true")
	}
	if l.sessionChanged() {
		t.Fatal("unchanged sessionChanged should be false")
	}
	a.side = gitPanel
	if !l.sessionChanged() {
		t.Fatal("changing a.side did not trigger sessionChanged")
	}

	// Block session.json with a directory so writeSession fails.
	blocker := filepath.Join(dir, "session.json")
	if err := os.Mkdir(blocker, 0o755); err != nil {
		t.Fatal(err)
	}
	a.side = filesPanel
	l.autosave()
	// Wait for background writeSession goroutine to fail and reset sessionKey.
	deadline := time.Now().Add(2 * time.Second)
	for {
		l.sessionMu.Lock()
		key := l.sessionKey
		l.sessionMu.Unlock()
		if key == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("autosave failure did not reset sessionKey")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	l.autosave()
	deadline = time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(blocker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("autosave did not retry after failure")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

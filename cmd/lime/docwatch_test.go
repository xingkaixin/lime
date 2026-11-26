package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
)

type fakeDocWatcher struct {
	events chan []mygo.FileEvent
	closed chan struct{}
	once   sync.Once
}

func newFakeDocWatcher() *fakeDocWatcher {
	return &fakeDocWatcher{
		events: make(chan []mygo.FileEvent, 4),
		closed: make(chan struct{}),
	}
}

func (w *fakeDocWatcher) Next(ctx context.Context) ([]mygo.FileEvent, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case ev, ok := <-w.events:
		if !ok {
			return nil, io.EOF
		}
		return ev, nil
	}
}

func (w *fakeDocWatcher) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

type docWatchHarness struct {
	t        *testing.T
	mu       sync.Mutex
	started  map[string][]*fakeDocWatcher
	mainQ    chan func()
	stopMain chan struct{}
}

func newDocWatchHarness(t *testing.T) *docWatchHarness {
	t.Helper()
	h := &docWatchHarness{
		t:        t,
		started:  make(map[string][]*fakeDocWatcher),
		mainQ:    make(chan func(), 16),
		stopMain: make(chan struct{}),
	}
	origStart := startDocWatch
	origMain := runDocWatchOnMain
	origLive := docWatchWindowLive
	startDocWatch = func(_ context.Context, dir string) (docDirWatcher, error) {
		w := newFakeDocWatcher()
		h.mu.Lock()
		h.started[dir] = append(h.started[dir], w)
		h.mu.Unlock()
		return w, nil
	}
	runDocWatchOnMain = func(fn func()) {
		done := make(chan struct{})
		select {
		case h.mainQ <- func() {
			defer close(done)
			fn()
		}:
		case <-h.stopMain:
			return
		}
		select {
		case <-done:
		case <-h.stopMain:
		}
	}
	docWatchWindowLive = func(*app) bool { return true }
	t.Cleanup(func() {
		close(h.stopMain)
		startDocWatch = origStart
		runDocWatchOnMain = origMain
		docWatchWindowLive = origLive
	})
	return h
}

func (h *docWatchHarness) waitStarted(dir string, count int) *fakeDocWatcher {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		list := h.started[dir]
		if len(list) >= count {
			w := list[count-1]
			h.mu.Unlock()
			return w
		}
		h.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for watch #%d on %q", count, dir)
	return nil
}

func (h *docWatchHarness) waitClosed(w *fakeDocWatcher) {
	h.t.Helper()
	select {
	case <-w.closed:
	case <-time.After(2 * time.Second):
		h.t.Fatal("timed out waiting for watcher to close")
	}
}

func (h *docWatchHarness) stepMain() {
	h.t.Helper()
	select {
	case fn := <-h.mainQ:
		fn()
	case <-time.After(2 * time.Second):
		h.t.Fatal("timed out waiting for main-thread watch callback")
	}
}

func writeWithFutureTime(t *testing.T, path, content string, offset time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(offset)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func TestDocWatchReloadsStandaloneFileWithoutFocusOrSidebar(t *testing.T) {
	h := newDocWatchHarness(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(p, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newApp("")
	defer a.l.closed(a)
	a.side = noPanel
	a.open([]string{p})
	d := a.docAt(p)

	watchDir := docWatchDir(p)
	w := h.waitStarted(watchDir, 1)

	writeWithFutureTime(t, p, "v2 from outside\n", 2*time.Second)
	w.events <- []mygo.FileEvent{{Op: mygo.FileHint, Path: "."}}
	h.stepMain()

	if got := d.st.Buffer.String(); got != "v2 from outside\n" {
		t.Fatalf("expected external edit to reload without focus, got %q", got)
	}
}

func TestDocWatchDeduplicatesDirAndReleasesOnClose(t *testing.T) {
	h := newDocWatchHarness(t)
	dirA := t.TempDir()
	dirB := t.TempDir()
	a1 := filepath.Join(dirA, "one.go")
	a2 := filepath.Join(dirA, "two.go")
	b1 := filepath.Join(dirB, "three.go")
	for _, p := range []string{a1, a2, b1} {
		if err := os.WriteFile(p, []byte("init\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a := newApp("")
	defer a.l.closed(a)
	a.open([]string{a1, a2, b1})

	keyA := docWatchDir(a1)
	keyB := docWatchDir(b1)
	wA := h.waitStarted(keyA, 1)
	wB := h.waitStarted(keyB, 1)

	h.mu.Lock()
	startedA := len(h.started[keyA])
	h.mu.Unlock()
	if startedA != 1 {
		t.Fatalf("expected 1 deduplicated watcher for %q, got %d", keyA, startedA)
	}

	a.remove(a.docAt(a1))
	select {
	case <-wA.closed:
		t.Fatal("watcher closed while another tab in the same dir is still open")
	default:
	}

	a.remove(a.docAt(a2))
	h.waitClosed(wA)
	if _, ok := a.l.docWatches.watches[keyA]; ok {
		t.Fatalf("expected %q watch entry to be removed", keyA)
	}

	a.l.closed(a)
	h.waitClosed(wB)
	if len(a.l.docWatches.watches) != 0 {
		t.Fatalf("expected no active watches after window close, got %d", len(a.l.docWatches.watches))
	}
}

func TestDocWatchSkipsDirtyTabAndReloadsOnlyTargetDir(t *testing.T) {
	h := newDocWatchHarness(t)
	dirA := t.TempDir()
	dirB := t.TempDir()
	pA := filepath.Join(dirA, "a.txt")
	pB := filepath.Join(dirB, "b.txt")
	pDirty := filepath.Join(dirA, "dirty.txt")
	for _, p := range []string{pA, pB, pDirty} {
		if err := os.WriteFile(p, []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a := newApp("")
	defer a.l.closed(a)
	a.open([]string{pA, pB, pDirty})
	dA, dB, dDirty := a.docAt(pA), a.docAt(pB), a.docAt(pDirty)
	dDirty.st.Buffer.Edit(editor.Edit{Start: 0, End: 0, Text: "unsaved "})

	wA := h.waitStarted(docWatchDir(pA), 1)
	_ = h.waitStarted(docWatchDir(pB), 1)

	writeWithFutureTime(t, pA, "new-a\n", 2*time.Second)
	writeWithFutureTime(t, pB, "new-b\n", 2*time.Second)
	writeWithFutureTime(t, pDirty, "new-dirty\n", 2*time.Second)

	wA.events <- []mygo.FileEvent{{Op: mygo.FileHint, Path: "."}}
	h.stepMain()

	if got := dA.st.Buffer.String(); got != "new-a\n" {
		t.Fatalf("a.txt should have reloaded, got %q", got)
	}
	if got := dDirty.st.Buffer.String(); got != "unsaved old\n" {
		t.Fatalf("dirty.txt should not be overwritten, got %q", got)
	}
	if got := dB.st.Buffer.String(); got != "old\n" {
		t.Fatalf("b.txt in unwatched event dir should not be reloaded yet, got %q", got)
	}
}

func TestDocWatchFollowsRenameAndSaveAs(t *testing.T) {
	h := newDocWatchHarness(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	p1 := filepath.Join(dir1, "f.txt")
	p2 := filepath.Join(dir2, "f.txt")
	if err := os.WriteFile(p1, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newApp("")
	defer a.l.closed(a)
	a.open([]string{p1})
	d := a.docAt(p1)

	w1 := h.waitStarted(docWatchDir(p1), 1)

	if err := a.writeIn(d, p2, d.encoding()); err != nil {
		t.Fatal(err)
	}
	h.waitClosed(w1)
	w2 := h.waitStarted(docWatchDir(p2), 1)

	writeWithFutureTime(t, p2, "two\n", 2*time.Second)
	w2.events <- []mygo.FileEvent{{Op: mygo.FileHint, Path: "."}}
	h.stepMain()
	if got := d.st.Buffer.String(); got != "two\n" {
		t.Fatalf("expected saved-as tab to reload from new dir, got %q", got)
	}
}

func TestDocWatchRecoversAfterWatcherEOF(t *testing.T) {
	h := newDocWatchHarness(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newApp("")
	defer a.l.closed(a)
	a.open([]string{p})
	watchDir := docWatchDir(p)
	w1 := h.waitStarted(watchDir, 1)

	close(w1.events)
	h.stepMain()
	h.waitClosed(w1)
	if _, ok := a.l.docWatches.watches[watchDir]; ok {
		t.Fatal("expected ended watch entry to be dropped")
	}

	a.l.syncDocWatches()
	_ = h.waitStarted(watchDir, 2)
}

func TestDocWatchFollowsSymlinkTargetDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}
	h := newDocWatchHarness(t)
	realDir := t.TempDir()
	linkDir := t.TempDir()
	realFile := filepath.Join(realDir, "real.txt")
	linkFile := filepath.Join(linkDir, "link.txt")
	if err := os.WriteFile(realFile, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Fatal(err)
	}

	a := newApp("")
	defer a.l.closed(a)
	a.open([]string{linkFile})
	d := a.docAt(linkFile)

	w := h.waitStarted(docWatchDir(realFile), 1)
	writeWithFutureTime(t, realFile, "after\n", 2*time.Second)
	w.events <- []mygo.FileEvent{{Op: mygo.FileHint, Path: "."}}
	h.stepMain()

	if got := d.st.Buffer.String(); got != "after\n" {
		t.Fatalf("expected symlinked file to reload when target changed, got %q", got)
	}
}

func TestReloadDocsQueuesWhileReloadInFlight(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "queued.txt")
	if err := os.WriteFile(p, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newApp("")
	a.open([]string{p})
	d := a.docAt(p)

	a.reloading = true
	writeWithFutureTime(t, p, "v2\n", 2*time.Second)
	a.reloadDocs([]*doc{d})
	if !a.reloadPending[d] {
		t.Fatal("expected doc to be queued while reload is in flight")
	}

	a.applyReloads(nil)
	if got := d.st.Buffer.String(); got != "v2\n" {
		t.Fatalf("expected queued doc reload to run after in-flight reload, got %q", got)
	}
}

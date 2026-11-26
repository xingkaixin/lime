package main

import (
	"context"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
)

// watchOpenDocs reports whether Lime watches the parent directories of open
// files for external edits while unfocused. Windows is excluded for the same
// reason as watchDirs: an open directory handle blocks renaming folders above it.
const watchOpenDocs = watchDirs && runtime.GOOS != "js"

const docWatchDebounce = 100 * time.Millisecond

type docDirWatcher interface {
	Next(context.Context) ([]mygo.FileEvent, error)
	Close() error
}

var (
	startDocWatch = func(ctx context.Context, dir string) (docDirWatcher, error) {
		return mygo.WatchFiles(ctx, dir, mygo.FileWatchOptions{
			HintsOnly: true,
			Debounce:  docWatchDebounce,
		})
	}
	runDocWatchOnMain  = mygo.RunOnMain
	docWatchWindowLive = func(a *app) bool { return watchOpenDocs && a.win != nil }
)

type docWatchSet struct {
	watches map[string]context.CancelFunc
}

func docWatchDir(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Dir(openedPathKey(path))
}

func (d *doc) watchTargetDir() string {
	if d.path == "" || d.kind != textDoc || d.cmp != nil {
		return ""
	}
	if d.watchPath != d.path {
		d.watchPath = d.path
		d.watchDir = docWatchDir(d.path)
	}
	return d.watchDir
}

// syncDocWatches reconciles directory watches with the parent directories of
// all open text tabs across live windows, deduplicating tabs that share a folder.
func (l *lime) syncDocWatches() {
	var want map[string]bool
	for _, a := range l.wins {
		if !docWatchWindowLive(a) {
			continue
		}
		for _, d := range a.allDocs() {
			if dir := d.watchTargetDir(); dir != "" {
				if want == nil {
					want = make(map[string]bool)
				}
				want[dir] = true
			}
		}
	}
	for dir, stop := range l.docWatches.watches {
		if !want[dir] {
			stop()
			delete(l.docWatches.watches, dir)
		}
	}
	if len(want) == 0 {
		return
	}
	if l.docWatches.watches == nil {
		l.docWatches.watches = make(map[string]context.CancelFunc, len(want))
	}
	for dir := range want {
		if l.docWatches.watches[dir] == nil {
			l.watchDocDir(dir)
		}
	}
}

func (l *lime) watchDocDir(dir string) {
	ctx, stop := context.WithCancel(context.Background())
	l.docWatches.watches[dir] = stop
	ended := func() {
		runDocWatchOnMain(func() {
			if ctx.Err() != nil {
				return
			}
			stop()
			delete(l.docWatches.watches, dir)
			l.reloadWatchedDir(dir)
		})
	}
	go func() {
		w, err := startDocWatch(ctx, dir)
		if err != nil {
			return
		}
		defer w.Close()
		for {
			events, err := w.Next(ctx)
			if err != nil {
				if ctx.Err() == nil {
					ended()
				}
				return
			}
			if len(events) > 0 {
				runDocWatchOnMain(func() {
					if ctx.Err() != nil {
						return
					}
					l.reloadWatchedDir(dir)
				})
			}
		}
	}()
}

func (l *lime) reloadWatchedDir(dir string) {
	for _, w := range l.wins {
		if !docWatchWindowLive(w) {
			continue
		}
		var docs []*doc
		for _, d := range w.allDocs() {
			if d.watchTargetDir() == dir {
				docs = append(docs, d)
			}
		}
		if len(docs) > 0 {
			w.reloadDocs(docs)
		}
	}
}

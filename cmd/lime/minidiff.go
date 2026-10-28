package main

import (
	"path/filepath"

	"github.com/egoist/mygo/ui/editor"
)

// The gutter marks a tab's changes as Sublime Text's mini_diff does:
// against the file's text in git, in the index or at HEAD as
// git_diff_target says, or against the text on disk for a file git does
// not track.

// diffRev is the revision of the files' texts in git that the gutter
// marks the changes against: "" is the index.
func (s *settings) diffRev() string {
	if s.GitDiffTarget == "head" {
		return "HEAD"
	}
	return ""
}

// readBases reads the texts in git of the files of docs, which the
// gutter marks their changes against: as they open and are saved, as the
// window comes to the front and as git changes them.
func (a *app) readBases(docs ...*doc) {
	var ds []*doc
	var paths []string
	for _, d := range docs {
		if d.path != "" && d.saved.Len() <= largeFile {
			ds, paths = append(ds, d), append(paths, d.path)
		}
	}
	if len(ds) == 0 {
		return
	}
	rev := a.set.diffRev()
	a.background(func() func() {
		objs := gitTexts(paths, rev)
		return func() {
			for i, d := range ds {
				switch o := objs[i]; {
				// Saved elsewhere since, or the same text.
				case d.path != paths[i] || o.id == d.base:
				case o.id == "":
					d.diffDisk()
				default:
					text, _ := splitLines(o.text)
					d.setDiffBase(o.id, text)
				}
			}
		}
	})
}

// gitTexts reads the files at paths as rev has them, with one git for
// the files of a folder, which finds the repository of each. It blocks,
// so call it off the main thread.
func gitTexts(paths []string, rev string) []object {
	objs := make([]object, len(paths))
	byDir := map[string][]int{}
	files := make([]string, len(paths))
	for i, p := range paths {
		// git keeps a link's target path, not the text the tab shows.
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		files[i] = p
		dir := filepath.Dir(p)
		byDir[dir] = append(byDir[dir], i)
	}
	for dir, at := range byDir {
		specs := make([]string, len(at))
		for k, i := range at {
			specs[k] = rev + ":./" + filepath.Base(files[i])
		}
		for k, o := range catObjects(dir, specs) {
			objs[at[k]] = o
		}
	}
	return objs
}

// diffDisk has the gutter mark the changes against the text on disk.
func (d *doc) diffDisk() {
	text := ""
	if d.saved.Len() <= largeFile {
		text = d.saved.String()
	}
	d.setDiffBase("", text)
}

// setDiffBase has the gutter mark the changes against text: the file's
// in git, of the object id, or on disk, of "".
func (d *doc) setDiffBase(id, text string) {
	d.base = id
	switch {
	case d.saved.Len() > largeFile || len(text) > largeFile:
		d.st.DiffBase = nil
	case d.st.DiffBase == nil:
		d.st.DiffBase = editor.NewBuffer(text)
	default:
		d.st.DiffBase.Reset(text)
	}
}

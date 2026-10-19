package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// docKind is what a doc shows: its text in the editor, or else a file
// that is no text.
type docKind int

const (
	textDoc   docKind = iota
	imageDoc          // shown as an image
	binaryDoc         // shown as a note that it is binary
)

// imageExts are the images DecodeBitmap reads.
var imageExts = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"}

// fileKind tells how the file at path shows: an image by its extension,
// binary by a NUL in its first 8000 bytes, as Git tells, or else text.
func fileKind(path string) docKind {
	if slices.Contains(imageExts, strings.ToLower(filepath.Ext(path))) {
		return imageDoc
	}
	f, err := os.Open(path)
	if err != nil {
		return textDoc
	}
	defer f.Close()
	b := make([]byte, 8000)
	n, _ := io.ReadFull(f, b)
	if bytes.IndexByte(b[:n], 0) >= 0 {
		return binaryDoc
	}
	return textDoc
}

// loadImage decodes the image of d off the main thread, once. One that
// does not decode shows as binary.
func (a *app) loadImage(d *doc) {
	if d.img != nil || d.loading || a.win == nil {
		return
	}
	d.loading = true
	path := d.path
	go func() {
		b, err := os.ReadFile(path)
		var img *ui.Bitmap
		if err == nil {
			img, err = ui.DecodeBitmap(b)
		}
		a.win.Update(func() {
			d.loading = false
			if err != nil {
				d.kind = binaryDoc
			}
			d.img = img
			// The tab may have moved to another window.
			a.l.redraw()
		})
	}()
}

// preview shows d, a file that is no text, in place of the editor of the
// group at gi.
func (a *app) preview(c *ui.Context, gi int, d *doc) {
	s := a.scheme
	box := ui.Column(c.Key(key(d))).Grow(1).FillWidth().MinHeight(0).Padding(24).Center().Background(s.bg).
		OnClick(func() { a.focusGroup(gi) })
	box.Children(func() {
		switch {
		case d.kind == binaryDoc:
			ui.Text(c, locales.PreviewBinary()).FontSize(13).TextColor(s.chrome().muted)
		case d.img != nil:
			ui.Image(c, d.img).Fill().Fit(ui.ScaleDown)
		default:
			a.loadImage(d)
		}
	})
}

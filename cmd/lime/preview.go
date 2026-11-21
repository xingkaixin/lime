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

type docKind int

const (
	textDoc docKind = iota
	imageDoc
	binaryDoc
	diffDoc
	commitDoc
	findDoc
	mdDoc
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
	// UTF-16 has NULs in the bytes of most characters.
	if bytes.HasPrefix(b[:n], []byte("\xff\xfe")) || bytes.HasPrefix(b[:n], []byte("\xfe\xff")) {
		return textDoc
	}
	if bytes.IndexByte(b[:n], 0) >= 0 {
		return binaryDoc
	}
	return textDoc
}

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

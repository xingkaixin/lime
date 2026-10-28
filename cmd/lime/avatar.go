package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// avatarURL is the picture GitHub keeps for the account of email, as Zed
// asks for it: by the email, from GitHub's CDN, without its API. Bots have
// none.
func avatarURL(email string) string {
	if strings.HasSuffix(email, "[bot]@users.noreply.github.com") {
		return ""
	}
	return "https://avatars.githubusercontent.com/u/e?email=" + url.QueryEscape(email) + "&s=128"
}

// loadAvatar reads the picture of email from the cache in dir, else
// fetches it there. An empty file marks an email with none. It blocks,
// so call it off the main thread.
func loadAvatar(dir, email string) *ui.Bitmap {
	sum := sha256.Sum256([]byte(strings.ToLower(email)))
	path := filepath.Join(dir, "avatars", hex.EncodeToString(sum[:]))
	b, err := os.ReadFile(path)
	if err != nil {
		if b, err = fetchAvatar(email); err != nil {
			return nil
		}
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
			os.WriteFile(path, b, 0o644)
		}
	}
	if len(b) == 0 {
		return nil
	}
	bm, _ := ui.DecodeBitmap(b)
	return bm
}

var personIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<g fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round">
		<circle cx="8" cy="5.5" r="2.5" />
		<path d="M3.5 13c.5-2.3 2.3-3.5 4.5-3.5s4 1.2 4.5 3.5" />
	</g>
</svg>`))

// avatar shows the picture of the author of email where the repository
// is on GitHub, and a person in a circle until it loads or where it has
// none, as Zed does.
func (a *app) avatar(c *ui.Context, email string, size float32) {
	ch := a.shell(c)
	var bm *ui.Bitmap
	if a.hist.github {
		bm = a.l.avatar(a, email)
	}
	box := ui.Box(c).Size(size, size).Shrink(0).Radius(size / 2).Clip().Center()
	box.Children(func() {
		if bm != nil {
			ui.Image(c, bm).Size(size, size)
			return
		}
		box.Background(ch.hover)
		ui.Icon(c, personIcon).Size(size*0.7, size*0.7).TextColor(ch.muted)
	})
}

// avatar returns the picture of email once loaded, nil until then or
// where there is none, loading it for the window a. On the main thread,
// which alone touches the map.
func (l *lime) avatar(a *app, email string) *ui.Bitmap {
	if bm, ok := l.avatars[email]; ok {
		return bm
	}
	if l.avatars == nil {
		l.avatars = map[string]*ui.Bitmap{}
	}
	l.avatars[email] = nil
	// Tests open no window, and fetch nothing.
	if a.win == nil || l.dir == "" {
		return nil
	}
	dir := l.dir
	go func() {
		bm := loadAvatar(dir, email)
		a.win.Update(func() {
			l.avatars[email] = bm
			l.redraw()
		})
	}()
	return nil
}

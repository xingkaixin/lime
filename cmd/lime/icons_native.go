//go:build !js

package main

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

// fileIconFonts are the icon fonts of file-icons
// (https://github.com/file-icons/atom), whose rules, glyphs and colours
// fileicons/update.sh writes to icons_gen.go.
//
//go:embed fileicons/*.ttf
var fileIconFonts embed.FS

var (
	iconFontMu     sync.Mutex
	iconFontLoaded = map[string]bool{}
)

// ensureIconFont registers the requested icon font on first use instead of
// parsing all five embedded TrueType fonts upfront on the first frame.
func ensureIconFont(font string) {
	if font == "" {
		return
	}
	iconFontMu.Lock()
	if iconFontLoaded[font] {
		iconFontMu.Unlock()
		return
	}
	iconFontLoaded[font] = true
	iconFontMu.Unlock()
	if data, err := fileIconFonts.ReadFile("fileicons/" + font + ".ttf"); err == nil {
		ui.RegisterFont(data, fileIconFamily(font))
	}
}

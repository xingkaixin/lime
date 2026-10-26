package main

import (
	"embed"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// fileIconFonts are the icon fonts of file-icons
// (https://github.com/file-icons/atom), whose rules, glyphs and colours
// fileicons/update.sh writes to icons_gen.go.
//
//go:embed fileicons/*.ttf
var fileIconFonts embed.FS

type fileIconRule struct {
	icon, dark, light string // the colours, for dark and light themes
	pattern           string
	priority          float64
	matchPath         bool // matches the path rather than the name
}

type fileIconGlyph struct {
	font, char string
	size       float64 // in CSS pixels, of a 16 pixel row
}

// rowIcon is the icon of a side bar row: an SVG, or a glyph of an icon
// font, and its colour, zero for the muted one.
type rowIcon struct {
	svg        *ui.SVG
	font, char string
	scale      float32 // of the row's size
	color      ui.Color
}

// tint is the icon's colour, or else fallback.
func (ic rowIcon) tint(fallback ui.Color) ui.Color {
	if ic.color == (ui.Color{}) {
		return fallback
	}
	return ic.color
}

func (ic rowIcon) view(c *ui.Context, size float32, tint ui.Color) {
	if ic.svg != nil {
		ui.Icon(c, ic.svg).Size(size, size).Shrink(0).TextColor(tint)
		return
	}
	ui.Box(c).Size(size, size).Shrink(0).Center().Children(func() {
		ui.Text(c, ic.char).Font(ic.font).FontSize(size * ic.scale).TextColor(tint).SingleLine()
	})
}

var (
	folderIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<path fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round" d="M4.5 4.5H12c.83 0 1.5.67 1.5 1.5v6c0 .83-.67 1.5-1.5 1.5H2A1.5 1.5 0 01.5 12V3.5a1 1 0 011-1h5a1 1 0 011 1v1" />
</svg>`))
	folderOpenIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<path fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round" d="m1.87 8 .7-2.74a1 1 0 01.96-.76h10.94a1 1 0 01.97 1.24l-1.75 7a1 1 0 01-.97.76H2A1.5 1.5 0 01.5 12V3.5a1 1 0 011-1h5a1 1 0 011 1v1" />
</svg>`))
)

var fileIconPatterns = sync.OnceValue(func() []*regexp.Regexp {
	files, _ := fileIconFonts.ReadDir("fileicons")
	for _, f := range files {
		data, err := fileIconFonts.ReadFile("fileicons/" + f.Name())
		if err == nil {
			ui.RegisterFont(data, fileIconFamily(strings.TrimSuffix(f.Name(), ".ttf")))
		}
	}
	compiled := make([]*regexp.Regexp, len(fileIconRules))
	for i, r := range fileIconRules {
		compiled[i] = regexp.MustCompile(r.pattern)
	}
	return compiled
})

func fileIconFamily(font string) string { return "fileicons-" + font }

// The suffixes file-icons looks past: a backup's, and a template's.
var (
	backupSuffix   = regexp.MustCompile(`~(?:orig|previous)$`)
	templateSuffix = regexp.MustCompile(`(?i)^([^.]*\.[^.]+)\.(?:inc?|dist|tm?pl|te?mp|ti?dy)$`)
)

// fileIconRuleOf caches the rule of each path, -1 for none.
var fileIconRuleOf = map[string]int{}

// matchFileIcon returns the rule of the file at p, as file-icons' path
// strategy finds it: by the path, then by the name without a backup's or
// a template's suffix, then by the whole name where that matched nothing
// of priority.
func matchFileIcon(p string) int {
	if i, ok := fileIconRuleOf[p]; ok {
		return i
	}
	compiled := fileIconPatterns()
	name := path.Base(p)
	filtered := templateSuffix.ReplaceAllString(backupSuffix.ReplaceAllString(name, ""), "$1")
	find := func(s string, byPath bool) int {
		for i, r := range fileIconRules {
			if r.matchPath == byPath && compiled[i].MatchString(s) {
				return i
			}
		}
		return -1
	}
	i := find("/"+strings.TrimPrefix(p, "/"), true)
	if i < 0 {
		i = find(filtered, false)
	}
	if filtered != name && (i < 0 || fileIconRules[i].priority < 1) {
		i = find(name, false)
	}
	fileIconRuleOf[p] = i
	return i
}

// fileIcon returns the icon of the file at p, coloured for a dark theme or
// a light one: a page of text where no rule matches, as file-icons falls
// back to, or for no path.
func fileIcon(p string, dark bool) rowIcon {
	name, colour := "text", ""
	if i := matchFileIcon(filepath.ToSlash(p)); p != "" && i >= 0 {
		r := fileIconRules[i]
		name, colour = r.icon, r.light
		if dark {
			colour = r.dark
		}
	}
	g := fileIconGlyphs[name]
	ic := rowIcon{font: fileIconFamily(g.font), char: g.char, scale: float32(g.size / 16)}
	if c, ok := fileIconColours[colour]; ok {
		if dark {
			ic.color = ui.Hex(c[0])
		} else {
			ic.color = ui.Hex(c[1])
		}
	}
	return ic
}

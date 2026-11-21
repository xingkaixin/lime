package main

import (
	"embed"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"lime/locales"
)

const (
	fileIconsDefault = "file-icons"
	fileIconsPierre  = "pierre"
)

type fileIconSet struct {
	name  string
	label func(locales.Messages) string
}

func (f fileIconSet) id() string { return "set_file_icons " + f.name }

var fileIconSets = []fileIconSet{
	{fileIconsDefault, locales.Messages.FileIconsDefault},
	{fileIconsPierre, locales.Messages.FileIconsPierre},
}

// fileIconFonts are the icon fonts of file-icons
// (https://github.com/file-icons/atom), whose rules, glyphs and colours
// fileicons/update.sh writes to icons_gen.go.
//
//go:embed fileicons/*.ttf
var fileIconFonts embed.FS

type fileIconRule struct {
	icon, dark, light string
	pattern           string
	priority          float64
	matchPath         bool
}

type fileIconGlyph struct {
	font, char string
	size       float64 // in CSS pixels, of a 16 pixel row
}

type rowIcon struct {
	svg        *ui.SVG
	dual       *ui.SVG // two-colour SVG; falls back to monochrome svg when overridden by selection tint
	font, char string
	scale      float32
	color      ui.Color
}

func (ic rowIcon) tint(fallback ui.Color) ui.Color {
	if ic.color == (ui.Color{}) {
		return fallback
	}
	return ic.color
}

func (ic rowIcon) view(c *ui.Context, size float32, tint ui.Color) {
	if ic.dual != nil && ic.color.A > 0 && tint.R == ic.color.R && tint.G == ic.color.G && tint.B == ic.color.B {
		img := ui.Image(c, ic.dual).Size(size, size).Shrink(0).TextColor(ic.color)
		if tint.A < ic.color.A {
			img.Opacity(float32(tint.A) / float32(ic.color.A))
		}
		return
	}
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

func (a *app) fileIcon(p string) rowIcon {
	if a.set.FileIcons == fileIconsPierre {
		return pierreFileIcon(p, a.scheme.dark)
	}
	return fileIcon(p, a.scheme.dark)
}

func (a *app) folderIcon(open bool) rowIcon {
	if a.set.FileIcons == fileIconsPierre {
		return pierreFolderIcon(open, a.scheme.accent)
	}
	if open {
		return rowIcon{svg: folderOpenIcon, color: a.scheme.accent}
	}
	return rowIcon{svg: folderIcon}
}

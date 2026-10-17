package main

import (
	"embed"
	"encoding/json"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// iconFiles are the monochrome icons of Catppuccin's icon theme for Zed,
// with the names and the suffixes of files it shows them for.
//
//go:embed icons/*.svg icons/icons.json
var iconFiles embed.FS

var iconMap = sync.OnceValue(func() (m struct{ Stems, Suffixes map[string]string }) {
	b, _ := iconFiles.ReadFile("icons/icons.json")
	json.Unmarshal(b, &m)
	return m
})

// iconSVGs are the icons parsed, by name, as the side bar shows them.
var iconSVGs = map[string]*ui.SVG{}

func icon(name string) *ui.SVG {
	s := iconSVGs[name]
	if s == nil {
		b, _ := iconFiles.ReadFile("icons/" + name + ".svg")
		s = ui.MustParseSVG(b)
		iconSVGs[name] = s
	}
	return s
}

// fileIcon picks the icon of a file by its name, as Zed does: the whole
// name, then what follows each dot in it, longest first.
func fileIcon(name string) *ui.SVG {
	m := iconMap()
	if i, ok := m.Stems[name]; ok {
		return icon(i)
	}
	for s, more := name, true; more; _, s, more = strings.Cut(s, ".") {
		if i, ok := m.Suffixes[s]; ok {
			return icon(i)
		}
		if i, ok := m.Suffixes[strings.ToLower(s)]; ok {
			return icon(i)
		}
	}
	return icon("_file")
}

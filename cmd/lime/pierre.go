package main

import (
	"embed"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// pierreIconSVGs holds the SVGs of Pierre Icons
// (https://github.com/pierrecomputer/vscode-icons), whose rules and palette
// fileicons/update-pierre.sh writes to pierre_gen.go.
//
//go:embed fileicons/pierre/*.svg
var pierreIconSVGs embed.FS

type pierreIconSpec struct {
	svg             string
	dark, light     string
	bgDark, bgLight string
	opacity         float64
}

var (
	pierreMu       sync.Mutex
	pierreSVGCache = map[string]*ui.SVG{}
)

func pierreSVG(name, bg string) *ui.SVG {
	key := name + "\x00" + bg
	pierreMu.Lock()
	defer pierreMu.Unlock()
	if s, ok := pierreSVGCache[key]; ok {
		return s
	}
	b, err := pierreIconSVGs.ReadFile("fileicons/pierre/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	if bg != "" {
		b = []byte(strings.Replace(string(b), ">", "><style>.bg{fill:"+bg+"}</style>", 1))
	}
	s := ui.MustParseSVG(b)
	pierreSVGCache[key] = s
	return s
}

func matchPierreIcon(p string) string {
	if p == "" {
		return "file-duo"
	}
	name := strings.ToLower(path.Base(filepath.ToSlash(p)))
	if ic, ok := pierreFileNames[name]; ok {
		return ic
	}
	for s := name; ; {
		_, rest, ok := strings.Cut(s, ".")
		if !ok || rest == "" {
			break
		}
		if ic, ok := pierreFileExts[rest]; ok {
			return ic
		}
		s = rest
	}
	return "file-duo"
}

func pierreFileIcon(p string, dark bool) rowIcon {
	sp := pierreIcons[matchPierreIcon(p)]
	ic := rowIcon{svg: pierreSVG(sp.svg, "")}
	fg, bg := sp.light, sp.bgLight
	if dark {
		fg, bg = sp.dark, sp.bgDark
	}
	if fg != "" {
		ic.color = ui.Hex(fg)
		if sp.opacity > 0 && sp.opacity < 1 {
			ic.color = ic.color.Alpha(float32(sp.opacity))
		}
	}
	if bg != "" {
		ic.dual = pierreSVG(sp.svg, bg)
	}
	return ic
}

func pierreFolderIcon(open bool, accent ui.Color) rowIcon {
	if open {
		return rowIcon{svg: pierreSVG("folder-open-duo", ""), color: accent}
	}
	return rowIcon{svg: pierreSVG("folder-duo", "")}
}

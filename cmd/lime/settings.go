package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// settings are what the app keeps between runs.
type settings struct {
	ColorScheme  string  `json:"color_scheme"`
	WordWrap     bool    `json:"word_wrap"`
	FontSize     float32 `json:"font_size"`
	SideBar      bool    `json:"side_bar"`
	SideBarWidth float32 `json:"side_bar_width"`
}

// dataDir is where the app keeps its settings and session.
func dataDir() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return ""
	}
	return dir
}

func loadSettings(dir string) settings {
	s := settings{ColorScheme: autoScheme, WordWrap: true, FontSize: 13, SideBar: true, SideBarWidth: 220}
	if dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
			json.Unmarshal(b, &s)
		}
	}
	return s
}

// saveSettings keeps the settings, and shows them in all windows.
func (l *lime) saveSettings() {
	l.redraw()
	if l.dir == "" {
		return
	}
	b, err := json.MarshalIndent(l.set, "", "\t")
	if err != nil {
		return
	}
	os.MkdirAll(l.dir, 0o755)
	os.WriteFile(filepath.Join(l.dir, "settings.json"), b, 0o644)
}

// redraw builds the windows anew, for a change they share.
func (l *lime) redraw() {
	for _, a := range l.wins {
		if a.win != nil {
			a.win.Update(func() {})
		}
	}
}

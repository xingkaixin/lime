package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// settings are what the app keeps between runs, in settings.json.
type settings struct {
	// Appearance is followSystem, lightMode or darkMode: whether the
	// windows show LightTheme or DarkTheme.
	Appearance   string  `json:"appearance"`
	LightTheme   string  `json:"light_theme"`
	DarkTheme    string  `json:"dark_theme"`
	FontSize     float32 `json:"font_size"`
	WordWrap     bool    `json:"word_wrap"`
	SideBar      bool    `json:"side_bar"`
	SideBarWidth float32 `json:"side_bar_width"`

	// ColorScheme is the one scheme older settings named, which
	// loadSettings reads into Appearance and the themes.
	ColorScheme string `json:"color_scheme,omitempty"`
}

const (
	followSystem = "system"
	lightMode    = "light"
	darkMode     = "dark"
)

const defaultFontSize = 13

// dataDir is where the app keeps its settings and session.
func dataDir() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return ""
	}
	return dir
}

func settingsPath(dir string) string { return filepath.Join(dir, "settings.json") }

func loadSettings(dir string) settings {
	s := settings{
		Appearance: followSystem, LightTheme: "GitHub Light", DarkTheme: "GitHub Dark",
		FontSize: defaultFontSize, WordWrap: true, SideBar: true, SideBarWidth: 220,
	}
	if dir != "" {
		if b, err := os.ReadFile(settingsPath(dir)); err == nil {
			json.Unmarshal(b, &s)
		}
	}
	s.migrate()
	return s
}

// migrate reads the color scheme of older settings into the appearance
// and the themes: "Auto" followed the system with GitHub's schemes, and
// any other scheme showed whatever the system's appearance.
func (s *settings) migrate() {
	name := s.ColorScheme
	if name == "" {
		return
	}
	s.ColorScheme = ""
	switch sc := schemeNamed(name); {
	case sc == nil:
		s.Appearance = followSystem
	case sc.dark:
		s.Appearance, s.DarkTheme = darkMode, name
	default:
		s.Appearance, s.LightTheme = lightMode, name
	}
}

// showScheme makes sc the theme of its appearance, and shows that
// appearance where the windows showed the other.
func (s *settings) showScheme(sc *scheme, shownDark bool) {
	if sc.dark {
		s.DarkTheme = sc.name
	} else {
		s.LightTheme = sc.name
	}
	if sc.dark != shownDark {
		s.Appearance = lightMode
		if sc.dark {
			s.Appearance = darkMode
		}
	}
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
	os.WriteFile(settingsPath(l.dir), b, 0o644)
}

// redraw builds the windows anew, for a change they share.
func (l *lime) redraw() {
	for _, a := range l.wins {
		if a.win != nil {
			a.win.Update(func() {})
		}
	}
}

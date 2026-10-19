package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
)

// settings are what the app keeps between runs, in settings.json.
type settings struct {
	// Language is the tag of a language of the app's catalogs, as
	// "zh-Hans", or followSystem.
	Language string `json:"language"`
	// Appearance is followSystem, lightMode or darkMode: whether the
	// windows show LightTheme or DarkTheme.
	Appearance   string  `json:"appearance"`
	LightTheme   string  `json:"light_theme"`
	DarkTheme    string  `json:"dark_theme"`
	UIFont       string  `json:"ui_font"`      // "" is the system's
	ContentFont  string  `json:"content_font"` // "" is the system's monospaced font
	FontSize     float32 `json:"font_size"`
	WordWrap     bool    `json:"word_wrap"`
	SideBar      bool    `json:"side_bar"`
	SideBarWidth float32 `json:"side_bar_width"`
	// Keys are the commands' keys the user changed, the one changed last
	// last.
	Keys []keyChange `json:"keys,omitempty"`

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
		Language: followSystem, Appearance: followSystem, LightTheme: "GitHub Light", DarkTheme: "GitHub Dark",
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

// locale is the language App.SetLocale shows for Language: "" follows
// the system.
func (s *settings) locale() string {
	if s.Language == followSystem {
		return ""
	}
	return i18n.Normalize(s.Language)
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

// setLanguage shows the language of tag, or the system's for
// followSystem, and keeps it.
func (l *lime) setLanguage(tag string) {
	l.set.Language = tag
	l.saveSettings()
	mygo.App.SetLocale(l.set.locale())
}

// reloadSettings reads settings.json again once it is saved in a tab,
// which the settings kept later would write over.
func (l *lime) reloadSettings() {
	l.set = loadSettings(l.dir)
	l.keys = newKeymap(l.set.Keys)
	mygo.App.SetLocale(l.set.locale())
	l.redraw()
	l.refreshMenu()
}

// redraw builds the windows anew, for a change they share.
func (l *lime) redraw() {
	for _, a := range l.wins {
		if a.win != nil {
			a.win.Update(func() {})
		}
	}
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
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
	TabSize      int     `json:"tab_size"`
	// TranslateTabsToSpaces indents with spaces when true and tabs when
	// false; unset, as the language's files usually do.
	TranslateTabsToSpaces        *bool `json:"translate_tabs_to_spaces,omitempty"`
	TrimTrailingWhiteSpaceOnSave bool  `json:"trim_trailing_white_space_on_save"`
	EnsureNewlineAtEOFOnSave     bool  `json:"ensure_newline_at_eof_on_save"`
	// AutoComplete shows completions as words are typed.
	AutoComplete bool `json:"auto_complete"`
	// DetectIndentation has a file indent as its lines do as it opens,
	// in place of TabSize and TranslateTabsToSpaces.
	DetectIndentation bool `json:"detect_indentation"`
	// FallbackEncoding is the name of the encoding a file that isn't
	// UTF-8 opens in, as Sublime Text's encodings are named.
	FallbackEncoding string `json:"fallback_encoding"`
	// DiffUnified lays commits' diffs out one over the other rather than
	// side by side.
	DiffUnified bool `json:"diff_unified"`
	// GitTreeView shows the git panel's changes in the tree of their
	// folders rather than as a list.
	GitTreeView bool `json:"git_tree_view"`
	// GitDiffTarget is the text in git that the gutter marks a file's
	// changes against: "index", or "head" for the last commit's.
	GitDiffTarget string `json:"git_diff_target"`
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

// loadSettings reads settings.json over the defaults, and returns the
// error of a file that does not parse.
func loadSettings(dir string) (settings, error) {
	s := settings{
		Language: followSystem, Appearance: followSystem, LightTheme: "GitHub Light", DarkTheme: "GitHub Dark",
		FontSize: defaultFontSize, WordWrap: true, SideBar: true, SideBarWidth: 220, TabSize: 4, AutoComplete: true,
		GitDiffTarget: "index", DetectIndentation: true, FallbackEncoding: defaultFallback,
	}
	var err error
	if dir != "" {
		if b, rerr := os.ReadFile(settingsPath(dir)); rerr == nil {
			err = json.Unmarshal(b, &s)
		}
	}
	s.migrate()
	return s, err
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

// indent is what TranslateTabsToSpaces has indenting insert.
func (s *settings) indent() editor.IndentStyle { return indentStyle(s.TranslateTabsToSpaces) }

func (s *settings) setIndent(style editor.IndentStyle) {
	s.TranslateTabsToSpaces = translateTabs(style)
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

// saveSettings keeps the settings, and shows them in all windows. A
// settings.json that does not parse is left for the user to fix.
func (l *lime) saveSettings() {
	l.redraw()
	if l.dir == "" || l.setErr != nil {
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
// which the settings kept later would write over. A file that does not
// parse leaves the settings as they are.
func (l *lime) reloadSettings() {
	s, err := loadSettings(l.dir)
	if l.setErr = err; err != nil {
		go mygo.Dialog.Error(locales.DialogUnableToReadSettings(), err.Error())
		return
	}
	rev := l.set.diffRev()
	l.set = s
	l.keys = newKeymap(l.set.Keys)
	mygo.App.SetLocale(l.set.locale())
	l.redraw()
	l.refreshMenu()
	if l.set.diffRev() != rev {
		for _, a := range l.wins {
			a.reloadChanged()
		}
	}
}

// redraw builds the windows anew, for a change they share.
func (l *lime) redraw() {
	for _, a := range l.wins {
		if a.win != nil {
			a.win.Update(func() {})
		}
	}
}

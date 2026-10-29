package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
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
	// Rulers are the columns guides are drawn at.
	Rulers           rulers     `json:"rulers"`
	DrawWhiteSpace   whiteSpace `json:"draw_white_space"`
	LineNumbers      bool       `json:"line_numbers"`
	HighlightLine    bool       `json:"highlight_line"`
	DrawIndentGuides bool       `json:"draw_indent_guides"`
	ScrollPastEnd    pastEnd    `json:"scroll_past_end"`
	// LinePaddingTop and LinePaddingBottom add DIPs above and below each
	// line.
	LinePaddingTop    float32 `json:"line_padding_top"`
	LinePaddingBottom float32 `json:"line_padding_bottom"`
	// FontOptions are Sublime Text's, of which the ligature ones count:
	// "no_liga", "no_clig" and "no_calt".
	FontOptions []string `json:"font_options"`
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
		Rulers: rulers{}, DrawWhiteSpace: whiteSpace{"selection"}, LineNumbers: true, DrawIndentGuides: true,
		ScrollPastEnd: true, FontOptions: []string{},
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

// display shows the text of st as the settings say.
func (s *settings) display(st *editor.State) {
	st.WrapGuides, st.Whitespace = s.Rulers, s.DrawWhiteSpace.mode()
	st.LineNumbers = editor.LineNumbersOn
	if !s.LineNumbers {
		st.LineNumbers = editor.LineNumbersOff
	}
	st.NoActiveLine, st.NoIndentGuides, st.NoScrollPastEnd = !s.HighlightLine, !s.DrawIndentGuides, !bool(s.ScrollPastEnd)
}

// rulers is Sublime Text's rulers: columns, or lists of a column and the
// ruler's style, which the editor leaves.
type rulers []int

func (r *rulers) UnmarshalJSON(b []byte) error {
	var items []any
	if err := json.Unmarshal(b, &items); err != nil || items == nil {
		return err
	}
	*r = rulers{}
	for _, it := range items {
		if l, ok := it.([]any); ok && len(l) > 0 {
			it = l[0]
		}
		if n, ok := it.(float64); ok {
			*r = append(*r, int(n))
		}
	}
	return nil
}

// pastEnd is scroll_past_end: a bool, or how far past the end Sublime Text
// scrolls, of which the editor tells none from some.
type pastEnd bool

func (p *pastEnd) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch v := v.(type) {
	case bool:
		*p = pastEnd(v)
	case float64:
		*p = v > 0
	}
	return nil
}

// font is the content font, as editors take it.
func (s *settings) font() ui.Font {
	var off []string
	for _, o := range s.FontOptions {
		switch o {
		case "no_liga", "no_clig", "no_calt":
			off = append(off, strings.TrimPrefix(o, "no_")+"=0")
		}
	}
	return ui.Font{Family: s.ContentFont, Size: s.FontSize, Features: strings.Join(off, ",")}
}

// lineHeight is the height of lines as a multiple of the font's size.
func (s *settings) lineHeight() float32 {
	if s.FontSize <= 0 {
		return 0
	}
	// 1.5 is the editor's own line height.
	return 1.5 + (s.LinePaddingTop+s.LinePaddingBottom)/s.FontSize
}

// whiteSpace is draw_white_space: Sublime Text's options of where white
// space is drawn, applied in order, or one of its older strings, "none",
// "selection" or "all".
type whiteSpace []string

func (w *whiteSpace) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*w = whiteSpace{s}
		return nil
	}
	return json.Unmarshal(b, (*[]string)(w))
}

const (
	wsSelection uint8 = 1 << iota
	wsLeading
	wsEnclosed
	wsTrailing
	wsIsolated
	wsAll = wsSelection | wsLeading | wsEnclosed | wsTrailing | wsIsolated
)

var wsPlaces = map[string]uint8{
	"selection": wsSelection, "leading": wsLeading, "enclosed": wsEnclosed, "trailing": wsTrailing,
	"isolated": wsIsolated, "all": wsAll, "none": wsAll,
}

// mode is the editor's nearest to the options: white space at the ends
// of lines alone is trailing, and anywhere else is all of it. Options of
// tabs, spaces or mixed indentation alone draw both.
func (w whiteSpace) mode() editor.Whitespace {
	var on uint8
	for _, o := range w {
		place, kind, _ := strings.Cut(o, "_")
		if place == "none" || kind == "none" {
			on &^= wsPlaces[place]
		} else {
			on |= wsPlaces[place]
		}
	}
	switch {
	case on&(wsLeading|wsEnclosed) != 0:
		return editor.WhitespaceAll
	case on&(wsTrailing|wsIsolated) != 0:
		return editor.WhitespaceTrailing
	case on&wsSelection != 0:
		return editor.WhitespaceSelection
	}
	return editor.WhitespaceNone
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

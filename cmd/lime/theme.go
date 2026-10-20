package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// scheme is a color scheme: GitHub's, as its Primer colors have it, or
// one of Sublime Text's or Flexoki's, its colors taken from their
// .sublime-color-scheme files. The chrome around the editor derives from it, as Sublime's
// Adaptive theme does.
type scheme struct {
	name        string
	dark        bool
	bg, fg      ui.Color
	caret, sel  ui.Color
	accent      ui.Color
	activeGuide ui.Color
	find        ui.Color
	bracket     ui.Color
	tokens      map[editor.TokenKind]editor.TokenStyle

	code *editor.Theme
}

var schemes = []*scheme{
	{
		name: "GitHub Dark", dark: true,
		bg: ui.Hex("#0d1117"), fg: ui.Hex("#e6edf3"), caret: ui.Hex("#2f81f7"), sel: ui.Hex("#264f78"),
		accent: ui.Hex("#2f81f7"), activeGuide: ui.Hex("#6e7681"), find: ui.Hex("#9e6a03"), bracket: ui.Hex("#8b949e"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#ff7b72", editor.TokenType: "#ffa657", editor.TokenFunction: "#d2a8ff",
			editor.TokenProperty: "#79c0ff", editor.TokenConstant: "#79c0ff", editor.TokenNumber: "#79c0ff",
			editor.TokenString: "#a5d6ff", editor.TokenEscape: "#79c0ff", editor.TokenRegexp: "#7ee787",
			editor.TokenComment: "#8b949e", editor.TokenOperator: "#ff7b72", editor.TokenTag: "#7ee787",
			editor.TokenAttribute: "#79c0ff", editor.TokenMeta: "#d2a8ff", editor.TokenHeading: "#79c0ff/b",
			editor.TokenLink: "#a5d6ff", editor.TokenCode: "#a5d6ff", editor.TokenInserted: "#7ee787",
			editor.TokenDeleted: "#ffa198", editor.TokenInvalid: "#f85149",
		}),
	},
	{
		name: "GitHub Light",
		bg:   ui.Hex("#ffffff"), fg: ui.Hex("#1f2328"), caret: ui.Hex("#0969da"), sel: ui.Hex("#0969da").Alpha(0.2),
		accent: ui.Hex("#0969da"), activeGuide: ui.Hex("#8c959f"), find: ui.Hex("#d4a72c"), bracket: ui.Hex("#6e7781"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#cf222e", editor.TokenType: "#953800", editor.TokenFunction: "#8250df",
			editor.TokenProperty: "#0550ae", editor.TokenConstant: "#0550ae", editor.TokenNumber: "#0550ae",
			editor.TokenString: "#0a3069", editor.TokenEscape: "#0550ae", editor.TokenRegexp: "#116329",
			editor.TokenComment: "#6e7781", editor.TokenOperator: "#cf222e", editor.TokenTag: "#116329",
			editor.TokenAttribute: "#0550ae", editor.TokenMeta: "#8250df", editor.TokenHeading: "#0550ae/b",
			editor.TokenLink: "#0a3069", editor.TokenCode: "#0a3069", editor.TokenInserted: "#1a7f37",
			editor.TokenDeleted: "#cf222e", editor.TokenInvalid: "#d1242f",
		}),
	},
	{
		name: "GitHub Dark Dimmed", dark: true,
		bg: ui.Hex("#22272e"), fg: ui.Hex("#adbac7"), caret: ui.Hex("#539bf5"), sel: ui.Hex("#539bf5").Alpha(0.2),
		accent: ui.Hex("#539bf5"), activeGuide: ui.Hex("#636e7b"), find: ui.Hex("#c69026"), bracket: ui.Hex("#768390"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#f47067", editor.TokenType: "#f69d50", editor.TokenFunction: "#dcbdfb",
			editor.TokenProperty: "#6cb6ff", editor.TokenConstant: "#6cb6ff", editor.TokenNumber: "#6cb6ff",
			editor.TokenString: "#96d0ff", editor.TokenEscape: "#6cb6ff", editor.TokenRegexp: "#8ddb8c",
			editor.TokenComment: "#768390", editor.TokenOperator: "#f47067", editor.TokenTag: "#8ddb8c",
			editor.TokenAttribute: "#6cb6ff", editor.TokenMeta: "#dcbdfb", editor.TokenHeading: "#6cb6ff/b",
			editor.TokenLink: "#96d0ff", editor.TokenCode: "#96d0ff", editor.TokenInserted: "#8ddb8c",
			editor.TokenDeleted: "#ff938a", editor.TokenInvalid: "#f47067",
		}),
	},
	{
		name: "GitHub Dark High Contrast", dark: true,
		bg: ui.Hex("#0a0c10"), fg: ui.Hex("#f0f3f6"), caret: ui.Hex("#71b7ff"), sel: ui.Hex("#ffffff").Alpha(0.2),
		accent: ui.Hex("#71b7ff"), activeGuide: ui.Hex("#9ea7b3"), find: ui.Hex("#f0b72f"), bracket: ui.Hex("#bdc4cc"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#ff9492", editor.TokenType: "#ffb757", editor.TokenFunction: "#dbb7ff",
			editor.TokenProperty: "#91cbff", editor.TokenConstant: "#91cbff", editor.TokenNumber: "#91cbff",
			editor.TokenString: "#addcff", editor.TokenEscape: "#91cbff", editor.TokenRegexp: "#72f088",
			editor.TokenComment: "#bdc4cc", editor.TokenOperator: "#ff9492", editor.TokenTag: "#72f088",
			editor.TokenAttribute: "#91cbff", editor.TokenMeta: "#dbb7ff", editor.TokenHeading: "#91cbff/b",
			editor.TokenLink: "#addcff", editor.TokenCode: "#addcff", editor.TokenInserted: "#72f088",
			editor.TokenDeleted: "#ffb1af", editor.TokenInvalid: "#ff9492",
		}),
	},
	{
		name: "GitHub Light High Contrast",
		bg:   ui.Hex("#ffffff"), fg: ui.Hex("#0e1116"), caret: ui.Hex("#0349b4"), sel: ui.Hex("#0e1116").Alpha(0.2),
		accent: ui.Hex("#0349b4"), activeGuide: ui.Hex("#88929d"), find: ui.Hex("#b58407"), bracket: ui.Hex("#66707b"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#a0111f", editor.TokenType: "#702c00", editor.TokenFunction: "#622cbc",
			editor.TokenProperty: "#023b95", editor.TokenConstant: "#023b95", editor.TokenNumber: "#023b95",
			editor.TokenString: "#032563", editor.TokenEscape: "#023b95", editor.TokenRegexp: "#024c1a",
			editor.TokenComment: "#66707b", editor.TokenOperator: "#a0111f", editor.TokenTag: "#024c1a",
			editor.TokenAttribute: "#023b95", editor.TokenMeta: "#622cbc", editor.TokenHeading: "#023b95/b",
			editor.TokenLink: "#032563", editor.TokenCode: "#032563", editor.TokenInserted: "#024c1a",
			editor.TokenDeleted: "#a0111f", editor.TokenInvalid: "#a0111f",
		}),
	},
	{
		name: "Mariana", dark: true,
		bg: ui.Hex("#303841"), fg: ui.Hex("#d8dee9"), caret: ui.Hex("#f9ae58"), sel: ui.Hex("#596673").Alpha(0.7),
		accent: ui.Hex("#5c99d6"), activeGuide: ui.Hex("#5fb4b4"), find: ui.Hex("#fac761"), bracket: ui.Hex("#f9ae58"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#c695c6", editor.TokenType: "#6699cc/i", editor.TokenFunction: "#5fb4b4",
			editor.TokenConstant: "#ec5f66/i", editor.TokenNumber: "#f9ae58", editor.TokenString: "#99c794",
			editor.TokenEscape: "#c695c6", editor.TokenRegexp: "#99c794", editor.TokenComment: "#a6acb9",
			editor.TokenOperator: "#f97b58", editor.TokenTag: "#ec5f66", editor.TokenAttribute: "#c695c6",
			editor.TokenMeta: "#6699cc", editor.TokenHeading: "#f97b58/b", editor.TokenLink: "#6699cc",
			editor.TokenInserted: "#99c794", editor.TokenDeleted: "#ec5f66",
		}),
	},
	{
		name: "Monokai", dark: true,
		bg: ui.Hex("#282923"), fg: ui.Hex("#f8f8f2"), caret: ui.Hex("#f8f8f0"), sel: ui.Hex("#555449").Alpha(0.7),
		accent: ui.Hex("#67d8ef"), activeGuide: ui.Hex("#9f570f"), find: ui.Hex("#ffe894"), bracket: ui.Hex("#f8f8f2"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#f92472", editor.TokenType: "#67d8ef/i", editor.TokenFunction: "#a6e22c",
			editor.TokenConstant: "#ac80ff", editor.TokenNumber: "#ac80ff", editor.TokenString: "#e7db74",
			editor.TokenEscape: "#ac80ff", editor.TokenRegexp: "#e7db74", editor.TokenComment: "#74705d",
			editor.TokenOperator: "#f92472", editor.TokenTag: "#f92472", editor.TokenAttribute: "#a6e22c",
			editor.TokenMeta: "#67d8ef", editor.TokenHeading: "#a6e22c/b", editor.TokenLink: "#67d8ef",
			editor.TokenInserted: "#a6e22c", editor.TokenDeleted: "#f92472",
		}),
	},
	{
		name: "Breakers",
		bg:   ui.Hex("#fcfdfd"), fg: ui.Hex("#333333"), caret: ui.Hex("#5fb4b4"), sel: ui.Hex("#d8dcdf").Alpha(0.7),
		accent: ui.Hex("#5c99d6"), activeGuide: ui.Hex("#5fb4b4"), find: ui.Hex("#fac761"), bracket: ui.Hex("#f9ae58"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#c695c6", editor.TokenType: "#6699cc/i", editor.TokenFunction: "#5fb4b4",
			editor.TokenConstant: "#ec5f66/i", editor.TokenNumber: "#f9ae58", editor.TokenString: "#80b979",
			editor.TokenEscape: "#c695c6", editor.TokenRegexp: "#80b979", editor.TokenComment: "#999999",
			editor.TokenOperator: "#f97b58", editor.TokenTag: "#ec5f66", editor.TokenAttribute: "#c695c6",
			editor.TokenMeta: "#6699cc", editor.TokenHeading: "#f97b58/b", editor.TokenLink: "#6699cc",
			editor.TokenInserted: "#80b979", editor.TokenDeleted: "#ec5f66",
		}),
	},
	{
		name: "Celeste",
		bg:   ui.Hex("#ffffff"), fg: ui.Hex("#000000"), caret: ui.Hex("#ff4d97"), sel: ui.Hex("#333333").Alpha(0.08),
		accent: ui.Hex("#f58a3d"), activeGuide: ui.Hex("#e46767"), find: ui.Hex("#ffdd33"), bracket: ui.Hex("#f58a3d"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#000000/i", editor.TokenType: "#000000/i", editor.TokenFunction: "#f58a3d",
			editor.TokenConstant: "#000000/i", editor.TokenNumber: "#e46767", editor.TokenString: "#957350",
			editor.TokenEscape: "#f55c3d", editor.TokenRegexp: "#957350", editor.TokenComment: "#888888/i",
			editor.TokenOperator: "#666666", editor.TokenPunctuation: "#666666", editor.TokenTag: "#228ec3",
			editor.TokenAttribute: "#22c3a8", editor.TokenHeading: "#f58a3d/b", editor.TokenLink: "#228ec3",
			editor.TokenInserted: "#22c3a8", editor.TokenDeleted: "#e46767",
		}),
	},
	{
		name: "Sixteen",
		bg:   ui.Hex("#ffffff"), fg: ui.Hex("#545454"), caret: ui.Hex("#383838"), sel: ui.Hex("#d6d6d6").Alpha(0.7),
		accent: ui.Hex("#3c97dd"), activeGuide: ui.Hex("#86c1b9"), find: ui.Hex("#f4c725"), bracket: ui.Hex("#f09642"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#ba8caf", editor.TokenType: "#545454/i", editor.TokenFunction: "#3c97dd",
			editor.TokenConstant: "#f09642", editor.TokenNumber: "#f09642", editor.TokenString: "#9dbf40",
			editor.TokenEscape: "#86c1b9", editor.TokenRegexp: "#86c1b9", editor.TokenComment: "#b8b8b8",
			editor.TokenOperator: "#86c1b9", editor.TokenPunctuation: "#999999", editor.TokenTag: "#d2322d",
			editor.TokenAttribute: "#f09642", editor.TokenMeta: "#d2322d", editor.TokenHeading: "#3c97dd/b",
			editor.TokenLink: "#3c97dd", editor.TokenInserted: "#9dbf40", editor.TokenDeleted: "#d2322d",
		}),
	},
	// Flexoki marks selections and matches with borders, which the editor
	// does not draw: they take ui-2 and yellow, which show without one.
	{
		name: "Flexoki Dark", dark: true,
		bg: ui.Hex("#100f0f"), fg: ui.Hex("#cecdc3"), caret: ui.Hex("#fffcf0"), sel: ui.Hex("#343331"),
		accent: ui.Hex("#4385be"), activeGuide: ui.Hex("#575653"), find: ui.Hex("#d0a215"), bracket: ui.Hex("#879a39"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#879a39", editor.TokenType: "#da702c", editor.TokenFunction: "#da702c",
			editor.TokenConstant: "#d0a215", editor.TokenNumber: "#8b7ec8", editor.TokenString: "#3aa99f",
			editor.TokenEscape: "#d14d41", editor.TokenRegexp: "#3aa99f", editor.TokenComment: "#575653",
			editor.TokenOperator: "#878580", editor.TokenPunctuation: "#878580", editor.TokenTag: "#4385be",
			editor.TokenAttribute: "#da702c", editor.TokenMeta: "#d0a215", editor.TokenHeading: "#575653/b",
			editor.TokenLink: "#3aa99f", editor.TokenInserted: "#879a39", editor.TokenDeleted: "#d14d41",
		}),
	},
	{
		name: "Flexoki Light",
		bg:   ui.Hex("#fffcf0"), fg: ui.Hex("#100f0f"), caret: ui.Hex("#100f0f"), sel: ui.Hex("#dad8ce"),
		accent: ui.Hex("#205ea6"), activeGuide: ui.Hex("#b7b5ac"), find: ui.Hex("#ad8301"), bracket: ui.Hex("#66800b"),
		tokens: tokens(map[editor.TokenKind]string{
			editor.TokenKeyword: "#66800b", editor.TokenType: "#bc5215", editor.TokenFunction: "#bc5215",
			editor.TokenConstant: "#ad8301", editor.TokenNumber: "#5e409d", editor.TokenString: "#24837b",
			editor.TokenEscape: "#af3029", editor.TokenRegexp: "#24837b", editor.TokenComment: "#b7b5ac",
			editor.TokenOperator: "#6f6e69", editor.TokenPunctuation: "#6f6e69", editor.TokenTag: "#205ea6",
			editor.TokenAttribute: "#bc5215", editor.TokenMeta: "#ad8301", editor.TokenHeading: "#b7b5ac/b",
			editor.TokenLink: "#24837b", editor.TokenInserted: "#66800b", editor.TokenDeleted: "#af3029",
		}),
	},
}

// tokens reads styles written as "#rrggbb", with "/i" for italic and
// "/b" for bold.
func tokens(m map[editor.TokenKind]string) map[editor.TokenKind]editor.TokenStyle {
	out := make(map[editor.TokenKind]editor.TokenStyle, len(m))
	for k, s := range m {
		out[k] = editor.TokenStyle{Color: ui.Hex(s[:7]), Italic: len(s) > 7 && s[8] == 'i', Bold: len(s) > 7 && s[8] == 'b'}
	}
	return out
}

func schemeNamed(name string) *scheme {
	for _, s := range schemes {
		if s.name == name {
			return s
		}
	}
	return nil
}

// scheme returns the scheme the windows show: the theme of the
// appearance, systemDark choosing while it follows the system.
func (s *settings) scheme(systemDark bool) *scheme {
	return s.theme(s.Appearance == darkMode || s.Appearance != lightMode && systemDark)
}

// theme returns the theme of the dark or the light appearance. A theme
// that is not one of the schemes of its appearance gives way to GitHub's.
func (s *settings) theme(dark bool) *scheme {
	name, fallback := s.LightTheme, "GitHub Light"
	if dark {
		name, fallback = s.DarkTheme, "GitHub Dark"
	}
	if sc := schemeNamed(name); sc != nil && sc.dark == dark {
		return sc
	}
	return schemeNamed(fallback)
}

var black = ui.RGB(0, 0, 0)

// danger is the color of what went wrong, as a key two commands have.
func (s *scheme) danger() ui.Color { return s.tokens[editor.TokenDeleted].Color }

// chrome holds the colors around the editor: tab bars, the side bar, the
// status bar, the quick panel. tab is the active tab's background.
type chrome struct {
	bg, hover, line, panel, tab ui.Color
	text, muted                 ui.Color
}

func (s *scheme) chrome() chrome {
	shade := func(dark, light float32) ui.Color {
		if s.dark {
			return s.bg.Mix(black, dark)
		}
		return s.bg.Mix(black, light)
	}
	panel := s.bg.Mix(ui.RGB(255, 255, 255), 0.06)
	if !s.dark {
		panel = s.bg
	}
	return chrome{
		bg: shade(0.2, 0.05), hover: shade(0.1, 0.025), line: shade(0.38, 0.13), panel: panel, tab: s.bg,
		text: s.fg, muted: s.fg.Mix(s.bg, 0.4),
	}
}

// shell is the chrome of the side bar, the tab bars and the status bar,
// which leave the window's material showing where it shows.
func (a *app) shell(c *ui.Context) chrome {
	ch := a.scheme.chrome()
	if c.Vibrancy() {
		ch.bg, ch.hover, ch.tab = ui.Transparent, ch.text.Alpha(0.08), ch.tab.Alpha(0.5)
	}
	return ch
}

// uiTheme is the window's theme for the scheme: the widgets of the side
// bar, the find bar and the quick panel.
func (s *scheme) uiTheme(base *ui.Theme) *ui.Theme {
	t := *base
	ch := s.chrome()
	t.Dark = s.dark
	t.Background, t.Text, t.TextMuted, t.Border = ch.bg, ch.text, ch.muted, ch.line
	t.Surface, t.SurfaceHover, t.SurfacePressed = ch.panel, ch.panel.Mix(s.fg, 0.08), ch.panel.Mix(s.fg, 0.14)
	t.Accent, t.AccentHover, t.AccentPressed = s.accent, s.accent.Mix(s.fg, 0.15), s.accent.Mix(black, 0.15)
	t.Selection, t.Focus = s.sel, s.accent.Alpha(0.6)
	return &t
}

// editorTheme is the editor's theme for the scheme.
func (s *scheme) editorTheme() *editor.Theme {
	if s.code != nil {
		return s.code
	}
	base := ui.LightTheme()
	if s.dark {
		base = ui.DarkTheme()
	}
	th := editor.DefaultTheme(base)
	clear(th.Tokens[:])
	for k, st := range s.tokens {
		th.Tokens[k] = st
	}
	th.Tokens[editor.TokenEmphasis] = editor.TokenStyle{Italic: true}
	th.Tokens[editor.TokenStrong] = editor.TokenStyle{Bold: true}
	th.Background, th.Text = s.bg, s.fg
	th.LineNumber, th.ActiveLineNumber = s.fg.Mix(s.bg, 0.55), s.fg
	th.Selection, th.InactiveSelection = s.sel, s.sel
	th.Cursor, th.MatchingBracket = s.caret, s.bracket
	th.Match, th.CurrentMatch = s.find.Alpha(0.3), s.find.Alpha(0.55)
	th.IndentGuide, th.ActiveIndentGuide = s.fg.Alpha(0.12), s.activeGuide.Alpha(0.7)
	th.Whitespace = s.fg.Alpha(0.3)
	red, orange, green := s.tokens[editor.TokenDeleted].Color, s.tokens[editor.TokenNumber].Color, s.tokens[editor.TokenInserted].Color
	th.Error, th.Warning, th.Info = red, orange, s.accent
	th.Added, th.Deleted, th.Modified = green, red, orange
	s.code = th
	return th
}

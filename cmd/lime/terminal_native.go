//go:build !js

package main

import (
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

const terminalSupported = true

type nativeTermSession struct {
	term     *terminal.Terminal
	scheme   *scheme
	fontFam  string
	fontSize float32
	fontOpts []string
}

func newNativeTermSession(o termSessionOpts) (termSession, error) {
	var keybinds []terminal.Keybind
	if runtime.GOOS == "linux" {
		// Match Windows Terminal and Zed's Ctrl+Shift+K clear on Linux too.
		keybinds = []terminal.Keybind{
			{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyK, Action: terminal.ActionClearScreen, Performable: true},
		}
	}
	th := terminalTheme(o.scheme)
	opts := terminal.Options{
		Dir:              o.dir,
		ShellIntegration: true,
		Font:             terminalFont(o.set),
		Theme:            th,
		DarkTheme:        th,
		Keybinds:         keybinds,
		NoExitMessage:    true,
		Padding:          terminal.Padding{X: 10, Y: 6},
		OnTitle:          o.onTitle,
		OnExit:           o.onExit,
		OpenLink:         o.openLink,
	}
	if o.onSearch != nil {
		opts.OnSearch = func(s terminal.SearchStatus) {
			o.onSearch(termSearchStatus{
				Text:      s.Text,
				Selected:  s.Selected,
				Total:     s.Total,
				Searching: s.Searching,
			})
		}
	}
	t, err := terminal.New(opts)
	if err != nil {
		return nil, err
	}
	s := &nativeTermSession{
		term:   t,
		scheme: o.scheme,
	}
	if o.set != nil {
		s.fontFam = o.set.ContentFont
		s.fontSize = o.set.FontSize
		s.fontOpts = slices.Clone(o.set.FontOptions)
	}
	return s, nil
}

func (s *nativeTermSession) view(c *ui.Context) ui.Element {
	return terminal.View(c, s.term)
}

func (s *nativeTermSession) syncAppearance(sc *scheme, set *settings) {
	if sc != nil && sc != s.scheme {
		s.scheme = sc
		th := terminalTheme(sc)
		s.term.SetTheme(th, th)
	}
	if set != nil && (set.ContentFont != s.fontFam || set.FontSize != s.fontSize || !slices.Equal(set.FontOptions, s.fontOpts)) {
		s.fontFam = set.ContentFont
		s.fontSize = set.FontSize
		s.fontOpts = slices.Clone(set.FontOptions)
		s.term.SetFont(terminalFont(set))
	}
}

func (s *nativeTermSession) dir() string         { return s.term.Dir() }
func (s *nativeTermSession) clearScreen() bool   { return s.term.ClearScreen() }
func (s *nativeTermSession) search(q string)     { s.term.Search(q) }
func (s *nativeTermSession) searchNext()         { s.term.SearchNext() }
func (s *nativeTermSession) searchPrev()         { s.term.SearchPrevious() }
func (s *nativeTermSession) endSearch()          { s.term.EndSearch() }
func (s *nativeTermSession) close() error        { return s.term.Close() }

func terminalFont(set *settings) terminal.Font {
	if set == nil {
		return terminal.Font{Size: defaultFontSize}
	}
	var features []string
	for _, o := range set.FontOptions {
		switch o {
		case "no_liga", "no_clig", "no_calt":
			features = append(features, "-"+strings.TrimPrefix(o, "no_"))
		}
	}
	return terminal.Font{
		Family:   set.ContentFont,
		Size:     set.FontSize,
		Features: features,
	}
}

func terminalTheme(sc *scheme) *terminal.Theme {
	if sc == nil {
		return terminal.DarkTheme()
	}
	base := terminal.LightTheme()
	if sc.dark {
		base = terminal.DarkTheme()
	}
	th := *base
	th.Foreground = sc.fg
	th.Background = sc.bg
	th.Cursor = sc.caret
	th.CursorText = sc.bg
	th.Selection = sc.sel
	th.Search = sc.find.Alpha(0.35)
	th.SearchSelected = sc.find.Alpha(0.65)
	return &th
}

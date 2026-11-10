package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lime/locales"
)

// prefs is the settings dialog of a window. Its changes show at once, and
// settings.json keeps them.
type prefs struct {
	open    bool
	page    string
	offset  ui.Point // where its title band dragged it, from the window's center
	preview *editor.State
	rulers  string // the rulers' columns as typed
	ruler   ui.Handle

	query     string // searches the commands of the shortcuts
	conflicts bool   // lists the keys of two commands or more alone
	rec       recording
}

const (
	pageGeneral    = "general"
	pageEditor     = "editor"
	pageAppearance = "appearance"
	pageFonts      = "fonts"
	pageShortcuts  = "shortcuts"
)

const (
	prefsW, prefsH = 720, 500
	prefsSideW     = 180
	prefsBandH     = 48
	prefsControlW  = 200
)

var (
	generalIcon    = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M2.5 4.5h6M11.5 4.5h2M2.5 8h2M7.5 8h6M2.5 11.5h7M12.5 11.5h1"/><circle cx="10" cy="4.5" r="1.5"/><circle cx="6" cy="8" r="1.5"/><circle cx="11" cy="11.5" r="1.5"/></svg>`))
	editorIcon     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 3.5h11M2.5 6.75h6M2.5 10h3.5M2.5 13.25h2.5"/><path d="M13.25 7.75 9.5 11.5 9 13.5l2-.5 3.75-3.75z"/></svg>`))
	appearanceIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="8" cy="8" r="5.75"/><path d="M8 2.25a5.75 5.75 0 0 1 0 11.5z" fill="currentColor" stroke="none"/></svg>`))
	fontsIcon      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12.5 5.25 3.5 8.5 12.5M3.2 9.5h4.1"/><circle cx="11.75" cy="10.25" r="2.25"/><path d="M14 8v4.5"/></svg>`))
	systemIcon     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><rect x="2" y="3" width="12" height="8.5" rx="1.5"/><path d="M5.5 14h5M8 11.5V14"/></svg>`))
	lightIcon      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><circle cx="8" cy="8" r="2.75"/><path d="M8 1.75v1.5M8 12.75v1.5M1.75 8h1.5M12.75 8h1.5M3.6 3.6l1.05 1.05M11.35 11.35l1.05 1.05M3.6 12.4l1.05-1.05M11.35 4.65l1.05-1.05"/></svg>`))
	darkIcon       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"><path d="M13.25 9.6A5.5 5.5 0 1 1 6.4 2.75a4.5 4.5 0 0 0 6.85 6.85z"/></svg>`))
	closeIcon      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M4 4l8 8M12 4l-8 8"/></svg>`))
	checkIcon      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2.25" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 8.5l3 3 6-7"/></svg>`))
	minusIcon      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round"><path d="M4 8h8"/></svg>`))
	plusIcon       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round"><path d="M4 8h8M8 4v8"/></svg>`))
)

const previewText = `// saveSettings keeps the settings in settings.json.
func (l *lime) saveSettings() {
	b, err := json.MarshalIndent(l.set, "", "\t")
	if err != nil {
		return
	}
	os.WriteFile(path, b, 0o644)
}
`

func (a *app) showSettings() {
	p := &a.prefs
	p.open = true
	if p.page == "" {
		p.page = pageGeneral
	}
	// The system's fonts take a while to list the first time.
	go ui.FontFamilies()
}

// fontLists are the families of the font settings, the system's font
// first, whose name the view says.
type fontLists struct{ all, monoFirst []string }

func (l *lime) fontNames(monoFirst bool) []string {
	if l.fonts == nil {
		all, mono, rest := []string{""}, []string{""}, []string(nil)
		for _, f := range ui.FontFamilies() {
			all = append(all, f.Name)
			if f.Monospaced {
				mono = append(mono, f.Name)
			} else {
				rest = append(rest, f.Name)
			}
		}
		l.fonts = &fontLists{all: all, monoFirst: append(mono, rest...)}
	}
	if monoFirst {
		return l.fonts.monoFirst
	}
	return l.fonts.all
}

func (a *app) settingsView(c *ui.Context) {
	p := &a.prefs
	if p.rec.id != "" && (!p.open || p.page != pageShortcuts) {
		a.stopRecording()
	}
	if !p.open {
		return
	}
	ch := a.scheme.chrome()
	w, h := c.Size()
	pw, ph := min(prefsW, w-32), min(prefsH, h-32)
	mx, my := (w-pw)/2, (h-ph)/2
	p.offset.X, p.offset.Y = max(-mx, min(p.offset.X, mx)), max(-my, min(p.offset.Y, my))
	dim := float32(0.25)
	if a.scheme.dark {
		dim = 0.45
	}
	ui.DialogBase(c, &p.open, func(backdrop, panel ui.Element) {
		backdrop.Background(black.Alpha(dim))
		panel.Row().Width(pw).Height(ph).Radius(10).Border(1, ch.line).Background(ch.panel).Clip().
			Shadow(0, 8, 28, 0, black.Alpha(0.35)).Translate(p.offset.X, p.offset.Y).Label(locales.SettingsTitle())
		ui.Column(c).Width(prefsSideW).FillHeight().Shrink(0).Background(ch.bg).BorderWidth(0, 1, 0, 0).BorderColor(ch.line).Children(func() {
			a.dragBand(ui.Row(c)).Padding(0, 18).Children(func() {
				ui.Text(c, locales.SettingsTitle()).FontSize(13).Bold()
			})
			ui.Sidebar(c, &p.page, func() {
				ui.SidebarItem(c, pageGeneral, generalIcon, locales.SettingsGeneral())
				ui.SidebarItem(c, pageEditor, editorIcon, locales.SettingsEditor())
				ui.SidebarItem(c, pageAppearance, appearanceIcon, locales.SettingsAppearance())
				ui.SidebarItem(c, pageFonts, fontsIcon, locales.SettingsFonts())
				ui.SidebarItem(c, pageShortcuts, shortcutsIcon, locales.SettingsShortcuts())
			}).Grow(1).FillWidth().Padding(0, 8).Background(ui.Transparent).Label(locales.SettingsTitle())
		})
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
			a.dragBand(ui.Row(c)).Padding(0, 12, 0, 24).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, a.pageTitle()).FontSize(15).Bold()
				btn := ui.ButtonBase(c).Width(28).Height(28).Radius(6).Center().Transition(colorFade).
					Label(locales.SettingsClose()).OnClick(func() { p.open = false })
				if btn.Hovered() {
					btn.Background(ch.hover)
				}
				btn.Children(func() { ui.Icon(c, closeIcon).Size(14, 14).TextColor(ch.muted) })
			})
			if p.page == pageShortcuts {
				a.shortcutsBar(c)
			}
			ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
				ui.Column(c).FillWidth().Padding(4, 24, 24).Gap(20).Children(func() {
					switch p.page {
					case pageEditor:
						a.editorPage(c)
					case pageAppearance:
						a.appearancePage(c)
					case pageFonts:
						a.fontsPage(c)
					case pageShortcuts:
						a.shortcutsPage(c)
					default:
						a.generalPage(c)
					}
				})
			})
		})
	})
}

// dragBand makes e the band along the top of the dialog, by which the
// pointer drags it.
func (a *app) dragBand(e ui.Element) ui.Element {
	if dx, dy, ok := e.Dragged(); ok {
		a.prefs.offset.X += dx
		a.prefs.offset.Y += dy
	}
	return e.FillWidth().Height(prefsBandH).Shrink(0).AlignItems(ui.Center)
}

func (a *app) pageTitle() string {
	switch a.prefs.page {
	case pageEditor:
		return locales.SettingsEditor()
	case pageAppearance:
		return locales.SettingsAppearance()
	case pageFonts:
		return locales.SettingsFonts()
	case pageShortcuts:
		return locales.SettingsShortcuts()
	}
	return locales.SettingsGeneral()
}

// settingsGroup holds rows of settings in a box, lines between them.
func (a *app) settingsGroup(c *ui.Context, rows func()) {
	ch := a.scheme.chrome()
	ui.Column(c).FillWidth().Gap(1).Radius(8).Border(1, ch.line).Background(ch.panel.Mix(ch.text, 0.02)).
		Dividers(1, ch.line).Clip().Children(rows)
}

// settingsRow is a setting: its label and what it does, and its control.
func (a *app) settingsRow(c *ui.Context, label, detail string, control func()) {
	ch := a.scheme.chrome()
	ui.Row(c).FillWidth().MinHeight(52).Padding(10, 14).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13)
			if detail != "" {
				ui.Text(c, detail).FontSize(12).TextColor(ch.muted)
			}
		})
		control()
	})
}

func (a *app) heading(c *ui.Context, title string, inUse bool) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(13).Bold()
		if inUse {
			a.tag(c, locales.SettingsInUse())
		}
	})
}

// tag marks a heading or a row, as the theme in use.
func (a *app) tag(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(11).Padding(1, 6).Radius(4).Background(a.scheme.accent.Alpha(0.16)).TextColor(a.scheme.chrome().text).Shrink(0)
}

func (a *app) generalPage(c *ui.Context) {
	a.settingsGroup(c, func() {
		a.settingsRow(c, locales.SettingsLanguage(), locales.SettingsLanguageDesc(), func() {
			tags := mygo.App.Languages()
			options := []string{locales.SettingsFollowSystem()}
			for _, t := range tags {
				options = append(options, i18n.Autonym(t))
			}
			choice := options[0]
			if i := slices.Index(tags, a.set.locale()); i >= 0 {
				choice = options[i+1]
			}
			if ui.Select(c, &choice, options).Width(prefsControlW).Label(locales.SettingsLanguage()).Changed() {
				tag := followSystem
				if i := slices.Index(options, choice); i > 0 {
					tag = tags[i-1]
				}
				a.l.setLanguage(tag)
			}
		})
	})
	if a.l.dir == "" {
		return
	}
	a.settingsGroup(c, func() {
		a.settingsRow(c, locales.SettingsFile(), locales.SettingsFileDesc(), func() {
			a.button(c, locales.SettingsOpenFile(), func() {
				a.prefs.open = false
				a.l.saveSettings()
				a.open([]string{settingsPath(a.l.dir)})
			})
		})
	})
}

func (a *app) button(c *ui.Context, label string, click func()) ui.Element {
	ch := a.scheme.chrome()
	btn := ui.ButtonBase(c).Height(28).Padding(0, 12).Shrink(0).Radius(6).Border(1, ch.line).Background(ch.panel).
		Transition(colorFade).Label(label).OnClick(click)
	if btn.Hovered() {
		btn.Background(ch.panel.Mix(ch.text, 0.06))
	}
	btn.Children(func() { ui.Text(c, label).FontSize(13) })
	return btn
}

var modes = []string{followSystem, lightMode, darkMode}

func (a *app) appearancePage(c *ui.Context) {
	ch := a.scheme.chrome()
	a.settingsGroup(c, func() {
		a.settingsRow(c, locales.SettingsMode(), locales.SettingsModeDesc(), func() {
			at := max(slices.Index(modes, a.set.Appearance), 0)
			seg := ui.SegmentedBase(c, &at, len(modes))
			seg.Track.Gap(2).Padding(2).Shrink(0).Radius(7).Background(ch.bg).Border(1, ch.line).Label(locales.SettingsMode()).Children(func() {
				labels := []string{locales.SettingsSystem(), locales.Light(), locales.Dark()}
				for i, ic := range []*ui.SVG{systemIcon, lightIcon, darkIcon} {
					s := seg.Segment(i).Height(24).Padding(0, 10).Gap(6).Radius(5).AlignItems(ui.Center).Label(labels[i])
					fg := ch.muted
					if i == at {
						fg = ch.text
						bg := ch.panel
						if a.scheme.dark {
							bg = ch.panel.Mix(ch.text, 0.14)
						}
						s.Background(bg).Shadow(0, 1, 2, 0, black.Alpha(0.15))
					}
					s.Children(func() {
						ui.Icon(c, ic).Size(14, 14).TextColor(fg)
						ui.Text(c, labels[i]).FontSize(12).TextColor(fg)
					})
				}
			})
			if seg.Track.Changed() {
				a.set.Appearance = modes[at]
				a.l.saveSettings()
			}
		})
		a.settingsRow(c, locales.SettingsLayoutStyle(), "", func() {
			m := locales.In(i18n.Locale())
			var options []string
			for _, s := range layoutStyles {
				options = append(options, s.label(m))
			}
			choice := a.set.layoutStyle().label(m)
			if ui.Select(c, &choice, options).Width(prefsControlW).Label(locales.SettingsLayoutStyle()).Changed() {
				a.set.LayoutStyle = layoutStyles[slices.Index(options, choice)].name
				a.l.saveSettings()
			}
		})
	})
	a.themes(c, false)
	a.themes(c, true)
}

// themes lists the schemes of the dark or the light appearance, to choose
// its theme from.
func (a *app) themes(c *ui.Context, dark bool) {
	title := locales.SettingsLightTheme()
	if dark {
		title = locales.SettingsDarkTheme()
	}
	chosen := a.set.theme(dark)
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		a.heading(c, title, a.scheme.dark == dark)
		ui.Grid(c).FillWidth().Columns(4).Gap(12).Children(func() {
			for _, sc := range schemes {
				if sc.dark != dark {
					continue
				}
				a.choiceCard(c, sc.name, sc == chosen, func() {
					if dark {
						a.set.DarkTheme = sc.name
					} else {
						a.set.LightTheme = sc.name
					}
					a.l.saveSettings()
				}, func() { themePicture(c, sc) })
			}
		})
	})
}

// choiceCard is an option to choose: its picture over its name, ringed
// and checked while chosen.
func (a *app) choiceCard(c *ui.Context, name string, on bool, choose func(), picture func()) {
	ch := a.scheme.chrome()
	card := ui.ButtonBase(c.Key(name)).Column().Radius(8).Border(1, ch.line).Background(ch.panel).Clip().
		Label(name).OnClick(choose)
	if on {
		card.BorderColor(a.scheme.accent).Shadow(0, 0, 0, 1, a.scheme.accent)
	}
	card.Children(func() {
		ui.Column(c).FillWidth().BorderWidth(0, 0, 1, 0).BorderColor(ch.line).Children(func() {
			picture()
			if on {
				ui.Box(c).Absolute().Top(6).Right(6).Width(16).Height(16).Radius(8).Center().Background(a.scheme.accent).Children(func() {
					ui.Icon(c, checkIcon).Size(10, 10).TextColor(ui.RGB(255, 255, 255))
				})
			}
		})
		ui.Text(c, name).FontSize(12).Padding(7, 10).MaxLines(2).Ellipsis("…")
	})
}

// themePicture draws sc as lines of code in its colors.
func themePicture(c *ui.Context, sc *scheme) {
	tok := func(k editor.TokenKind) ui.Color { return sc.tokens[k].Color }
	lines := [][]struct {
		color ui.Color
		w     float32
	}{
		{{tok(editor.TokenKeyword), 14}, {tok(editor.TokenFunction), 34}},
		{{tok(editor.TokenType), 20}, {tok(editor.TokenString), 28}},
		{{tok(editor.TokenComment), 44}},
	}
	ui.Column(c).FillWidth().Height(58).Padding(12, 10).Gap(7).Background(sc.bg).Children(func() {
		for _, l := range lines {
			ui.Row(c).Gap(4).Children(func() {
				for _, t := range l {
					ui.Box(c).Width(t.w).Height(4).Radius(2).Background(t.color)
				}
			})
		}
	})
}

func (a *app) fontsPage(c *ui.Context) {
	ch := a.scheme.chrome()
	a.settingsGroup(c, func() {
		a.settingsRow(c, locales.SettingsUIFont(), locales.SettingsUIFontDesc(), func() {
			a.fontChoice(c, &a.set.UIFont, locales.SettingsUIFont(), locales.SettingsSystemDefault(), false)
		})
		a.settingsRow(c, locales.SettingsContentFont(), locales.SettingsContentFontDesc(), func() {
			a.fontChoice(c, &a.set.ContentFont, locales.SettingsContentFont(), locales.SettingsSystemMono(), true)
		})
		a.settingsRow(c, locales.SettingsFontSize(), locales.SettingsFontSizeDesc(), func() {
			a.stepper(c, fmt.Sprintf("%g", a.set.FontSize), locales.SettingsSmaller(), locales.SettingsLarger(), func(by int) {
				a.setFontSize(a.set.FontSize + float32(by))
			})
		})
	})
	p := &a.prefs
	if p.preview == nil {
		buf := editor.NewBuffer(previewText)
		buf.SetLanguage(editor.LanguageFor("main.go"))
		p.preview = &editor.State{Buffer: buf, ReadOnly: true, NoActiveLine: true, NoFolding: true}
	}
	st := p.preview
	st.Theme, st.Font, st.LineHeight = a.scheme.editorTheme(), a.set.font(), a.set.lineHeight()
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		a.heading(c, locales.SettingsPreview(), false)
		ui.Column(c).FillWidth().Height(150).PaddingY(8).Radius(8).Border(1, ch.line).Background(a.scheme.bg).Clip().Children(func() {
			editor.View(c.Key("preview"), st).Grow(1).FillWidth().MinHeight(0)
		})
	})
}

// fontChoice chooses the family *family names among the system's, ""
// being the system's font, which system names.
func (a *app) fontChoice(c *ui.Context, family *string, label, system string, monoFirst bool) {
	names := a.l.fontNames(monoFirst)
	names[0] = system
	choice := *family
	if choice == "" {
		choice = system
	}
	if ui.Combobox(c, &choice, names).Width(prefsControlW).Shrink(0).Label(label).Changed() {
		if choice == system {
			choice = ""
		}
		*family = choice
		a.l.saveSettings()
	}
}

// stepper shows value between buttons that step it down and up, by
// calling step with -1 and 1.
func (a *app) stepper(c *ui.Context, value, less, more string, step func(by int)) {
	ch := a.scheme.chrome()
	button := func(ic *ui.SVG, label string, by int) {
		btn := ui.ButtonBase(c).Width(28).Height(26).Center().Transition(colorFade).Label(label).
			OnClick(func() { step(by) })
		if btn.Hovered() {
			btn.Background(ch.panel.Mix(ch.text, 0.06))
		}
		btn.Children(func() { ui.Icon(c, ic).Size(12, 12).TextColor(ch.text) })
	}
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Radius(6).Border(1, ch.line).Background(ch.panel).Clip().Children(func() {
		button(minusIcon, less, -1)
		ui.Box(c).Width(40).Height(26).Center().BorderWidth(0, 1, 0, 1).BorderColor(ch.line).Children(func() {
			ui.Text(c, value).FontSize(13).FontFeatures("tnum")
		})
		button(plusIcon, more, 1)
	})
}

var indentStyles = []editor.IndentStyle{editor.IndentAuto, editor.IndentTabs, editor.IndentSpaces}

// whiteSpaces are the choices of draw_white_space the dialog offers.
var whiteSpaces = []whiteSpace{{}, {"selection"}, {"trailing", "isolated"}, {"all"}}

func (a *app) editorPage(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		a.heading(c, locales.SettingsDisplay(), false)
		a.settingsGroup(c, func() {
			a.settingsRow(c, locales.SettingsRulers(), "", func() {
				p := &a.prefs
				if !p.ruler.Focused(c) {
					p.rulers = formatRulers(a.set.Rulers)
				}
				ui.TextInput(c, &p.rulers).Width(prefsControlW).Label(locales.SettingsRulers()).Bind(&p.ruler).OnChange(func() {
					a.set.Rulers = parseRulers(p.rulers)
					a.l.saveSettings()
				})
			})
			a.settingsRow(c, locales.SettingsWhiteSpace(), "", func() {
				options := []string{locales.SettingsWhiteSpaceNone(), locales.SettingsWhiteSpaceSelection(), locales.SettingsWhiteSpaceTrailing(), locales.SettingsWhiteSpaceAll()}
				i := slices.IndexFunc(whiteSpaces, func(w whiteSpace) bool { return w.mode() == a.set.DrawWhiteSpace.mode() })
				choice := options[i]
				if ui.Select(c, &choice, options).Width(prefsControlW).Label(locales.SettingsWhiteSpace()).Changed() {
					a.set.DrawWhiteSpace = whiteSpaces[slices.Index(options, choice)]
					a.l.saveSettings()
				}
			})
			a.settingsRow(c, locales.SettingsLineNumbers(), "", func() {
				if ui.Switch(c, &a.set.LineNumbers).Label(locales.SettingsLineNumbers()).Changed() {
					a.l.saveSettings()
				}
			})
			a.settingsRow(c, locales.SettingsHighlightLine(), "", func() {
				if ui.Switch(c, &a.set.HighlightLine).Label(locales.SettingsHighlightLine()).Changed() {
					a.l.saveSettings()
				}
			})
			a.settingsRow(c, locales.SettingsScrollBars(), "", func() {
				if ui.Switch(c, &a.set.ShowScrollBars).Label(locales.SettingsScrollBars()).Changed() {
					a.l.saveSettings()
				}
			})
			a.settingsRow(c, locales.SettingsCursorStyle(), "", func() {
				m := locales.In(i18n.Locale())
				var options []string
				for _, s := range cursorStyles {
					options = append(options, s.label(m))
				}
				choice := a.set.cursorStyle().label(m)
				if ui.Select(c, &choice, options).Width(prefsControlW).Label(locales.SettingsCursorStyle()).Changed() {
					a.set.CursorStyle = cursorStyles[slices.Index(options, choice)].name
					a.l.saveSettings()
				}
			})
			a.settingsRow(c, locales.SettingsTypingMode(), "", func() {
				options := []string{locales.TypingNormal(), locales.TypingRampage()}
				choice := options[0]
				if a.set.rampage() {
					choice = options[1]
				}
				if ui.Select(c, &choice, options).Width(prefsControlW).Label(locales.SettingsTypingMode()).Changed() {
					a.set.TypingMode = []string{normalTyping, rampageTyping}[slices.Index(options, choice)]
					a.l.saveSettings()
				}
			})
		})
	})
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		a.heading(c, locales.SettingsIndentation(), false)
		a.settingsGroup(c, func() {
			a.settingsRow(c, locales.SettingsTabSize(), locales.SettingsTabSizeDesc(), func() {
				a.stepper(c, strconv.Itoa(a.set.TabSize), locales.SettingsTabSizeSmaller(), locales.SettingsTabSizeLarger(), func(by int) {
					a.set.TabSize = max(1, min(a.set.TabSize+by, 8))
					a.l.saveSettings()
				})
			})
			a.settingsRow(c, locales.SettingsIndentWith(), locales.SettingsIndentWithDesc(), func() {
				options := []string{locales.SettingsFollowLanguage(), locales.SettingsTabs(), locales.SettingsSpaces()}
				choice := options[slices.Index(indentStyles, a.set.indent())]
				if ui.Select(c, &choice, options).Width(prefsControlW).Label(locales.SettingsIndentWith()).Changed() {
					a.set.setIndent(indentStyles[slices.Index(options, choice)])
					a.l.saveSettings()
				}
			})
			a.settingsRow(c, locales.SettingsDetectIndentation(), locales.SettingsDetectIndentationDesc(), func() {
				if ui.Switch(c, &a.set.DetectIndentation).Label(locales.SettingsDetectIndentation()).Changed() {
					a.l.saveSettings()
				}
			})
		})
	})
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		a.heading(c, locales.SettingsOnSave(), false)
		a.settingsGroup(c, func() {
			a.settingsRow(c, locales.SettingsTrimWhiteSpace(), locales.SettingsTrimWhiteSpaceDesc(), func() {
				if ui.Switch(c, &a.set.TrimTrailingWhiteSpaceOnSave).Label(locales.SettingsTrimWhiteSpace()).Changed() {
					a.l.saveSettings()
				}
			})
			a.settingsRow(c, locales.SettingsEnsureNewline(), locales.SettingsEnsureNewlineDesc(), func() {
				if ui.Switch(c, &a.set.EnsureNewlineAtEOFOnSave).Label(locales.SettingsEnsureNewline()).Changed() {
					a.l.saveSettings()
				}
			})
		})
	})
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		a.heading(c, locales.SettingsDiffLayout(), false)
		ui.Grid(c).FillWidth().Columns(2).Gap(12).Children(func() {
			for _, unified := range []bool{false, true} {
				name, picture := locales.CompareSideBySide(), splitPicture
				if unified {
					name, picture = locales.CompareUnified(), unifiedPicture
				}
				a.choiceCard(c, name, a.set.DiffUnified == unified, func() {
					a.set.DiffUnified = unified
					a.l.saveSettings()
				}, func() {
					ui.Box(c).FillWidth().Height(96).Center().Background(a.scheme.bg).Children(func() {
						ui.Image(c, picture).TextColor(a.scheme.chrome().text)
					})
				})
			}
		})
	})
}

func formatRulers(cols []int) string {
	s := make([]string, len(cols))
	for i, n := range cols {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}

// parseRulers reads the columns of rulers typed apart by commas or spaces.
func parseRulers(s string) []int {
	cols := []int{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if n, err := strconv.Atoi(f); err == nil && n > 0 {
			cols = append(cols, n)
		}
	}
	return cols
}

// splitPicture and unifiedPicture are a diff laid out side by side and
// unified, in the colors of the text and of lines deleted and added.
var (
	splitPicture = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="176" height="76" viewBox="0 0 176 76">
	<rect x="4" y="6" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="6" width="34" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="4" y="18" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="18" width="52" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="0" y="26" width="87" height="11" fill="#e5534b" fill-opacity=".22"/>
	<rect x="4" y="30" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="30" width="44" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="0" y="38" width="87" height="11" fill="#e5534b" fill-opacity=".22"/>
	<rect x="4" y="42" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="42" width="28" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="4" y="66" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="66" width="40" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="87.5" y="0" width="1" height="76" fill="currentColor" fill-opacity=".15"/>
	<rect x="93" y="6" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="104" y="6" width="34" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="93" y="18" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="104" y="18" width="52" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="89" y="26" width="87" height="11" fill="#2da44e" fill-opacity=".22"/>
	<rect x="93" y="30" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="104" y="30" width="50" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="89" y="38" width="87" height="11" fill="#2da44e" fill-opacity=".22"/>
	<rect x="93" y="42" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="104" y="42" width="36" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="89" y="50" width="87" height="11" fill="#2da44e" fill-opacity=".22"/>
	<rect x="93" y="54" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="104" y="54" width="24" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="93" y="66" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="104" y="66" width="40" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
</svg>`))
	unifiedPicture = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="176" height="76" viewBox="0 0 176 76">
	<rect x="4" y="6" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="6" width="34" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="4" y="18" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="18" width="52" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="0" y="26" width="176" height="11" fill="#e5534b" fill-opacity=".22"/>
	<rect x="4" y="30" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="30" width="44" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="0" y="38" width="176" height="11" fill="#e5534b" fill-opacity=".22"/>
	<rect x="4" y="42" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="42" width="28" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="0" y="50" width="176" height="11" fill="#2da44e" fill-opacity=".22"/>
	<rect x="4" y="54" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="54" width="50" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
	<rect x="0" y="62" width="176" height="11" fill="#2da44e" fill-opacity=".22"/>
	<rect x="4" y="66" width="6" height="3" rx="1.5" fill="currentColor" fill-opacity=".3"/>
	<rect x="15" y="66" width="36" height="3" rx="1.5" fill="currentColor" fill-opacity=".6"/>
</svg>`))
)

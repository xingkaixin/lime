# Settings, Keymaps & Menus

Lime stores user preferences and key overrides in `settings.json` with Sublime Text-compatible schemas, deterministic keymap precedence, platform-specific default bindings, and live synchronization with native menus.

## Settings Persistence, Reloading & Migration

- **Unparseable file write guard**: Any `settings.json` parse error is recorded in `lime.setErr`; while `setErr != nil`, `saveSettings` updates open windows in memory but refuses to overwrite `settings.json`, protecting hand-edited files from accidental data loss until they parse cleanly (`cmd/lime/app.go`, `cmd/lime/settings.go`).
- **Live reload on tab save**: Saving `settings.json` in an editor tab triggers `reloadSettings` so manual edits take effect immediately and are not overwritten by later UI writes; if the saved file fails to parse, `setErr` is recorded and the previous in-memory settings stay active (`cmd/lime/settings.go`).
- **Instant-apply settings dialog**: `prefs` stores only dialog UI state with no staging copy; every settings change applies immediately to all windows and persists to `settings.json` (`cmd/lime/settingsview.go`).
- **Legacy `color_scheme` migration**: The deprecated `color_scheme` field is kept solely for `settings.migrate` (called during `loadSettings`) and cleared after migration. `"Auto"` or any unrecognized scheme name migrates to `Appearance = "system"` (`followSystem`) with the default GitHub themes, whereas any known named scheme pins `Appearance` to that scheme's light or dark polarity (`LightTheme` or `DarkTheme`) regardless of the system appearance (`cmd/lime/settings.go`).
- **Color scheme polarity switching**: `showScheme` assigns the chosen scheme to `LightTheme` or `DarkTheme` according to its polarity. Choosing a scheme opposite the currently displayed appearance switches `Appearance` to match so the change is immediately visible; choosing a same-polarity scheme preserves the current `Appearance` (such as `"system"`) (`cmd/lime/settings.go`).

## Settings Schema & Sublime Text Compatibility

- **Zero `tab_size` fallback**: A `tab_size` of `0` in `settings.json` resolves to the editor default (`4`) rather than zero width, preserving compatibility with older or default-omitted settings (`cmd/lime/indentation.go`).
- **Tri-state `translate_tabs_to_spaces`**: `TranslateTabsToSpaces` is a `*bool` where `nil` (unset) follows the language's default indentation, `true` forces spaces, and `false` forces tabs (`cmd/lime/settings.go`).
- **Ligature subset for `font_options`**: `font_options` accepts Sublime Text's option strings and honors only the ligature-disabling flags (`"no_liga"`, `"no_clig"`, and `"no_calt"`), translating them to OpenType font features (`cmd/lime/settings.go`).
- **Line height base multiplier**: Additive `line_padding_top` and `line_padding_bottom` values convert to `editor.State.LineHeight` as `1.5 + (LinePaddingTop+LinePaddingBottom)/FontSize` (returning `0` when `FontSize <= 0`), where `1.5` matches MyGo editor's built-in default line height multiplier (`cmd/lime/settings.go`).
- **Plain wrap guides for `rulers`**: `rulers` accepts both bare column integers and Sublime Text's `[column, style]` arrays, discarding the style element because MyGo's editor draws only plain wrap guides (`cmd/lime/settings.go`).
- **Boolean collapse for `scroll_past_end`**: `scroll_past_end` accepts both booleans and Sublime Text's numeric scroll-fraction values, collapsing any positive value (`> 0`) to `true` because MyGo's editor supports only a boolean toggle (`cmd/lime/settings.go`).
- **Dual syntax for `draw_white_space`**: `draw_white_space` accepts both Sublime Text's legacy single strings (`"none"`, `"selection"`, `"all"`) and newer ordered option arrays, applying array entries sequentially (`cmd/lime/settings.go`).
- **Lossy whitespace mode mapping**: `whiteSpace.mode` maps Sublime Text's fine-grained whitespace flags onto MyGo editor's four `editor.Whitespace` modes (`None`, `Selection`, `Trailing`, `All`): whitespace at line ends alone (`trailing`/`isolated`) maps to `WhitespaceTrailing`, any leading or enclosed whitespace maps to `WhitespaceAll`, selection-only maps to `WhitespaceSelection`, and options targeting tabs, spaces, or mixed indentation alone draw both tabs and spaces (`cmd/lime/settings.go`).
- **Sublime Text encoding names**: `fallback_encoding` specifies the encoding used for non-UTF-8 files using Sublime Text's encoding names (`cmd/lime/settings.go`).
- **Override semantics for exclude patterns**: `FolderExcludePatterns` and `FileExcludePatterns` completely replace the built-in side bar and indexing exclude defaults whenever set (non-nil, including an empty slice) instead of merging with them (`cmd/lime/settings.go`).

## Keymap Persistence, Precedence & Routing

- **Sublime Text key syntax**: User key overrides (`keyChange`) persist in `settings.json` using Sublime Text's key syntax (e.g., `"super+shift+d"`), with space-separated strokes for chords (e.g., `"super+k super+u"`) (`cmd/lime/keymap.go`).
- **Unambiguous `+` stroke parsing**: `parseStroke` splits stroke strings on `"+"` unambiguously because no key is named `"+"`; `Shift+=` is always written instead (`cmd/lime/keymap.go`).
- **`Cmd+K` chord representation**: `appKey.chord` specifically represents Sublime Text's `Cmd+K` chord prefix followed by `mods+key`, rather than arbitrary two-stroke chords (`cmd/lime/keys.go`).
- **Initialization order and key reservations**: `initKeys` runs in `init` after commands are registered. `defaultKeys` populates window bindings (`appKeys`) before editor bindings (`editorKeys`); editor bindings with no associated command (such as arrow navigation) go into `fixedKeys` and, along with `systemKeys()`, are added to `reserved` so commands cannot claim them (`cmd/lime/keymap.go`).
- **Editor command routing**: Setting `command.ed` to an `editor.Command` links the command to its default bindings in `editorKeys` and routes its non-chord shortcuts directly to MyGo's editor keymap (`km.editor`) rather than the window keymap (`km.window`) (`cmd/lime/commands.go`).
- **Last-changed-wins conflict precedence**: When multiple commands share a shortcut, the command modified most recently runs; `keymap.users` maintains the invariant that index `0` is always the winning command for that key (`cmd/lime/keymap.go`).
- **`setKeys` reset vs. unbind and persistence order**: `setKeys` treats `nil` as restoring Sublime Text's defaults (removing the command's override), distinct from an empty slice which unbinds the command. Modified commands are appended to the end of `settings.Keys` so chronological order encodes last-changed-wins precedence across reloads (`cmd/lime/keymap.go`, `cmd/lime/settings.go`).
- **Menu shortcut selection**: `keymap.keys` stores each command's bindings in priority order, and native menus display the first eligible (non-chord, non-shadowed) binding in that slice (`cmd/lime/keymap.go`).

## Platform Defaults & Compatibility Policies

- **Strict Sublime Text editor keymap**: `editorKeys` replaces MyGo editor's built-in defaults with Sublime Text's per-platform bindings and unbinds every other default editor key (`cmd/lime/keys.go`).
- **Chord exclusion in `sublimeOnly`**: When binding omitted default editor keys to `editor.NoCommand`, `sublimeOnly` skips default editor chords (`b.ThenKey != 0`) because their `Cmd+K` prefix is handled at the window level before reaching the editor (`cmd/lime/keys.go`).
- **Reserved OS and window manager shortcuts**: `systemKeys()` reserves shortcuts intercepted by the OS or window manager before the app receives them: `Alt+F4` on non-macOS, and `Cmd+Q`, `Cmd+H`, `Opt+Cmd+H`, `Cmd+M`, ``Cmd+` ``, and `Ctrl+Cmd+F` on macOS (`cmd/lime/keymap.go`).
- **Explicit macOS system text bindings**: `macEditorKeys` explicitly binds macOS Cocoa/Emacs text navigation and editing keys (`Ctrl+A/E/B/F/P/N/H/D`, etc.) because Sublime Text relies on macOS system text bindings that MyGo's editor does not inherit automatically (`cmd/lime/keys.go`).
- **Linux workspace shortcut avoidance**: On Linux, `AddCursorAbove`/`AddCursorBelow` bind to `Alt+Shift+arrows` instead of Windows's `Ctrl+Alt+arrows` because Linux desktop environments reserve `Ctrl+Alt+arrows` for workspace switching (`cmd/lime/keys.go`).
- **Zed side-bar panel shortcuts**: `toggle_files_panel` (`Cmd+Shift+E`, `Ctrl+Shift+E` on non-macOS) and `toggle_git_panel` (`Ctrl+Shift+G`) adopt Zed's shortcuts because Sublime Text has no corresponding side-bar panels (`cmd/lime/keys.go`).
- **Zed Markdown preview chord**: `open_markdown_preview_to_the_side` adopts Zed's `Cmd+K V` (without `Cmd` on the second stroke) because Sublime Text has no Markdown preview and reserves `Cmd+K Cmd+V` for paste-from-history (`cmd/lime/keys.go`).

## Shortcuts UI, Recording & Menu Synchronization

- **Remappable command grouping and exclusions**: `shortcutGroups` lists remappable commands grouped by menu and deliberately excludes built-in editor navigation keys (such as arrows) and system menu commands (such as Copy), which users cannot rebind (`cmd/lime/shortcutsview.go`).
- **Exclusive input capture while recording**: While recording a shortcut, `record` claims and consumes all keyboard events (`InputKeyDown`, `InputText`, `InputCompose`, and `InputCommand`) so no keypress leaks to other controls or triggers commands (`cmd/lime/shortcutsview.go`).
- **Shortcut validation and refusal rules**: `refusal` permits any `Cmd+K` chord stroke, rejects non-chord keys without `Cmd`, `Ctrl`, or `Alt` (other than `F1`–`F12`) because they would be typed as text, and rejects reserved system or editor keys (`cmd/lime/shortcutsview.go`).
- **Hidden-row recording cancellation**: After building the immediate-mode shortcuts list, if a search filter or conflicts toggle hides the command currently being recorded (`!r.seen`), recording is stopped and the view invalidated so recording state and paused menu shortcuts do not stay stuck (`cmd/lime/shortcutsview.go`).
- **Single recorder per command in conflicts view**: When a command appears in more than one conflict group, only its first rendered row mounts the recorder widget (guarded by `!r.seen`), avoiding duplicate recorders competing for focus (`cmd/lime/shortcutsview.go`).

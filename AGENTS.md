# AGENTS.md

Lime is a plain text editor modeled on Sublime Text (no packages), built on
MyGo native UI (`ui`, `ui/editor`). Pure Go: everything builds with
`CGO_ENABLED=0`.

Before changing UI code, read `.agents/skills/mygo-maintenance/SKILL.md`.
`go tool mygo install-skills` manages it; don't edit it by hand.

## Layout

All code is `package main` in `cmd/lime`. Keep it one package.

- `app.go`: docs, groups, layouts, the `lime` and `app` types; open, save, close
- `commands.go`: every command, by Sublime Text's command name
- `keys.go`: default keys per platform; `keymap.go`: the user's key changes
- `input.go`: the editor's mouse, remapped to Sublime Text's
- `menu.go`: the menu bar; `contextmenu.go`: tab and side bar menus
- `view.go` (window, tab bar, status bar), `sidebar.go`, `panel.go` (Goto
  Anything, command palette), `find.go`, `tabdrag.go`, `preview.go`: window UI
- `compare.go`: diff tabs, of a tab against a file, a tab, the clipboard or disk
- `project.go`: folders, `.sublime-project` files, file operations
- `git.go`: the side bar's work tree and branch, switched from the quick panel
- `history.go`: the side bar's History tab: commits, their files and diffs
- `settings.go`, `settingsview.go`, `shortcutsview.go`: settings.json and its dialog
- `session.go`: hot exit (session.json)
- `theme.go`: color schemes and the chrome colors derived from them
- `locales/`: `en.json` (source), `zh-Hans.json`; `catalog_gen.go` is generated

## Terms

- `lime`: process-wide state: settings, keymap, menu, windows
- `app`: one window, not the application
- `doc`: a tab. `group`: a pane of tabs. `place`: the focused group and doc
- `settings` (`a.set`): persisted. `prefs`: the settings dialog's UI state

## Rules

- Sublime Text is the reference for behavior, command ids and per-platform keys.
- A new command touches `makeCommands`, its key in `appKeys` or the editor
  keymaps, `shortcutGroups` (or the Shortcuts page won't list it), and
  `newMenu` if it has a menu item.
- Every user-visible string goes through `locales`: add the key to both JSON
  files, then run `go tool mygo generate`. Never edit `catalog_gen.go`.
- UI state changes only on the main thread. Run dialogs, folder walks and
  decoding in a goroutine; publish results with `win.Update`.
- settings.json and session.json must keep loading files from older versions.
  Migrate in `settings.migrate` / `restoreSession`.
- `input.go` calls MyGo's private editor input via `go:linkname` and a
  reflected field offset. After a MyGo upgrade, run `TestSublimeInput` on macOS.

## Verify

```sh
go tool mygo vet .
go tool mygo i18n check .
CGO_ENABLED=0 go test ./...
mise run dev   # the app with live reload
```

Tests drive the UI with `ui.NewTester(a.view, …)` on `newApp("")`, which
writes no files.

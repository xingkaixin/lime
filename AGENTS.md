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
- `definition.go`: Goto Definition, from the definitions in the open tabs
- `completion.go`: word completions, from the words of the open tabs
- `jump.go`: Jump Back and Jump Forward, through where the caret jumped from
- `indentation.go`: each file's own indentation, detected as it opens
- `format.go`: a file's line endings; `encoding.go`: the encodings files
  are read and saved in
- `compare.go`: diff tabs, of a tab against a file, a tab, the clipboard or disk
- `project.go`: folders, `.sublime-project` files, file operations
- `recent.go`: Open Recent and Quick Switch Project, of the files, folders
  and projects opened lately
- `git.go`: the side bar's work tree and branch, switched from the quick panel
- `remote.go`: Push, Pull and Fetch, and the side bar's counts of the commits
  to push and to pull
- `changes.go`: the side bar's Changes tab, of the work tree's changes, staged
  or not; `history.go`: its History tab; `commitview.go`: a tab of the diffs
  of a commit's files, or of the staged or unstaged changes; `avatar.go`:
  commit authors' pictures, from GitHub
- `gitmarks.go`: the side bar's files and folders, tinted as git status
  has them, and dimmed where git ignores them
- `minidiff.go`: what the gutter marks changes against: the file in git
  (`git_diff_target`), or on disk if git doesn't track it
- `settings.go`, `settingsview.go`, `shortcutsview.go`: settings.json and its dialog
- `session.go`: hot exit (session.json)
- `theme.go`: color schemes and the chrome colors derived from them
- `locales/`: `en.json` (source), `zh-Hans.json`; `catalog_gen.go` is generated
- `website/`: the landing page, which runs Lime compiled to WebAssembly on a
  few of this repository's files and their last commits, in memory; `git.js`
  stands in for git (`git_js.go`). `mise run site:dev` builds and serves it

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

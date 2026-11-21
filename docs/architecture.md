# Architecture & Runtime Lifecycle

Lime is a single-package pure-Go (`CGO_ENABLED=0`) desktop editor built on MyGo, modeled on Sublime Text's features and keybindings with main-thread UI state and headless in-memory testing.

## Scope & Process-Wide State

- **Sublime Text scope without packages**: Lime implements a plain text editor covering tabs, groups, Goto Anything, the command palette, a folder side bar, syntax highlighting, word wrap, multiple carets, and column selection on Sublime Text's keybindings, deliberately omitting a package or plugin system (`cmd/lime/main.go`).
- **Hermetic persistence toggle (`lime.dir`)**: `lime.dir` stores settings and session files on disk; setting it to `""` disables all settings and session persistence so tests created via `newApp("")` run hermetically in memory (`cmd/lime/app.go`).
- **Focused-window menu routing (`lime.last`)**: `lime.last` tracks the most recently focused window (`*app`) so process-wide menu actions—notably the macOS global menu bar—route to the last active window (`cmd/lime/app.go`).
- **Prompt memory release for large closed documents**: Closing any document whose buffer exceeds `largeFile` (16 MiB) schedules `debug.FreeOSMemory()` via `time.AfterFunc` after 1 second, returning memory to the OS once closing frames finish rather than waiting minutes for Go's runtime scavenger to release it as the heap grows (`cmd/lime/app.go`).

## Startup & macOS Lifecycle

- **Launch-time file opens before window creation**: `mygo.App.OnOpenFile` (invoked by Finder, the Dock, and `open(1)`) can fire before `mygo.App.WhenReady` creates OS windows (`a.win == nil`), so the handler invokes `a.open` directly when `a.win == nil` and dispatches via `a.win.Update` only once a window exists (`cmd/lime/main.go`).
- **macOS Dock persistence after closing all windows**: On macOS, `main` registers a no-op `mygo.App.OnWindowAllClosed` listener because MyGo exits when the last window closes unless a listener is attached, whereas macOS applications stay running in the Dock (`cmd/lime/main.go`).

## Command Registry

- **Unified Sublime Text command IDs**: A single `command` registry backs keyboard shortcuts, menus, and the command palette, using Sublime Text's command IDs wherever Sublime Text defines them (`cmd/lime/commands.go`).
- **Deferred initialization in `init()`**: `commands` and `commandByID` are populated in `init()` via `makeCommands()` rather than package-level variable initializers to avoid a Go initialization cycle, since command handlers such as `showCommandPalette` read `commands` (`cmd/lime/commands.go`).
- **Command palette visibility (`command.caption`)**: Leaving `caption` as `nil` excludes a command from the command palette, which filters entries on `c.caption != nil` (`cmd/lime/commands.go`, `cmd/lime/panel.go`).
- **Dynamic availability (`command.enabled`)**: Leaving `enabled` as `nil` marks a command as always enabled; when non-nil, the command palette hides the command whenever `enabled(a)` returns `false` (`cmd/lime/commands.go`, `cmd/lime/panel.go`).

## Main-Thread UI Discipline & Background Work

- **Standardized off-thread work (`app.background`)**: `app.background` runs blocking work in a goroutine and publishes the returned completion closure on the main UI thread via `a.win.Update`; when `a.win == nil` (headless windows such as tests), it runs both closures synchronously inline for deterministic execution (`cmd/lime/git.go`).
- **Non-blocking disk change scanning (`changedOnDisk`)**: `changedOnDisk` performs blocking `stat` and file-read I/O over snapshotted document paths, file stamps, and encodings, so callers must invoke it off the main thread (`cmd/lime/app.go`).
- **Non-blocking Git commit reads (`readCommit`)**: `readCommit` performs blocking Git and disk I/O to read a commit's message, changed files, hunks, and file texts, so it must only run off the main UI thread (`cmd/lime/commitview.go`).
- **Main-thread-only avatar cache (`lime.avatars`)**: `lime.avatars` has no mutex because all reads and writes occur exclusively on the main thread; background goroutines decode images off-thread and publish results back to the map via `a.win.Update` (`cmd/lime/avatar.go`).

## Headless Test Execution

- **No avatar I/O without a window or data directory**: `lime.avatar` returns early without spawning disk cache or network fetches when `a.win == nil` or `l.dir == ""`, keeping tests free of network and filesystem I/O (`cmd/lime/avatar.go`).
- **No notice-clear timers without a window**: `app.notify` skips scheduling the 4-second (`noticeTime`) clear timer when `a.win == nil`, because headless test apps created via `newApp("")` never open a window and have no UI frame loop to process `win.Update` (`cmd/lime/view.go`).
- **Testing mid-flight async states**: Because headless tests execute `app.background` synchronously inline, tests that assert behavior while an asynchronous load is still in flight—such as attempting to commit before Amend finishes loading `HEAD`'s message—must set the pending state directly rather than relying on goroutine timing (`cmd/lime/main_test.go`).

## Embedded Assets & Code Generation

- **Embedded Pierre file icons**: `pierreIconSVGs` embeds file-icon SVGs from Pierre Icons (`https://github.com/pierrecomputer/vscode-icons`), while `fileicons/update-pierre.sh` generates the matching rules and color palette into `pierre_gen.go` (`cmd/lime/pierre.go`).

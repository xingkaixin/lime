# Lime Design & Decision Records

Non-obvious architectural, behavioral, platform, and compatibility decisions extracted from the Lime codebase.

| Document | Scope | Decisions |
| --- | --- | ---: |
| [Architecture & Runtime Lifecycle](./architecture.md) | Process-wide state, command registry, macOS lifecycle, main-thread UI discipline, and platform builds. | 18 |
| [Editor & Input](./editor-and-input.md) | Sublime Text mousemap and chords, carets and cursor styles, multi-cursor completions, word boundaries, and paste/drag behavior. | 32 |
| [UI, Chrome & Layout](./ui-and-layout.md) | Window chrome, title bar, tab bar overflow math, pane layouts and dividers, sidebar, dialogs, markdown preview, themes, and animations. | 114 |
| [Files, Projects & Session](./files-and-session.md) | Dirty checks, large-file thresholds, encodings, line endings, indentation detection, safe saving, `.sublime-project`, watching, and hot exit. | 68 |
| [Git Integration](./git-integration.md) | Git execution flags, repository resolution, Changes/History/Commit views, hunk staging, mini-diff, blame, sidebar marks, remotes, avatars, and permalinks. | 105 |
| [Search & Navigation](./search-and-navigation.md) | Find/Replace bar, Find in Files (`Where` filters, parallel search, Replace in Files), Goto Anything, Goto Definition, and Jump history. | 43 |
| [Settings, Keymaps & Menus](./settings-and-keymaps.md) | `settings.json` schema and migration, Sublime Text compatibility, keymap precedence, platform defaults, and menu synchronization. | 39 |
| [Website Demo & Build Tooling](./website-and-build.md) | WebAssembly demo architecture, in-memory `limeFS` and `limeGit`, browser input/theme/touch behavior, `mise.toml`, and icon/theme codegen. | 26 |

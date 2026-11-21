# Website Demo & Build Tooling

Design decisions governing Lime's WebAssembly landing-page demo, its in-memory filesystem and Git shims, browser interaction adaptations, multi-platform build tasks, and file-icon code generation.

## WebAssembly Platform Constraints & Stubs

- **Avatar fetching stubbed**: `fetchAvatar` returns empty bytes on `GOOS=js` builds to avoid linking `net/http` and `crypto/tls`, which would increase `lime.wasm` by several megabytes (`cmd/lime/avatar_js.go`).
- **In-memory Git delegation**: On `GOOS=js`, `gitWith` delegates to `globalThis.limeGit` backed by an in-memory repository because browsers cannot spawn external `git` processes (`cmd/lime/git_js.go`).
- **Branch and remote commands hidden**: `realGit` suppresses command palette captions for branch and remote commands in `GOOS=js` builds because `website/git.js` does not implement branches or remotes (`cmd/lime/git.go`).
- **Worktree discard omitted**: The Changes sidebar omits the Discard menu item on `GOOS=js` because the website's Git shim does not implement worktree restore and browsers have no OS Trash (`cmd/lime/changes.go`).
- **Hunk discard omitted**: Commit and diff views omit the Discard Hunk button when `runtime.GOOS == "js"` because `website/git.js` only applies patches to the index, not the working tree (`cmd/lime/commitview.go`).
- **No tab tear-off**: On `runtime.GOOS == "js"`, dropping a tab outside any tab bar leaves it in place instead of spawning a new window because browsers lack global screen cursor coordinates and multi-window support (`cmd/lime/tabdrag.go`).

## Demo Bundle & In-Memory Filesystem (`limeFS`)

- **Curated file subset**: `demoFiles` bundles 12 repository files at `HEAD` to showcase Lime's features while keeping the demo payload small and fast to load (`website/build.go`).
- **Deduplicated `demo.json` format**: Commits are serialized newest-first with per-commit file trees referencing deduplicated file contents in `blobs` by integer index; `parent` stores the tree immediately before the oldest commit so its diff can be computed (`website/build.go`).
- **Git batch trailing newline**: `tree` reads `size+1` bytes per object from `git cat-file --batch` because Git's batch protocol appends a trailing newline delimiter after each object payload (`website/build.go`).
- **Node-compatible `fs` shim**: `globalThis.fs` (populated via `limeFS`) provides an in-memory Node `fs` implementation because Go's `GOOS=js` `syscall` package expects Node's `fs` API in browsers (`website/fs.js`).

## In-Memory Git Shim (`limeGit`)

- **Minimal command surface**: `limeGit` answers commands from `cmd/lime/git_js.go` using the demo's commits, an in-memory index, and the work tree in `limeFS`, implementing only the exact subcommands, flags, and output formats Lime invokes (`website/git.js`).
- **Synthetic object IDs**: `oid` computes deterministic 40-hex-character IDs via a fast 5-round FNV-1a loop rather than SHA-1 because the demo only needs stable content-derived IDs for Git output formatting (`website/git.js`).
- **EOF newline diffing**: Before running Myers diff, `hunks` appends a `\0` sentinel to any final line lacking a trailing newline so adding or removing an EOF newline produces a diff change and emits `\ No newline at end of file` (`website/git.js`).
- **Hunk count recounting**: `apply` updates index file text in forward or `--reverse` mode while ignoring line-count fields in `@@` hunk headers, because Lime invokes `git apply --recount` with partial-hunk patches whose header counts may not match the hunk body (`website/git.js`).

## Browser Input, Viewport, Theme & Initial State

- **Pre-selected recurring word**: Initial demo tabs open with `"command"` (located in `"type command struct"` in `cmd/lime/commands.go`) pre-selected so visitors clicking Quick Add Next (`Ctrl+D`) immediately see multi-cursor selection (`website/app.html`).
- **Synthetic demo keystrokes**: `limePress` dispatches single strokes and chords (such as `"Ctrl+Shift+P"` or `"Ctrl+K Ctrl+B"`) in the browser build's Linux keymap, mapping `Ctrl` to `metaKey` on macOS and `ctrlKey` elsewhere because MyGo's web backend maps macOS Command to Control (`website/app.html`).
- **Platform keycap formatting**: Landing-page shortcut badges render the demo's Linux-keymap `Ctrl` as `⌘` on macOS to match the browser build's Command-to-Control mapping (`website/index.html`).
- **Touch scrolling passthrough**: `#mygo canvas` sets `touch-action: pan-y` under `@media (pointer: coarse)` so vertical finger swipes scroll the landing page instead of being swallowed by Lime, which has no touch scrolling (`website/app.html`).
- **Parent-driven theme sync**: When embedded in the landing page, a `MutationObserver` mirrors the parent document's `data-theme` attribute onto `document.documentElement.dataset.mygoTheme`; when opened standalone, `mygoTheme` stays unset so Lime follows the browser theme (`website/app.html`).
- **Scaled desktop viewport on narrow screens**: Below `860px` container width, the demo iframe stays at a fixed `960x620` desktop window size and CSS-scales down to fit instead of crushing desktop editor chrome at mobile widths (`website/index.html`).

## Build Tooling

- **Single multi-platform `mygo` build**: `build:all` passes all six desktop targets (`darwin`, `windows`, and `linux` across `arm64` and `amd64`) to one `go tool mygo build` invocation because parallel Windows builds would race on the `.syso` resource files `mygo` writes into `cmd/lime` (`mise.toml`).

## File Icon Code Generation

- **`file-icons/atom` pipeline**: `fileicons/update.sh` fetches `file-icons/atom` (MIT) and runs `go run fileicons/gen.go <icondb.json> <icons.less> <octicons.css>` to generate `icons_gen.go` containing compiled rules, glyphs, and colors (`cmd/lime/fileicons/gen.go`).
- **Atom Octicons parsing**: `gen.go` parses `octicons.css` alongside `icons.less` because `file-icons/atom` rules reference Atom's bundled Octicons, which are not defined in `icons.less` (`cmd/lime/fileicons/gen.go`).
- **Guaranteed fallback glyph**: `text-icon` is pre-seeded in the generated glyph set even if no rule uses it because runtime icon matching in `icons.go` falls back to `text` when no rule matches (`cmd/lime/fileicons/gen.go`).
- **RE2 regex overrides**: Upstream JavaScript icon regexes containing backreferences or lookarounds unsupported by Go RE2 are manually rewritten in `overrides`, assuming forward-slash path separators (matching Git paths and `filepath.ToSlash` normalization in `icons.go`) and expanding or relaxing lookarounds (`cmd/lime/fileicons/gen.go`).
- **Pierre Icons pipeline**: `update-pierre.sh` fetches Pierre Icons (`pierrecomputer/vscode-icons`, MIT) and runs `go run fileicons/gen_pierre.go <pierre.json> <svgs-dir> <out-svg-dir>` to emit `pierre_gen.go` and copy only the used SVGs (`cmd/lime/fileicons/gen_pierre.go`).
- **Dotless uppercase filenames**: Dotless all-uppercase entries in Pierre's `fileExtensions` (such as `LICENSE`) are also registered under `fileNames` to work around upstream's classification of standard dotless filenames as extensions (`cmd/lime/fileicons/gen_pierre.go`).

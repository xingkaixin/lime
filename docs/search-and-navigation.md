# Search & Navigation

Design decisions for Lime's Find/Replace bar, Find and Replace in Files, Quick Panel and Goto Anything, symbol extraction and Goto Definition, and Jump History.

## Find & Replace Bar

- **Window-scoped find state**: `finding` persists the last-used query, replacement string, and search options across editors in a window so opening the Find panel in another tab starts with those settings, matching Sublime Text (`cmd/lime/find.go`).
- **Single-line query seeding**: `showFind` seeds the find query from the current selection only when that selection spans a single line, otherwise keeping the previous query, matching Sublime Text (`cmd/lime/find.go`).
- **Multiple-selection query sampling**: When multiple selections exist, `selectionQuery` reads only the newest selection and ignores multiline selections, matching Sublime Text's find-from-selection semantics (`cmd/lime/find.go`).
- **Quick Find updates shared query**: Selection-based find commands such as Quick Add Next (`quickFind`) update the window's shared find query to the newest resulting selection so subsequent Find operations reuse it, matching Sublime Text (`cmd/lime/find.go`).
- **Skip next occurrence**: `skipNext` implements Sublime Text's `find_under_expand_skip`, advancing to the next match while dropping the previous newest selection and keeping all earlier selections (`cmd/lime/find.go`).
- **Independent inactive panels**: An open find panel in an inactive group or inactive diff side does not sync with the window's shared find state, keeping its own query and options independently (`cmd/lime/find.go`).
- **Typed-query guard**: Only edits typed directly into the find panel replace the window's shared query (detected by comparing against `s.seen`), preventing an open panel from overwriting a query updated externally by commands like Quick Add Next (`cmd/lime/find.go`).

## Find & Replace in Files

- **Sublime Text `Where` tokens**: The `Where` field uses Sublime Text's exact special scope tokens (`<open folders>` and `<open files>`) for compatibility (`cmd/lime/findinfiles.go`).
- **In-process `.gitignore` stack**: `fifIgnore` evaluates `.gitignore` rules via a stack of per-directory layers, requiring depth-first traversal (`filepath.WalkDir`) so layers can be pruned as directories exit (`cmd/lime/findinfiles.go`).
- **Negated character classes in `.gitignore`**: Gitignore negated character classes (`[!...]`) are translated to Go `path.Match` syntax (`[^...]`) (`cmd/lime/findinfiles.go`).
- **Escaped separator after globstar**: Globstar matching preserves Git's rule that an escaped separator after `**` (`**\/`) requires at least one directory segment, unlike unescaped `**/` (`cmd/lime/findinfiles.go`).
- **In-memory snapshots and background execution**: `searchFiles` searches open tabs' in-memory snapshots rather than their on-disk files so unsaved edits are included; it blocks and must run off the main UI thread (`cmd/lime/findinfiles.go`).
- **Symlink deduplication**: Search deduplicates files and maps them to open tabs by canonical `realPath` so symlinks (such as `CLAUDE.md` -> `AGENTS.md`) are searched once using the open tab's unsaved text (`cmd/lime/findinfiles.go`).
- **Regular files only**: `regular` restricts disk reads to regular files and symlinks resolving to regular files because opening a FIFO/named pipe would block a search worker indefinitely (`cmd/lime/findinfiles.go`).
- **Tab-equivalent disk decoding**: `readSearched` decodes disk files with fallback encoding and normalizes `\r\n` and `\r` to `\n` to match how editor tabs load them, while skipping binary files and files larger than `largeFile` (16 MiB) (`cmd/lime/findinfiles.go`).
- **Result line truncation**: Matched lines in Find Results are truncated at `maxResultLine` (500 bytes) so minified single-line files do not flood the results buffer (`cmd/lime/findinfiles.go`).
- **Uncapped Replace in Files with confirmation**: Matching Sublime Text, `replaceInFiles` searches without the `maxResults` cap (`math.MaxInt`) and prompts for confirmation with match and file counts before applying replacements (`cmd/lime/findinfiles.go`).
- **In-memory replacement without auto-save**: Matching Sublime Text, `replaceFound` opens matched files that are not yet open in tabs and modifies buffers in memory without auto-saving so the user can review or undo before saving (`cmd/lime/findinfiles.go`).
- **Symlink-safe tab reuse on replace**: `tabOf` matches open tabs by canonical `realPath` in addition to literal path so Replace in Files never opens a second tab for a symlink whose Save All would overwrite the first tab (`cmd/lime/findinfiles.go`).
- **Single-step undo and regex captures**: `matcher.replace` batches all edits per buffer into one `Buffer.Edit` call so each file's replacements undo as a single step, and expands regex `$1` and `${name}` captures consistently with the Find panel (`cmd/lime/findinfiles.go`).

## Quick Panel & Fuzzy Ranking

- **Locale-independent command search**: Quick panel command items store their English caption in `alias` so Command Palette queries match English command names regardless of the active UI locale (`cmd/lime/panel.go`).
- **Live-preview caret restoration**: Panels with live-preview selection (such as Goto Symbol) save the current tab's selections and scroll position in `peek` before moving the caret so closing the panel restores the prior view (`cmd/lime/panel.go`).
- **Selection stability across background refreshes**: In `panel.refresh()`, typing a new query resets selection to index `0` and triggers preview, whereas re-listing for the same query (such as when background folder indexing finishes) preserves the selected row by `(label, detail)` (`cmd/lime/panel.go`).
- **Sublime-style fuzzy scoring**: `fuzzy` performs ordered subsequence matching with score bonuses for contiguous runs and word boundaries, treating spaces in the query as wildcards/separators, matching Sublime Text (`cmd/lime/panel.go`).
- **Additive filename bonus**: Filename matches receive an additive `nameMatchBonus` (`+1,000,000`) rather than a multiplicative boost so filename matches always outrank path matches even when long filenames yield negative fuzzy scores (`cmd/lime/panel.go`).
- **Stable top-K heap comparison**: In `rankTop`'s min-heap loop, input indices strictly increase (`i > top[0].idx` always holds), so an equal score can never beat `top[0]` under stable tie-breaking and checking `s > top[0].score` suffices (`cmd/lime/panel.go`).
- **Compare panel browse-item ordering**: In the Compare quick panel, "Browse Files" is placed first when the query is empty but appended after ranked matches once text is typed so pressing Enter selects the best file or tab match instead of opening the system file dialog (`cmd/lime/compare.go`).

## Goto Anything & Goto Symbol

- **Unified Goto Anything syntax**: `showGoto` follows Sublime Text's unified query syntax across open and project files, supporting `:line[:col]` and `@symbol` either standalone for the current tab or suffixed to a filename query (`cmd/lime/panel.go`).
- **Non-numeric colon preservation**: `parseGotoLoc` splits only trailing numeric `:line` or `:line:col` suffixes (or a bare trailing `:`) so filenames containing non-numeric colons (such as `foo:bar`) stay intact (`cmd/lime/panel.go`).
- **Target resolution and preview scope**: `symbolList` lists symbols from the current tab when no filename prefix is given, or from the top-ranked Goto Anything file otherwise (loading closed files in the background), and enables live caret preview only when targeting the current tab (`cmd/lime/panel.go`).
- **CRLF-to-LF symbol alignment**: Goto Symbol positions parsed from an unopened CRLF file on disk must align accurately after the opened tab normalizes line endings to LF (`cmd/lime/main_test.go`).
- **Project indexing cap**: Goto Anything file indexing is capped at `maxIndexed` (50,000) files to prevent runaway traversal and memory growth when very large directories (such as a home directory) are opened (`cmd/lime/project.go`).

## Symbol Extraction & Goto Definition

- **Cross-language defining keywords**: Symbol extraction uses a cross-language `definers` keyword set because MyGo's tokenizers classify keywords but do not tag the symbol names those keywords define (`cmd/lime/definition.go`).
- **Multi-language extraction rules**: `symbols` collects definitions from identifiers after defining keywords, function tokens preceded by a type or identifier token (`TokenType` or `TokenVariable`, covering C-style declarations like `int f(`), and Markdown headings (`cmd/lime/definition.go`).
- **Receivers and qualified symbol paths**: `definedAfter` skips a parenthesized prefix (for Go method receivers) and takes the final segment of dot- or colon-qualified paths (for definitions such as Lua's `function M.f` or Ruby's `def self.f`) (`cmd/lime/definition.go`).
- **Go grouped declaration scanning**: Go grouped `type` and `const` blocks are tokenized with `go/scanner` instead of line-by-line scanning because Go's automatic semicolon insertion prevents continuation lines of multiline expressions from being mistaken for new declarations (`cmd/lime/definition.go`).
- **ATX-only Markdown headings**: Markdown symbol extraction recognizes only ATX (`#`) headings and excludes Setext headings because Setext underlines (`---`) also close YAML front matter and would produce false headings (`cmd/lime/definition.go`).
- **Open-tab Goto Definition**: `gotoDefinition` follows Sublime Text's `goto_definition` behavior but intentionally resolves symbols from the current window's open tabs rather than a project-wide index, preferring the current tab before other open tabs and opening a quick panel on multiple matches (`cmd/lime/definition.go`).

## Jump History

- **Per-window jump criteria and threshold**: `jumps` maintains a per-window history modeled after Zed, recording the prior caret position on tab switches or single moves of at least `jumpLines` (10) lines (`cmd/lime/jump.go`).
- **Settled-frame caret tracking**: `jumps.last` stores the caret observed on the previous settled frame rather than the most recently recorded jump entry (`cmd/lime/jump.go`).
- **Frame-based sampling with transient suppression**: `trackJumps` samples the caret per frame to capture all movement sources uniformly, while pausing whenever a panel, `Ctrl+Tab` cycle, or unfocused editor is active so transient preview, incremental search, and tab-cycle positions are ignored and only the final resting position is compared against `jumps.last` (`cmd/lime/jump.go`).
- **Stack navigation and near-spot skipping**: `jump` implements Sublime Text's `jump_back` and `jump_forward` stack semantics while skipping entries within `jumpLines` of the current caret, preventing no-op jumps when closing a tab leaves adjacent history entries close together (`cmd/lime/jump.go`).

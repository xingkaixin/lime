# Editor & Input

Lime wraps MyGo's editor input, completion, and command pipelines to match Sublime Text's mousemaps, key chords, completions, and text transforms while preserving deliberate Zed-compatible defaults.

## Mouse Gestures & Navigation

- **Sublime Text mousemap remapping**: MyGo's editor follows Zed's mouse modifier conventions, so `mouseGesture` remaps pointer presses to Sublime Text's per-platform mousemaps (`cmd/lime/input.go`). On macOS, `Cmd+click` adds a caret (rewritten to `Alt`), `Cmd+Shift+click` subtracts a selection, and middle-click or `Alt+click` / `Alt+Cmd+click` starts column selection; on Windows and Linux, `Ctrl+click` adds a caret, `Alt+click` subtracts a selection, and `Ctrl+Alt+Shift+click`, `Shift+right-click`, or (on Windows) middle-click starts column selection.
- **Selection subtraction**: `subtract` implements Sublime Text's `Cmd+Shift+click` (macOS) and `Alt+click` (non-macOS) by dispatching a plain click to resolve the target offset, removing any existing selection containing that offset, and leaving a single caret at the click position when it was the only selection (`cmd/lime/input.go`).
- **Mouse side buttons**: `KeyBack` and `KeyForward` bind to `jump_back` and `jump_forward` (following Zed) rather than Sublime Text's tab switching (`cmd/lime/view.go`).

## Key Chords & Menu Interception

- **Window-scope chord precedence**: `shortcuts` registers window-level bindings (tabs, groups, panels) and `Cmd+K` chords at the window scope before editor bindings so active chords take precedence over editor commands and suppress non-chord window shortcuts (`cmd/lime/view.go`).
- **Canceling unrecognized chord keys**: The editor input wrapper (`app.input`) remaps mouse events and, while `a.chord` is active, cancels the chord and consumes `InputKeyDown` for any unrecognized second key so it does not type into the buffer (`cmd/lime/input.go`).
- **Standalone modifier presses in chords**: Modifier-only keydowns (such as pressing `Cmd` again for `Cmd+K Cmd+U`) arrive with `Key == KeyUnknown` and `PhysicalKey == KeyUnknown`, so they do not cancel an active chord (`cmd/lime/input.go`).
- **Suppressing rerouted presses until key-up**: After an invalid second key cancels a chord, `a.chordKeyUp` records that key and `shortcuts` keeps suppressing regular window shortcuts until `c.KeyDown(a.chordKeyUp)` is false, because focus changes can rebuild and reroute the still-held press before key-up (`cmd/lime/view.go`).
- **Disabling IME during chords**: `d.st.NoInputMethod` is set to `a.chord` while awaiting the second key so input methods (notably GTK's) do not intercept unmodified keys like `V` in `Cmd+K V` and commit text without sending a key event, which would leave the chord waiting (`cmd/lime/view.go`).
- **Swallowing chord character text**: `d.st.OnInput` swallows `InputText` events while `a.chord` is active because the second key's character text arrives before the frame that executes the window shortcut and must not be inserted into the buffer (`cmd/lime/view.go`).
- **Chord event ordering in tests**: Because a real OS window delivers the second key's text before the frame that runs the chord (whereas `ui.NewTester` runs a frame per call), chord tests send `tt.Type` before `tt.Key` to verify text suppression (`cmd/lime/mdpreview_test.go`).
- **Pausing menu accelerators**: Native OS menu bars intercept accelerators before the window receives key events, so `pauseMenu` strips menu accelerators while waiting for a chord's second key (including until `a.chordKeyUp` releases) or recording a custom shortcut (`cmd/lime/menu.go`).
- **Clearing MyGo default role shortcuts**: MyGo assigns default role shortcuts (such as Close on Windows) when constructing a menu, so `clearMenuKeys` strips accelerators from built menu items whenever menu shortcuts are paused (`cmd/lime/menu.go`).

## Word Completions

- **Cross-tab word aggregation**: `complete` serves words from all open text tabs in the window with the current tab's words first, matching Sublime Text's `auto_complete` (`cmd/lime/completion.go`).
- **Buffer size limit**: `completeSizeLimit` (4 MiB) matches Sublime Text's `auto_complete_size_limit`; buffers larger than 4 MiB skip word scanning and do not auto-complete on typing (`cmd/lime/completion.go`).
- **Synchronous vs. background scanning**: Buffers up to `syncScanLimit` (128 KiB) are scanned synchronously (~1 ms) on demand; larger buffers scan in a background goroutine while serving the last cached `wordList` to keep typing responsive (`cmd/lime/completion.go`).
- **Re-evaluating pending requests after async scans**: When a background scan completes, `wordList` re-evaluates any pending completion request recorded in `d.asked` (if the tab, buffer version, and cursor offset are unchanged) so the popup updates with the newly scanned words (`cmd/lime/completion.go`).
- **Excluded tokens**: `scanWords` excludes single-character words (no completion value) and digit-leading tokens so typing numbers does not trigger completions that hijack `Enter` on newline (`cmd/lime/completion.go`).
- **Subsequence prefix filter**: `matchesPrefix` uses case-insensitive ordered subsequence matching to stay consistent with MyGo `editor.State`'s internal completion filter (`cmd/lime/completion.go`).
- **Code-only auto-completion**: `completesTyping` enables auto-completion on typing only in recognized code languages (excluding plain text and Markdown, and buffers above `completeSizeLimit`), matching Sublime Text's `auto_complete_selector` rule of completing in code but not prose (`cmd/lime/completion.go`).
- **Multi-cursor prefix uniformity**: `sameWords` suppresses auto-completion on typing when multiple carets have differing word prefixes, because MyGo's editor replaces the word only at carets matching the primary prefix and appends the completion beside differing words (`cmd/lime/completion.go`).
- **Dismissing exact-word matches**: Non-explicit auto-completion dismisses the popup when the typed prefix already matches an existing word, preventing that exact word from being selected first and hijacking `Enter` instead of inserting a newline (`cmd/lime/completion.go`).
- **Pre-insertion popup dismissal**: Because MyGo's editor narrows an open completion popup client-side on text input without re-querying `OnCompletion`, `typing` hooks `OnInput` before insertion to close the popup whenever the next typed character would form an existing exact word, letting `complete` decide whether completions should show (`cmd/lime/completion.go`).

## Case Conversion & Blank-Line Deletion

- **Routing case conversions through Lime**: `convertCase` leaves `command.ed` unset so key bindings route through Lime's window keymap and `caseSpans` preprocessing instead of directly to MyGo's editor command; in diff tabs, it falls back to `DiffState.Run` because MyGo's `DiffState` does not expose selections (`cmd/lime/commands.go`).
- **Case-conversion selection preprocessing**: `caseSpans` prepares selections to match Sublime Text's CaseConversion package: empty carets expand to their enclosing word, `trim` strips outer whitespace for identifier conversions (snake, kebab, camel, pascal), and overlapping spans merge via `SetSelections` so each word converts once; carets outside words and whitespace-only selections stay excluded so MyGo does not absorb and drop adjacent whitespace or punctuation (`cmd/lime/commands.go`).
- **Merged-span restore bailout**: After conversion, `caseSpans` skips shifting and restoring excluded `rest` selections if `len(after) != len(before)`, because a span converted to empty text can merge with an adjacent touching selection and break the 1-to-1 index correspondence (`cmd/lime/commands.go`).
- **Swap case selection retention**: `swapCase` matches Sublime Text's `swap_case` behavior and leaves swapped spans selected for consistency with MyGo editor's `upper_case` command (`cmd/lime/commands.go`).
- **Delete blank lines scope and surplus mode**: `deleteBlankLines` matches Sublime Text's DeleteBlankLines package: a single empty caret deletes whitespace-only lines across the entire buffer, selections operate on their spanned lines, and `surplus` keeps the first blank line of each consecutive run (`cmd/lime/commands.go`).
- **Preserving the trailing newline**: On a blank final line, `deleteBlankLines` deletes only `[LineStart, LineEnd]` rather than the preceding line break so the file keeps its trailing newline (`cmd/lime/commands.go`).

## JSON Formatting

- **Atomic formatting and minification**: `formatJSON` indents (or compacts with `minify`) each non-empty selection, or the whole buffer when nothing is selected, atomically: if any selection fails to parse, no buffer edits are applied, the caret moves to the failure offset, and the status bar reports the error (`cmd/lime/commands.go`).
- **Preserving surrounding whitespace**: `formatJSON` trims leading and trailing whitespace before formatting and replaces only the inner span so surrounding whitespace such as a file's trailing newline stays intact (`cmd/lime/commands.go`).
- **Extracting scanner error tokens**: `jsonError` parses Go `encoding/json`'s scanner error format (`"invalid character '...' ..."`) to extract the offending token for localized `StatusInvalidJSON` status bar errors, falling back to `StatusIncompleteJSON` for other syntax errors (`cmd/lime/commands.go`).
- **Matching editor indentation units**: `indentUnit` mirrors MyGo editor's unexported `hardTabs`/`tabSize` resolution via public `editor.State` and `Language` fields (defaulting to 4 spaces) so JSON formatting uses the same indentation unit as the editor (`cmd/lime/commands.go`).

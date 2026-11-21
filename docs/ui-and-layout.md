# UI, Chrome & Layout

Design decisions and invariants governing Lime's window chrome, pane layouts, tabs, sidebar, dialogs, specialized views, theming, and custom editor overlays.

## Window Chrome, Title Bar & Menus

- **Window bounds persistence**: `makeWindow` assigns MyGo's persistent `StateKey("main")` to the primary window (`wins[0]`) to restore its last bounds, while additional windows restore explicit saved bounds `r` or default dimensions (`cmd/lime/main.go`).
- **Integrated title band**: `titleBand` defines the shared top chrome height (`max(TitleBar.Height, tabBarHeight)`, where `tabBarHeight` is `34`) across the sidebar and top-row tab bars when window controls are inset; it returns `0` when the window uses a native title bar or is in full screen (`cmd/lime/view.go`).
- **Sidebar minimum width for window controls**: `SideBarWidth` is clamped to at least `c.TitleBar().Left + controlsGap` (`controlsGap = 12`) so integrated window controls (e.g., macOS traffic lights) sitting over the sidebar top never clip or overlap the sidebar boundary (`cmd/lime/view.go`).
- **Window-controls spacer without sidebar**: When the sidebar is hidden, the window-controls spacer is placed as a sibling outside the first group's tab bar container so it drags the window without skewing `g.bar` bounds measured during tab dragging (`cmd/lime/view.go`).
- **Trailing tab-bar space**: Empty space after tabs acts as a draggable title bar on top-row groups with an integrated title band (`band > 0`); elsewhere, double-clicking empty tab-bar space opens a new tab, matching Sublime Text (`cmd/lime/view.go`).
- **Locale-driven menu rebuilds**: Native menu labels are evaluated eagerly as static strings at creation time, so `OnLocaleChanged` rebuilds the entire menu via `refreshMenu` (`cmd/lime/main.go`).
- **Windowless menu invocation**: When no windows are open (as on macOS after closing the last window), invoking a menu command first reopens windows via `l.reopen()`; `new_window` returns early so it does not create a duplicate window (`cmd/lime/menu.go`).
- **Custom Edit menu items**: Providing a custom `Submenu` on MyGo's `RoleEditMenu` replaces its default items rather than appending, so Lime explicitly re-lists Undo, Redo, Cut, Copy, Paste, Delete, and Select All (plus macOS Speech and Paste and Match Style) before Convert Case (`cmd/lime/menu.go`).
- **Platform Settings menu placement**: Settings goes in the Application menu on macOS and in the File menu on Windows and Linux (`cmd/lime/menu.go`).

## Pane Layouts, Dividers & Focus Management

- **Uniform grid layouts**: Window pane layouts follow Sublime Text's fixed uniform `cols×rows` grids rather than an arbitrary binary split tree (`cmd/lime/app.go`).
- **Group chrome styles**: Groups support two layout styles: `compact` (groups meet at hairlines) and `comfortable` (groups rendered as spaced cards) (`cmd/lime/app.go`).
- **Shared column dividers**: All rows share the single `a.cols` divider slice so dragging a column divider resizes that column uniformly across every row, matching Sublime Text's grid (`cmd/lime/view.go`).
- **Equal-pane split ratios**: Because nested binary splits express each ratio relative to the remaining panes, `even` places dividers at `100/(n-i)%` at step `i` rather than `100/n%` (`cmd/lime/view.go`).
- **Empty-group placeholder editor**: Empty groups render an untabbed placeholder editor (with folding disabled) matching Sublime Text; typing into it promotes the buffer into a real tab via `adopt()` (`cmd/lime/app.go`).
- **Untouched untitled tab replacement**: Opening a file replaces the current tab in place when it is an empty, untouched untitled text tab instead of leaving an unused blank tab open (`cmd/lime/app.go`).
- **Cascading view closure**: Closing a source doc closes every diff tab and Markdown preview tab referencing it across all windows (`closeViews`) so no diff tab edits an orphaned buffer and no preview tracks a closed doc (`cmd/lime/app.go`).
- **Focus identity by place**: Keyboard focus is keyed by `place{group, doc}` rather than `*doc` alone so moving a doc into another group produces a new `place` value and causes MyGo's `FocusBind` to re-acquire keyboard focus in the new pane (`cmd/lime/app.go`).
- **Clearing focus on non-editor tabs**: Only editor-backed tabs (`textDoc` or `findDoc`) bind focus. When the active doc has no editor (e.g., a binary preview), `app.focused` is cleared to `place{}` because leaving an unbound `place` in `FocusBind` causes MyGo to wait indefinitely for that element and leave clicks into other groups unfollowed (`cmd/lime/app.go`, `cmd/lime/main_test.go`).
- **Layout-change focus recovery**: Changing layouts rebuilds the editor inside a new split subtree while `a.focused` stays unchanged, so `FocusBind` alone does not re-request focus; `e.AutoFocus()` is called when the editor matches `a.focused` (`cmd/lime/view.go`).
- **One-frame focus mount delay**: A newly shown editor receives keyboard focus on the frame after it mounts, requiring two frames before focus-dependent logic (such as jump tracking) observes it as focused (`cmd/lime/main_test.go`).
- **DiffView group focus**: MyGo's `editor.DiffView` owns its internal editors outside `FocusBind`'s reach, so `diffView` detects focus via `col.FocusWithin()` on the enclosing column to activate the tab's group on click (`cmd/lime/compare.go`).
- **Pointer identity keys**: `key` converts struct pointers to address strings via `fmt.Sprintf("%p", p)` for `c.Key`, because MyGo keys pointers by formatting their pointed-to value, which changes as struct fields mutate and would break element identity (`cmd/lime/view.go`).

## Tab Bar, Overflow Math & Tab Dragging

- **Tab basename disambiguation**: `tabDirs` disambiguates open tabs sharing a basename by showing the shortest distinguishing trailing parent directory path, matching Zed and VS Code (`cmd/lime/app.go`).
- **MRU tab cycling**: `cycleTabs` implements Sublime Text's `next_view_in_stack` for `Ctrl+Tab`, snapshotting the MRU tab order when cycling begins and walking it without reordering MRU until `Ctrl` is released (`cmd/lime/app.go`).
- **Comfortable tab bar indent**: In `comfortable` layout, the tab bar indents by `cardRadius` (`8`) so the active tab aligns past the rounded top-left corner of the editor card below instead of leaving a visual notch (`cmd/lime/view.go`).
- **Non-stealing tab clicks**: `KeepFocus()` is applied to the tab bar so clicking tabs switches documents without stealing keyboard focus from the editor, matching Sublime Text (`cmd/lime/view.go`).
- **Instant tab snap on resize**: Animations are disabled when the tab bar width changes (e.g., dragging a split divider or resizing the window) so tabs snap immediately instead of trailing behind via `tabMotion` (`cmd/lime/view.go`).
- **Snapshot iteration during tab build**: `g.docs` is cloned before iterating to build tabs because a tab's immediate-mode context menu can close tabs and mutate `g.docs` mid-loop (`cmd/lime/view.go`).
- **Instant active-tab colors**: Color transitions are disabled on the active tab (`motion.Colors = !active`) so switching tabs snaps active-tab colors immediately with the editor while inactive tabs still fade on hover (`cmd/lime/view.go`).
- **O(1) overflow width**: Because flex-shrink guarantees every tab has shrunk to `tabMinW` (`92`) whenever tabs overflow, overflow width is computed in O(1) as `len(g.docs)*tabMinW - room` (`cmd/lime/view.go`).
- **Conditional overflow clipping**: `ClipX` and wheel scrolling apply only when tabs overflow (`over > 0`), keeping the new-tab button visible; omitting `ClipX` when tabs fit prevents cutting off tabs still sliding into place after a tab close shrinks the row immediately (`cmd/lime/view.go`).
- **Event-driven auto-scroll**: `scrollTabs` auto-scrolls the tab bar only when the active document changes or its index shifts (e.g., closing earlier tabs), plus edge-scrolling during drag (`dragScroll`), so manual wheel scrolling is not overwritten on every frame (`cmd/lime/view.go`).
- **First-frame layout guard**: Initial active-tab scroll-into-view is guarded by `room > 0 || d == nil` because the tab bar has zero `LayoutBounds` width on its first frame; updating `g.shown` or `g.at` early would skip scrolling once layout completes (`cmd/lime/view.go`).
- **Dual-axis wheel scrolling**: `wheel` scrolls overflowing tabs from whichever of `DX` or `DY` has larger magnitude so both vertical mouse wheels and horizontal touchpads scroll horizontally, matching Sublime Text and Zed (`cmd/lime/view.go`).
- **Sublime-style tab dragging**: Dragging a tab along a bar slides sibling tabs aside; pulling it out moves it to another group/window or drops it into a new window; dragging a window's sole tab moves the window itself and docks it when dropped onto another window's tab bar (`cmd/lime/tabdrag.go`).
- **Dragged tab slot reservation**: A dragged tab remains in its tab-bar slot with `Opacity(0)` rather than being removed, preserving the gap while siblings slide around it and `draggedTab` paints the floating tab under the pointer (`cmd/lime/view.go`).
- **Deferred drop execution**: `dropTab` is deferred via `a.win.Update` because `dragTab` runs mid-frame while iterating tabs, so mutating groups or opening/focusing windows must wait until the current frame finishes (`cmd/lime/tabdrag.go`).
- **Suppressing auto-scroll during drag reorder**: `makeWay` updates `g.at = g.active` after reordering so `scrollTabs` does not auto-scroll when the active tab's index shifts mid-drag, leaving `dragScroll` at bar ends as the sole scroll trigger during drag (`cmd/lime/tabdrag.go`).
- **Stationary edge-scroll invalidation**: `dragScroll` calls `c.Invalidate()` explicitly because holding the pointer stationary at a tab bar edge produces no OS pointer events and must self-invalidate to drive continuous scrolling (`cmd/lime/tabdrag.go`).

## Status Bar & Notices

- **Narrow-window encoding shrink**: The encoding button (the longest status bar item) uses `Shrink(1)` and `MinWidth(0)` so it yields width first in narrow windows instead of pushing out shorter items (`cmd/lime/encoding.go`).
- **Notice timer generation guard**: Notices track a monotonically increasing generation counter (`notice.n`) so an earlier notice's `time.AfterFunc` callback cannot clear a newer notice that replaced it (`cmd/lime/view.go`).

## Sidebar, File Tree & File Icons

- **Zed-style panel toggling**: `togglePanel` hides the sidebar when clicking the currently active panel and switches directly when clicking another panel, matching Zed's status bar panel buttons (`cmd/lime/sidebar.go`).
- **Persisted sidebar visibility**: `showPanel` persists `settings.SideBar` when toggling visibility so newly opened windows inherit the current sidebar state (`cmd/lime/sidebar.go`).
- **Sidebar top title row**: `sideBarTitle` sits flush at the window's top level with the tab bars after the OS window controls, showing repository worktree and branch switchers modeled on Zed's title bar (`cmd/lime/sidebar.go`).
- **Worktree vs. branch truncation**: In the sidebar title row, the worktree button sets `Shrink(0).MaxWidth(120)` so the branch button truncates first, because worktree names are usually short (most often `main`) (`cmd/lime/sidebar.go`).
- **Tree nesting indentation**: Each nesting level indents by `treeIcon + treeGap` (`16 + 6`) so child icons align under their parent's label, matching Sublime Text's sidebar (`cmd/lime/sidebar.go`).
- **Distinct folder element keys**: Folder subtrees use three distinct MyGo UI keys to prevent element-identity collisions and keep expand/collapse animations independent: the folder row (`dir`), its children column (`dir/` with a trailing path separator), and the outer wrapper (`treeBox` named key type) (`cmd/lime/sidebar.go`).
- **Instant tree selection highlight**: Color transitions are disabled on selected tree rows (`motion.Colors = !selected`) because selection text switches to `AccentText` immediately and the background must snap in sync instead of lagging in a fade (`cmd/lime/sidebar.go`).
- **One-frame motion suppression on path replacement**: `project.jump` disables sidebar animations for one frame after switching projects or renaming files/folders because row paths are replaced rather than moved, avoiding misleading layout motion (`cmd/lime/project.go`).
- **Inline creation focus latch**: Inline file/folder creation latches `creation.focused` once the input receives focus so focus loss cancels creation without prematurely canceling on the first frame before initial focus arrives (`cmd/lime/project.go`).
- **Blank-space context menu**: Right-clicking blank space in the sidebar (`projectMenu`) creates new files and folders inside the project's first root folder, matching VS Code's explorer (`cmd/lime/contextmenu.go`).
- **Platform drag-to-copy modifier**: Holding `copyMods` on drop copies rather than moves: Option (`ui.Alt`) on macOS (matching Finder) and Ctrl (`ui.Ctrl`) on other platforms (matching Explorer) (`cmd/lime/sidebar.go`).
- **Source-driven drag cursor**: Because MyGo uses the cursor of the pressed drag-source element throughout a drag, `treeSource` sets `row.Cursor(ui.CursorCopy)` on the source row while `copyMods` are held (`cmd/lime/sidebar.go`).
- **Drop collision prompt**: Dropping a file or folder onto an existing name (`dropInto`) prompts with Replace, Keep Both, or Cancel (modeled on macOS Finder) instead of silently overwriting or failing (`cmd/lime/project.go`).
- **Changes view unbuilt sentinel**: `rowsGen == 0` is reserved as the unbuilt sentinel so a zero-initialized `changes` struct still triggers `layRows` when `dataGen` is `0` (`layRows` bumps `dataGen` to `1`) (`cmd/lime/changes.go`).
- **File icon source and sizing**: File icon fonts, rules, glyphs, and colors come from `github.com/file-icons/atom` and are generated into `icons_gen.go` by `fileicons/update.sh`; `fileIconGlyph.size` stores CSS pixels relative to a `16px` reference row (`scale = size/16`) (`cmd/lime/icons.go`).
- **Three-stage icon matching**: `matchFileIcon` mirrors file-icons' path strategy: match the full path first, then the filename with backup (`~orig`, `~previous`) and template (`.tmpl`, `.dist`, etc.) suffixes stripped, then fall back to the raw filename if no rule of priority `>= 1` matched (`cmd/lime/icons.go`).
- **Two-color icon selection fallback**: `rowIcon` keeps both a two-color SVG (`dual`) and a monochrome fallback (`svg`) so rows render two-color icons normally and fall back to monochrome tinting when overridden by a selection tint (`cmd/lime/icons.go`).
- **Icon palette and contrast adjustments**: Base hues are ported from `colours.less` (Base16) with light/medium/dark HSL lightness offsets (`colour()`), plus hardcoded contrast adjustments (`adjust`) that darken low-contrast icons on light themes and brighten them on dark themes (`cmd/lime/fileicons/gen.go`).

## Find Bar, Quick Panel & Dialogs

- **Bottom-anchored find bar**: MyGo's editor places its find bar in the top-right corner with non-zero offsets; Lime re-attaches it to the bottom-left at full width (Sublime Text layout) and explicitly sets `Top(0)` and `Right(0)` to clear the editor's defaults (`cmd/lime/find.go`).
- **Narrow-group find bar priority**: Match status and action buttons share a fixed slot width that shrinks down to `findSlotMin` (`48`) in narrow groups before the query input yields width (`cmd/lime/find.go`).
- **Scoped find bar transitions**: The find panel and replace row enable only `Colors` transitions by default so pane resizes and divider drags do not animate them while `Enter`/`Exit` slide and collapse animations still play; `Position` and `Size` transitions are enabled only during the `findMotionTime` (`200ms`) window after toggling the replace row so only height changes animate (`cmd/lime/find.go`).
- **Listless input panel mode**: The quick panel doubles as Sublime Text's listless input panel when `submit != nil`, rendering `prompt` above the input and passing the raw query to `submit` on Enter (`cmd/lime/panel.go`).
- **Deferred unpeek scroll restore**: Because MyGo's `SetSelections` automatically scrolls selections into view during the editor's build pass, `scrollBack()` runs after `groupsView` builds in `view.go` and restores the saved `ScrollY` only if selections have not moved again (e.g., from `pick()`) (`cmd/lime/panel.go`).
- **Async system font enumeration**: `showSettings` launches a detached `go ui.FontFamilies()` call because first-time system font listing is slow, priming MyGo's cache asynchronously to avoid stalling the main thread when opening the Fonts tab (`cmd/lime/settingsview.go`).
- **System-font slot-0 placeholder**: Cached `fontLists` slices reserve index `0` as an empty placeholder so `fontChoice` can substitute the localized system-font label (System Default vs. System Monospace) on render (`cmd/lime/settingsview.go`).
- **Shortcut recorder focus latch**: The shortcut recorder latches `focused` once `AutoFocus` grants focus so subsequent focus loss ends recording without immediately canceling on the initial frame before focus arrives (`cmd/lime/shortcutsview.go`).
- **Non-stealing recorder controls**: `smallButton` sets `.KeepFocus()` so clicking conflict-resolution or filter buttons does not steal keyboard focus from the active shortcut recorder and terminate recording (`cmd/lime/shortcutsview.go`).

## Diff Views, Commit View, Blame & Image Preview

- **Aligned side-by-side diff headers**: Both diff header panes use `Grow(1).Basis(0)` around a `1px` separator so they line up evenly with `editor.DiffView`'s equally split panes (`cmd/lime/compare.go`).
- **Consistent hunk action buttons**: The custom Revert hunk action (`revertAction`) mirrors the sizing and theme styling of MyGo's built-in `DiffView` hunk action buttons (`cmd/lime/compare.go`).
- **Virtualized multi-file commit view**: Commit tabs follow Zed's stacked multi-file commit view (reused without the commit header for staged/unstaged worktree changes) and flatten every diff line into a single virtualized `ui.List` (like `godiff`) so only visible rows are laid out regardless of diff size (`cmd/lime/commitview.go`).
- **Folded binary diffs**: Binary files in commit view start folded without diff text (showing only a binary note if expanded), matching Zed (`cmd/lime/commitview.go`).
- **Matching diff tints**: Commit view diff line and gutter tint mix factors (`0.12`/`0.09` light, `0.2`/`0.15` dark) match MyGo's built-in `DiffView` tints (`cmd/lime/commitview.go`).
- **Post-paint blame column**: The Toggle Blame column paints in `row.DrawOver` (after children paint) rather than inside the left box child because the editor updates its scroll position during its own paint pass, preventing 1-frame scroll lag (`cmd/lime/blame.go`).
- **Editor line-height default**: Blame label vertical centering uses `cmp.Or(st.LineHeight, 1.5)` to match MyGo `editor.State`'s default line-height multiplier (`1.5` when `0`) (`cmd/lime/blame.go`).
- **Folded-line blame skipping**: Lines hidden inside a MyGo fold report the same `LineTop` as the line after them, so checking `st.LineTop(line+1) == top` skips folded lines and labels the block by the visible line after the fold (matching click hit-testing in `lineAt`) (`cmd/lime/blame.go`).
- **Process-wide image decode redraw**: After asynchronous image decoding completes, `loadImage` triggers `a.l.redraw()` across all windows rather than only the originating window because the tab may have moved to another window while decoding (`cmd/lime/preview.go`).

## Markdown Preview

- **Live buffer tracking**: Markdown preview tabs follow Zed's model, rendering another tab's live buffer text (including unsaved edits) rather than only saved disk contents (`cmd/lime/mdpreview.go`).
- **Side preview workflow**: `openPreview` follows Zed's "open preview to the side" UX: side mode splits a single-group window into two columns first, keeps keyboard focus on the source tab, and focuses an existing preview of the same source in that group rather than opening duplicates (`cmd/lime/mdpreview.go`).
- **Debounced re-parse and frame re-arming**: Preview updates debounce by `200ms` (`previewPause`, matching Zed) so typing in long files does not reparse on every keystroke; because MyGo's `c.After(wait)` schedules a wakeup for one frame only, every intermediate frame during the pause re-arms `c.After(wait)` (`cmd/lime/mdpreview.go`).
- **Chunked virtualization for long files**: Because MyGo's `markdown.View` builds and lays out every block on every frame, documents longer than `16KB` (`previewWhole`) split into `ui.List` rows of roughly `4KB` (`previewPart`), also starting a new part at every top-level heading so anchor links can jump directly to a row's top. Once split, a preview stays split even as text shrinks to preserve scroll position; cross-part footnotes (rendered literally as `[^1]`) and per-part text selection are accepted trade-offs (`cmd/lime/mdpreview.go`).
- **Duplicate-safe part keys**: Chunked list rows are keyed by `partKey` (part text plus duplicate occurrence index) so `ui.List` preserves row identity across edits while distinguishing identical parts (`cmd/lime/mdpreview.go`).
- **Inter-block link reference extraction**: Link reference definitions are not emitted as AST blocks by `parser.Document` and sit in inter-block gaps alongside trailing block syntax (e.g., a table's last `|` or a heading's closing `#`s); they are extracted from gaps and prepended to every split part so reference links resolve across parts (`cmd/lime/mdpreview.go`).
- **Directory-scoped image cache**: MyGo caches loaded Markdown images per `markdown.Options` instance, so `previewOptions` reuses `p.opts` across frames and recreates it only when the source file's directory changes so relative images reload from the new folder (`cmd/lime/mdpreview.go`).
- **Explicit default theme spacing**: `p.theme` is initialized from `markdown.DefaultTheme(c.Theme())` rather than left zero because `previewTab` directly reads its `BlockSpacing`, `FontSize`, and `Headings` top margins to space split list rows consistently with a whole Markdown view (`cmd/lime/mdpreview.go`).
- **Link routing**: `followLink` matches Zed's preview: `http`/`https` URLs open in the browser, `#` anchors scroll to headings in the preview, relative file paths open in editor tabs, and all other schemes do nothing (`cmd/lime/mdpreview.go`).
- **Next-pass anchor scrolling**: Anchor links align target headings to the top of the viewport (matching browsers) on the build pass after the click (moving `p.anchor` to `p.heading`), first attempting `ui.List.ScrollTo` for parts starting with the heading and otherwise falling back to `scrollToAnchor` via `opts.Block` before the heading builds (`cmd/lime/mdpreview.go`).

## Themes, Contrast & Vibrancy

- **Scheme-derived adaptive chrome**: Window chrome colors are derived algorithmically from the active editor color scheme (GitHub, Sublime Text, Flexoki, or Pierre), modeled on Sublime Text's Adaptive theme, rather than maintained as separate UI themes (`cmd/lime/theme.go`).
- **System dark-mode read order**: `c.Theme().Dark` is read before `c.SetTheme(th)` because prior to `SetTheme` it reflects the window/system dark-mode state needed when Appearance follows the system (`cmd/lime/view.go`).
- **Instant scheme switches**: UI animations are disabled when switching color schemes so chrome colors snap immediately in sync with the editor's instant color change instead of fading behind it (`cmd/lime/view.go`).
- **Vibrancy-aware shell chrome**: `shell(c)` separates vibrancy-aware shell surfaces (sidebar, tab bars, status bar) from `chrome()` so they switch to transparent/translucent fills when OS window vibrancy is active, letting native material show through while other chrome stays opaque (`cmd/lime/theme.go`).
- **Flexoki borderless fills**: Upstream Flexoki marks selections and matches with borders, which the editor does not draw; Lime substitutes Flexoki's `ui-2` and `yellow` fills so they remain legible without borders (`cmd/lime/theme.go`).
- **Sidebar git tint contrast**: Sidebar git status text colors (`gitTint`) derive from gutter diff colors and progressively mix toward black (light schemes) or white (dark schemes) until reaching WCAG AA's minimum text contrast ratio of `4.5` against the sidebar background (`cmd/lime/gitmarks.go`).
- **Foreground luminance threshold**: Choosing white vs. black foreground text uses WCAG 2.x's relative luminance threshold of `0.179`, where white and black have equal contrast against a background color (`cmd/lime/shortcutsview.go`).

## Custom Carets & Typing Effects

- **Caretless theme overlay**: Each `scheme` caches a `caretless` `editor.Theme` with a transparent cursor color to hide the built-in caret underneath custom cursor styles (`glow`, `spark`, `smooth`), which paint over the focused editor via `drawCarets` using `a.scheme.caret` (`cmd/lime/theme.go`, `cmd/lime/caret.go`).
- **Post-paint caret geometry**: `caretRects` reads layout and scroll state written during the editor's last paint pass, so callers must invoke it after the editor paints (e.g., in `DrawOver`); carets on lines out of view or scrolled past are excluded (`cmd/lime/caret.go`).
- **Smooth caret coordinate space and timing**: Smooth carets store positions relative to `caretOrigin` (content space) so scrolling moves carets immediately with the text, while caret navigation within the text glides with a cubic ease-out over `120ms` (`glideTime`, matching Neovide) (`cmd/lime/caret.go`).
- **Frame delta clamp**: Frame delta time (`elapsed`) is clamped to at most `0.05s` so caret animations and sparks do not jump or expire immediately after an idle pause (`cmd/lime/caret.go`).
- **Spark update-before-spawn order**: `spark` advances existing sparks by `dt` before spawning new sparks on caret movement, because after an idle pause `dt` belongs to the pause before the new sparks existed and would displace them from the caret (`cmd/lime/caret.go`).
- **Rampage content-space coordinates**: Rampage particle and trail coordinates are stored relative to `caretOrigin` so active effects scroll along with the text buffer (`cmd/lime/rampage.go`).
- **Rampage update-before-spawn order**: `drawRampage` advances existing particles and trails by `elapsed` `dt` before spawning new bursts and trails so an idle pause's `dt` does not immediately age or displace newly spawned effects on their first frame (`cmd/lime/rampage.go`).
- **Rampage particle cap**: Active Rampage particles are capped at `600` (evicting the oldest) to bound rendering cost when holding a repeating key with multiple carets (`cmd/lime/rampage.go`).

# Git Integration

Design decisions, execution invariants, and UI conventions for Lime's Git features across subprocess execution, repository resolution, Changes, History, Commit views, mini-diff, blame, sidebar marks, remotes, avatars, and permalinks.

## Subprocess Execution and Locking

- **Off-main-thread execution**: `runGit` blocks on subprocess I/O and must always run off the main UI thread; on failure it returns Git's printed output as the error message (`cmd/lime/git.go`).
- **Lockless reads and non-interactive credentials**: Git subprocesses set `GIT_OPTIONAL_LOCKS=0` so read-only calls do not contend on `.git/index.lock` with external Git processes, and `GIT_TERMINAL_PROMPT=0` so Git never blocks waiting for TTY credentials in a GUI process, relying instead on the user's credential helper or SSH agent (`cmd/lime/git_exec.go`).
- **Combined failure diagnostics**: Failure messages combine stderr and stdout because some Git errors, such as `pull` conflicts, print diagnostics to stdout (`cmd/lime/git_exec.go`).
- **Windows console suppression**: `hideConsole` provides a platform hook to prevent flashing console windows when spawning Git from a GUI app (`cmd/lime/git_exec.go`); on Windows it sets `CreationFlags` to `0x08000000` (`CREATE_NO_WINDOW`) in addition to `HideWindow` because `git` is a console executable (`cmd/lime/git_windows.go`).
- **Per-repository write serialization**: `repoLocks` serializes Git write operations with a per-repository-root mutex so rapid UI actions (such as clicking staging checkboxes quickly) do not collide on `.git/index.lock`, while writes in different repositories run concurrently (`cmd/lime/git.go`).

## Repository, Worktree, and Branch Resolution

- **Main worktree naming and ordering**: The main worktree is labeled `"main"` (`mainTree`) rather than its directory basename, matching Zed; Lime assigns `mainTree` to the first entry because `git worktree list --porcelain` always lists the main worktree first (`cmd/lime/git.go`).
- **Linked worktree detection**: Linked worktrees are detected by comparing `git rev-parse --git-dir` against `--git-common-dir`, since a linked worktree keeps a separate gitdir inside the main repository's `.git/worktrees` (`cmd/lime/git.go`).
- **Flicker-free repository refresh**: Refreshing the same folder's Git state in `readGit` keeps existing repository metadata visible until the background read completes (`cmd/lime/git.go`).
- **Speculative status and history refresh**: Because a folder's repository root rarely moves, `readGit` immediately launches worktree status and history refreshes when a root is already known, re-running them only if `readRepo` discovers that the root changed (`cmd/lime/git.go`).
- **Tag-safe branch names**: `git for-each-ref` uses `%(refname:lstrip=2)` instead of `%(refname:short)` so branch names are not prefixed with `heads/` when a tag shares the same name (`cmd/lime/git.go`).
- **Combined branch and stash picker**: `showBranches` presents switchable branches and applicable stashes in a single quick panel, following Zed's branch picker UX (`cmd/lime/git.go`).
- **Tag-safe remote branch tracking**: `git switch --track` receives the full `refs/remotes/<name>` ref so a tag of the same name cannot shadow the remote branch (`cmd/lime/git.go`).
- **Locale-independent unmerged-branch detection**: Deleting a branch with `-d` forces `-c advice.forceDeleteBranch=true` so Git emits its `"branch -D"` hint even if disabled in user config; Lime detects unmerged-branch rejection by matching `"branch -D"` in the error output because Git returns no distinct exit code and that command snippet remains untranslated across locales (`cmd/lime/git.go`).

## Project Indexing and `.gitignore` Handling

- **Ancestor ignore loading**: The fallback directory walker loads `.git/info/exclude` and ancestor `.gitignore` files up to the enclosing Git repository root so opening a repository subdirectory still honors repository-level ignore rules (`cmd/lime/project.go`).
- **Trailing-space escaping in `.gitignore`**: Unescaped trailing spaces are trimmed, while an odd number of backslashes preceding a trailing space preserves the space and strips the escape backslash (`cmd/lime/project.go`).
- **Trailing `/**` matching**: A `.gitignore` pattern of the form `prefix/**` matches entries inside `prefix` but not `prefix` itself (`cmd/lime/project.go`).
- **Submodules and embedded repositories**: Because `git ls-files` reports a checked-out submodule or nested repository as a single directory entry, `walkFoldersWith` detects directories containing `.git` and re-queues them so their files are also indexed (`cmd/lime/project.go`).

## Changes Panel and Commit Workflows

- **Staging-state sections**: The Changes tab groups files into `conflicted`, `staged`, and `unstaged` sections by staging state, matching Zed's git panel (`cmd/lime/changes.go`).
- **Unified row keying**: `changeKey` encodes three row kinds in `(sec, path)` without a tag enum: section headers use an empty path, folders append a trailing `/` to avoid colliding with same-named files, and files use their path (`cmd/lime/changes.go`).
- **Off-main-thread status reads**: `readStatus` blocks on Git subprocess I/O and must run off the main UI thread (`cmd/lime/changes.go`).
- **Latest-read-wins concurrency**: A monotonic generation counter (`gen`) discards stale, out-of-order background `git status` results so only the latest read updates state (`cmd/lime/changes.go`).
- **Flicker-free refresh**: `readChanges` keeps existing rows visible while refreshing the same repository root in the background (`cmd/lime/changes.go`).
- **Space-safe porcelain v2 parsing**: `statusField` uses `strings.SplitN(line, " ", n+1)` rather than full splitting because the final field in `git status --porcelain=v2` is a path that may contain spaces (`cmd/lime/changes.go`).
- **Normalized status badges**: `statusKind` maps copies (`C`) to renames (`R`) and file type changes (`T`) to modifications (`M`) (`cmd/lime/changes.go`).
- **Unstaging before the initial commit**: Unstaging runs `git reset` instead of `git restore --staged` because `git reset` succeeds before the first commit (when `HEAD` does not exist), whereas `git restore --staged` fails (`cmd/lime/changes.go`).
- **Individual conflict staging**: `chosen(conflicted)` returns `nil`, excluding conflicted files from bulk selection actions so each conflict is staged individually after resolution (`cmd/lime/changes.go`).
- **Selection toggle direction**: `stageChosen` decides whether to stage or unstage selected rows based on whether the last-selected row (`cur`) is in the Staged section, matching Space in Zed's git panel (`cmd/lime/changes.go`).
- **Recoverable discard and intent-to-add ordering**: `discardFiles` restores tracked unstaged files from the index via `git restore`, while moving new files to the OS Trash rather than permanently unlinking them so they remain recoverable. `trashed` classifies both untracked (`?`) and unstaged intent-to-add (`A`, from `git add -N`) files as Trash-bound because Git stores no text for either; intent-to-add files are removed from the index with `git rm --cached` before moving to Trash (and excluded from `git restore`), because `git restore` would overwrite them with the empty index entry (`cmd/lime/changes.go`).
- **Commit Tracked fallback**: When no changes are staged and Amend is off, `commitArgs` switches to `git commit -a` and relabels the button to commit tracked unstaged changes directly (matching Zed's Commit Tracked), becoming ready when there are committable changes and a non-empty message (`cmd/lime/changes.go`).
- **Message-only amend**: Amend mode allows committing with no staged or unstaged file changes—requiring only that `HEAD`'s message has finished loading and the message is non-empty—because amending may change only the commit message (`cmd/lime/changes.go`).
- **Amend draft preservation**: Toggling Amend (`amendChanged`) stashes the currently typed commit message in `draft` while loading `HEAD`'s message asynchronously, and restores `draft` if Amend is unchecked (`cmd/lime/changes.go`).
- **Race-safe post-commit message reset**: After a background `git commit` succeeds, `commitBox` clears the commit message (or restores the pre-amend `draft` after amending) only if the user did not edit the message while `git commit` was running (`cmd/lime/changes.go`).
- **Uncommit constraints**: Following Zed's Uncommit, `uncommit` soft-resets `HEAD~1` after confirmation and fills the commit box with `HEAD`'s message only when the box is empty. It refuses root commits (no parent) and merge commits (more than one parent), because `git reset --soft HEAD~1` only resets to the first parent and would lose the other parent links (`cmd/lime/changes.go`).
- **Pushed-HEAD divergence check**: `headPushed` checks whether `HEAD` is already an ancestor of `@{upstream}` so amending or uncommitting a pushed commit warns that rewriting `HEAD` will make the branch diverge (`cmd/lime/changes.go`).

## Commit View, Worktree Diffs, and Hunk Staging

- **Sentinel hashes for worktree tabs**: Staged and unstaged worktree diff tabs reuse `commitView` using non-hex sentinel hashes (`"staged"`, `"unstaged"`) that cannot collide with real commit hashes (`cmd/lime/commitview.go`).
- **Porcelain diff flags and raw-byte `textconv` invariant**: Worktree diffs invoke porcelain `git diff` (unlike `diff-tree`) to honor user diff settings, but force `--no-textconv`, `--no-color`, `--no-ext-diff`, and explicit `a/`/`b/` prefixes because parsed hunks are applied directly back to raw bytes via `git apply` when staging or discarding (`cmd/lime/commitview.go`). Even when a `textconv` filter alters displayed diffs, discarding a hunk or file applies against raw worktree/index bytes, restoring the worktree to the index while keeping staged changes and updating open tabs (`cmd/lime/main_test.go`).
- **Index object spec**: An empty revision string constructs `:path` object specs for `git cat-file`, which Git resolves against the index (`cmd/lime/commitview.go`).
- **Untracked files in worktree diffs**: Because `git diff` omits untracked (`?`) files, `commitView` reads them directly from disk (with large-file and binary guards) and synthesizes a single added hunk (`cmd/lime/commitview.go`).
- **On-disk read for unstaged new-side text**: For unstaged worktree diffs, the new-side text exists only on disk (not as a Git object), so it is read from the filesystem with a size bound instead of via `catFiles` (`cmd/lime/commitview.go`).
- **Both-sides size check**: Before running `git diff` on unstaged changes, both the on-disk worktree file size and the indexed Git blob size are checked against `largeFile`, because a full deletion or rewrite can produce a huge patch even when the new file is small or absent (`cmd/lime/commitview.go`).
- **Git < 2.42 batch-check fallback**: `git cat-file -Z --batch-check` requires Git 2.42+, so on error `largeWorktreeChanges` falls back to per-file `git cat-file -s` (`cmd/lime/commitview.go`).
- **Space-safe missing-object parsing**: In `git cat-file --batch` output, `<spec> missing` lines can contain spaces in the path and split into three whitespace fields, so `strings.HasSuffix(line, " missing")` must be checked before parsing header fields (`cmd/lime/commitview.go`).
- **Zero-line hunk start offset**: In unified diff headers, Git reports the line immediately preceding a zero-line hunk (pure add or delete), so `hunkStart` adds 1 when `lines == 0` to obtain the 1-based position (`cmd/lime/commitview.go`).
- **Direct single-hunk application**: `stageHunk` synthesizes and applies a single hunk directly via `git apply --cached --recount` (with `--reverse` to unstage) without recalculating offsets from other hunks, because the tab diffs the index directly against `HEAD` or the worktree, so the index-side line number already matches the index (`cmd/lime/commitview.go`).

## History and File History

- **Canonical change vocabulary and paths**: `change.status` uses single-byte codes across commits and worktrees: `A` (added), `M` (modified), `D` (deleted), `R` (renamed, with `oldPath`), `?` (untracked), and `U` (conflict). `change.path` and `change.oldPath` are always stored relative to the worktree root using forward slashes (`cmd/lime/history.go`).
- **History scope encoding**: `history.file` stores a root-relative forward-slash path, with `""` representing repository-wide commit history (`cmd/lime/history.go`).
- **GitHub remote flag**: `history.github` records whether the repository has a GitHub remote to gate commit author avatar requests (`cmd/lime/history.go`).
- **Rename tracking across commits and merges**: `history.paths` parallels `history.commits` to store a tracked file's path at each historical commit so clicking a commit opens its diff at the correct path. Because `git log --follow --name-only` emits no filename list for merge commits, `readFileCommits` carries forward the last known path from the subsequent (newer) commit (`cmd/lime/history.go`).
- **Latest-read-wins concurrency**: `history.gen` is a monotonic generation counter used to discard stale background `git log` results (`cmd/lime/history.go`).
- **Flicker-free refresh**: `readHistory` retains the currently displayed commit list while refreshing the same repository root in the background (`cmd/lime/history.go`).
- **File History UX**: `fileHistory` filters the git panel's History tab to a single file's commits, modeled on Sublime Merge's File History (`cmd/lime/history.go`).
- **Non-blocking tracked check**: `tracked` avoids blocking the main thread with a Git query by reusing an open text tab's mini-diff base (`d.base != ""`) as a best-effort tracked indicator, while still permitting closed files and oversized files (`> largeFile`) (`cmd/lime/history.go`).
- **Symlink resolution before relativizing**: `fileHistory` resolves symlinks on the target file before calling `filepath.Rel` against `a.git.root`, because `git rev-parse --show-toplevel` returns the symlink-resolved repository path (`cmd/lime/history.go`).

## Gutter Mini-Diff

- **Sublime Text `mini_diff` parity**: The gutter marks changes against the file's text in Git (the index or `HEAD` according to `git_diff_target`), falling back to on-disk text for files Git does not track (`cmd/lime/minidiff.go`).
- **Deferred launch loading**: When files open before the window exists (`a.win == nil`), `readBases` is deferred until window activation rather than blocking application launch (`cmd/lime/app.go`).
- **Blame cache invalidation**: Any event that refreshes Git mini-diff bases can invalidate line commit history, so `readBases` marks active blame caches stale (`cmd/lime/minidiff.go`).
- **Encoding decode fallback**: If a Git blob cannot be decoded with the file's encoding, mini-diff falls back to `diffDisk()` instead of decoding lossily, avoiding false gutter markers on unchanged lines (`cmd/lime/minidiff.go`).
- **Per-directory batching**: `gitTexts` batches `git cat-file` with one Git process per directory so Git discovers each folder's repository or submodule; it blocks on subprocess I/O and must run off the main thread (`cmd/lime/minidiff.go`).
- **Symlink resolution**: Because Git stores a symlink's target path rather than the target file's text, `gitTexts` resolves symlinks with `filepath.EvalSymlinks` before reading the Git object (`cmd/lime/minidiff.go`).
- **Missing-before-valid stream test**: `TestMiniDiff` intentionally opens the untracked file before the tracked file so `git cat-file --batch` processes a missing object ahead of a valid one, catching stream-desync regressions (`cmd/lime/minidiff_test.go`).

## Git Blame

- **Zed-modeled UX and saved-snapshot scope**: Blame displays delayed inline blame at the end of the caret's line once the caret rests (`inline_blame`) and a toggleable column beside line numbers (`Toggle Blame`) where clicking opens the commit. Git blames the saved snapshot, so unsaved edited lines show no blame (`cmd/lime/blame.go`).
- **Line-count cap**: `maxBlameLines` caps blame at 50,000 lines to bound CPU and memory cost because Git traverses history per line and porcelain output is parsed whole (`cmd/lime/blame.go`).
- **Eligibility constraints**: `blameable` requires a Git-tracked plain text file within `maxBlameLines`, excludes CR-only files whose line splitting differs between Git and the editor, and disables blame on `js`/WebAssembly because the website's Git shim lacks blame (`cmd/lime/blame.go`).
- **In-memory snapshot blaming**: `blameFile` blames the re-encoded in-memory saved snapshot via `--contents -` instead of the file on disk (which Git or external tools may have modified since), discards results if Git's line count disagrees with the tab's (for example, UTF-16), and blocks so it must run off the main thread (`cmd/lime/blame.go`).
- **Trailing-newline line count**: Unlike `editor.Snapshot.LineCount()`, `git blame` does not count an empty trailing line after a final newline (or in an empty file), so `gitLineCount` subtracts 1 to match Git (`cmd/lime/blame.go`).
- **Porcelain metadata sharing**: `git blame --porcelain` emits author and summary metadata only on the first line of each commit, so `parseBlame` shares and mutates `*blamed` pointers keyed by commit hash (`cmd/lime/blame.go`).
- **Async staleness recovery**: If a document is saved again or renamed while `blameFile` runs in the background, returning early without updating `b.saved`/`b.read` suffices because the next frame's `syncBlame` automatically re-triggers `readBlame` (`cmd/lime/blame.go`).
- **Inline blame formatting and width thresholds**: Following Zed, `inlineBlame` shows author, relative time, and up to 60 columns of commit subject within available `room` columns, returning `""` when author and time do not fit; if fewer than 10 columns (`n < 10`) remain for the subject, the subject is omitted rather than showing an unreadable stub (`cmd/lime/blame.go`).

## Sidebar Git Marks

- **Status tinting and ignored dimming**: File and folder names in the sidebar are tinted by Git status (matching Zed's project panel), while Git-ignored entries are dimmed (matching Sublime Text's sidebar) (`cmd/lime/gitmarks.go`).
- **Mark precedence and untracked tint**: The `gitMark` enum ordering (`clean < ignored < added < deleted < modified < conflict`) lets `max()` roll up folder marks; folders take the highest mark of their descendants excluding ignored entries. Untracked files reuse the `gitAdded` mark and tint (`cmd/lime/gitmarks.go`).
- **Visibility-gated, flicker-free refresh**: `readMarks` only refreshes while the files sidebar is visible and retains previous marks until the background read completes. `gitMarks.folders` records the last-read folder list and resets to `nil` when the files panel is hidden so rendering lazily triggers a fresh read when shown again (`cmd/lime/gitmarks.go`).
- **Single-flight refresh coalescing**: Only one background mark read runs at a time (`reading`), and concurrent triggers (such as Save All or bursts of disk changes) coalesce into at most one follow-up read via the `again` flag (`cmd/lime/gitmarks.go`).
- **Symlink-safe worktree prefixing**: A sidebar folder is positioned in its worktree using `git rev-parse --show-prefix` rather than comparing against `--show-toplevel`, because Git resolves symlinks in `--show-toplevel` while the sidebar path may contain symlinks. `gitBase.prefix` is always `""` or ends with `/`, allowing direct concatenation with relative paths (`cmd/lime/gitmarks.go`).
- **Nested repositories and submodules**: `readGitBases` reads Git status for each sidebar folder's worktree and recursively scans nested repositories reported as single entries (blocking on Git subprocesses, so called off the main thread). Because `--untracked-files=all` expands normal untracked directories into files, any `?` entry ending in `/` is guaranteed to be a nested repository and queued for scanning. In porcelain v2 output, submodule entries with `'S'` at byte 5 and `'M'` (modified tracked files) at byte 7 or `'U'` (untracked files) at byte 8 are also queued for nested scanning (`cmd/lime/gitmarks.go`).
- **Ignored directories and propagation**: `readTreeStatus` passes `--ignored=matching` (off the main thread) so ignored directories like `node_modules` return as single entries instead of enumerating all ignored files. Ignored entries (`!`) skip the ancestor-propagation loop so they never mark parent folders as ignored; conversely, because files inside ignored folders are omitted by `--ignored=matching`, `gitBase.mark` walks ancestor path segments so descendants inherit `gitIgnored` (`cmd/lime/gitmarks.go`).
- **Porcelain v2 rename/copy parsing**: In `git status --porcelain=v2 -z` output, type `2` rename/copy entries have 9 header fields plus an extra NUL-terminated original-path token that must be skipped with `i+1` (`cmd/lime/gitmarks.go`).
- **Nested repo mark boundaries and Unicode normalization**: A nested repository root takes the mark assigned by its enclosing repository, while entries inside take marks from the nested repository. Paths are normalized to Unicode NFC so Git's macOS output matches NFD directory listings (`cmd/lime/gitmarks.go`).

## Remotes and Sync

- **Selective write locking**: Among remote commands, only `pull` acquires `repoLock(dir)` because only `pull` modifies the index and worktree, allowing `push` and `fetch` to run without blocking index/worktree operations (`cmd/lime/remote.go`).
- **Combined sync button**: `syncButton` combines ahead and behind commit counts into one control to preserve horizontal space for the branch name; clicking pulls incoming commits before pushing, matching Zed's git panel (`cmd/lime/remote.go`).

## GitHub Avatars

- **GitHub-only loading and placeholder**: Matching Zed, `avatar` loads author pictures only when the repository is hosted on GitHub and shows a circular person icon placeholder while loading or when unavailable (`cmd/lime/avatar.go`).
- **Direct CDN lookup and bot exclusion**: `avatarURL` fetches avatars directly from GitHub's CDN by email without calling the GitHub API (matching Zed), and short-circuits `[bot]@users.noreply.github.com` addresses because bots have no avatar (`cmd/lime/avatar.go`).
- **Disk cache with negative sentinels**: `loadAvatar` reads from or fetches into a disk cache and writes an empty sentinel file for missing avatars to avoid repeated requests; it blocks on disk and network I/O and must run off the main thread (`cmd/lime/avatar.go`).

## GitHub Permalinks

- **Commit-pinned links**: Permalinks target `HEAD`'s commit SHA rather than a mutable branch name, matching Zed's Copy/Open Permalink to Line (`cmd/lime/permalink.go`).
- **Eligibility and WebAssembly exclusion**: `linkable` requires a Git-tracked plain text file (`d.base != ""`) and disables permalinks on WebAssembly (`runtime.GOOS == "js"`) because the website's in-memory `git.js` has no remotes (`cmd/lime/permalink.go`).
- **Remote selection**: `githubRepo` chooses `origin` when it points to GitHub and otherwise falls back to the first GitHub remote (or `""` if none); it blocks on Git subprocesses and must run off the main thread (`cmd/lime/permalink.go`).
- **Selection line-range boundary**: `selectedLines` excludes the trailing line of a multi-line selection that ends at column 0 from the `#L` anchor range (`cmd/lime/permalink.go`).
- **Plain view for rendered extensions**: Extensions in `rendered` (prose, notebooks, and tabular files that GitHub renders by default, where `#L` line anchors do not work) append `?plain=1` (`cmd/lime/permalink.go`).
- **Reachability and staleness checks**: `readPermalink` verifies both whether a remote-tracking branch contains `HEAD` (warning when the GitHub link will 404) and whether the file matches `HEAD` (warning when line numbers may be stale); it blocks on Git subprocesses and must run off the main thread (`cmd/lime/permalink.go`).

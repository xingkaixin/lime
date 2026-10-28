package main

import (
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lime/locales"
)

// section is a group of the Changes tab's files, as Zed's git panel
// groups them by staging.
type section uint8

const (
	conflicted section = iota
	staged
	unstaged
)

// changes is the side bar's Changes tab: the work tree's changes, read
// for root, by section.
type changes struct {
	root    string
	files   [3][]change
	gen     int // counts the readings, for the last to win
	list    ui.ListState
	sel     ui.Selection[changeKey]
	cur     int // the row chosen last, -1 for none
	rows    []changeRow
	closed  map[changeKey]bool // the folders closed in the tree
	message string             // of the commit to make
	amend   bool               // the commit replaces HEAD
	draft   string             // the message typed before amend loaded HEAD's
}

// changeKey names a row of the Changes tab: a section's header, of no
// path, a folder of it, by its path and a slash, or a file of it.
type changeKey struct {
	sec  section
	path string
}

// changeRow is a row of the Changes tab: a header, a folder or a file.
type changeRow struct {
	key   changeKey
	file  *change // a file's
	dir   string  // a folder's path, in the tree
	depth int
}

func (r changeRow) header() bool { return r.file == nil && r.dir == "" }

// readStatus reads the work tree's changes by section, as git status
// tells them. It blocks, so call it off the main thread.
func readStatus(root string) [3][]change {
	out, _ := runGit(root, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	var files [3][]change
	f := strings.Split(out, "\x00")
	for i := 0; i < len(f); i++ {
		line := f[i]
		if len(line) < 3 {
			continue
		}
		switch line[0] {
		case '?':
			files[unstaged] = append(files[unstaged], change{status: '?', path: line[2:]})
		case 'u':
			files[conflicted] = append(files[conflicted], change{status: 'U', path: statusField(line, 10)})
		case '1', '2':
			ch := change{path: statusField(line, 8)}
			if line[0] == '2' && i+1 < len(f) {
				i++
				ch.path, ch.oldPath = statusField(line, 9), f[i]
			}
			if x := line[2]; x != '.' {
				in := ch
				in.status = statusKind(x)
				files[staged] = append(files[staged], in)
			}
			if y := line[3]; y != '.' {
				files[unstaged] = append(files[unstaged], change{status: statusKind(y), path: ch.path})
			}
		}
	}
	return files
}

// statusField is field n of a line of git status, from 0: the last, a
// path, may hold spaces.
func statusField(line string, n int) string {
	f := strings.SplitN(line, " ", n+1)
	if len(f) <= n {
		return ""
	}
	return f[n]
}

// statusKind is the letter the tab shows: a copy shows as a rename, and
// a file whose type changed as modified.
func statusKind(c byte) byte {
	switch c {
	case 'C':
		return 'R'
	case 'T':
		return 'M'
	}
	return c
}

// readChanges reads the Changes tab again, keeping what shows until it is
// read.
func (a *app) readChanges() {
	root := a.git.root
	w := &a.changes
	if root != w.root {
		*w = changes{root: root, gen: w.gen, cur: -1}
	}
	if root == "" {
		return
	}
	w.gen++
	gen := w.gen
	a.background(func() func() {
		files := readStatus(root)
		return func() {
			if w.gen == gen {
				w.files = files
			}
		}
	})
}

// readWorktree reads the work tree's changes again: the Changes tab's,
// while it shows, and the tabs of their diffs.
func (a *app) readWorktree() {
	if a.side == gitPanel {
		a.readChanges()
	}
	for _, d := range a.allDocs() {
		if cv := d.commit; cv != nil && cv.worktree() && cv.root == a.git.root {
			a.loadCommit(cv)
		}
	}
}

// layRows lays the Changes tab out: each section with its files under
// its header, as a list, or in the tree of their folders, folders first,
// whose rows a closed folder hides.
func (w *changes) layRows(tree bool) {
	w.rows = w.rows[:0]
	for _, sec := range []section{conflicted, staged, unstaged} {
		files := w.files[sec]
		if len(files) == 0 {
			continue
		}
		w.rows = append(w.rows, changeRow{key: changeKey{sec: sec}})
		if !tree {
			for i := range files {
				w.rows = append(w.rows, changeRow{key: changeKey{sec, files[i].path}, file: &files[i]})
			}
			continue
		}
		order := make([]int, len(files))
		for i := range order {
			order[i] = i
		}
		slices.SortFunc(order, func(i, j int) int { return treeCompare(files[i].path, files[j].path) })
		var last []string
		for _, i := range order {
			dirs := strings.Split(files[i].path, "/")
			dirs = dirs[:len(dirs)-1]
			same := 0
			for same < len(dirs) && same < len(last) && dirs[same] == last[same] {
				same++
			}
			for d := same; d < len(dirs); d++ {
				dir := strings.Join(dirs[:d+1], "/")
				w.addRow(changeRow{key: changeKey{sec, dir + "/"}, dir: dir, depth: d})
			}
			last = dirs
			w.addRow(changeRow{key: changeKey{sec, files[i].path}, file: &files[i], depth: len(dirs)})
		}
	}
}

// addRow adds r unless a folder it is in is closed.
func (w *changes) addRow(r changeRow) {
	p := r.dir
	if r.file != nil {
		p = r.file.path
	}
	for d := path.Dir(p); d != "."; d = path.Dir(d) {
		if w.closed[changeKey{r.key.sec, d + "/"}] {
			return
		}
	}
	w.rows = append(w.rows, r)
}

// treeCompare orders paths as the tree shows them: in each folder, its
// folders before its files, each by name.
func treeCompare(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		if aDir, bDir := i < len(as)-1, i < len(bs)-1; aDir != bDir {
			if aDir {
				return -1
			}
			return 1
		}
		return strings.Compare(as[i], bs[i])
	}
	return len(as) - len(bs)
}

// under returns the files of sec in the folder dir.
func (w *changes) under(sec section, dir string) []change {
	var files []change
	for _, f := range w.files[sec] {
		if strings.HasPrefix(f.path, dir+"/") {
			files = append(files, f)
		}
	}
	return files
}

// stageFiles stages files, or unstages them, as their paths name them
// literally.
func (a *app) stageFiles(files []change, unstage bool) {
	if len(files) == 0 {
		return
	}
	var paths strings.Builder
	for _, f := range files {
		paths.WriteString(f.path + "\x00")
		if f.oldPath != "" {
			paths.WriteString(f.oldPath + "\x00")
		}
	}
	// reset, unlike restore --staged, unstages before the first commit.
	cmd := []string{"--literal-pathspecs", "add", "--pathspec-from-file=-", "--pathspec-file-nul"}
	if unstage {
		cmd = []string{"--literal-pathspecs", "reset", "-q", "--pathspec-from-file=-", "--pathspec-file-nul"}
	}
	a.gitRunWith(paths.String(), nil, cmd...)
}

// chosen returns the files of sec chosen, or in a folder chosen. A file
// in conflict is staged alone, as it is resolved.
func (w *changes) chosen(sec section) []change {
	if sec == conflicted {
		return nil
	}
	var files []change
	for _, f := range w.files[sec] {
		in := w.sel.Has(changeKey{sec, f.path})
		for d := path.Dir(f.path); !in && d != "."; d = path.Dir(d) {
			in = w.sel.Has(changeKey{sec, d + "/"})
		}
		if in {
			files = append(files, f)
		}
	}
	return files
}

// toggle opens the folder of k, or closes it.
func (w *changes) toggle(k changeKey, open bool) {
	if w.closed == nil {
		w.closed = map[changeKey]bool{}
	}
	w.closed[k] = !open
}

// stageChosen stages the files chosen, or unstages them as the row
// chosen last is staged, as Space does in Zed's git panel.
func (a *app) stageChosen() {
	w := &a.changes
	if w.cur < 0 || w.cur >= len(w.rows) {
		return
	}
	if w.rows[w.cur].key.sec == staged {
		a.stageFiles(w.chosen(staged), true)
		return
	}
	a.stageFiles(w.chosen(unstaged), false)
}

// changeMenu is the menu of the rows chosen.
func (a *app) changeMenu(m *ui.Menu) {
	w := &a.changes
	toStage, toUnstage := w.chosen(unstaged), w.chosen(staged)
	if m.Item(locales.ChangesStage()).Disabled(len(toStage) == 0).Chosen() {
		a.stageFiles(toStage, false)
	}
	if m.Item(locales.ChangesUnstage()).Disabled(len(toUnstage) == 0).Chosen() {
		a.stageFiles(toUnstage, true)
	}
	// The website's git restores nothing, and browsers have no Trash.
	if runtime.GOOS == "js" {
		return
	}
	m.Separator()
	if m.Item(locales.ChangesDiscard()).Disabled(len(toStage) == 0).Chosen() {
		a.confirmDiscard(toStage)
	}
}

// confirm asks before what can't be undone, and runs yes if the user
// goes ahead.
func (a *app) confirm(message, detail, button string, yes func()) {
	go func() {
		res, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  a.win,
			Type:    mygo.MessageWarning,
			Message: message,
			Detail:  detail,
			Buttons: []string{button, locales.ButtonCancel()},
		})
		if res.Button == 0 {
			a.win.Update(yes)
		}
	}()
}

// trashed reports whether discarding f, not staged, moves its file to
// the Trash: an untracked file, or one added with intent to add, whose
// text git has none of.
func trashed(f change) bool { return f.status == '?' || f.status == 'A' }

func (a *app) confirmDiscard(files []change) {
	var detail []string
	if slices.ContainsFunc(files, func(f change) bool { return !trashed(f) }) {
		detail = append(detail, locales.DialogDiscardLost())
	}
	if slices.ContainsFunc(files, trashed) {
		detail = append(detail, locales.DialogDiscardUntracked())
	}
	a.confirm(locales.DialogDiscardFiles(len(files)), strings.Join(detail, " "), locales.ButtonDiscard(), func() { a.discardFiles(files) })
}

// discardFiles discards the changes of files not staged: a tracked file
// takes the index's text again, and a new one moves to the Trash, whence
// it can come back.
func (a *app) discardFiles(files []change) {
	root := a.git.root
	var tracked, added strings.Builder
	var trash []string
	for _, f := range files {
		switch {
		case !trashed(f):
			tracked.WriteString(f.path + "\x00")
			continue
		case f.status == 'A':
			added.WriteString(f.path + "\x00")
		}
		trash = append(trash, filepath.Join(root, filepath.FromSlash(f.path)))
	}
	paths, unadd := tracked.String(), added.String()
	a.background(func() func() {
		var failed string
		var err error
		// A file added with intent to add leaves the index first: restore
		// would empty it.
		if unadd != "" {
			gitWrites.Lock()
			_, err = gitWith(root, unadd, "--literal-pathspecs", "rm", "--cached", "-q", "--pathspec-from-file=-", "--pathspec-file-nul")
			gitWrites.Unlock()
		}
		for i := 0; err == nil && i < len(trash); i++ {
			if err = mygo.Shell.TrashItem(trash[i]); err != nil {
				failed = trash[i]
			}
		}
		return func() {
			switch {
			case failed != "":
				go mygo.Dialog.Error(locales.DialogUnableToDelete(filepath.Base(failed)), err.Error())
			case err != nil:
				msg, _, _ := strings.Cut(err.Error(), "\n")
				a.notifyError(msg)
			}
			if paths != "" {
				a.gitRunWith(paths, nil, "--literal-pathspecs", "restore", "--pathspec-from-file=-", "--pathspec-file-nul")
				return
			}
			a.refreshFolders()
			a.readGit()
		}
	})
}

// showChange shows the change of r: the diffs of its section, at its
// file, or a file in conflict itself.
func (a *app) showChange(r changeRow) {
	if r.file == nil {
		return
	}
	switch r.key.sec {
	case conflicted:
		a.open([]string{filepath.Join(a.changes.root, filepath.FromSlash(r.file.path))})
	case staged:
		a.showCommit(commit{hash: stagedChanges}).at = r.file.path
	case unstaged:
		a.showCommit(commit{hash: unstagedChanges}).at = r.file.path
	}
}

// The side bar's git panel shows the tab at gitTab.
const (
	changesTab = iota
	historyTab
)

// gitTabs switches the git panel between its Changes and History, as
// Zed's does, with the button that shows the changes as a tree.
func (a *app) gitTabs(c *ui.Context) {
	ch := a.shell(c)
	labels := []string{locales.GitChanges(), locales.GitHistory()}
	ui.Row(c).FillWidth().Padding(0, 8, 8, 8).AlignItems(ui.Center).Children(func() {
		tabs := ui.TabsBase(c.Key("gitTabs"), &a.gitTab, len(labels))
		tabs.List.Gap(2).Padding(2).Radius(7).Background(ch.bg).Border(1, ch.line).Children(func() {
			for i, label := range labels {
				t := tabs.Tab(i).Height(24).Padding(0, 12).Radius(5).Center().Label(label).Transition(colorFade)
				fg := ch.muted
				switch {
				case i == a.gitTab:
					fg = ch.text
					bg := ch.panel
					if a.scheme.dark {
						bg = ch.panel.Mix(ch.text, 0.14)
					}
					t.Background(bg).Shadow(0, 1, 2, 0, black.Alpha(0.15))
				case t.Hovered():
					t.Background(ch.hover)
				}
				t.Children(func() { ui.Text(c, label).FontSize(12).TextColor(fg) })
			}
		})
		ui.Box(c).Grow(1)
		if a.gitTab == changesTab {
			a.barButton(c, treeViewIcon, locales.ChangesViewAsTree(), a.set.GitTreeView, func() {
				a.set.GitTreeView = !a.set.GitTreeView
				a.changes.sel.Clear()
				a.l.saveSettings()
			})
		}
	})
}

var treeViewIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
	<path fill="none" stroke="#4c4f69" stroke-linecap="round" stroke-linejoin="round" d="M2.5 3.5h11M7.5 8h6M7.5 12.5h6M4.5 4v8.5h1M4.5 8h1" />
</svg>`))

// changesView lists the work tree's changes by section, each file of
// which shows its diff, over the message of the commit to make.
func (a *app) changesView(c *ui.Context) {
	a.changesList(c)
	a.commitBox(c)
}

func (a *app) changesList(c *ui.Context) {
	w := &a.changes
	w.layRows(a.set.GitTreeView)
	if len(w.rows) == 0 {
		ui.Column(c).Grow(1).FillWidth().Padding(16).Center().Children(func() {
			ui.Text(c, locales.ChangesNone()).FontSize(12).TextColor(a.shell(c).muted).TextAlign(ui.Center)
		})
		return
	}
	w.list.Selected = &w.cur
	w.list.Header = func(i int) bool { return w.rows[i].header() }
	colors := a.scheme.editorTheme()
	list := ui.List(c.Key("changes"), &w.list, len(w.rows)).ItemKey(func(i int) any { return w.rows[i].key }).Selection(&w.sel).
		Rows(func(r ui.ListRow) {
			a.changeRow(c, w.rows[r.Index], r.Selected(), colors.Added, colors.Deleted, colors.Modified)
		})
	list.Grow(1).FillWidth().Label(locales.GitChanges())
	if list.Changed() && w.cur >= 0 && w.cur < len(w.rows) {
		a.showChange(w.rows[w.cur])
	}
	if w.list.Shortcut(c, 0, ui.KeySpace) {
		a.stageChosen()
	}
	for _, open := range []bool{false, true} {
		key := ui.KeyLeft
		if open {
			key = ui.KeyRight
		}
		if w.list.Shortcut(c, 0, key) && w.cur >= 0 && w.cur < len(w.rows) && w.rows[w.cur].dir != "" {
			w.toggle(w.rows[w.cur].key, open)
		}
	}
}

// commitArgs are the arguments of the git that commits the changes
// staged, or with none, the tracked files' changes, as Zed's Commit
// Tracked does, with the label of its button; ready when there are
// changes to commit and a message.
func (w *changes) commitArgs() (args []string, label string, ready bool) {
	args, label = []string{"commit", "-F", "-"}, locales.ChangesCommit()
	ready = len(w.files[staged]) > 0
	switch {
	case w.amend:
		// Amending may change the message alone.
		args, ready = []string{"commit", "--amend", "-F", "-"}, true
	case !ready:
		args, label = []string{"commit", "-a", "-F", "-"}, locales.ChangesCommitTracked()
		ready = slices.ContainsFunc(w.files[unstaged], func(f change) bool { return f.status != '?' })
	}
	return args, label, ready && strings.TrimSpace(w.message) != ""
}

// amendChanged loads HEAD's message to amend it, keeping the one typed,
// which comes back as amend is off again.
func (a *app) amendChanged() {
	w := &a.changes
	if !w.amend {
		w.message, w.draft = w.draft, ""
		return
	}
	w.draft = w.message
	root := w.root
	a.background(func() func() {
		body, err := runGit(root, "show", "-s", "--format=%B", "HEAD")
		return func() {
			switch {
			case !w.amend || w.root != root:
			case err != nil:
				w.amend, w.draft = false, ""
				msg, _, _ := strings.Cut(err.Error(), "\n")
				a.notifyError(msg)
			default:
				w.message = strings.TrimSpace(body)
			}
		}
	})
}

// commitBox is the message of the commit to make and its button. The
// message clears once the commit is made, unless edited meanwhile, or
// after amending, is the one typed before.
func (a *app) commitBox(c *ui.Context) {
	w := &a.changes
	ch := a.shell(c)
	_, label, ready := w.commitArgs()
	commit := func() {
		args, _, ready := w.commitArgs()
		if !ready {
			return
		}
		msg := w.message
		a.gitRunWith(msg, func() {
			if w.message == msg {
				w.message = w.draft
			}
			w.amend, w.draft = false, ""
		}, args...)
	}
	ui.Column(c).FillWidth().Shrink(0).Padding(8).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(ch.line).Children(func() {
		ui.TextArea(c.Key("commitMessage"), &w.message).MinHeight(0).Lines(2, 10).FillWidth().FontSize(13).
			Placeholder(locales.ChangesMessage()).Label(locales.ChangesMessage()).OnShortcut(ui.Cmd, ui.KeyEnter, commit)
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Checkbox(c.Key("amend"), &w.amend, locales.ChangesAmend()).Shrink(0).OnClick(a.amendChanged)
			th := c.Theme()
			btn := ui.ButtonBase(c.Key("commit")).Grow(1).Height(28).Radius(6).Center().Background(th.Accent).
				Transition(colorFade).Label(label).Disabled(!ready).OnClick(commit)
			switch {
			case !ready:
				btn.Opacity(0.5)
			case btn.Hovered():
				btn.Background(th.Accent.Mix(th.AccentText, 0.1))
			}
			btn.Children(func() { ui.Text(c, label).FontSize(13).TextColor(th.AccentText) })
		})
	})
}

// stageBox is the check box of a file of sec, or of all of them: checked
// in Staged, where a click unstages, and unchecked elsewhere, where it
// stages.
func (a *app) stageBox(c *ui.Context, sec section, files []change, all bool) {
	on := sec == staged
	var label string
	switch {
	case all && on:
		label = locales.ChangesUnstageAll()
	case all:
		label = locales.ChangesStageAll()
	case on:
		label = locales.ChangesUnstage()
	default:
		label = locales.ChangesStage()
	}
	ui.Checkbox(c, &on, "").Shrink(0).Label(label).Tooltip(label).OnClick(func() { a.stageFiles(files, sec == staged) })
}

// changeRow is a section's header, a folder, or a file with its name,
// its folder in the list and the letter of its change, tinted as the
// editor's gutter tints it.
func (a *app) changeRow(c *ui.Context, r changeRow, selected bool, added, deleted, modified ui.Color) {
	th := c.Theme()
	ch := a.shell(c)
	if r.header() {
		label := [...]string{locales.ChangesConflicts(), locales.ChangesStaged(), locales.ChangesUnstaged()}[r.key.sec]
		ui.Row(c).FillWidth().Height(th.Space(7)).Padding(0, th.Space(3)).AlignItems(ui.Center).Background(ch.bg).Children(func() {
			ui.Text(c, label).FontSize(11).Bold().TextColor(ch.muted).SingleLine()
			ui.Box(c).Grow(1)
			if r.key.sec != conflicted {
				a.stageBox(c, r.key.sec, a.changes.files[r.key.sec], true)
			}
		})
		return
	}
	text, muted := ch.text, ch.muted
	if selected {
		text, muted = th.AccentText, th.AccentText.Alpha(0.8)
	}
	indent := th.Space(3) + (treeIcon+treeGap)*float32(r.depth)
	if r.dir != "" {
		open := !a.changes.closed[r.key]
		ic := rowIcon{svg: folderIcon}
		if open {
			ic = rowIcon{svg: folderOpenIcon, color: a.scheme.accent}
		}
		if selected {
			ic.color = text
		}
		ui.Row(c).FillWidth().Height(th.Space(7)).Gap(treeGap).Padding(0, th.Space(3)).PaddingStart(indent).AlignItems(ui.Center).
			Expanded(open).OnClick(func() { a.changes.toggle(r.key, !open) }).ContextMenu(a.changeMenu).Children(func() {
			ic.view(c, treeIcon, ic.tint(muted))
			ui.Text(c, path.Base(r.dir)).FontSize(13).TextColor(text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			ui.Box(c).Grow(1)
			a.stageBox(c, r.key.sec, a.changes.under(r.key.sec, r.dir), false)
		})
		return
	}
	tint := modified
	switch r.file.status {
	case 'A', '?':
		tint = added
	case 'D', 'U':
		tint = deleted
	}
	if selected {
		tint = th.AccentText
	}
	p := r.file.path
	ui.Row(c).FillWidth().Height(th.Space(7)).Gap(treeGap).Padding(0, th.Space(3)).PaddingStart(indent).AlignItems(ui.Center).
		ContextMenu(a.changeMenu).Children(func() {
		ic := fileIcon(p, a.scheme.dark)
		if selected {
			ic.view(c, treeIcon, text)
		} else {
			ic.view(c, treeIcon, ic.tint(muted))
		}
		ui.Text(c, path.Base(p)).FontSize(13).TextColor(text).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(180)
		if dir := path.Dir(p); dir != "." && !a.set.GitTreeView {
			ui.Text(c, dir).FontSize(12).TextColor(muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		ui.Box(c).Grow(1)
		ui.Text(c, string(r.file.status)).Font(a.codeFont()).FontSize(12).TextColor(tint).Shrink(0)
		a.stageBox(c, r.key.sec, []change{*r.file}, false)
	})
}

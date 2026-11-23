package main

import (
	"bufio"
	"encoding/json"
	"maps"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
)

// session is what the app keeps of its windows between runs, as Sublime
// Text's hot exit does: the tabs, with the text of those not saved, the
// layout and the folders; and what Open Recent lists.
type session struct {
	Windows []sessionWindow `json:"windows"`
	Recent  recentList      `json:"recent"`
}

type sessionWindow struct {
	Layout      string                                `json:"layout"`
	Cols        []float32                             `json:"cols,omitempty"`
	Rows        []float32                             `json:"rows,omitempty"`
	Focus       int                                   `json:"focus"`
	Groups      []sessionGroup                        `json:"groups"`
	Folders     []string                              `json:"folders,omitempty"`
	Project     string                                `json:"project,omitempty"`
	ProjectMeta map[string]json.RawMessage            `json:"project_meta,omitempty"`
	FolderMeta  map[string]map[string]json.RawMessage `json:"folder_meta,omitempty"`
	Bounds      mygo.Rectangle                        `json:"bounds"`
	// SideBar is one of panelNames; older sessions, without it, open
	// with settings' side bar.
	SideBar string `json:"side_bar,omitempty"`
}

var panelNames = [...]string{noPanel: "none", filesPanel: "files", gitPanel: "git"}

type sessionGroup struct {
	Active int          `json:"active"`
	Docs   []sessionDoc `json:"docs"`
}

type sessionDoc struct {
	Path   string   `json:"path,omitempty"`
	Text   *string  `json:"text,omitempty"`
	Syntax string   `json:"syntax"`
	Sels   [][2]int `json:"sels,omitempty"`
	// Raw is the unsaved text in place of Text when it isn't UTF-8, as of
	// a GBK file, which a JSON string would turn into U+FFFD.
	Raw []byte `json:"raw,omitempty"`
	// TabSize and TranslateTabsToSpaces are the file's own indentation,
	// unset where it follows the settings.
	TabSize               int    `json:"tab_size,omitempty"`
	TranslateTabsToSpaces *bool  `json:"translate_tabs_to_spaces,omitempty"`
	LineEnding            string `json:"line_ending,omitempty"`
	// Encoding is the name of the file's encoding, as Sublime Text names
	// it; sessions before encodings have none.
	Encoding string `json:"encoding,omitempty"`
	// unsaved is the text Text or Raw takes as the session is written, off the
	// main thread.
	unsaved *editor.Snapshot
}

func (l *lime) sessionPath() string {
	if l.dir == "" {
		return ""
	}
	return filepath.Join(l.dir, "session.json")
}

func (l *lime) session() session { return l.sessionFor(false) }

func (l *lime) sessionFor(autosave bool) session {
	s := session{Recent: recentList{slices.Clone(l.recent.Files), slices.Clone(l.recent.Folders)}}
	for _, a := range l.wins {
		s.Windows = append(s.Windows, a.sessionFor(autosave))
	}
	return s
}

func (a *app) session() sessionWindow { return a.sessionFor(false) }

func (a *app) sessionFor(autosave bool) sessionWindow {
	s := sessionWindow{Layout: a.layout.name, Cols: slices.Clone(a.cols), Rows: slices.Clone(a.rows), Focus: a.focus,
		Folders: slices.Clone(a.proj.folders), Project: a.proj.path, ProjectMeta: a.proj.meta, FolderMeta: maps.Clone(a.proj.folderMeta), SideBar: panelNames[a.side]}
	if a.win != nil {
		s.Bounds = a.win.Bounds()
	}
	for _, g := range a.groups {
		sg := sessionGroup{Active: g.active, Docs: []sessionDoc{}}
		for i, d := range g.docs {
			// A diff tab's texts and a preview's are kept in their own
			// tabs, git keeps a commit's, and a search is run again.
			if d.kind == diffDoc || d.kind == commitDoc || d.kind == findDoc || d.kind == mdDoc {
				if i < g.active {
					sg.Active--
				}
				continue
			}
			sd := sessionDoc{Path: d.path, Syntax: syntaxName(d.st.Buffer.Language()), TabSize: d.tabSize, TranslateTabsToSpaces: translateTabs(d.indent)}
			if d.enc != nil {
				sd.Encoding = d.enc.name
			}
			if d.dirty() {
				snap := d.st.Buffer.Snapshot()
				// Periodic autosave skips oversized unsaved buffers to avoid
				// flattening and writing hundreds of MB every few seconds;
				// explicit saveSession on quit still preserves them.
				if !autosave || snap.Len() <= largeFile {
					sd.unsaved, sd.LineEnding = &snap, endingType(d.lineEnding())
				}
			}
			for _, sel := range d.st.Selections() {
				sd.Sels = append(sd.Sels, [2]int{sel.Anchor, sel.Head})
			}
			sg.Docs = append(sg.Docs, sd)
		}
		s.Groups = append(s.Groups, sg)
	}
	return s
}

func (l *lime) saveSession() error {
	l.sessionGen++
	return l.writeSession(l.session(), l.sessionGen)
}

// writeSession writes s, the session numbered gen, unless a later one is
// written already, by way of a temporary file so that a write cut short
// leaves the last one whole. It copies the unsaved texts out of their
// snapshots, which it can do on any goroutine.
func (l *lime) writeSession(s session, gen uint64) error {
	p := l.sessionPath()
	if p == "" {
		return nil
	}
	l.writing.Lock()
	defer l.writing.Unlock()
	if gen < l.written {
		return nil
	}
	for _, w := range s.Windows {
		for _, g := range w.Groups {
			for i := range g.Docs {
				if u := g.Docs[i].unsaved; u != nil {
					text := u.String()
					g.Docs[i].unsaved = nil
					if utf8.ValidString(text) {
						g.Docs[i].Text = &text
					} else {
						g.Docs[i].Raw = []byte(text)
					}
				}
			}
		}
	}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(f)
	err = json.NewEncoder(bw).Encode(s)
	if err == nil {
		err = bw.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	l.written = gen
	return nil
}

// sessionChanged tells the sessions apart cheaply, by what an edit, a
// tab or the layout changes.
func (l *lime) sessionChanged() bool {
	b := l.sessionKeyBuf[:0]
	writeBool := func(v bool) {
		if v {
			b = append(b, '1')
		} else {
			b = append(b, '0')
		}
	}
	for _, f := range l.recent.Files {
		b = append(b, 'F')
		b = append(b, f...)
		b = append(b, 0)
	}
	for _, f := range l.recent.Folders {
		b = append(b, 'D')
		b = append(b, f...)
		b = append(b, 0)
	}
	for _, a := range l.wins {
		b = append(b, '#')
		b = append(b, a.layout.name...)
		for _, v := range a.cols {
			b = append(b, ':')
			b = strconv.AppendUint(b, uint64(math.Float32bits(v)), 10)
		}
		b = append(b, ';')
		for _, v := range a.rows {
			b = append(b, ':')
			b = strconv.AppendUint(b, uint64(math.Float32bits(v)), 10)
		}
		b = append(b, ':')
		b = strconv.AppendInt(b, int64(a.focus), 10)
		b = append(b, 0)
		b = append(b, a.proj.path...)
		for _, f := range a.proj.folders {
			b = append(b, 0)
			b = append(b, f...)
		}
		b = append(b, ':')
		b = strconv.AppendInt(b, int64(a.side), 10)
		for _, g := range a.groups {
			b = append(b, '|')
			b = strconv.AppendInt(b, int64(g.active), 10)
			for _, d := range g.docs {
				b = append(b, ',')
				b = append(b, d.path...)
				b = append(b, 0)
				b = strconv.AppendUint(b, d.st.Buffer.Version(), 10)
				writeBool(d.dirty())
				b = append(b, syntaxName(d.st.Buffer.Language())...)
				b = append(b, ':')
				b = strconv.AppendInt(b, int64(d.tabSize), 10)
				b = append(b, ':')
				b = strconv.AppendInt(b, int64(d.indent), 10)
				b = append(b, d.lineEnding()...)
				b = append(b, 0)
				b = append(b, d.encoding().name...)
			}
		}
	}
	l.sessionKeyBuf = b
	l.sessionMu.Lock()
	changed := string(b) != l.sessionKey
	if changed {
		l.sessionKey = string(b)
	}
	l.sessionMu.Unlock()
	return changed
}

// autosave keeps the session as it changes, for a run that ends without
// closing the windows, as the system restarting or the app crashing.
func (l *lime) autosave() bool {
	if l.quitting || len(l.wins) == 0 || !l.sessionChanged() {
		return false
	}
	l.sessionGen++
	gen, s := l.sessionGen, l.sessionFor(true)
	l.sessionMu.Lock()
	l.sessionKeyGen = gen
	l.pendingSession = &s
	l.pendingGen = gen
	start := !l.autosaving
	if start {
		l.autosaving = true
	}
	l.sessionMu.Unlock()
	if !start {
		return true
	}
	go func() {
		for {
			l.sessionMu.Lock()
			next, nextGen := l.pendingSession, l.pendingGen
			l.pendingSession = nil
			if next == nil {
				l.autosaving = false
				l.sessionMu.Unlock()
				return
			}
			l.sessionMu.Unlock()
			if err := l.writeSession(*next, nextGen); err != nil {
				l.sessionMu.Lock()
				if l.sessionKeyGen == nextGen && l.pendingSession == nil {
					l.sessionKey = ""
				}
				l.sessionMu.Unlock()
			}
		}
	}()
	return true
}

func (l *lime) restoreSession() {
	p := l.sessionPath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var s struct {
		session
		sessionWindow // of a run before Lime had several windows
	}
	if json.Unmarshal(b, &s) != nil {
		return
	}
	if len(s.Windows) == 0 && len(s.Groups) > 0 {
		s.Windows = append(s.Windows, s.sessionWindow)
	}
	l.recent = s.Recent
	for _, sw := range s.Windows {
		l.newWindow().restore(sw)
	}
	l.sessionChanged()
}

var syntaxByName = sync.OnceValue(func() map[string]*editor.Language {
	list := syntaxes()
	m := make(map[string]*editor.Language, len(list))
	for _, l := range list {
		m[syntaxName(l)] = l
	}
	return m
})

type restoredFile struct {
	sd   sessionDoc
	kind docKind
	disk string
	enc  *fileEncoding
	st   stamp
	ok   bool
}

func prepareRestoreDoc(sd sessionDoc, fallback string) restoredFile {
	if sd.Raw != nil {
		text := string(sd.Raw)
		sd.Text = &text
	}
	res := restoredFile{sd: sd, kind: textDoc, enc: encodingNamed(sd.Encoding), ok: true}
	if sd.Path == "" {
		return res
	}
	if k := fileKind(sd.Path); k != textDoc && sd.Text == nil {
		res.kind = k
		res.st = stampOf(sd.Path)
		return res
	}
	raw, st, err := readTextWithStamp(sd.Path)
	if err != nil && sd.Text == nil {
		res.ok = false
		return res
	}
	res.st = st
	switch {
	case res.enc != nil:
		res.disk, res.enc = reopenText(raw, res.enc, fallback)
	// The bytes kept of a file that isn't UTF-8 are the file's own,
	// which the buffer held as they were.
	case sd.Raw != nil:
		res.disk, res.enc = reopenText(raw, utf8Encoding, fallback)
	default:
		res.disk, res.enc = openText(raw, fallback)
	}
	return res
}

func buildRestoredDoc(r restoredFile) *doc {
	if !r.ok {
		return nil
	}
	sd := r.sd
	if r.kind != textDoc {
		d := newDocWithStamp(sd.Path, "", r.st)
		d.kind = r.kind
		return d
	}
	d := newDocWithStamp(sd.Path, r.disk, r.st)
	d.enc = r.enc
	d.tabSize, d.indent = sd.TabSize, indentStyle(sd.TranslateTabsToSpaces)
	if sd.Text != nil {
		d.st.Buffer.Set(*sd.Text)
	}
	for _, e := range lineEndings {
		if e.typ == sd.LineEnding {
			d.setLineEnding(e.text)
		}
	}
	if l, ok := syntaxByName()[sd.Syntax]; ok {
		d.st.Buffer.SetLanguage(l)
	}
	var sels []editor.Selection
	n := d.st.Buffer.Len()
	for _, s := range sd.Sels {
		sels = append(sels, editor.Selection{Anchor: min(max(s[0], 0), n), Head: min(max(s[1], 0), n)})
	}
	if len(sels) > 0 {
		d.st.SetSelections(sels...)
	}
	return d
}

func (a *app) restore(s sessionWindow) {
	for _, l := range layouts {
		if l.name == s.Layout {
			a.setLayout(l)
		}
	}
	a.cols, a.rows, a.bounds = s.Cols, s.Rows, s.Bounds
	totalDocs := 0
	for gi, sg := range s.Groups {
		if gi >= len(a.groups) {
			break
		}
		totalDocs += len(sg.Docs)
	}
	prepared := make([][]restoredFile, min(len(s.Groups), len(a.groups)))
	fallback := a.set.FallbackEncoding
	if totalDocs > 1 {
		var wg sync.WaitGroup
		slots := make(chan struct{}, min(totalDocs, max(runtime.GOMAXPROCS(0), 2)))
		for gi := range prepared {
			sg := s.Groups[gi]
			prepared[gi] = make([]restoredFile, len(sg.Docs))
			for di, sd := range sg.Docs {
				wg.Go(func() {
					slots <- struct{}{}
					defer func() { <-slots }()
					prepared[gi][di] = prepareRestoreDoc(sd, fallback)
				})
			}
		}
		wg.Wait()
	} else {
		for gi := range prepared {
			sg := s.Groups[gi]
			prepared[gi] = make([]restoredFile, len(sg.Docs))
			for di, sd := range sg.Docs {
				prepared[gi][di] = prepareRestoreDoc(sd, fallback)
			}
		}
	}
	for gi, files := range prepared {
		g := a.groups[gi]
		for _, rf := range files {
			if d := buildRestoredDoc(rf); d != nil {
				// A file following the settings, as all did in sessions before
				// files had their own indentation, detects its own.
				if d.tabSize == 0 && d.indent == editor.IndentAuto && a.set.DetectIndentation {
					d.guessIndent()
				}
				g.docs = append(g.docs, d)
			}
		}
		g.active = min(max(s.Groups[gi].Active, 0), len(g.docs)-1)
	}
	a.proj.meta, a.proj.folderMeta = s.ProjectMeta, s.FolderMeta
	if s.Project != "" && s.ProjectMeta == nil && s.FolderMeta == nil {
		if pf, err := readProject(s.Project); err == nil {
			a.proj.loadProjectFields(s.Project, pf)
		}
	}
	a.proj.folders, a.proj.path = s.Folders, s.Project
	if i := slices.Index(panelNames[:], s.SideBar); i >= 0 {
		a.side = sidePanel(i)
	}
	a.focusGroup(min(max(s.Focus, 0), len(a.groups)-1))
}

// restoreDoc makes the tab sd kept: the file as it is on disk, with the
// unsaved text over it as an edit that Undo takes back. A file gone with
// nothing unsaved is left out. A file of a session before encodings opens
// in the one it has, or fallback names.
func restoreDoc(sd sessionDoc, fallback string) *doc {
	return buildRestoredDoc(prepareRestoreDoc(sd, fallback))
}

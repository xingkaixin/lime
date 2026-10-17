package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
)

// session is what the app keeps of its windows between runs, as Sublime
// Text's hot exit does: the tabs, with the text of those not saved, the
// layout and the folders.
type session struct {
	Windows []sessionWindow `json:"windows"`
}

type sessionWindow struct {
	Layout  string         `json:"layout"`
	Cols    []float32      `json:"cols,omitempty"`
	Rows    []float32      `json:"rows,omitempty"`
	Focus   int            `json:"focus"`
	Groups  []sessionGroup `json:"groups"`
	Folders []string       `json:"folders,omitempty"`
	Project string         `json:"project,omitempty"`
	Bounds  mygo.Rectangle `json:"bounds"`
}

type sessionGroup struct {
	Active int          `json:"active"`
	Docs   []sessionDoc `json:"docs"`
}

type sessionDoc struct {
	Path   string   `json:"path,omitempty"`
	Text   *string  `json:"text,omitempty"` // the unsaved text, when there is some
	Syntax string   `json:"syntax"`
	Sels   [][2]int `json:"sels,omitempty"`
	// unsaved is the text Text takes as the session is written, off the
	// main thread.
	unsaved *editor.Snapshot
}

func (l *lime) sessionPath() string {
	if l.dir == "" {
		return ""
	}
	return filepath.Join(l.dir, "session.json")
}

func (l *lime) session() session {
	var s session
	for _, a := range l.wins {
		s.Windows = append(s.Windows, a.session())
	}
	return s
}

func (a *app) session() sessionWindow {
	s := sessionWindow{Layout: a.layout.name, Cols: slices.Clone(a.cols), Rows: slices.Clone(a.rows), Focus: a.focus,
		Folders: slices.Clone(a.proj.folders), Project: a.proj.path}
	if a.win != nil {
		s.Bounds = a.win.Bounds()
	}
	for _, g := range a.groups {
		sg := sessionGroup{Active: g.active, Docs: []sessionDoc{}}
		for _, d := range g.docs {
			sd := sessionDoc{Path: d.path, Syntax: syntaxName(d.st.Buffer.Language())}
			if d.dirty() {
				snap := d.st.Buffer.Snapshot()
				sd.unsaved = &snap
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

// saveSession writes the session now, as on quitting.
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
					g.Docs[i].Text = &text
				}
			}
		}
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(p+".tmp", p); err != nil {
		return err
	}
	l.written = gen
	return nil
}

// sessionChanged tells the sessions apart cheaply, by what an edit, a
// tab or the layout changes.
func (l *lime) sessionChanged() bool {
	var b strings.Builder
	for _, a := range l.wins {
		fmt.Fprint(&b, "#", a.layout.name, a.cols, a.rows, a.focus, a.proj.path, a.proj.folders)
		for _, g := range a.groups {
			fmt.Fprint(&b, "|", g.active)
			for _, d := range g.docs {
				fmt.Fprint(&b, ",", d.path, d.st.Buffer.Version(), d.dirty(), syntaxName(d.st.Buffer.Language()))
			}
		}
	}
	key := b.String()
	changed := key != l.sessionKey
	l.sessionKey = key
	return changed
}

// autosave keeps the session as it changes, for a run that ends without
// closing the windows, as the system restarting or the app crashing.
func (l *lime) autosave() {
	if !l.quitting && len(l.wins) > 0 && l.sessionChanged() {
		l.sessionGen++
		go l.writeSession(l.session(), l.sessionGen)
	}
}

// restoreSession brings back the windows the last run kept.
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
	for _, sw := range s.Windows {
		l.newWindow().restore(sw)
	}
	l.sessionChanged()
}

func (a *app) restore(s sessionWindow) {
	for _, l := range layouts {
		if l.name == s.Layout {
			a.setLayout(l)
		}
	}
	a.cols, a.rows, a.bounds = s.Cols, s.Rows, s.Bounds
	for gi, sg := range s.Groups {
		if gi >= len(a.groups) {
			break
		}
		g := a.groups[gi]
		for _, sd := range sg.Docs {
			if d := restoreDoc(sd); d != nil {
				g.docs = append(g.docs, d)
			}
		}
		g.active = min(max(sg.Active, 0), len(g.docs)-1)
	}
	a.proj.folders, a.proj.path = s.Folders, s.Project
	a.focusGroup(min(max(s.Focus, 0), len(a.groups)-1))
}

// restoreDoc makes the tab sd kept: the file as it is on disk, with the
// unsaved text over it as an edit that Undo takes back. A file gone with
// nothing unsaved is left out.
func restoreDoc(sd sessionDoc) *doc {
	var disk string
	if sd.Path != "" {
		text, err := readText(sd.Path)
		if err != nil && sd.Text == nil {
			return nil
		}
		disk = text
	}
	d := newDoc(sd.Path, disk)
	if sd.Text != nil {
		d.st.Buffer.Set(*sd.Text)
	}
	for _, l := range syntaxes() {
		if syntaxName(l) == sd.Syntax {
			d.st.Buffer.SetLanguage(l)
		}
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

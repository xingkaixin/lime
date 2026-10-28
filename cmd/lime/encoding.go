package main

import (
	"cmp"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"

	"lime/locales"
)

// fileEncoding is an encoding files are read and saved in, by Sublime
// Text's name for it. The buffer holds the text as UTF-8 whatever the
// file's encoding.
type fileEncoding struct {
	name  string
	label func(locales.Messages) string // nil shows name
	enc   encoding.Encoding             // nil for UTF-8
	bom   string
}

var utf8Encoding = &fileEncoding{name: "UTF-8"}

// encodings are the ones of Sublime Text's that the files of most
// languages are in.
var encodings = []*fileEncoding{
	utf8Encoding,
	{name: "UTF-8 with BOM", bom: "\xef\xbb\xbf"},
	{name: "UTF-16 LE with BOM", enc: unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), bom: "\xff\xfe"},
	{name: "UTF-16 BE with BOM", enc: unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM), bom: "\xfe\xff"},
	{name: defaultFallback, label: locales.Messages.EncodingWindows1252, enc: charmap.Windows1252},
	{name: "Western (ISO 8859-1)", label: locales.Messages.EncodingISO88591, enc: charmap.ISO8859_1},
	{name: "GBK", enc: simplifiedchinese.GBK},
	{name: "GB18030", enc: simplifiedchinese.GB18030},
	{name: "Big5", enc: traditionalchinese.Big5},
	{name: "Shift JIS", enc: japanese.ShiftJIS},
	{name: "EUC-KR", enc: korean.EUCKR},
}

var errNotUTF8 = errors.New("text isn't UTF-8")

// defaultFallback is Sublime Text's fallback_encoding.
const defaultFallback = "Western (Windows 1252)"

func (e *fileEncoding) title(m locales.Messages) string {
	if e.label != nil {
		return e.label(m)
	}
	return e.name
}

func (e *fileEncoding) reopenID() string { return "reopen " + e.name }
func (e *fileEncoding) saveID() string   { return "save " + e.name }

// encodingNamed is the encoding of name, nil for none.
func encodingNamed(name string) *fileEncoding {
	for _, e := range encodings {
		if strings.EqualFold(e.name, name) {
			return e
		}
	}
	return nil
}

// detectEncoding is the encoding of raw, a file's bytes: the one its BOM
// says, else UTF-8 where raw is UTF-8, else the fallback named.
func detectEncoding(raw, fallback string) *fileEncoding {
	for _, e := range encodings {
		if e.bom != "" && strings.HasPrefix(raw, e.bom) {
			return e
		}
	}
	if utf8.ValidString(raw) {
		return utf8Encoding
	}
	return cmp.Or(encodingNamed(fallback), encodingNamed(defaultFallback))
}

// decode is raw, bytes in e, as UTF-8 without e's BOM, and whether the
// text saves back as raw was. Bytes e has no character for decode to
// U+FFFD, which would save as another character; UTF-8 keeps them as
// they are.
func (e *fileEncoding) decode(raw string) (string, bool) {
	raw = strings.TrimPrefix(raw, e.bom)
	if e.enc == nil {
		return raw, true
	}
	text, err := e.enc.NewDecoder().String(raw)
	if err != nil {
		return "", false
	}
	back, err := e.enc.NewEncoder().String(text)
	return text, err == nil && back == raw
}

// openText is raw, a file's bytes, as UTF-8, and the encoding it is in:
// as detectEncoding has it, or UTF-8 where that would not save back as
// raw was, which keeps the bytes as they are.
func openText(raw, fallback string) (string, *fileEncoding) {
	e := detectEncoding(raw, fallback)
	if text, ok := e.decode(raw); ok {
		return text, e
	}
	return raw, utf8Encoding
}

// reopenText is raw, the file of a doc in e read again, in e where its
// BOM, or its characters past ASCII being UTF-8, don't say otherwise and
// it saves back as it was; else as openText has it.
func reopenText(raw string, e *fileEncoding, fallback string) (string, *fileEncoding) {
	if d := detectEncoding(raw, e.name); d == e || e.bom == "" && d == utf8Encoding && ascii(raw) {
		if text, ok := e.decode(raw); ok {
			return text, e
		}
	}
	return openText(raw, fallback)
}

func ascii(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// encode is text, UTF-8, in e and after e's BOM, or the error of a
// character e has none for. Bytes of text that aren't UTF-8 are an error
// too, which the encoders of x/text would save as U+FFFD.
func (e *fileEncoding) encode(text []byte) ([]byte, error) {
	if e.enc != nil {
		if !utf8.Valid(text) {
			return nil, errNotUTF8
		}
		var err error
		if text, err = e.enc.NewEncoder().Bytes(text); err != nil {
			return nil, err
		}
	}
	if e.bom == "" {
		return text, nil
	}
	return append([]byte(e.bom), text...), nil
}

// encoding is the encoding of d's file, UTF-8 for a new one.
func (d *doc) encoding() *fileEncoding { return cmp.Or(d.enc, utf8Encoding) }

func encodingCommands() []command {
	var c []command
	for _, e := range encodings {
		c = append(c,
			command{id: e.reopenID(), caption: func(m locales.Messages) string { return m.CmdReopenWithEncoding(e.title(m)) },
				run: withDoc(func(a *app, d *doc) { a.reopen(d, e) })},
			command{id: e.saveID(), caption: func(m locales.Messages) string { return m.CmdSaveWithEncoding(e.title(m)) },
				run: withSource(func(a *app, d *doc) {
					if d.path == "" {
						d.enc = e
						a.saveAs(d)
						return
					}
					a.writeIn(d, d.path, e)
				})})
	}
	return c
}

// reopenable reports whether d's file can be read again in another
// encoding: not over changes not saved, as in Sublime Text, which would
// lose them.
func reopenable(d *doc) bool { return d.kind == textDoc && d.path != "" && !d.dirty() }

// reopen reads d's file again in e.
func (a *app) reopen(d *doc, e *fileEncoding) {
	if !reopenable(d) {
		if d.path != "" {
			a.notifyError(locales.StatusReopenUnsaved())
		}
		return
	}
	raw, err := readText(d.path)
	if err != nil {
		go mygo.Dialog.Error(locales.DialogUnableToOpen(filepath.Base(d.path)), err.Error())
		return
	}
	text, ok := e.decode(raw)
	if !ok {
		a.notifyError(locales.StatusNotInEncoding(e.title(locales.In(i18n.Locale()))))
		return
	}
	text, ending := splitLines(text)
	d.enc = e
	d.st.Buffer.Reset(text)
	d.st.Select(0, 0)
	d.setLineEnding(ending)
	d.saved, d.savedEnding = d.st.Buffer.Snapshot(), ending
	d.diffDisk()
	a.l.diskChanged(d)
	d.disk = stampOf(d.path)
}

// encodingButton shows d's encoding, and reopens or saves d in another
// from a menu, as Sublime Text's status bar does.
func (a *app) encodingButton(c *ui.Context, d *doc) {
	tx := locales.In(i18n.Locale())
	name := d.encoding().title(tx)
	// The longest of the status bar's items gives way in a narrow window.
	a.statusMenu(c, name, locales.StatusEncoding(name), func(m *ui.Menu) {
		m.Submenu(locales.MenuReopenWithEncoding(), func(m *ui.Menu) {
			for _, e := range encodings {
				if m.Item(e.title(tx)).Chosen() {
					a.run(e.reopenID())
				}
			}
		}).Disabled(!reopenable(d))
		m.Submenu(locales.MenuSaveWithEncoding(), func(m *ui.Menu) {
			for _, e := range encodings {
				if m.Item(e.title(tx)).Chosen() {
					a.run(e.saveID())
				}
			}
		})
	}).Shrink(1).MinWidth(0)
}

package main

import (
	"cmp"
	"strings"
	"unicode/utf8"

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

// defaultFallback is Sublime Text's fallback_encoding.
const defaultFallback = "Western (Windows 1252)"

func (e *fileEncoding) title(m locales.Messages) string {
	if e.label != nil {
		return e.label(m)
	}
	return e.name
}

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

// decode is raw, bytes in e, as UTF-8 without e's BOM. Bytes e has no
// character for decode to U+FFFD, but for UTF-8, which keeps them.
func (e *fileEncoding) decode(raw string) string {
	raw = strings.TrimPrefix(raw, e.bom)
	if e.enc == nil {
		return raw
	}
	text, err := e.enc.NewDecoder().String(raw)
	if err != nil {
		return raw
	}
	return text
}

// encode is text, UTF-8, in e and after e's BOM, or the error of a
// character e has none for.
func (e *fileEncoding) encode(text []byte) ([]byte, error) {
	if e.enc != nil {
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

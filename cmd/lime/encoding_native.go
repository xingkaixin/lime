//go:build !js

package main

import (
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"

	"lime/locales"
)

type xTextCodec struct{ enc encoding.Encoding }

func (c xTextCodec) decode(raw string) (string, bool) {
	text, err := c.enc.NewDecoder().String(raw)
	if err != nil {
		return "", false
	}
	back, err := c.enc.NewEncoder().String(text)
	return text, err == nil && back == raw
}

func (c xTextCodec) encode(text []byte) ([]byte, error) {
	return c.enc.NewEncoder().Bytes(text)
}

// encodings are the ones of Sublime Text's that the files of most
// languages are in.
var encodings = []*fileEncoding{
	utf8Encoding,
	{name: "UTF-8 with BOM", bom: "\xef\xbb\xbf"},
	{name: "UTF-16 LE with BOM", codec: xTextCodec{unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)}, bom: "\xff\xfe"},
	{name: "UTF-16 BE with BOM", codec: xTextCodec{unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)}, bom: "\xfe\xff"},
	{name: defaultFallback, label: locales.Messages.EncodingWindows1252, codec: xTextCodec{charmap.Windows1252}},
	{name: "Western (ISO 8859-1)", label: locales.Messages.EncodingISO88591, codec: xTextCodec{charmap.ISO8859_1}},
	{name: "GBK", codec: xTextCodec{simplifiedchinese.GBK}},
	{name: "GB18030", codec: xTextCodec{simplifiedchinese.GB18030}},
	{name: "Big5", codec: xTextCodec{traditionalchinese.Big5}},
	{name: "Shift JIS", codec: xTextCodec{japanese.ShiftJIS}},
	{name: "EUC-KR", codec: xTextCodec{korean.EUCKR}},
}

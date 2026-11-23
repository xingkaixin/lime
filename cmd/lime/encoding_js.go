//go:build js

package main

import (
	"errors"
	"strings"
	"sync"
	"syscall/js"
	"unicode/utf16"
	"unicode/utf8"

	"lime/locales"
)

var errUnrepresentable = errors.New("character cannot be encoded")

// utf16Codec implements UTF-16 LE/BE without linking golang.org/x/text/encoding.
type utf16Codec struct{ bigEndian bool }

func (c utf16Codec) decode(raw string) (string, bool) {
	if len(raw)%2 != 0 {
		return "", false
	}
	u16 := make([]uint16, len(raw)/2)
	for i := range u16 {
		if c.bigEndian {
			u16[i] = uint16(raw[2*i])<<8 | uint16(raw[2*i+1])
		} else {
			u16[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
		}
	}
	var b strings.Builder
	b.Grow(len(u16))
	for i := 0; i < len(u16); i++ {
		r := rune(u16[i])
		switch {
		case utf16.IsSurrogate(r):
			if r >= 0xdc00 || i+1 >= len(u16) {
				return "", false
			}
			r2 := rune(u16[i+1])
			if r2 < 0xdc00 || r2 > 0xdfff {
				return "", false
			}
			b.WriteRune(utf16.DecodeRune(r, r2))
			i++
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), true
}

func (c utf16Codec) encode(text []byte) ([]byte, error) {
	var out []byte
	for len(text) > 0 {
		r, sz := utf8.DecodeRune(text)
		text = text[sz:]
		r1, r2 := utf16.EncodeRune(r)
		if r1 == 0xfffd && r2 == 0xfffd {
			u := uint16(r)
			if c.bigEndian {
				out = append(out, byte(u>>8), byte(u))
			} else {
				out = append(out, byte(u), byte(u>>8))
			}
		} else {
			for _, u := range [2]uint16{uint16(r1), uint16(r2)} {
				if c.bigEndian {
					out = append(out, byte(u>>8), byte(u))
				} else {
					out = append(out, byte(u), byte(u>>8))
				}
			}
		}
	}
	return out, nil
}

// iso88591Codec maps bytes 0x00..0xFF 1:1 to U+0000..U+00FF (avoiding WHATWG's
// TextDecoder("iso-8859-1") alias to Windows-1252).
type iso88591Codec struct{}

func (iso88591Codec) decode(raw string) (string, bool) {
	if isASCII(raw) {
		return raw, true
	}
	var b strings.Builder
	b.Grow(len(raw) * 2)
	for i := range len(raw) {
		b.WriteRune(rune(raw[i]))
	}
	return b.String(), true
}

func (iso88591Codec) encode(text []byte) ([]byte, error) {
	out := make([]byte, 0, len(text))
	for len(text) > 0 {
		r, sz := utf8.DecodeRune(text)
		text = text[sz:]
		if r > 0xff {
			return nil, errUnrepresentable
		}
		out = append(out, byte(r))
	}
	return out, nil
}

// win1252High maps bytes 0x80..0x9F for Windows-1252; 0 denotes undefined bytes.
var win1252High = [32]rune{
	0x20ac, 0, 0x201a, 0x0192, 0x201e, 0x2026, 0x2020, 0x2021,
	0x02c6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017d, 0,
	0, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014,
	0x02dc, 0x2122, 0x0161, 0x203a, 0x0153, 0, 0x017e, 0x0178,
}

var win1252Rev = func() map[rune]byte {
	m := make(map[rune]byte, 27)
	for i, r := range win1252High {
		if r != 0 {
			m[r] = byte(0x80 + i)
		}
	}
	return m
}()

type windows1252Codec struct{}

func (windows1252Codec) decode(raw string) (string, bool) {
	if isASCII(raw) {
		return raw, true
	}
	var b strings.Builder
	b.Grow(len(raw) * 2)
	for i := range len(raw) {
		c := raw[i]
		if c < 0x80 || c >= 0xa0 {
			b.WriteRune(rune(c))
			continue
		}
		r := win1252High[c-0x80]
		if r == 0 {
			return "", false
		}
		b.WriteRune(r)
	}
	return b.String(), true
}

func (windows1252Codec) encode(text []byte) ([]byte, error) {
	out := make([]byte, 0, len(text))
	for len(text) > 0 {
		r, sz := utf8.DecodeRune(text)
		text = text[sz:]
		switch {
		case r < 0x80 || (r >= 0xa0 && r <= 0xff):
			out = append(out, byte(r))
		default:
			b, ok := win1252Rev[r]
			if !ok {
				return nil, errUnrepresentable
			}
			out = append(out, b)
		}
	}
	return out, nil
}

type byteRange struct{ lo, hi byte }

// jsCJKCodec decodes via the browser's TextDecoder and lazily derives its
// reverse encode table from the same TextDecoder on first use, avoiding ~750 KB
// of static CJK lookup tables inside lime.wasm.
type jsCJKCodec struct {
	label   string
	single  []byteRange
	lead    []byteRange
	trail   []byteRange
	gb18030 bool
	once    sync.Once
	rev     map[rune][]byte
}

func (c *jsCJKCodec) initRev() {
	c.once.Do(func() {
		td := js.Global().Get("TextDecoder")
		if td.IsUndefined() {
			return
		}
		dec := td.New(c.label, map[string]any{"fatal": false})
		rev := make(map[rune][]byte, 16384)
		addSingle := func(rs []byteRange) {
			var bs []byte
			for _, rg := range rs {
				for b := int(rg.lo); b <= int(rg.hi); b++ {
					bs = append(bs, byte(b))
				}
			}
			if len(bs) == 0 {
				return
			}
			sep := make([]byte, len(bs)*2)
			for i, b := range bs {
				sep[i*2] = b
			}
			arr := js.Global().Get("Uint8Array").New(len(sep))
			js.CopyBytesToJS(arr, sep)
			parts := strings.Split(dec.Call("decode", arr).String(), "\x00")
			for i, b := range bs {
				if i >= len(parts) {
					break
				}
				r, sz := utf8.DecodeRuneInString(parts[i])
				if sz == len(parts[i]) && r != utf8.RuneError {
					if _, exists := rev[r]; !exists {
						rev[r] = []byte{b}
					}
				}
			}
		}
		addSingle(c.single)

		var pairs []uint16
		for _, lr := range c.lead {
			for l := int(lr.lo); l <= int(lr.hi); l++ {
				for _, tr := range c.trail {
					for t := int(tr.lo); t <= int(tr.hi); t++ {
						pairs = append(pairs, uint16(l)<<8|uint16(t))
					}
				}
			}
		}
		if len(pairs) > 0 {
			sep := make([]byte, len(pairs)*3)
			for i, p := range pairs {
				sep[i*3] = byte(p >> 8)
				sep[i*3+1] = byte(p)
			}
			arr := js.Global().Get("Uint8Array").New(len(sep))
			js.CopyBytesToJS(arr, sep)
			parts := strings.Split(dec.Call("decode", arr).String(), "\x00")
			for i, p := range pairs {
				if i >= len(parts) {
					break
				}
				r, sz := utf8.DecodeRuneInString(parts[i])
				if sz == len(parts[i]) && r != utf8.RuneError {
					if _, exists := rev[r]; !exists {
						rev[r] = []byte{byte(p >> 8), byte(p)}
					}
				}
			}
		}
		if c.gb18030 {
			// Derive all 39,420 BMP 4-byte GB18030 mappings directly from the
			// browser's TextDecoder so encoding matches the decoder's exact revision.
			const bmp4Count = 39420
			sep := make([]byte, bmp4Count*5)
			for ptr := uint32(0); ptr < bmp4Count; ptr++ {
				p := ptr
				b1 := byte(p/12600 + 0x81)
				p %= 12600
				b2 := byte(p/1260 + 0x30)
				p %= 1260
				b3 := byte(p/10 + 0x81)
				b4 := byte(p%10 + 0x30)
				base := int(ptr) * 5
				sep[base], sep[base+1], sep[base+2], sep[base+3] = b1, b2, b3, b4
			}
			arr := js.Global().Get("Uint8Array").New(len(sep))
			js.CopyBytesToJS(arr, sep)
			parts := strings.Split(dec.Call("decode", arr).String(), "\x00")
			for ptr := 0; ptr < bmp4Count && ptr < len(parts); ptr++ {
				r, sz := utf8.DecodeRuneInString(parts[ptr])
				// Pointer 39417 (84 31 A4 37) legitimately encodes U+FFFD in GB18030.
				if sz == len(parts[ptr]) && (r != utf8.RuneError || ptr == 39417) {
					if _, exists := rev[r]; !exists {
						base := ptr * 5
						rev[r] = []byte{sep[base], sep[base+1], sep[base+2], sep[base+3]}
					}
				}
			}
		}
		c.rev = rev
	})
}

func appendGB18030Ptr(dst []byte, ptr uint32) []byte {
	b1 := byte(ptr/12600 + 0x81)
	ptr %= 12600
	b2 := byte(ptr/1260 + 0x30)
	ptr %= 1260
	b3 := byte(ptr/10 + 0x81)
	b4 := byte(ptr%10 + 0x30)
	return append(dst, b1, b2, b3, b4)
}

func (c *jsCJKCodec) encode(text []byte) ([]byte, error) {
	if ascii(string(text)) {
		return text, nil
	}
	c.initRev()
	if c.rev == nil {
		return nil, errUnrepresentable
	}
	out := make([]byte, 0, len(text))
	for len(text) > 0 {
		r, sz := utf8.DecodeRune(text)
		text = text[sz:]
		if r < 0x80 {
			out = append(out, byte(r))
			continue
		}
		if bs, ok := c.rev[r]; ok {
			out = append(out, bs...)
			continue
		}
		if c.gb18030 && r >= 0x10000 {
			out = appendGB18030Ptr(out, 189000+uint32(r-0x10000))
			continue
		}
		return nil, errUnrepresentable
	}
	return out, nil
}

func (c *jsCJKCodec) decode(raw string) (string, bool) {
	if isASCII(raw) {
		return raw, true
	}
	td := js.Global().Get("TextDecoder")
	if td.IsUndefined() {
		return "", false
	}
	buf := js.Global().Get("Uint8Array").New(len(raw))
	js.CopyBytesToJS(buf, []byte(raw))
	dec := td.New(c.label, map[string]any{"fatal": true})
	var out string
	ok := true
	func() {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		out = dec.Call("decode", buf).String()
	}()
	if !ok {
		return "", false
	}
	back, err := c.encode([]byte(out))
	return out, err == nil && string(back) == raw
}

var encodings = []*fileEncoding{
	utf8Encoding,
	{name: "UTF-8 with BOM", bom: "\xef\xbb\xbf"},
	{name: "UTF-16 LE with BOM", codec: utf16Codec{bigEndian: false}, bom: "\xff\xfe"},
	{name: "UTF-16 BE with BOM", codec: utf16Codec{bigEndian: true}, bom: "\xfe\xff"},
	{name: defaultFallback, label: locales.Messages.EncodingWindows1252, codec: windows1252Codec{}},
	{name: "Western (ISO 8859-1)", label: locales.Messages.EncodingISO88591, codec: iso88591Codec{}},
	{name: "GBK", codec: &jsCJKCodec{label: "gbk", single: []byteRange{{0x80, 0x80}}, lead: []byteRange{{0x81, 0xfe}}, trail: []byteRange{{0x40, 0x7e}, {0x80, 0xfe}}}},
	{name: "GB18030", codec: &jsCJKCodec{label: "gb18030", lead: []byteRange{{0x81, 0xfe}}, trail: []byteRange{{0x40, 0x7e}, {0x80, 0xfe}}, gb18030: true}},
	{name: "Big5", codec: &jsCJKCodec{label: "big5", lead: []byteRange{{0x81, 0xfe}}, trail: []byteRange{{0x40, 0x7e}, {0xa1, 0xfe}}}},
	{name: "Shift JIS", codec: &jsCJKCodec{label: "shift_jis", single: []byteRange{{0xa1, 0xdf}}, lead: []byteRange{{0x81, 0x9f}, {0xe0, 0xfc}}, trail: []byteRange{{0x40, 0x7e}, {0x80, 0xfc}}}},
	{name: "EUC-KR", codec: &jsCJKCodec{label: "euc-kr", lead: []byteRange{{0x81, 0xfe}}, trail: []byteRange{{0x41, 0x5a}, {0x61, 0x7a}, {0x81, 0xfe}}}},
}

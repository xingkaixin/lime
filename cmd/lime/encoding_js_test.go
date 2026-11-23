//go:build js

package main

import "testing"

func TestWasmNFCString(t *testing.T) {
	if got := nfcString("ascii.go"); got != "ascii.go" {
		t.Fatalf("ascii = %q", got)
	}
	if got := nfcString("中文.txt"); got != "中文.txt" {
		t.Fatalf("cjk = %q", got)
	}
	if got := nfcString("cafe\u0301.txt"); got != "caf\u00e9.txt" {
		t.Fatalf("nfd -> nfc = %q, want %q", got, "caf\u00e9.txt")
	}
}

func TestWasmLegacyEncodings(t *testing.T) {
	// ISO-8859-1 must map 0x80 to U+0080, not U+20AC.
	iso := encodingNamed("Western (ISO 8859-1)")
	dec, ok := iso.decode("\x80\xe9")
	if !ok || dec != "\u0080\u00e9" {
		t.Fatalf("iso decode = %q, ok=%v", dec, ok)
	}
	enc, err := iso.encode([]byte(dec))
	if err != nil || string(enc) != "\x80\xe9" {
		t.Fatalf("iso encode = %x, err=%v", enc, err)
	}

	// Windows-1252 round-trips café and rejects undefined 0x81.
	w1252 := encodingNamed(defaultFallback)
	if _, ok := w1252.decode("\x81"); ok {
		t.Fatal("win1252 should reject undefined byte 0x81")
	}
	enc, err = w1252.encode([]byte("café€"))
	if err != nil {
		t.Fatalf("win1252 encode café€: %v", err)
	}
	dec, ok = w1252.decode(string(enc))
	if !ok || dec != "café€" {
		t.Fatalf("win1252 roundtrip = %q ok=%v", dec, ok)
	}

	// CJK encodings round-trip via browser TextDecoder-backed reverse tables.
	cases := []struct {
		name string
		text string
	}{
		{"Shift JIS", "日本語ｶﾀｶﾅ\n"},
		{"GBK", "你好世界€\n"},
		{"GB18030", "你好世界€\u4dff\u9fa6😀\n"},
		{"Big5", "繁體中文\n"},
		{"EUC-KR", "한국어\n"},
	}
	for _, tc := range cases {
		e := encodingNamed(tc.name)
		raw, err := e.encode([]byte(tc.text))
		if err != nil {
			t.Fatalf("%s encode %q: %v", tc.name, tc.text, err)
		}
		got, ok := e.decode(string(raw))
		if !ok || got != tc.text {
			t.Fatalf("%s roundtrip = %q (ok=%v), want %q", tc.name, got, ok, tc.text)
		}
	}
	// Verify GB18030 exact bytes for € (A2 E3), U+4DFF (82 35 8F 32), U+9FA6 (82 35 8F 33).
	gb18030 := encodingNamed("GB18030")
	if b, err := gb18030.encode([]byte("€")); err != nil || string(b) != "\xa2\xe3" {
		t.Fatalf("GB18030 encode € = %x, err=%v, want a2e3", b, err)
	}
	if s, ok := gb18030.decode("\xa2\xe3"); !ok || s != "€" {
		t.Fatalf("GB18030 decode a2e3 = %q ok=%v, want €", s, ok)
	}
	if b, err := gb18030.encode([]byte("\u9fa6")); err != nil || string(b) != "\x82\x35\x8f\x33" {
		t.Fatalf("GB18030 encode U+9FA6 = %x, err=%v, want 82358f33", b, err)
	}
	if s, ok := gb18030.decode("\x82\x35\x8f\x33"); !ok || s != "\u9fa6" {
		t.Fatalf("GB18030 decode 82358f33 = %q ok=%v, want U+9FA6", s, ok)
	}
	if s, ok := gb18030.decode("\x82\x35\x8f\x32"); !ok || s != "\u4dff" {
		t.Fatalf("GB18030 decode 82358f32 = %q ok=%v, want U+4DFF", s, ok)
	}
	if b, err := gb18030.encode([]byte("\ufffd")); err != nil || string(b) != "\x84\x31\xa4\x37" {
		t.Fatalf("GB18030 encode U+FFFD = %x, err=%v, want 8431a437", b, err)
	}
	if s, ok := gb18030.decode("\x84\x31\xa4\x37"); !ok || s != "\ufffd" {
		t.Fatalf("GB18030 decode 8431a437 = %q ok=%v, want U+FFFD", s, ok)
	}
	// GBK encodes € as 0x80 (matching x/text GBK).
	gbk := encodingNamed("GBK")
	if b, err := gbk.encode([]byte("€")); err != nil || string(b) != "\x80" {
		t.Fatalf("GBK encode € = %x, err=%v, want 80", b, err)
	}
	// Unrepresentable emoji in Shift JIS returns an error (falls back to UTF-8 on save).
	if _, err := encodingNamed("Shift JIS").encode([]byte("😀")); err == nil {
		t.Fatal("Shift JIS should reject 😀")
	}
}

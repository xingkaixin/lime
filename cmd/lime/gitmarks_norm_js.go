//go:build js

package main

import "syscall/js"

// nfcString uses the browser's String.prototype.normalize("NFC") for non-ASCII
// paths so lime.wasm avoids linking golang.org/x/text/unicode/norm tables.
func nfcString(s string) string {
	if isASCII(s) {
		return s
	}
	return js.Global().Get("String").Get("prototype").Get("normalize").Call("call", s, "NFC").String()
}

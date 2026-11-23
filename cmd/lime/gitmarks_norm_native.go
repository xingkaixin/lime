//go:build !js

package main

import "golang.org/x/text/unicode/norm"

func nfcString(s string) string {
	if isASCII(s) {
		return s
	}
	return norm.NFC.String(s)
}

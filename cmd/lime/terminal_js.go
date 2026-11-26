//go:build js

package main

import "errors"

const terminalSupported = false

func newNativeTermSession(termSessionOpts) (termSession, error) {
	return nil, errors.ErrUnsupported
}

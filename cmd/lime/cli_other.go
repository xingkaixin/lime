//go:build !darwin && !windows

package main

import "errors"

const cliSupported = false

func installCLIPlatform() (string, error) {
	return "", errors.ErrUnsupported
}

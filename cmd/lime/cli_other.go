//go:build !darwin && !windows

package main

import "errors"

const cliSupported = false

func launchCLI([]string) error {
	return errors.ErrUnsupported
}

func installCLIPlatform() (string, error) {
	return "", errors.ErrUnsupported
}

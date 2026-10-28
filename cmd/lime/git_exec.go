//go:build !js

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// hideConsole keeps git from opening a console window on Windows.
var hideConsole = func(*exec.Cmd) {}

// gitWith runs git as runGit does, with input on its standard input.
func gitWith(dir, input string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// Reads alone leave the index as it is, for the git running elsewhere,
	// and git asks for no password on a terminal, which a window has none
	// of: the user's credential helper or ssh agent answers.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(input)
	hideConsole(cmd)
	var out, said bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &said
	if err := cmd.Run(); err != nil {
		// Some failures print to standard output, as pull's conflicts.
		if msg := strings.TrimSpace(said.String() + "\n" + out.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return out.String(), nil
}

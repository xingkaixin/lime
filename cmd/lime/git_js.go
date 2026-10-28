package main

import (
	"errors"
	"syscall/js"
)

// gitWith runs git as the page provides it, limeGit, as browsers run no
// programs: the website's demo keeps a repository in memory.
func gitWith(dir, input string, args ...string) (string, error) {
	git := js.Global().Get("limeGit")
	if git.Type() != js.TypeFunction {
		return "", errors.New("git is not available")
	}
	argv := make([]any, len(args))
	for i, a := range args {
		argv[i] = a
	}
	r := git.Invoke(dir, input, argv)
	if msg := r.Get("error"); msg.Truthy() {
		return "", errors.New(msg.String())
	}
	return r.Get("out").String(), nil
}

package main

import (
	"github.com/egoist/mygo"

	"lime/locales"
)

// push pushes the branch to its upstream, or with none, publishes it to
// origin as its upstream.
func (a *app) push() {
	if a.git.upstream || a.git.detached {
		a.remoteRun(locales.StatusPushing(), "push")
		return
	}
	a.remoteRun(locales.StatusPushing(), "push", "-u", "origin", a.git.branch)
}

func (a *app) pull()  { a.remoteRun(locales.StatusPulling(), "pull") }
func (a *app) fetch() { a.remoteRun(locales.StatusFetching(), "fetch") }

// remoteRun runs git with the remote in the background, one at a time,
// which the status bar tells of until it ends, and shows what git says
// of a failure.
func (a *app) remoteRun(status string, args ...string) {
	if a.remote != "" {
		return
	}
	a.remote = status
	dir := a.git.root
	// Only pull writes the index and the work tree.
	lock := args[0] == "pull"
	a.background(func() func() {
		if lock {
			gitWrites.Lock()
		}
		_, err := gitWith(dir, "", args...)
		if lock {
			gitWrites.Unlock()
		}
		return func() {
			a.remote = ""
			if err != nil {
				go mygo.Dialog.Error(locales.DialogGitFailed("git "+args[0]), err.Error())
			}
			a.refreshFolders()
			for _, w := range a.l.wins {
				w.reloadChanged()
			}
			a.readGit()
		}
	})
}

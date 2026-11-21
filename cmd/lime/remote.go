package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lime/locales"
)

var remoteTimeout = 60 * time.Second

func (a *app) push()  { a.remoteRun(locales.StatusPushing(), "push") }
func (a *app) pull()  { a.remoteRun(locales.StatusPulling(), "pull") }
func (a *app) fetch() { a.remoteRun(locales.StatusFetching(), "fetch") }

func (a *app) remoteRun(status, command string) {
	if a.remote != "" {
		return
	}
	a.remote = status
	dir := a.git.root
	// Only pull writes the index and the work tree.
	lock := command == "pull"
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
		defer cancel()
		args := []string{command}
		if command == "push" {
			args = []string{"-c", "push.autoSetupRemote=true", "push"}
		}
		var err error
		if lock {
			mu := repoLock(dir)
			mu.Lock()
			_, err = gitWithContext(ctx, dir, "", args...)
			mu.Unlock()
		} else {
			_, err = gitWithContext(ctx, dir, "", args...)
		}
		return func() {
			a.remote = ""
			if err != nil {
				go mygo.Dialog.Error(locales.DialogGitFailed("git "+command), err.Error())
			}
			a.refreshFolders()
			for _, w := range a.l.wins {
				w.reloadChanged()
			}
			a.readGit()
		}
	})
}

// syncButton counts the commits the branch is ahead of its upstream and
// behind it, as one button to keep room for the branch's name: a click
// pulls those behind first, as Zed's git panel does, or pushes.
func (a *app) syncButton(c *ui.Context) {
	var counts []string
	if a.git.ahead > 0 {
		counts = append(counts, "↑"+strconv.Itoa(a.git.ahead))
	}
	if a.git.behind > 0 {
		counts = append(counts, "↓"+strconv.Itoa(a.git.behind))
	}
	switch {
	case a.git.behind > 0:
		a.gitButton(c, nil, strings.Join(counts, " "), locales.CmdPull(), a.pull).Shrink(0)
	case a.git.ahead > 0:
		a.gitButton(c, nil, strings.Join(counts, " "), locales.CmdPush(), a.push).Shrink(0)
	}
}

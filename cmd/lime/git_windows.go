package main

import (
	"os/exec"
	"strconv"
	"syscall"
)

func init() {
	hideConsole = func(cmd *exec.Cmd) {
		// CREATE_NO_WINDOW: git is a console program, which would open a
		// console window from a GUI one.
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	}
}

func cancelGitTree(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
		hideConsole(kill)
		return kill.Run()
	}
}

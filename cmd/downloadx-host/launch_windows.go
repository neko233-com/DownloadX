//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const detachedProcess = 0x00000008
const createNewProcessGroup = 0x00000200

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}

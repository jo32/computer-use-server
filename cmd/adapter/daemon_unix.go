//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func detachCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

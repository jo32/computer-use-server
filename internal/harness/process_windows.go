//go:build windows

package harness

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}

func cleanupProcess(cmd *exec.Cmd) {}

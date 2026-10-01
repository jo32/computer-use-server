//go:build windows

package main

import "os/exec"

func detachCommand(*exec.Cmd) {}

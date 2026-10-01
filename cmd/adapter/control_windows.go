//go:build windows

package main

import (
	"computer-use-server/internal/server"
	"errors"
	"time"
)

// Keep the existing Windows web service working. Local CLI administration is
// currently supported by the macOS/Linux Unix socket interface.
func startLocalControl(*server.Server, runtimeInfo, func()) (func(), error) { return func() {}, nil }
func newControlClient(string) (*controlClient, error) {
	return nil, errors.New("local CLI control is supported on macOS and Linux; use the Windows browser console")
}
func controlClientWithTimeout(dir string, _ time.Duration) (*controlClient, error) {
	return newControlClient(dir)
}

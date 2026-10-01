//go:build windows

package main

import (
	"computer-use-server/internal/server"
	"errors"
)

// Keep the existing Windows web service working. Local CLI administration is
// currently supported by the macOS/Linux Unix socket interface.
func startLocalControl(*server.Server) (func(), error) { return func() {}, nil }
func newControlClient(string) (*controlClient, error) {
	return nil, errors.New("local CLI control is supported on macOS and Linux; use the Windows browser console")
}

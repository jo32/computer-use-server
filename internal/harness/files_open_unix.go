//go:build !windows

package harness

import (
	"os"
	"syscall"
)

// Nonblocking open lets the regular-file check reject FIFOs without hanging.
func openRead(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

//go:build windows

package harness

import "os"

func openRead(root *os.Root, path string) (*os.File, error) { return root.Open(path) }

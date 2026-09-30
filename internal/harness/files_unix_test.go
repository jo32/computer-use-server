//go:build !windows

package harness

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadFIFOIsRejectedWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := NewFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := registryForTest(t)
	f.Register(r)
	done := make(chan error, 1)
	go func() { _, err := invoke(t, r, "read_file", map[string]any{"path": "pipe"}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked")
	}
}

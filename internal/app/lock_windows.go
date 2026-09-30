//go:build windows

package app

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

func lockData(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	overlapped := &windows.Overlapped{}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("another ReadyRig process is using this data directory: %w", err)
	}
	return func() { windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlapped); f.Close() }, nil
}

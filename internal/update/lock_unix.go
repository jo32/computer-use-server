//go:build !windows

package update

import (
	"os"
	"syscall"
)

func lock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
func unlock(f *os.File)     { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }
func Reexec(exe string, args []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}

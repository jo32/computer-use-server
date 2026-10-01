//go:build !windows && !darwin

package update

import (
	"os"
	"syscall"
)

func Reexec(exe string, args []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}

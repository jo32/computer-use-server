package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func Reexec(exe string, args []string) error {
	contents := filepath.Dir(filepath.Dir(exe))
	bundle := filepath.Dir(contents)
	if filepath.Ext(bundle) == ".app" && filepath.Base(contents) == "Contents" && filepath.Base(filepath.Dir(exe)) == "MacOS" {
		// exec changes the process's PID version while AppKit/RunningBoard still
		// considers it to be terminating. LaunchServices must register a fresh
		// app process, otherwise macOS rejects its menu bar scene and activation.
		// -n avoids reactivating the old app before its shutdown completes.
		command := exec.Command("/usr/bin/open", append([]string{"-n", "-a", bundle, "--args"}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("relaunch app: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}

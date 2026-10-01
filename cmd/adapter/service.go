package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func systemdQuote(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("invalid service path")
	}
	// Specifiers still expand in quotes. ExecStart's ':' prefix below disables
	// environment expansion, so literal dollar signs in paths stay intact.
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return "\"" + strings.ReplaceAll(value, "\"", "\\\"") + "\"", nil
}

func serviceUnit(executable, dataDir string) (string, error) {
	exe, err := systemdQuote(executable)
	if err != nil {
		return "", err
	}
	data, err := systemdQuote(dataDir)
	if err != nil {
		return "", err
	}
	return "[Unit]\nDescription=ReadyRig local agent service\nAfter=network.target\n\n" +
		"[Service]\nType=simple\nExecStart=:" + exe + " serve --foreground --data-dir " + data + "\nRestart=on-failure\nRestartSec=5\nUMask=0077\n\n" +
		"[Install]\nWantedBy=default.target\n", nil
}

func manageService(args []string, dataDir string) error {
	if len(args) != 1 {
		return errors.New("usage: readyrig service install|start|stop|restart|status|uninstall|print")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	unit, err := serviceUnit(executable, dataDir)
	if err != nil {
		return err
	}
	if args[0] == "print" {
		fmt.Print(unit)
		return nil
	}
	legacyUnit := strings.Replace(unit, " serve --foreground --data-dir ", " serve --data-dir ", 1)
	if runtime.GOOS != "linux" {
		return errors.New("systemd service management requires Linux; use 'readyrig serve' on macOS")
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(configDir, "systemd", "user", "readyrig.service")
	run := func(arguments ...string) error {
		command := exec.Command("systemctl", append([]string{"--user"}, arguments...)...)
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		return command.Run()
	}
	switch args[0] {
	case "install":
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		// Do not replace an existing unit with a different executable or data directory.
		if existing, err := os.ReadFile(path); err == nil {
			if string(existing) == legacyUnit {
				if err := os.WriteFile(path, []byte(unit), 0600); err != nil {
					return err
				}
			} else if string(existing) != unit {
				return fmt.Errorf("%s already exists with different settings; uninstall it first", path)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		} else {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			_, err = f.WriteString(unit)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		if err := run("daemon-reload"); err != nil {
			return fmt.Errorf("saved %s, but the systemd user manager is unavailable: %w", path, err)
		}
		fmt.Println("Installed", path+". Run 'readyrig service start' to enable and start it.")
		return nil
	case "start":
		return run("enable", "--now", "readyrig.service")
	case "stop", "restart", "status":
		return run(args[0], "readyrig.service")
	case "uninstall":
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(existing) != unit && string(existing) != legacyUnit {
			return errors.New("installed service uses different settings; use its original binary and --data-dir to uninstall it")
		}
		if err := run("disable", "--now", "readyrig.service"); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		return run("daemon-reload")
	default:
		return errors.New("unknown service command")
	}
}

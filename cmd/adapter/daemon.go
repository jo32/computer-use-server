package main

import (
	"computer-use-server/internal/app"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type runtimeInfo struct {
	PID       int    `json:"pid"`
	Mode      string `json:"mode"`
	Dashboard string `json:"dashboard,omitempty"`
}

func daemonArgs(f *flag.FlagSet) []string {
	args := []string{"serve", "--foreground"}
	f.VisitAll(func(v *flag.Flag) {
		if v.Name != "foreground" && v.Name != "if-needed" {
			args = append(args, "--"+v.Name+"="+v.Value.String())
		}
	})
	return args
}

func startDaemon(f *flag.FlagSet, o *startupOptions, out io.Writer) error {
	if runtime.GOOS == "windows" {
		return errors.New("background CLI mode supports macOS and Linux; use 'readyrig serve --foreground' on Windows")
	}
	if client, err := controlClientWithTimeout(o.DataDir, time.Second); err == nil {
		defer client.close()
		fmt.Fprintln(out, "ReadyRig is already running. Use 'readyrig restart' to apply startup changes.")
		return printRunning(client, o.DataDir, out)
	}
	if err := os.MkdirAll(o.DataDir, 0700); err != nil {
		return err
	}
	logPath := filepath.Join(o.DataDir, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	if err := logFile.Chmod(0600); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, daemonArgs(f)...)
	command.Stdout, command.Stderr = logFile, logFile
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "READYRIG_DAEMON_CHILD=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "READYRIG_DAEMON_CHILD=1")
	detachCommand(command)
	if err := command.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-exited:
			// Concurrent starts can share the winner's instance after the data lock is acquired.
			if client, probeErr := controlClientWithTimeout(o.DataDir, time.Second); probeErr == nil {
				defer client.close()
				return printRunning(client, o.DataDir, out)
			}
			return fmt.Errorf("ReadyRig failed to start (%v); see %s or run 'readyrig serve --foreground'", err, logPath)
		case <-deadline.C:
			_ = command.Process.Kill()
			<-exited
			return fmt.Errorf("ReadyRig did not become ready within 20 seconds; see %s", logPath)
		case <-ticker.C:
			client, err := controlClientWithTimeout(o.DataDir, time.Second)
			if err == nil {
				defer client.close()
				fmt.Fprintln(out, "ReadyRig started in the background.")
				return printRunning(client, o.DataDir, out)
			}
		}
	}
}

func printRunning(client *controlClient, dir string, out io.Writer) error {
	if raw, err := client.request(http.MethodGet, "/api/runtime", nil); err == nil {
		var info runtimeInfo
		if err := json.Unmarshal(raw, &info); err != nil {
			return err
		}
		fmt.Fprintf(out, "Process: %d (%s)\n", info.PID, info.Mode)
		if info.Dashboard != "" {
			fmt.Fprintln(out, "Dashboard:", info.Dashboard)
		}
	}
	raw, err := client.request(http.MethodGet, "/api/connection", nil)
	if err != nil {
		return err
	}
	var connection struct {
		Gateway string `json:"gateway"`
	}
	if err := json.Unmarshal(raw, &connection); err != nil {
		return err
	}
	fmt.Fprintln(out, "Agent API:", connection.Gateway)
	fmt.Fprintln(out, "Logs:", filepath.Join(dir, "daemon.log"))
	fmt.Fprintln(out, "Open 'readyrig' for the terminal dashboard; 'readyrig stop' stops the service.")
	return nil
}

func stopDaemon(dir string, out io.Writer) error {
	client, err := controlClientWithTimeout(dir, time.Second)
	if err != nil {
		// A held data lock distinguishes a starting or unreachable process from a stopped one.
		if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
			fmt.Fprintln(out, "ReadyRig is already stopped.")
			return nil
		}
		unlock, lockErr := app.LockData(dir)
		if lockErr != nil {
			return fmt.Errorf("cannot reach the running instance: %w", err)
		}
		unlock()
		fmt.Fprintln(out, "ReadyRig is already stopped.")
		return nil
	}
	if _, err := client.request(http.MethodPost, "/api/daemon/stop", nil); err != nil {
		return err
	}
	defer client.close()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		unlock, err := app.LockData(dir)
		if err == nil {
			unlock()
			fmt.Fprintln(out, "ReadyRig stopped.")
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("ReadyRig is still shutting down; check its log before restarting")
}

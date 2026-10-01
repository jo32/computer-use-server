package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBackgroundLifecycleEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("background CLI runs on macOS/Linux")
	}
	home := t.TempDir()
	data, workspace := filepath.Join(home, "private data"), filepath.Join(home, "work space")
	binary := filepath.Join(home, "readyrig")
	if out, err := exec.Command("go", "build", "-tags", "nogui", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, append(args, "--data-dir", data)...)
		cmd.Env = append(os.Environ(), "HOME="+home, "READYRIG_NO_UPDATE=1", "READYRIG_CLI_TEST_PROCESS=", "READYRIG_DAEMON_CHILD=")
		return cmd.CombinedOutput()
	}
	cli := func(args ...string) []byte {
		t.Helper()
		out, err := run(args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return out
	}
	t.Cleanup(func() { _, _ = run("stop") })
	cli("init", "--workspace", workspace, "--gateway", "127.0.0.1:0", "--ui", "127.0.0.1:0", "--no-chrome", "--cloud-url", "", "--no-update")
	started := cli("serve", "--allow-shell", "--full-access")
	if !strings.Contains(string(started), "started in the background") || !strings.Contains(string(started), "Dashboard:") {
		t.Fatal(string(started))
	}
	info := func() runtimeInfo {
		t.Helper()
		client, err := newControlClient(data)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := client.request(http.MethodGet, "/api/runtime", nil)
		if err != nil {
			t.Fatal(err)
		}
		var info runtimeInfo
		if err := json.Unmarshal(raw, &info); err != nil {
			t.Fatal(err)
		}
		return info
	}
	first := info()
	if first.Mode != "daemon" || first.PID <= 0 {
		t.Fatal(first)
	}
	if file, err := os.Stat(filepath.Join(data, "daemon.log")); err != nil || file.Mode().Perm() != 0600 {
		t.Fatal("daemon log is not private", err)
	}
	cli("serve")
	if info().PID != first.PID {
		t.Fatal("repeated serve started a duplicate")
	}
	var state struct {
		Enabled  map[string]bool `json:"enabled"`
		Projects struct {
			FullAccess bool `json:"full_access"`
		} `json:"project_access"`
	}
	if err := json.Unmarshal(cli("status"), &state); err != nil || !state.Enabled["terminal"] || !state.Projects.FullAccess {
		t.Fatal("startup overrides lost", state, err)
	}
	cli("call", "write_file", `{"path":"alive.txt","content":"daemon survived parent exit"}`)
	if content, err := os.ReadFile(filepath.Join(workspace, "alive.txt")); err != nil || string(content) != "daemon survived parent exit" {
		t.Fatal("daemon did not execute after parent exit", err)
	}
	// Lifecycle and runtime endpoints must remain confined to the private socket.
	var connection struct {
		Gateway string `json:"gateway"`
	}
	_ = json.Unmarshal(cli("connection"), &connection)
	for _, endpoint := range []string{"/api/runtime", "/api/daemon/stop"} {
		res, err := http.Get(connection.Gateway + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatalf("private route reachable through agent API: %s %d", endpoint, res.StatusCode)
		}
	}
	// Scripts must get help instead of entering or waiting in a TUI.
	if help := cli(); !strings.Contains(string(help), "terminal dashboard needs an interactive terminal") {
		t.Fatal(string(help))
	}
	if info().PID != first.PID {
		t.Fatal("noninteractive invocation changed the daemon")
	}
	cli("capability", "computer", "on")
	cli("capability", "terminal", "off")
	cli("restart")
	if info().PID == first.PID {
		t.Fatal("restart reused the old process")
	}
	if err := json.Unmarshal(cli("status"), &state); err != nil || state.Enabled["terminal"] || !state.Enabled["computer"] || state.Projects.FullAccess {
		t.Fatal("capability choices or session-only access did not restart correctly", state, err)
	}
	cli("stop")
	cli("stop")
	if _, err := newControlClient(data); err == nil {
		t.Fatal("stopped service still responds")
	}
	if _, err := os.Stat(filepath.Join(data, "control.json")); !os.IsNotExist(err) {
		t.Fatal("control endpoint was not cleaned up")
	}
	// A listener failure must report failure and return, without leaving a daemon.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	out, err := run("serve", "--gateway", occupied.Addr().String())
	if err == nil || !strings.Contains(string(out), "failed to start") || !strings.Contains(string(out), "daemon.log") {
		t.Fatalf("startup failure not reported: %v %s", err, out)
	}
	if _, err := newControlClient(data); err == nil {
		t.Fatal("failed startup left an instance running")
	}
}

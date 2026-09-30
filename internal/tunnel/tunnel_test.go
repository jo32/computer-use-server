package tunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real subprocess exercises output capture, cancellation and reaping without
// opening a public tunnel or using the network in automated tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv("READYRIG_TEST_TUNNEL"); mode != "" {
		config := ""
		for i, arg := range os.Args {
			if arg == "--config" && i+1 < len(os.Args) {
				config = os.Args[i+1]
			}
		}
		body, _ := os.ReadFile(config)
		capture, _ := json.Marshal(map[string]any{"args": os.Args[1:], "config": config, "contents": string(body), "token": os.Getenv("TUNNEL_TOKEN"), "url": os.Getenv("TUNNEL_URL")})
		if path := os.Getenv("READYRIG_TEST_CAPTURE"); path != "" {
			f, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			fmt.Fprintln(f, string(capture))
			f.Close()
		}
		if mode == "flood" {
			for i := 0; i < 150; i++ {
				fmt.Println("diagnostic", i)
			}
			fmt.Println(strings.Repeat("x", 50000))
		}
		if mode == "registered-first" {
			fmt.Println("INF Registered tunnel connection")
		}
		fmt.Println("| https://readyrig-test-share.trycloudflare.com |")
		if mode != "url-only" && mode != "registered-first" {
			fmt.Println("INF Registered tunnel connection")
		}
		if mode == "crash" {
			time.Sleep(50 * time.Millisecond)
			os.Exit(9)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	os.Exit(m.Run())
}

func helper(t *testing.T, mode string) (*Manager, string) {
	t.Helper()
	t.Setenv("READYRIG_TEST_TUNNEL", mode)
	capture := filepath.Join(t.TempDir(), "capture.jsonl")
	t.Setenv("READYRIG_TEST_CAPTURE", capture)
	command, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Dir: t.TempDir(), Command: command, StartupTimeout: 3 * time.Second})
	t.Cleanup(m.Close)
	return m, capture
}

func awaitState(t *testing.T, m *Manager, state string) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Status()
		if s.State == state {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %s, got %+v", state, m.Status())
	return Status{}
}

func TestLifecycleAndConfigurationIsolation(t *testing.T) {
	m, capture := helper(t, "ready")
	t.Setenv("TUNNEL_TOKEN", "named-tunnel-secret")
	t.Setenv("TUNNEL_URL", "https://wrong-target.example")
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := m.Start("http://127.0.0.1:7332"); err != nil {
			t.Fatal(err)
		}
	}
	s := awaitState(t, m, "ready")
	if s.URL != "https://readyrig-test-share.trycloudflare.com" {
		t.Fatal(s)
	}
	body, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(body), "\n"); lines != 1 {
		t.Fatal("duplicate start spawned subprocesses", string(body))
	}
	var info struct {
		Args                         []string
		Config, Contents, Token, URL string
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatal(err)
	}
	if info.Contents != "{}\n" || info.Token != "" || info.URL != "" {
		t.Fatalf("inherited named configuration: %+v", info)
	}
	if !strings.Contains(strings.Join(info.Args, " "), "--url http://127.0.0.1:7332") || !strings.Contains(strings.Join(info.Args, " "), "--protocol http2") {
		t.Fatal(info.Args)
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	if s = m.Status(); s.State != "stopped" || s.URL != "" {
		t.Fatal(s)
	}
	if _, err := os.Stat(info.Config); !os.IsNotExist(err) {
		t.Fatal("temporary configuration was not cleaned", err)
	}
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, "ready")
	m.Close()
	if err := m.Start("http://127.0.0.1:7332"); err == nil {
		t.Fatal("closed manager restarted")
	}
}

func TestProvisionedURLIsNotReadyAndStartupTimeout(t *testing.T) {
	m, _ := helper(t, "url-only")
	m.opts.StartupTimeout = 150 * time.Millisecond
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	s := awaitState(t, m, "error")
	if s.URL != "" || !strings.Contains(s.Error, "超时") {
		t.Fatal(s)
	}
}

func TestCrashRevokesURL(t *testing.T) {
	m, _ := helper(t, "crash")
	if err := m.Start("http://localhost:7332"); err != nil {
		t.Fatal(err)
	}
	s := awaitState(t, m, "error")
	if s.URL != "" || s.Error == "" {
		t.Fatal(s)
	}
}

func TestStopDuringStartup(t *testing.T) {
	m, _ := helper(t, "url-only")
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	// Cancellation is also valid before the subprocess has started.
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	if s := m.Status(); s.State != "stopped" || s.URL != "" {
		t.Fatal(s)
	}
}

func TestRelativeExecutableOverride(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Dir(executable))
	m, _ := helper(t, "ready")
	m.opts.Command = "." + string(filepath.Separator) + filepath.Base(executable)
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, "ready")
}

func TestOutputOrderAndBoundedDiagnostics(t *testing.T) {
	for _, mode := range []string{"registered-first", "flood"} {
		t.Run(mode, func(t *testing.T) {
			m, _ := helper(t, mode)
			if err := m.Start("http://[::1]:7332"); err != nil {
				t.Fatal(err)
			}
			s := awaitState(t, m, "ready")
			if len(s.Logs) > 80 {
				t.Fatal("unbounded logs", len(s.Logs))
			}
			for _, line := range s.Logs {
				if len(line) > 4096 {
					t.Fatal("unbounded diagnostic line")
				}
			}
			s.Logs[0] = "mutated snapshot"
			if m.Status().Logs[0] == "mutated snapshot" {
				t.Fatal("snapshot aliases manager logs")
			}
		})
	}
}

func TestTargetBoundaryAndMissingOverride(t *testing.T) {
	for _, target := range []string{"https://127.0.0.1:7332", "http://example.com:80", "http://192.168.1.1:7332", "http://user:pass@127.0.0.1:7332", "http://127.0.0.1:7332/secret", "http://127.0.0.1:7332?token=secret", "http://127.0.0.1"} {
		if err := validateTarget(target); err == nil {
			t.Fatal("accepted nonlocal/plain target", target)
		}
	}
	m := New(Options{Command: filepath.Join(t.TempDir(), "missing")})
	defer m.Close()
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	if s := awaitState(t, m, "error"); !strings.Contains(s.Error, "找不到") {
		t.Fatal(s)
	}
}

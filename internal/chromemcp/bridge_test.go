package chromemcp

import (
	"bufio"
	"computer-use-server/internal/harness"
	"computer-use-server/internal/store"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real child process exercises framing, initialization, paging, errors and cancellation.
func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("RELAY_MCP_TEST_CHILD") != "1" {
		return
	}
	initialized := false
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			ID     any                        `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &req)
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18"}
		case "notifications/initialized":
			initialized = true
			continue
		case "tools/list":
			if !initialized {
				os.Exit(4)
			}
			tool := func(name string) map[string]any {
				return map[string]any{"name": name, "description": "test tool", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "number"}}}}}, "annotations": map[string]any{"readOnlyHint": true}, "outputSchema": map[string]any{"type": "object"}}
			}
			if req.Params["cursor"] == nil {
				result = map[string]any{"tools": []any{tool("echo")}, "nextCursor": "page2"}
			} else {
				result = map[string]any{"tools": []any{tool("fail"), tool("block")}}
			}
		case "tools/call":
			var name string
			_ = json.Unmarshal(req.Params["name"], &name)
			if name == "block" {
				time.Sleep(time.Minute)
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "Chrome reply"}, map[string]any{"type": "image", "mimeType": "image/png", "data": "aGVsbG8="}}, "structuredContent": map[string]any{"echo": req.Params["arguments"]}, "isError": name == "fail"}
		default:
			continue
		}
		// Notifications must not be mistaken for responses.
		fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","method":"notifications/message","params":{}}`)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}
func testBridge(t *testing.T) (*Bridge, *harness.Registry, *store.Store) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := harness.New(s)
	b := New(r)
	b.detect = func(context.Context, Options) (target, error) { return target{Key: "test"}, nil }
	b.resolve = func(Options, target) (string, []string, []string, error) {
		return os.Args[0], []string{"-test.run=^TestMCPHelperProcess$"}, append(os.Environ(), "RELAY_MCP_TEST_CHILD=1"), nil
	}
	if err := b.Start(Options{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.SetPaused(true); b.Close(); r.WaitBackground(); s.Close() })
	waitFor(t, func() bool { return b.Status().State == "ready" })
	return b, r, s
}
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatal("condition timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestBridgeDiscoveryForwardingAuditAndDisable(t *testing.T) {
	b, r, s := testBridge(t)
	specs := r.Specs()
	if len(specs) != 3 || specs[0].Name != "chrome_echo" || specs[0].Mutating || specs[0].OutputSchema == nil {
		t.Fatalf("bad tools: %#v", specs)
	}
	out, call, err := r.Invoke(context.Background(), "chrome_echo", harness.Invocation{Session: "agent", Client: "test", Arguments: json.RawMessage(`{"value":42}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.MCPResult["content"].([]any)) != 2 || out.MCPResult["structuredContent"] == nil {
		t.Fatal("content lost", out)
	}
	saved, err := s.Get(call.ID)
	if err != nil || saved.Status != "success" || !strings.Contains(string(saved.Result), "Chrome reply") {
		t.Fatal(saved, err)
	}
	out, call, err = r.Invoke(context.Background(), "chrome_fail", harness.Invocation{Arguments: json.RawMessage(`{}`)})
	if err == nil || call.Status != "error" || out.MCPResult["isError"] != true {
		t.Fatal("upstream failure not preserved", out, call, err)
	}
	r.Enable("browser", false)
	_, call, err = r.Invoke(context.Background(), "chrome_echo", harness.Invocation{Arguments: json.RawMessage(`{}`)})
	if err == nil || call.Status != "denied" {
		t.Fatal("disabled browser accepted a call")
	}
	b.Refresh()
	waitFor(t, func() bool { return b.Status().State == "disabled" })
	if len(r.Specs()) != 0 {
		t.Fatal("disabled tools still published")
	}
	r.Enable("browser", true)
	b.Refresh()
	waitFor(t, func() bool { return b.Status().State == "ready" })
}
func TestBridgeCancellationStopsChildAndCanReconnect(t *testing.T) {
	b, r, _ := testBridge(t)
	b.mu.Lock()
	old := b.client
	b.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, _, err := r.Invoke(ctx, "chrome_block", harness.Invocation{Arguments: json.RawMessage(`{}`)})
		finished <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel blocked")
	}
	select {
	case <-old.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("child survived cancellation")
	}
	b.Refresh()
	waitFor(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.client != nil && b.client != old })
	r.SetPaused(true)
	_, call, err := r.Invoke(context.Background(), "chrome_echo", harness.Invocation{Arguments: json.RawMessage(`{}`)})
	if err == nil || call.Status != "denied" {
		t.Fatal("paused call accepted")
	}
}
func TestLocalURLAndChromeProbe(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:9222", "http://example.com:9222", "http://127.0.0.1:9222/path", "http://user:pw@127.0.0.1:9222", "http://127.0.0.1:9222?x=1", "http://localhost"} {
		if _, err := localURL(raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	var endpoint string
	var remote, redirect bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirect {
			http.Redirect(w, r, "http://example.com", 302)
			return
		}
		ws := "ws" + strings.TrimPrefix(endpoint, "http") + "/devtools/browser/test"
		if remote {
			ws = "ws://example.com:9222/devtools/browser/test"
		}
		json.NewEncoder(w).Encode(map[string]any{"Browser": "Chrome/150.0", "webSocketDebuggerUrl": ws})
	}))
	defer server.Close()
	endpoint = server.URL
	target, err := probeURL(context.Background(), endpoint)
	if err != nil || len(target.Args) != 1 || !strings.HasPrefix(target.Args[0], "--ws-endpoint=") {
		t.Fatal(target, err)
	}
	remote = true
	if _, err := probeURL(context.Background(), endpoint); err == nil {
		t.Fatal("accepted remote WebSocket")
	}
	remote = false
	redirect = true
	if _, err := probeURL(context.Background(), endpoint); err == nil {
		t.Fatal("followed redirect")
	}
}

func TestDiscoveryRequiresChromeAndLiveDebugging(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "chrome")
	if err := os.WriteFile(binary, []byte("test binary marker"), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	marker := filepath.Join(dir, "DevToolsActivePort")
	if err := os.WriteFile(marker, []byte(port+"\n/devtools/browser/test"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := Options{UserDataDir: dir}
	if _, err := discoverAt(context.Background(), opts, nil, []string{dir}); err == nil {
		t.Fatal("browser not installed")
	}
	target, err := discoverAt(context.Background(), opts, []string{binary}, []string{dir})
	if err != nil || len(target.Args) != 2 || target.Args[0] != "--autoConnect" {
		t.Fatal(target, err)
	}
	listener.Close()
	if _, err := discoverAt(context.Background(), opts, []string{binary}, []string{dir}); err == nil {
		t.Fatal("stale debugging file accepted")
	}
	for _, bad := range []string{"0\n/devtools/browser/test", "65536\n/devtools/browser/test", "1234\nhttp://example.com"} {
		os.WriteFile(marker, []byte(bad), 0600)
		if _, err := discoverAt(context.Background(), opts, []string{binary}, []string{dir}); err == nil {
			t.Fatal("invalid marker accepted", bad)
		}
	}
}

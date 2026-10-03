package server

import (
	"bufio"
	"computer-use-server/internal/harness"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mcpClient talks to the gateway handler directly, one JSON-RPC call at a time.
type mcpClient struct {
	t   *testing.T
	s   *Server
	sid string
	id  int
}

func newMCPClient(t *testing.T, s *Server) *mcpClient {
	t.Helper()
	c := &mcpClient{t: t, s: s}
	w := request(s.Gateway(), "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test"}}}`, s.AccessPath)
	c.sid = w.Header().Get("Mcp-Session-Id")
	if w.Code != 200 || c.sid == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	var init struct {
		Result struct {
			ServerInfo   map[string]string `json:"serverInfo"`
			Capabilities struct {
				Tools map[string]bool `json:"tools"`
			} `json:"capabilities"`
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	json.Unmarshal(w.Body.Bytes(), &init)
	if init.Result.ServerInfo["version"] == "" || init.Result.ServerInfo["version"] == "0.4.0" || !init.Result.Capabilities.Tools["listChanged"] || !strings.Contains(init.Result.Instructions, "use_tool") {
		t.Fatalf("initialize: %s", w.Body.String())
	}
	return c
}
func (c *mcpClient) post(body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/"+c.s.AccessPath+"/mcp", strings.NewReader(body))
	r.Header.Set("Mcp-Session-Id", c.sid)
	w := httptest.NewRecorder()
	c.s.Gateway().ServeHTTP(w, r)
	return w
}

type toolReply struct {
	IsError bool             `json:"isError"`
	Content []map[string]any `json:"content"`
}

func (c *mcpClient) call(name string, args any) toolReply {
	c.t.Helper()
	c.id++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.id + 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	w := c.post(string(b))
	var out struct {
		Result toolReply `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		c.t.Fatalf("%d %s %v", w.Code, w.Body.String(), err)
	}
	return out.Result
}
func envelope(t *testing.T, r toolReply) map[string]any {
	t.Helper()
	last := r.Content[len(r.Content)-1]
	var env map[string]any
	if err := json.Unmarshal([]byte(last["text"].(string)), &env); err != nil {
		t.Fatalf("last content block is not the JSON envelope: %v %v", last, err)
	}
	return env
}
func workspaceOf(t *testing.T, s *Server, name, content string) {
	t.Helper()
	// fixture's Files root is private to it, so write through the tool itself.
	c := newMCPClient(t, s)
	if r := c.call("write_file", map[string]any{"path": name, "content": content}); r.IsError {
		t.Fatalf("seed %s: %v", name, r.Content)
	}
}

func TestMCPFileReadsArePlainTextWithMetadataAfter(t *testing.T) {
	s := fixture(t)
	workspaceOf(t, s, "notes.txt", "alpha\nbeta\n")
	r := newMCPClient(t, s).call("read_file", map[string]any{"path": "notes.txt"})
	if r.IsError || len(r.Content) != 1 {
		t.Fatalf("a text result is one block, with no repeated metadata: %+v", r.Content)
	}
	if r.Content[0]["text"] != "     1\talpha\n     2\tbeta\n" {
		t.Fatalf("body: %q", r.Content[0]["text"])
	}
}
func TestMCPImageFilesAreImageBlocks(t *testing.T) {
	s := fixture(t)
	c := newMCPClient(t, s)
	c.call("write_file", map[string]any{"path": "dot.png", "content": "iVBORw0KGgo=", "encoding": "base64"})
	r := c.call("read_file", map[string]any{"path": "dot.png"})
	if r.IsError || r.Content[0]["type"] != "image" || r.Content[0]["mimeType"] != "image/png" || r.Content[0]["data"] != "iVBORw0KGgo=" {
		t.Fatalf("%+v", r.Content)
	}
	if strings.Contains(r.Content[len(r.Content)-1]["text"].(string), "iVBORw0KGgo=") {
		t.Fatal("image bytes repeated in the text envelope")
	}
}
func TestMCPErrorsCarryACode(t *testing.T) {
	s := fixture(t)
	c := newMCPClient(t, s)
	for name, tc := range map[string]struct {
		tool string
		args any
		code string
	}{"bad argument": {"read_file", map[string]any{"path": 5}, "invalid_arguments"}, "unknown tool": {"nope", map[string]any{}, "unknown_tool"}, "missing file": {"read_file", map[string]any{"path": "absent"}, "not_found"}} {
		r := c.call(tc.tool, tc.args)
		if !r.IsError || envelope(t, r)["error_code"] != tc.code {
			t.Errorf("%s: %+v", name, r)
		}
	}
	s.Registry.Enable("files", false)
	r := c.call("read_file", map[string]any{"path": "x"})
	if !r.IsError || envelope(t, r)["error_code"] != "capability_disabled" {
		t.Fatalf("%+v", r)
	}
}
func listedNames(t *testing.T, c *mcpClient) map[string]bool {
	t.Helper()
	w := c.post(`{"jsonrpc":"2.0","id":9,"method":"tools/list"}`)
	var out struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range out.Result.Tools {
		names[tool.Name] = true
	}
	return names
}
func TestMCPToolListFollowsCapabilitiesAndGroups(t *testing.T) {
	s := fixture(t)
	s.Registry.RegisterHelp()
	p := harness.NewProcesses(t.TempDir())
	p.Register(s.Registry)
	s.Registry.ReplaceCategory("browser", []harness.Tool{{Spec: harness.Spec{Name: "chrome_click", Category: "browser", InputSchema: map[string]any{"type": "object"}}, External: true}, {Spec: harness.Spec{Name: "chrome_lighthouse_audit", Category: "browser", Group: "advanced", InputSchema: map[string]any{"type": "object"}}, External: true}})
	c := newMCPClient(t, s)
	names := listedNames(t, c)
	if names["exec_command"] || names["chrome_lighthouse_audit"] || !names["read_file"] || !names["chrome_click"] || !names["use_tool"] || !names["help"] {
		t.Fatalf("default listing: %v", names)
	}
	s.Registry.Enable("terminal", true)
	if names = listedNames(t, c); !names["exec_command"] || !names["write_stdin"] {
		t.Fatalf("enabled capability missing: %v", names)
	}
	s.Registry.Enable("files", false)
	if names = listedNames(t, c); names["read_file"] || names["edit_file"] {
		t.Fatalf("disabled capability still advertised: %v", names)
	}
	s.Registry.ExposeAll = true
	if !listedNames(t, c)["chrome_lighthouse_audit"] {
		t.Fatal("ExposeAll ignored")
	}
	specs := request(s.Gateway(), "GET", "/api/v1/tools", "", s.AccessPath)
	if !strings.Contains(specs.Body.String(), "read_file") {
		t.Fatal("the REST catalogue must keep every tool, including disabled ones")
	}
}
func TestMCPNonZeroExitIsNotAnError(t *testing.T) {
	s := fixture(t)
	p := harness.NewProcesses(t.TempDir())
	t.Cleanup(func() { p.Stop(); s.Registry.WaitBackground() })
	p.Register(s.Registry)
	s.Registry.Enable("terminal", true)
	r := newMCPClient(t, s).call("exec_command", map[string]any{"command": "echo oops >&2; exit 9"})
	if r.IsError || !strings.Contains(r.Content[0]["text"].(string), "[exit code 9]") {
		t.Fatalf("%+v", r)
	}
	if len(r.Content) != 1 {
		t.Fatalf("the exit code is in the text; no JSON block is needed: %+v", r.Content)
	}
	w := request(s.Gateway(), "POST", "/api/v1/tools/exec_command", `{"command":"exit 3"}`, s.AccessPath)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"exit_code":3`) || !strings.Contains(w.Body.String(), `"status":"success"`) || strings.Contains(w.Body.String(), `"error":"command`) {
		t.Fatal("a non-zero exit is a result, not a failed call:", w.Code, w.Body.String())
	}
}
func TestRESTErrorCodesAndStatuses(t *testing.T) {
	s := fixture(t)
	h := s.Gateway()
	for path, want := range map[string]struct {
		code int
		err  string
	}{"/api/v1/tools/nothing": {422, "unknown_tool"}, "/api/v1/tools/read_file": {422, "invalid_arguments"}} {
		w := request(h, "POST", path, `{}`, s.AccessPath)
		if w.Code != want.code || !strings.Contains(w.Body.String(), `"error_code":"`+want.err+`"`) {
			t.Errorf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	s.Registry.Enable("files", false)
	w := request(h, "POST", "/api/v1/tools/read_file", `{"path":"x"}`, s.AccessPath)
	if w.Code != 423 || !strings.Contains(w.Body.String(), `"error_code":"capability_disabled"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	s.Registry.Enable("files", true)
	seed := request(h, "POST", "/api/v1/tools/write_file", `{"path":"p.png","content":"iVBORw0KGgo=","encoding":"base64"}`, s.AccessPath)
	if seed.Code != 200 {
		t.Fatal(seed.Body.String())
	}
	w = request(h, "POST", "/api/v1/tools/read_file", `{"path":"p.png"}`, s.AccessPath)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"images":[{"mimeType":"image/png","data":"iVBORw0KGgo="}]`) {
		t.Fatal("REST lost the image", w.Code, w.Body.String())
	}
}
func TestMCPCancelledNotificationStopsTheCall(t *testing.T) {
	s := fixture(t)
	started := make(chan struct{})
	s.Registry.Register(harness.Tool{Spec: harness.Spec{Name: "hang", Category: "files", Parallel: true, InputSchema: harness.Schema(map[string]any{})}, Run: func(ctx context.Context, _ harness.Invocation) (harness.Output, error) {
		close(started)
		<-ctx.Done()
		return harness.Output{}, ctx.Err()
	}})
	c := newMCPClient(t, s)
	done := make(chan toolReply, 1)
	go func() {
		w := c.post(`{"jsonrpc":"2.0","id":"req-7","method":"tools/call","params":{"name":"hang","arguments":{}}}`)
		var out struct {
			Result toolReply `json:"result"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		done <- out.Result
	}()
	<-started
	if w := c.post(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"req-7","reason":"user"}}`); w.Code != 202 {
		t.Fatal(w.Code)
	}
	select {
	case r := <-done:
		if !r.IsError || envelope(t, r)["error_code"] != "cancelled" {
			t.Fatalf("%+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not reach the running tool")
	}
	other := newMCPClient(t, s)
	if w := other.post(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"req-7"}}`); w.Code != 202 {
		t.Fatal(w.Code)
	}
}

// sseLines yields each data payload of an event stream.
func sseLines(body io.Reader) <-chan string {
	ch := make(chan string, 32)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(body)
		for sc.Scan() {
			switch line := sc.Text(); {
			case strings.HasPrefix(line, "data: "):
				ch <- strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, ": connected"):
				ch <- "connected"
			}
		}
	}()
	return ch
}
func nextEvent(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("event stream closed")
		}
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5s")
	}
	return ""
}
func TestMCPEventStreamCarriesListChangesAndTaskCompletion(t *testing.T) {
	s := fixture(t)
	p := harness.NewProcesses(t.TempDir())
	p.Register(s.Registry)
	t.Cleanup(func() { p.Stop(); s.Registry.WaitBackground() })
	ts := httptest.NewServer(s.Gateway())
	defer ts.Close()
	base := ts.URL + "/" + s.AccessPath + "/mcp"
	init, err := http.Post(base, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`))
	if err != nil {
		t.Fatal(err)
	}
	init.Body.Close()
	sid := init.Header.Get("Mcp-Session-Id")
	plain, _ := http.NewRequest("GET", base, nil)
	plain.Header.Set("Mcp-Session-Id", sid)
	if resp, err := http.DefaultClient.Do(plain); err != nil || resp.StatusCode != 405 {
		t.Fatalf("a GET without an event-stream Accept must stay 405: %v", err)
	}
	missing, _ := http.NewRequest("GET", base, nil)
	missing.Header.Set("Accept", "text/event-stream")
	missing.Header.Set("Mcp-Session-Id", "unknown")
	if resp, err := http.DefaultClient.Do(missing); err != nil || resp.StatusCode != 404 {
		t.Fatalf("unknown session must be 404: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, _ := http.NewRequestWithContext(ctx, "GET", base, nil)
	stream.Header.Set("Accept", "text/event-stream")
	stream.Header.Set("Mcp-Session-Id", sid)
	resp, err := http.DefaultClient.Do(stream)
	if err != nil || resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %v %v", err, resp)
	}
	events := sseLines(resp.Body)
	if e := nextEvent(t, events); e != "connected" {
		t.Fatal(e)
	}
	s.Registry.Enable("terminal", true)
	var changed struct{ Method string }
	json.Unmarshal([]byte(nextEvent(t, events)), &changed)
	if changed.Method != "notifications/tools/list_changed" {
		t.Fatalf("got %+v", changed)
	}
	call, _ := http.NewRequest("POST", base, strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"exec_command","arguments":{"command":"sleep 0.4; exit 5","background":true}}}`))
	call.Header.Set("Mcp-Session-Id", sid)
	if r, err := http.DefaultClient.Do(call); err != nil {
		t.Fatal(err)
	} else {
		r.Body.Close()
	}
	var done struct {
		Method string
		Params struct {
			Level  string
			Logger string
			Data   struct {
				Kind   string
				Detail map[string]any
			}
		}
	}
	json.Unmarshal([]byte(nextEvent(t, events)), &done)
	if done.Method != "notifications/message" || done.Params.Data.Kind != "task_finished" || done.Params.Data.Detail["exit_code"] != float64(5) || done.Params.Data.Detail["tool"] != "exec_command" {
		t.Fatalf("got %+v", done)
	}
}

func TestMCPNextCallReportsFinishedBackgroundJob(t *testing.T) {
	s := fixture(t)
	p := harness.NewProcesses(t.TempDir())
	p.Register(s.Registry)
	t.Cleanup(func() { p.Stop(); s.Registry.WaitBackground() })
	s.Registry.Enable("terminal", true)
	c := newMCPClient(t, s)
	start := c.call("exec_command", map[string]any{"command": "sleep 0.3; printf done; exit 3", "background": true})
	if start.IsError || strings.Contains(envelopeText(start), "[notice]") {
		t.Fatalf("%+v", start)
	}
	s.Registry.WaitBackground()
	next := c.call("exec_command", map[string]any{"command": "true"})
	text := next.Content[0]["text"].(string) + next.Content[1]["text"].(string)
	if !strings.Contains(text, "[notice] background exec_command task_finished: error, exit code 3") {
		t.Fatalf("no notice: %+v", next.Content)
	}
	if third := c.call("exec_command", map[string]any{"command": "true"}); strings.Contains(envelopeText(third), "[notice]") {
		t.Fatal("notice repeated")
	}
	w := request(s.Gateway(), "POST", "/api/v1/tools/exec_command", `{"command":"sleep 0.3; printf x","background":true}`, s.AccessPath)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	s.Registry.WaitBackground()
	w = request(s.Gateway(), "POST", "/api/v1/tools/glob", `{"pattern":"*"}`, s.AccessPath)
	if !strings.Contains(w.Body.String(), `"notices":[{`) {
		t.Fatalf("REST lost the notice: %s", w.Body.String())
	}
}
func envelopeText(r toolReply) string {
	var all strings.Builder
	for _, c := range r.Content {
		if s, ok := c["text"].(string); ok {
			all.WriteString(s)
		}
	}
	return all.String()
}

func TestMCPResultsCarryProgressLinesAtMostEveryInterval(t *testing.T) {
	s := fixture(t)
	s.Registry.AddProgress(func(session string) []harness.Event {
		return []harness.Event{{Session: session, Kind: "task_progress", Data: map[string]any{"text": "exec_command abc  running 40s  (2 KB output, last output 3s ago)  make", "session_id": "abc"}}}
	})
	c := newMCPClient(t, s)
	first := c.call("list_directory", map[string]any{})
	if !strings.Contains(envelopeText(first), "[progress] exec_command abc  running 40s") {
		t.Fatalf("%+v", first.Content)
	}
	if second := c.call("list_directory", map[string]any{}); strings.Contains(envelopeText(second), "[progress]") {
		t.Fatal("progress repeated inside the interval")
	}
	s.Registry.SetProgressInterval(time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	if third := c.call("list_directory", map[string]any{}); !strings.Contains(envelopeText(third), "[progress]") {
		t.Fatal("progress never came back")
	}
	w := request(s.Gateway(), "POST", "/api/v1/tools/list_directory", `{}`, s.AccessPath)
	if !strings.Contains(w.Body.String(), `"kind":"task_progress"`) {
		t.Fatalf("REST lost the progress: %s", w.Body.String())
	}
}

package server

import (
	"computer-use-server/internal/harness"
	"context"
	"strings"
	"testing"
)

func TestMCPJSONOnlyResultsAreCompact(t *testing.T) {
	s := fixture(t)
	s.Registry.Register(harness.Tool{Spec: harness.Spec{Name: "plain", Category: "files", Parallel: true, InputSchema: harness.Schema(map[string]any{})}, Run: func(context.Context, harness.Invocation) (harness.Output, error) {
		return harness.Output{Value: map[string]any{"a": 1}}, nil
	}})
	r := newMCPClient(t, s).call("plain", map[string]any{})
	if len(r.Content) != 1 || r.Content[0]["text"] != `{"result":{"a":1}}` {
		t.Fatalf("a JSON-only result carries no call id or empty error: %+v", r.Content)
	}
}
func TestMCPErrorsStayCompactAndKeepTheirCode(t *testing.T) {
	s := fixture(t)
	r := newMCPClient(t, s).call("read_file", map[string]any{"path": "absent"})
	if !r.IsError || len(r.Content) != 1 {
		t.Fatalf("%+v", r.Content)
	}
	if text := r.Content[0]["text"].(string); !strings.Contains(text, `"error_code":"not_found"`) || strings.Contains(text, "call_id") {
		t.Fatal(text)
	}
}
func TestMCPBatchReturnsEveryResultInOneReply(t *testing.T) {
	s := fixture(t)
	s.Registry.RegisterHelp()
	workspaceOf(t, s, "a.txt", "alpha\n")
	r := newMCPClient(t, s).call("batch", map[string]any{"calls": []any{
		map[string]any{"tool": "read_file", "arguments": map[string]any{"path": "a.txt"}},
		map[string]any{"tool": "glob", "arguments": map[string]any{"pattern": "*.txt"}},
	}})
	if r.IsError || len(r.Content) != 1 {
		t.Fatalf("%+v", r.Content)
	}
	text := r.Content[0]["text"].(string)
	if !strings.Contains(text, "### 1 read_file\n     1\talpha") || !strings.Contains(text, "### 2 glob\na.txt") {
		t.Fatalf("%s", text)
	}
	w := request(s.Gateway(), "POST", "/api/v1/tools/batch", `{"calls":[{"tool":"read_file","arguments":{"path":"a.txt"}}]}`, s.AccessPath)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"content":"alpha"`) {
		t.Fatalf("REST clients need each call's result: %d %s", w.Code, w.Body.String())
	}
}

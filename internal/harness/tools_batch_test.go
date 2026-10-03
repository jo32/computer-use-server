package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"computer-use-server/internal/store"
)

func call(tool string, args map[string]any) map[string]any {
	return map[string]any{"tool": tool, "arguments": args}
}

func TestBatchRunsSeveralToolsInOneRequest(t *testing.T) {
	r, _, root := filesForTest(t)
	r.RegisterHelp()
	put(t, root, "a.txt", "alpha\n")
	put(t, root, "b.txt", "beta\n")
	out, err := invoke(t, r, "batch", map[string]any{"calls": []any{
		call("read_file", map[string]any{"path": "a.txt"}),
		call("read_file", map[string]any{"path": "b.txt"}),
		call("search_files", map[string]any{"query": "beta"}),
		call("read_file", map[string]any{"path": "missing.txt"}),
		call("nope", map[string]any{}),
	}})
	if err != nil {
		t.Fatalf("one failing call must not fail the batch: %v", err)
	}
	for _, want := range []string{"### 1 read_file\n     1\talpha\n", "### 2 read_file\n     1\tbeta\n", "### 3 search_files\nb.txt:1:beta\n", "### 4 read_file\nerror [not_found]", "### 5 nope\nerror [unknown_tool]"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, out.Text)
		}
	}
	results := asMap(t, out)["results"].([]batchResult)
	if len(results) != 5 || !results[0].OK || results[3].OK || results[3].Code != "not_found" || results[4].Code != "unknown_tool" {
		t.Fatalf("results: %+v", results)
	}
	if m, ok := results[1].Result.(map[string]any); !ok || m["content"] != "beta" {
		t.Fatalf("REST clients need each call's own result: %+v", results[1])
	}
	rows, _, err := r.store.List(store.Filter{Query: "read_file", Limit: 50})
	if err != nil || len(rows) < 3 {
		t.Fatalf("inner calls must be audited individually: %d rows %v", len(rows), err)
	}
}
func TestBatchLimitsAndNesting(t *testing.T) {
	r, _, _ := filesForTest(t)
	r.RegisterHelp()
	var many []any
	for i := 0; i <= maxBatch; i++ {
		many = append(many, call("list_projects", map[string]any{}))
	}
	for name, calls := range map[string][]any{"empty": {}, "too many": many, "nested": {call("batch", map[string]any{"calls": []any{}})}, "no tool": {map[string]any{"arguments": map[string]any{}}}} {
		if _, err := invoke(t, r, "batch", map[string]any{"calls": calls}); err == nil {
			t.Errorf("%s batch accepted", name)
		}
	}
	if _, err := invoke(t, r, "batch", map[string]any{"calls": []any{call("read_file", map[string]any{"path": "x"})}, "extra": 1}); err == nil {
		t.Error("unknown argument accepted")
	}
}
func TestBatchWritesRunInOrderAndCanStopOnError(t *testing.T) {
	r, _, root := filesForTest(t)
	r.RegisterHelp()
	calls := []any{
		call("write_file", map[string]any{"path": "one.txt", "content": "1"}),
		call("write_file", map[string]any{"path": "two.txt"}),
		call("write_file", map[string]any{"path": "three.txt", "content": "3"}),
	}
	out, err := invoke(t, r, "batch", map[string]any{"calls": calls, "stop_on_error": true})
	if err != nil {
		t.Fatal(err)
	}
	results := asMap(t, out)["results"].([]batchResult)
	if !results[0].OK || results[1].OK || results[2].Code != "skipped" {
		t.Fatalf("%+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "three.txt")); err == nil {
		t.Fatal("a call after the failure still ran")
	}
	if _, err := invoke(t, r, "batch", map[string]any{"calls": calls}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "three.txt")); err != nil || string(b) != "3" {
		t.Fatalf("without stop_on_error the rest must run: %q %v", b, err)
	}
}
func TestReadOnlyBatchRunsInParallel(t *testing.T) {
	r := registryForTest(t)
	r.RegisterHelp()
	for _, name := range []string{"slow_a", "slow_b", "slow_c"} {
		r.Register(Tool{Spec: Spec{Name: name, Category: "files", Parallel: true, InputSchema: Schema(map[string]any{})}, Run: func(context.Context, Invocation) (Output, error) {
			time.Sleep(300 * time.Millisecond)
			return Output{Value: map[string]any{"ok": true}}, nil
		}})
	}
	start := time.Now()
	out, err := invoke(t, r, "batch", map[string]any{"calls": []any{call("slow_a", nil), call("slow_b", nil), call("slow_c", nil)}})
	if err != nil || time.Since(start) > 700*time.Millisecond {
		t.Fatalf("three 300 ms read-only calls took %v (%v)", time.Since(start), err)
	}
	if !strings.Contains(out.Text, "### 3 slow_c") {
		t.Fatal(out.Text)
	}
}
func TestBatchHonoursPauseAndCapabilities(t *testing.T) {
	r, _, root := filesForTest(t)
	r.RegisterHelp()
	put(t, root, "a.txt", "alpha\n")
	r.Enable("files", false)
	out, err := invoke(t, r, "batch", map[string]any{"calls": []any{call("read_file", map[string]any{"path": "a.txt"})}})
	if err != nil || asMap(t, out)["results"].([]batchResult)[0].Code != "capability_disabled" {
		t.Fatalf("batch bypassed a disabled capability: %v %v", out.Value, err)
	}
	r.Enable("files", true)
	r.SetPaused(true)
	if _, err := invoke(t, r, "batch", map[string]any{"calls": []any{call("read_file", map[string]any{"path": "a.txt"})}}); ErrorCode(err) != "control_paused" {
		t.Fatalf("batch ran while paused: %v", err)
	}
}

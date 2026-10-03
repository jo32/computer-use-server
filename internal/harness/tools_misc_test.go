package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"computer-use-server/internal/store"
)

func TestListTasksCanWaitForOneOrAllJobs(t *testing.T) {
	r, _ := execForTest(t)
	start := func(command string) any {
		out, err := invokeAs(t, r, "w", "exec_command", map[string]any{"command": command, "yield_time_ms": 1})
		if err != nil {
			t.Fatal(err)
		}
		return asMap(t, out)["session_id"]
	}
	quick, slow := start("sleep 0.4"), start("sleep 1.5")
	began := time.Now()
	out, err := invokeAs(t, r, "w", "list_tasks", map[string]any{"wait": "any", "yield_time_ms": 10000})
	if err != nil || time.Since(began) > 1200*time.Millisecond {
		t.Fatalf("wait=any took %v: %v", time.Since(began), err)
	}
	states := map[any]any{}
	for _, task := range asMap(t, out)["tasks"].([]map[string]any) {
		states[task["session_id"]] = task["running"]
	}
	if states[quick] != false || states[slow] != true {
		t.Fatalf("after wait=any: %v", states)
	}
	began = time.Now()
	out, err = invokeAs(t, r, "w", "list_tasks", map[string]any{"wait": "all", "yield_time_ms": 10000})
	if err != nil || time.Since(began) > 3*time.Second || strings.Contains(out.Text, "running") {
		t.Fatalf("wait=all: %v after %v\n%s", err, time.Since(began), out.Text)
	}
	began = time.Now()
	if _, err = invokeAs(t, r, "w", "list_tasks", map[string]any{"wait": "all", "yield_time_ms": 10000}); err != nil || time.Since(began) > 300*time.Millisecond {
		t.Fatal("nothing running: the wait must return at once", time.Since(began))
	}
	invokeAs(t, r, "w", "exec_command", map[string]any{"command": "sleep 20", "yield_time_ms": 1})
	began = time.Now()
	if _, err = invokeAs(t, r, "w", "list_tasks", map[string]any{"wait": "any", "yield_time_ms": 400}); err != nil || time.Since(began) < 350*time.Millisecond || time.Since(began) > time.Second {
		t.Fatalf("wait must end at its limit: %v %v", time.Since(began), err)
	}
	if _, err = invokeAs(t, r, "w", "list_tasks", map[string]any{"wait": "sometime"}); err == nil {
		t.Fatal("bad wait accepted")
	}
}
func TestCommandsGetALongDefaultTimeout(t *testing.T) {
	r, p := execForTest(t)
	for args, want := range map[string]int{`{"command":"sleep 20","yield_time_ms":1}`: 600, `{"command":"sleep 20","yield_time_ms":1,"background":true}`: 3600, `{"command":"sleep 20","yield_time_ms":1,"timeout":90}`: 90} {
		out, _, err := r.Invoke(context.Background(), "exec_command", Invocation{Session: "s", Arguments: []byte(args)})
		if err != nil {
			t.Fatal(err)
		}
		p.mu.Lock()
		got := p.items[asMap(t, out)["session_id"].(string)].timeout
		p.mu.Unlock()
		if got != want {
			t.Errorf("%s: timeout %d, want %d", args, got, want)
		}
	}
}
func TestFullSessionTableForgetsTheOldestFinishedOne(t *testing.T) {
	r, p := execForTest(t)
	p.mu.Lock()
	for i := 0; i < 128; i++ {
		done := make(chan struct{})
		close(done)
		p.items[ID()] = &process{done: done, session: "old", ended: time.Now().Add(-time.Duration(i) * time.Second), cancel: func() {}}
	}
	p.mu.Unlock()
	out, err := invoke(t, r, "exec_command", map[string]any{"command": "printf ok"})
	if err != nil || asMap(t, out)["stdout"] != "ok" {
		t.Fatalf("a full table of finished sessions blocked a new command: %v %v", out.Value, err)
	}
	p.mu.Lock()
	n := len(p.items)
	p.mu.Unlock()
	if n > 128 {
		t.Fatalf("%d sessions kept", n)
	}
}
func TestActivityLabelsAreDerivedWhenAbsent(t *testing.T) {
	r, _ := execForTest(t)
	f, err := NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Register(r)
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"exec_command", map[string]any{"command": "echo   hello\nworld"}, "echo hello world"},
		{"write_file", map[string]any{"path": "notes/a.txt", "content": "x"}, "write notes/a.txt"},
		{"exec_command", map[string]any{"command": "echo mine", "description": "my own words"}, "my own words"},
	}
	for _, c := range cases {
		_, call, err := func() (Output, store.Call, error) {
			b, _ := json.Marshal(c.args)
			return r.Invoke(context.Background(), c.tool, Invocation{Session: "s", Arguments: b})
		}()
		if err != nil {
			t.Fatal(err)
		}
		saved, _ := r.store.Get(call.ID)
		var args map[string]any
		json.Unmarshal(saved.Arguments, &args)
		if args["description"] != c.want {
			t.Errorf("%s: label %q, want %q", c.tool, args["description"], c.want)
		}
	}
	long := strings.Repeat("word ", 60)
	if got := deriveLabel("exec_command", map[string]any{"command": long}); len(got) > 100 || !strings.HasSuffix(got, "…") {
		t.Errorf("long command label not clipped: %q", got)
	}
	for tool, args := range map[string]map[string]any{"write_stdin": {"terminate": true}, "computer_action": {"actions": []any{1, 2, 3}}, "batch": {"calls": []any{1}}} {
		if deriveLabel(tool, args) == "" {
			t.Errorf("no label for %s", tool)
		}
	}
	r.Register(Tool{Spec: Spec{Name: "reader", Category: "files", Parallel: true, InputSchema: Schema(map[string]any{})}, Run: func(context.Context, Invocation) (Output, error) { return Output{}, nil }})
	_, call, _ := r.Invoke(context.Background(), "reader", Invocation{Session: "s", Arguments: []byte(`{}`)})
	if saved, _ := r.store.Get(call.ID); strings.Contains(string(saved.Arguments), "description") {
		t.Error("a read-only call was labelled")
	}
}
func TestMissingPermissionFailsFastAndHelpSaysWhy(t *testing.T) {
	r := registryForTest(t)
	r.RegisterHelp()
	ran := false
	r.Enable("computer", true)
	r.Register(Tool{Spec: Spec{Name: "screen_tool", Category: "computer", Parallel: true, InputSchema: Schema(map[string]any{})}, Run: func(context.Context, Invocation) (Output, error) {
		ran = true
		return Output{Value: map[string]any{}}, nil
	}})
	missing := "turn on ReadyRig under Screen Recording"
	r.PermissionCheck = func(s Spec) string {
		if s.Name == "screen_tool" {
			return missing
		}
		return ""
	}
	_, call, err := r.Invoke(context.Background(), "screen_tool", Invocation{Session: "s", Arguments: []byte(`{}`)})
	if ErrorCode(err) != "permission_required" || !strings.Contains(err.Error(), "Screen Recording") || ran {
		t.Fatalf("%v ran=%v", err, ran)
	}
	if saved, _ := r.store.Get(call.ID); saved.Status != "denied" {
		t.Fatalf("audit status %q", saved.Status)
	}
	got := helpResult(t, r, map[string]any{"name": "screen_tool"})
	h := got.Tools[0]
	if h.Available || !h.Enabled || h.UnavailableReason != "permission_required" || h.Permission != missing {
		t.Fatalf("help: %+v", h)
	}
	listed := helpResult(t, r, map[string]any{})
	found := false
	for _, tool := range listed.Tools {
		found = found || tool.Name == "screen_tool"
	}
	if !found {
		t.Fatal("a tool that lacks a permission should stay visible, with the reason")
	}
	missing = ""
	if _, err = invoke(t, r, "screen_tool", map[string]any{}); err != nil || !ran {
		t.Fatalf("granted permission still blocked: %v", err)
	}
}
func TestFileToolsReportSpecificErrorCodes(t *testing.T) {
	r, _, root := filesForTest(t)
	put(t, root, "dup.txt", "x x\n")
	put(t, root, "bin.dat", "a\x00b")
	os.MkdirAll(filepath.Join(root, "dir"), 0755)
	for name, c := range map[string]struct {
		tool string
		args map[string]any
		code string
	}{
		"missing file":     {"read_file", map[string]any{"path": "absent.txt"}, "not_found"},
		"binary read":      {"read_file", map[string]any{"path": "bin.dat"}, "binary_file"},
		"escape":           {"read_file", map[string]any{"path": "../etc/passwd"}, "outside_project"},
		"edit no match":    {"edit_file", map[string]any{"path": "dup.txt", "old_string": "zzz", "new_string": "y"}, "no_match"},
		"edit ambiguous":   {"edit_file", map[string]any{"path": "dup.txt", "old_string": "x", "new_string": "y"}, "ambiguous_match"},
		"edit binary":      {"edit_file", map[string]any{"path": "bin.dat", "old_string": "a", "new_string": "y"}, "binary_file"},
		"create over file": {"write_file", map[string]any{"path": "dup.txt", "content": "z", "create_only": true}, "file_exists"},
		"write to dir":     {"write_file", map[string]any{"path": "dir", "content": "z"}, "is_directory"},
	} {
		if _, err := invoke(t, r, c.tool, c.args); ErrorCode(err) != c.code {
			t.Errorf("%s: code %q (%v), want %s", name, ErrorCode(err), err, c.code)
		}
	}
}
func TestSingleProjectResultsCarryNoProjectTag(t *testing.T) {
	a := t.TempDir()
	p, err := NewProjects(a, "")
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewFiles(a)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Projects = p
	r := registryForTest(t)
	f.Register(r)
	os.WriteFile(filepath.Join(a, "x.txt"), []byte("x"), 0644)
	out, err := invoke(t, r, "read_file", map[string]any{"path": "x.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, has := asMap(t, out)["project"]; has {
		t.Fatal("a lone project needs no tag")
	}
	if err = p.Change("add", "", "Second", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	out, _ = invoke(t, r, "read_file", map[string]any{"path": "x.txt"})
	if _, has := asMap(t, out)["project"]; !has {
		t.Fatal("with two projects the tag is needed")
	}
}

package harness

import (
	"strings"
	"testing"
	"time"
)

func TestLongPollReturnsWhenOutputArrives(t *testing.T) {
	r, _ := execForTest(t)
	out, err := invoke(t, r, "exec_command", map[string]any{"command": "sleep 0.3; echo first; sleep 20", "yield_time_ms": 1})
	if err != nil || asMap(t, out)["running"] != true {
		t.Fatal(out.Value, err)
	}
	id := asMap(t, out)["session_id"]
	start := time.Now()
	out, err = invoke(t, r, "write_stdin", map[string]any{"session_id": id, "return_on": "output", "yield_time_ms": 15000})
	if err != nil || asMap(t, out)["stdout"] != "first\n" || time.Since(start) > 3*time.Second {
		t.Fatalf("long poll: %v %v after %v", out.Value, err, time.Since(start))
	}
	// Drained output must not wake the next poll: with nothing new it waits out its time.
	start = time.Now()
	out, err = invoke(t, r, "write_stdin", map[string]any{"session_id": id, "return_on": "output", "yield_time_ms": 400})
	if err != nil || asMap(t, out)["stdout"] != "" || time.Since(start) < 350*time.Millisecond {
		t.Fatalf("spurious wake-up: %v after %v", out.Value, time.Since(start))
	}
	if _, err = invoke(t, r, "write_stdin", map[string]any{"session_id": id, "return_on": "later"}); ErrorCode(err) != "invalid_arguments" {
		t.Fatalf("bad return_on: %v", err)
	}
	if _, err = invoke(t, r, "write_stdin", map[string]any{"session_id": id, "yield_time_ms": 20001}); err == nil {
		t.Fatal("wait above the limit accepted")
	}
}
func TestLongPollSeesOutputThatWasAlreadyWaiting(t *testing.T) {
	r, _ := execForTest(t)
	out, _ := invoke(t, r, "exec_command", map[string]any{"command": "echo early; sleep 20", "yield_time_ms": 1})
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	res, err := invoke(t, r, "write_stdin", map[string]any{"session_id": asMap(t, out)["session_id"], "return_on": "output", "yield_time_ms": 10000})
	if err != nil || asMap(t, res)["stdout"] != "early\n" || time.Since(start) > time.Second {
		t.Fatalf("%v %v after %v", res.Value, err, time.Since(start))
	}
}
func TestLongPollEndsWhenTheProcessExits(t *testing.T) {
	r, _ := execForTest(t)
	out, _ := invoke(t, r, "exec_command", map[string]any{"command": "sleep 0.4; exit 3", "yield_time_ms": 1})
	start := time.Now()
	res, err := invoke(t, r, "write_stdin", map[string]any{"session_id": asMap(t, out)["session_id"], "return_on": "output", "yield_time_ms": 15000})
	v := asMap(t, res)
	if err != nil || v["running"] != false || v["exit_code"] != 3 || time.Since(start) > 3*time.Second {
		t.Fatalf("%v %v after %v", v, err, time.Since(start))
	}
}
func TestRunningResultsReportProgress(t *testing.T) {
	r, _ := execForTest(t)
	out, err := invoke(t, r, "exec_command", map[string]any{"command": "printf abc; sleep 20", "yield_time_ms": 700})
	if err != nil {
		t.Fatal(err)
	}
	v := asMap(t, out)
	if v["running"] != true || v["output_bytes"] != int64(3) || v["elapsed_ms"].(int64) < 600 || v["idle_ms"].(int64) < 300 {
		t.Fatalf("progress fields: %v", v)
	}
	if !strings.Contains(out.Text, "[running 0s; 3 B output so far, last output 0s ago; session_id=") {
		t.Fatalf("text: %q", out.Text)
	}
	finished, _ := invoke(t, r, "exec_command", map[string]any{"command": "printf done"})
	if fv := asMap(t, finished); fv["elapsed_ms"] == nil || fv["output_bytes"] != int64(4) || fv["idle_ms"] != nil {
		t.Fatalf("finished fields: %v", fv)
	}
}
func TestListTasksIsPerSession(t *testing.T) {
	r, _ := execForTest(t)
	bg, err := invokeAs(t, r, "alice", "exec_command", map[string]any{"command": "echo hello; sleep 20", "yield_time_ms": 300})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = invokeAs(t, r, "alice", "exec_command", map[string]any{"command": "exit 0"}); err != nil {
		t.Fatal(err)
	}
	out, err := invokeAs(t, r, "alice", "list_tasks", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	tasks := asMap(t, out)["tasks"].([]map[string]any)
	if len(tasks) != 2 || tasks[0]["session_id"] != asMap(t, bg)["session_id"] || tasks[0]["running"] != true || tasks[1]["running"] != false || tasks[1]["exit_code"] != 0 {
		t.Fatalf("tasks: %v", tasks)
	}
	lines := strings.Split(strings.TrimSpace(out.Text), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "running ") || !strings.Contains(lines[0], "6 B output") || !strings.Contains(lines[0], "echo hello; sleep 20") || !strings.Contains(lines[1], "exited 0 after") {
		t.Fatalf("text:\n%s", out.Text)
	}
	other, err := invokeAs(t, r, "bob", "list_tasks", map[string]any{})
	if err != nil || len(asMap(t, other)["tasks"].([]map[string]any)) != 0 || other.Text != "[no command sessions]" {
		t.Fatalf("another session saw alice's tasks: %v %v", other.Value, err)
	}
	if _, err = invokeAs(t, r, "alice", "list_tasks", map[string]any{"x": 1}); err == nil {
		t.Fatal("unknown argument accepted")
	}
}
func TestProgressIsThrottledAndSkipsPolledTools(t *testing.T) {
	old := progressMinAge
	progressMinAge = 0
	t.Cleanup(func() { progressMinAge = old })
	r, _ := execForTest(t)
	r.SetProgressInterval(150 * time.Millisecond)
	if _, err := invokeAs(t, r, "p", "exec_command", map[string]any{"command": "sleep 20", "yield_time_ms": 1}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"write_stdin", "list_tasks", "help"} {
		if got := r.ProgressFor("p", tool); got != nil {
			t.Fatalf("%s already reports running work itself: %v", tool, got)
		}
	}
	if got := r.ProgressFor("someone-else", "glob"); got != nil {
		t.Fatalf("progress leaked across sessions: %v", got)
	}
	first := r.ProgressFor("p", "glob")
	if len(first) != 1 || first[0].Kind != "task_progress" || !strings.Contains(first[0].Data["text"].(string), "exec_command ") || !strings.Contains(first[0].Data["text"].(string), "running 0s") || first[0].Data["running"] != true {
		t.Fatalf("first: %+v", first)
	}
	if again := r.ProgressFor("p", "glob"); again != nil {
		t.Fatal("progress repeated within the interval")
	}
	time.Sleep(200 * time.Millisecond)
	if later := r.ProgressFor("p", "glob"); len(later) != 1 {
		t.Fatal("progress never returned")
	}
	progressMinAge = time.Hour
	time.Sleep(200 * time.Millisecond)
	if young := r.ProgressFor("p", "glob"); young != nil {
		t.Fatalf("a job still in its first seconds was reported: %v", young)
	}
}
func TestProgressListIsCapped(t *testing.T) {
	r := registryForTest(t)
	r.SetProgressInterval(time.Millisecond)
	r.AddProgress(func(session string) []Event {
		var out []Event
		for i := 0; i < 9; i++ {
			out = append(out, Event{Session: session, Kind: "task_progress", Data: map[string]any{"text": "job"}})
		}
		return out
	})
	got := r.ProgressFor("s", "glob")
	if len(got) != 6 || got[5].Data["text"] != "+4 more running tasks; list_tasks shows all" {
		t.Fatalf("%d events, last %v", len(got), got[len(got)-1].Data)
	}
}

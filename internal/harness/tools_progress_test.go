package harness

import (
	"context"
	"encoding/json"
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
	if _, err = invoke(t, r, "write_stdin", map[string]any{"session_id": id, "yield_time_ms": 45001}); err == nil {
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
	if fv := asMap(t, finished); fv["elapsed_ms"] != nil || fv["output_bytes"] != nil || fv["idle_ms"] != nil || fv["truncated"] != nil || fv["timed_out"] != nil {
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

func TestResultsDoNotRepeatTheirOwnJob(t *testing.T) {
	ev := func(kind, id string) Event { return Event{Kind: kind, Data: map[string]any{"session_id": id}} }
	all := []Event{ev("task_progress", "a"), ev("task_finished", "a"), ev("task_progress", "b"), ev("task_finished", "b")}
	cases := []struct {
		name  string
		value any
		want  int
	}{
		{"result about a job that ended", map[string]any{"session_id": "a", "running": false}, 2},
		{"result about a job still running", map[string]any{"session_id": "a", "running": true}, 3},
		{"result about no job", map[string]any{"glob": 1}, 4},
		{"not a map", "text", 4},
	}
	for _, c := range cases {
		if got := WithoutOwn(all, c.value); len(got) != c.want {
			t.Errorf("%s: kept %d, want %d", c.name, len(got), c.want)
		}
	}
	if got := WithoutOwn(all, map[string]any{"session_id": "b", "running": false}); len(got) != 2 || got[0].Data["session_id"] != "a" {
		t.Fatal("dropped the wrong job")
	}
}

func TestAbandonedPollLeavesTheJobRunning(t *testing.T) {
	r, _ := execForTest(t)
	out, err := invoke(t, r, "exec_command", map[string]any{"command": "sleep 20", "yield_time_ms": 1})
	if err != nil || asMap(t, out)["running"] != true {
		t.Fatal(out.Value, err)
	}
	id := asMap(t, out)["session_id"]
	// The client gives up on a long wait (timeout, disconnect, notifications/cancelled).
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	b, _ := json.Marshal(map[string]any{"session_id": id, "yield_time_ms": 30000})
	if _, _, err := r.Invoke(ctx, "write_stdin", Invocation{Session: "test-session", Arguments: b}); err == nil {
		t.Fatal("the abandoned wait should report its cancellation")
	}
	time.Sleep(300 * time.Millisecond)
	after, err := invoke(t, r, "write_stdin", map[string]any{"session_id": id, "yield_time_ms": 1})
	if err != nil || asMap(t, after)["running"] != true {
		t.Fatalf("abandoning a wait killed the job: %v %v", after.Value, err)
	}
	// The same holds for an abandoned wait that wanted output, and for one that was sending input.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	b, _ = json.Marshal(map[string]any{"session_id": id, "yield_time_ms": 30000, "return_on": "output"})
	r.Invoke(ctx2, "write_stdin", Invocation{Session: "test-session", Arguments: b})
	time.Sleep(300 * time.Millisecond)
	if again, err := invoke(t, r, "write_stdin", map[string]any{"session_id": id, "yield_time_ms": 1}); err != nil || asMap(t, again)["running"] != true {
		t.Fatalf("abandoning an output wait killed the job: %v %v", again.Value, err)
	}
	// An explicit terminate still stops it.
	if res, err := invoke(t, r, "write_stdin", map[string]any{"session_id": id, "terminate": true}); err != nil || asMap(t, res)["terminated"] != true {
		t.Fatalf("%v %v", res.Value, err)
	}
}
func TestWaitCapFitsTheShortestRequestTimeoutOnTheRelayPath(t *testing.T) {
	// The cloud relay dropped a request at about 55 s while 45 s worked.
	if maxPollWait > 45000 {
		t.Fatalf("maxPollWait is %d ms; waits above 45 s are cut off on the relay path", maxPollWait)
	}
	r, _ := execForTest(t)
	if _, err := invoke(t, r, "write_stdin", map[string]any{"session_id": "x", "yield_time_ms": maxPollWait + 1}); ErrorCode(err) != "invalid_arguments" {
		t.Fatalf("%v", err)
	}
}

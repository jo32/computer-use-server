package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type limitedBuffer struct {
	mu        sync.Mutex
	b         bytes.Buffer
	all       bytes.Buffer
	total     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := MaxFileBytes - b.total
	if len(p) > left {
		p = p[:max(0, left)]
		b.truncated = true
	}
	b.total += len(p)
	b.b.Write(p)
	b.all.Write(p)
	return n, nil
}
func (b *limitedBuffer) Drain() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.b.String()
	b.b.Reset()
	return s, b.truncated
}

func (b *limitedBuffer) Snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.all.String(), b.truncated
}

type process struct {
	mu             sync.Mutex
	stdin          io.WriteCloser
	stdout, stderr limitedBuffer
	done           chan struct{}
	cancel         context.CancelFunc
	session        string
	cwd            string
	code           int
	timedOut       bool
	cancelled      bool
	ended          time.Time
}
type Processes struct {
	mu       sync.Mutex
	items    map[string]*process
	root     string
	Projects *Projects
}

func NewProcesses(root string) *Processes {
	if canonical, err := filepath.EvalSymlinks(root); err == nil {
		root = canonical
	}
	return &Processes{items: map[string]*process{}, root: root}
}
func (p *Processes) Stop() {
	p.mu.Lock()
	for _, v := range p.items {
		v.cancel()
	}
	p.mu.Unlock()
}
func (p *Processes) Register(r *Registry) {
	r.Register(Tool{Spec: Spec{Name: "exec_command", Category: "terminal", Description: "Run a host shell command. This is NOT an OS sandbox. cwd is project-relative or a permitted absolute path; project defaults to the active project. Returns a process session_id if still running after yield_time_ms; use write_stdin to poll or provide stdin. Output is capped at 1 MiB per stream. Default timeout 60 s; maximum 600 s. No PTY.", Mutating: true, Parallel: true, InputSchema: Schema(map[string]any{"command": Prop("string", "Shell command"), "project": Prop("string", "Project ID from list_projects; defaults to active project"), "cwd": Prop("string", "Project-relative or permitted absolute directory"), "timeout": Prop("integer", "Seconds, default 60, max 600"), "yield_time_ms": Prop("integer", "Initial wait, default 1000, max 10000"), "env": Prop("object", "Additional environment variables")}, "command")}, Run: p.start})
	r.Register(Tool{Spec: Spec{Name: "write_stdin", Category: "terminal", Description: "Write to or poll a running command owned by this session. Empty chars polls. terminate=true cancels the process group. close_stdin=true signals EOF. Only unread output is returned.", Mutating: true, Parallel: true, InputSchema: Schema(map[string]any{"session_id": Prop("string", "Process session_id from exec_command"), "chars": Prop("string", "Input bytes"), "yield_time_ms": Prop("integer", "Wait duration, default 1000, max 10000"), "terminate": Prop("boolean", "Cancel process"), "close_stdin": Prop("boolean", "Close stdin")}, "session_id")}, Run: p.input})
}
func (p *Processes) start(ctx context.Context, in Invocation) (Output, error) {
	var a struct {
		Command, Cwd, Project string
		Timeout               int
		Yield                 int `json:"yield_time_ms"`
		Env                   map[string]string
	}
	if err := json.Unmarshal(in.Arguments, &a); err != nil {
		return Output{}, err
	}
	if strings.TrimSpace(a.Command) == "" {
		return Output{}, errors.New("command is empty")
	}
	if a.Timeout == 0 {
		a.Timeout = 60
	}
	if a.Timeout < 1 || a.Timeout > 600 || a.Yield < 0 || a.Yield > 10000 {
		return Output{}, errors.New("invalid timeout or yield_time_ms")
	}
	if a.Cwd == "" {
		a.Cwd = "."
	}
	var cwd string
	var err error
	if p.Projects != nil {
		cwd, err = p.Projects.Directory(a.Project, a.Cwd)
	} else {
		if a.Project != "" {
			return Output{}, errors.New("projects unavailable")
		}
		if err = pathOK(a.Cwd); err == nil {
			cwd, err = filepath.EvalSymlinks(filepath.Join(p.root, a.Cwd))
			if err == nil {
				rel, e := filepath.Rel(p.root, cwd)
				if e != nil || !filepath.IsLocal(rel) {
					err = errors.New("cwd escapes workspace")
				}
			}
		}
	}
	if err != nil {
		return Output{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, pr := range p.items {
		select {
		case <-pr.done:
			if time.Since(pr.ended) > 10*time.Minute {
				delete(p.items, id)
			}
		default:
		}
	}
	if len(p.items) >= 128 {
		return Output{}, errors.New("process capacity reached; wait for old sessions to expire")
	}
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	processCtx, cancel := context.WithTimeout(context.Background(), time.Duration(a.Timeout)*time.Second)
	pr := &process{done: make(chan struct{}), cancel: cancel, session: in.Session, cwd: cwd}
	cmd := exec.CommandContext(processCtx, "/bin/sh", "-c", a.Command)
	cmd.Dir = cwd
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "LANG=en_US.UTF-8", "DEBIAN_FRONTEND=noninteractive", "TERM=dumb"}
	for k, v := range a.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") {
			cancel()
			return Output{}, errors.New("invalid environment key")
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	configureProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout = &pr.stdout
	cmd.Stderr = &pr.stderr
	pr.stdin, err = cmd.StdinPipe()
	if err != nil {
		cancel()
		return Output{}, err
	}
	if err = cmd.Start(); err != nil {
		cancel()
		return Output{}, err
	}
	id := ID()
	p.items[id] = pr
	go func() {
		err := cmd.Wait()
		cleanupProcess(cmd)
		pr.code = 0
		if err != nil {
			pr.code = -1
			var e *exec.ExitError
			if errors.As(err, &e) {
				pr.code = e.ExitCode()
			}
		}
		pr.timedOut = errors.Is(processCtx.Err(), context.DeadlineExceeded)
		pr.cancelled = errors.Is(processCtx.Err(), context.Canceled)
		pr.ended = time.Now()
		pr.stdin.Close()
		cancel()
		close(pr.done)
	}()
	// Do not hold the manager lock while waiting: the local emergency stop must stay responsive.
	p.mu.Unlock()
	out, err := poll(ctx, pr, id, a.Yield)
	if err == nil && out.Value.(map[string]any)["running"] == true {
		out.Cancel = pr.cancel
		out.Snapshot = func() any { return processSnapshot(pr, id) }
		out.Completion = func() (any, error) {
			<-pr.done
			value := processSnapshot(pr, id)
			if pr.cancelled {
				return value, context.Canceled
			}
			if pr.code != 0 {
				return value, fmt.Errorf("command exited with code %d", pr.code)
			}
			return value, nil
		}
	}
	p.mu.Lock()
	return out, err
}
func (p *Processes) input(ctx context.Context, in Invocation) (Output, error) {
	var a struct {
		ID        string `json:"session_id"`
		Chars     string
		Yield     int `json:"yield_time_ms"`
		Terminate bool
		Close     bool `json:"close_stdin"`
	}
	if err := json.Unmarshal(in.Arguments, &a); err != nil {
		return Output{}, err
	}
	if a.Yield < 0 || a.Yield > 10000 {
		return Output{}, errors.New("invalid yield_time_ms")
	}
	p.mu.Lock()
	pr, ok := p.items[a.ID]
	p.mu.Unlock()
	if !ok || pr.session != in.Session {
		return Output{}, errors.New("process session not found")
	}
	if a.Terminate {
		pr.cancel()
	}
	if a.Chars != "" {
		done := make(chan error, 1)
		go func() { pr.mu.Lock(); defer pr.mu.Unlock(); _, err := io.WriteString(pr.stdin, a.Chars); done <- err }()
		select {
		case err := <-done:
			if err != nil {
				return Output{}, err
			}
		case <-ctx.Done():
			pr.cancel()
			return Output{}, ctx.Err()
		case <-time.After(5 * time.Second):
			pr.cancel()
			return Output{}, errors.New("stdin write timed out")
		}
	}
	if a.Close {
		pr.stdin.Close()
	}
	return poll(ctx, pr, a.ID, a.Yield)
}
func poll(ctx context.Context, pr *process, id string, yield int) (Output, error) {
	if yield == 0 {
		yield = 1000
	}
	timer := time.NewTimer(time.Duration(yield) * time.Millisecond)
	defer timer.Stop()
	finished := false
	select {
	case <-pr.done:
		finished = true
	case <-timer.C:
	case <-ctx.Done():
		pr.cancel()
		return Output{}, ctx.Err()
	}
	stdout, a := pr.stdout.Drain()
	stderr, b := pr.stderr.Drain()
	value := map[string]any{"cwd": pr.cwd, "stdout": stdout, "stderr": stderr, "truncated": a || b, "session_id": id, "running": !finished}
	if finished {
		value["exit_code"] = pr.code
		value["timed_out"] = pr.timedOut
		if pr.cancelled {
			return Output{Value: value}, context.Canceled
		}
		if pr.code != 0 {
			return Output{Value: value}, fmt.Errorf("command exited with code %d", pr.code)
		}
	}
	return Output{Value: value}, nil
}

// Snapshot never drains the stream consumed by a remote agent's write_stdin calls.
func processSnapshot(pr *process, id string) map[string]any {
	stdout, a := pr.stdout.Snapshot()
	stderr, b := pr.stderr.Snapshot()
	value := map[string]any{"cwd": pr.cwd, "stdout": stdout, "stderr": stderr, "truncated": a || b, "session_id": id, "running": true}
	select {
	case <-pr.done:
		value["running"] = false
		value["exit_code"] = pr.code
		value["timed_out"] = pr.timedOut
		value["cancelled"] = pr.cancelled
	default:
	}
	return value
}

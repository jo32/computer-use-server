package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// responseCap bounds the text returned per stream per call; longer output
	// is returned as its head and tail and the whole stream is saved to a spill file.
	responseCap = 30 * 1024
	headKeep    = 8 * 1024
	// unreadCap bounds unread output kept in memory between polls.
	unreadCap = 4 << 20
	// The console's live view keeps the start and the end of the stream.
	snapHead = 64 << 10
	snapTail = 256 << 10
	// spillCap bounds the file saved for one stream.
	spillCap      = 64 << 20
	spillLifetime = 24 * time.Hour
	maxTimeout    = 4 * 60 * 60
	// maxPollWait bounds a write_stdin wait; keep it under the shortest HTTP timeout on the path.
	maxPollWait = 20000
)

// streamBuf collects one output stream of a process.
type streamBuf struct {
	mu         sync.Mutex
	unread     []byte
	dropped    int64
	head, tail []byte
	omitted    int64
	total      int64
	spillDir   string
	spillName  string
	spill      *os.File
	spillBytes int64
	last       time.Time
	signal     chan struct{}
}

func newStreamBuf(spillDir, name string) *streamBuf {
	return &streamBuf{spillDir: spillDir, spillName: name}
}
func (b *streamBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if b.spill == nil && b.spillDir != "" && b.total+int64(n) > responseCap {
		// Everything written so far is still in head: it is at most responseCap.
		if f, err := os.OpenFile(filepath.Join(b.spillDir, b.spillName), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600); err == nil {
			b.spill = f
			f.Write(b.head)
			b.spillBytes = int64(len(b.head))
		}
	}
	if b.spill != nil && b.spillBytes < spillCap {
		keep := p
		if room := spillCap - b.spillBytes; int64(len(keep)) > room {
			keep = keep[:room]
		}
		b.spill.Write(keep)
		b.spillBytes += int64(len(keep))
	}
	b.total += int64(n)
	b.last = time.Now()
	if b.signal != nil {
		select {
		case b.signal <- struct{}{}:
		default:
		}
	}
	b.unread = append(b.unread, p...)
	if len(b.unread) > unreadCap {
		b.dropped += int64(len(b.unread) - unreadCap)
		b.unread = append([]byte(nil), b.unread[len(b.unread)-unreadCap:]...)
	}
	rest := p
	if room := snapHead - len(b.head); room > 0 {
		take := min(room, len(rest))
		b.head = append(b.head, rest[:take]...)
		rest = rest[take:]
	}
	if len(rest) > 0 {
		b.tail = append(b.tail, rest...)
		if extra := len(b.tail) - snapTail; extra > 0 {
			b.omitted += int64(extra)
			b.tail = append([]byte(nil), b.tail[extra:]...)
		}
	}
	return n, nil
}

func (b *streamBuf) unreadLen() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.unread)
}
func (b *streamBuf) totalBytes() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}
func (b *streamBuf) lastWrite() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last
}

// Close releases the spill file once the process has ended.
func (b *streamBuf) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spill != nil {
		b.spill.Close()
	}
}

// path names the spill file for read_file, or "" when the output fit in memory.
func (b *streamBuf) path() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spill == nil {
		return ""
	}
	return spillPrefix + b.spillName
}
func clean(b []byte) string { return strings.ToValidUTF8(string(b), "�") }

// headBytes shortens b to at most n bytes without splitting a character.
func headBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	for n > 0 && b[n]&0xC0 == 0x80 {
		n--
	}
	return b[:n]
}
func tailBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	b = b[len(b)-n:]
	for len(b) > 0 && b[0]&0xC0 == 0x80 {
		b = b[1:]
	}
	return b
}

// elide keeps the start and end of data when it exceeds limit.
func elide(data []byte, limit, head int, extra int64) (string, bool) {
	if len(data) <= limit && extra == 0 {
		return clean(data), false
	}
	if len(data) <= limit {
		return clean(data), true
	}
	h, t := headBytes(data, head), tailBytes(data, limit-head)
	omitted := int64(len(data)-len(h)-len(t)) + extra
	return clean(h) + fmt.Sprintf("\n… [%d bytes omitted] …\n", omitted) + clean(t), true
}

// Drain returns the output not yet read, cut to the response cap.
func (b *streamBuf) Drain() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, dropped := b.unread, b.dropped
	b.unread, b.dropped = nil, 0
	text, cut := elide(data, responseCap, headKeep, dropped)
	return text, cut
}

// Snapshot never consumes: it is the console's view of everything so far.
func (b *streamBuf) Snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.omitted == 0 {
		return clean(append(append([]byte(nil), b.head...), b.tail...)), false
	}
	return clean(b.head) + fmt.Sprintf("\n… [%d bytes omitted] …\n", b.omitted) + clean(b.tail), true
}

type process struct {
	mu             sync.Mutex
	stdin          io.WriteCloser
	stdout, stderr *streamBuf
	done           chan struct{}
	cancel         context.CancelFunc
	session        string
	cwd            string
	timeout        int
	started        time.Time
	command        string
	activity       chan struct{}
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
	// SpillDir keeps the full output of commands whose output exceeds a response.
	SpillDir string
}

func NewProcesses(root string) *Processes {
	if canonical, err := filepath.EvalSymlinks(root); err == nil {
		root = canonical
	}
	return &Processes{items: map[string]*process{}, root: root}
}

// tagged adds the project a command's working directory belongs to.
func (p *Processes) tagged(run Handler) Handler {
	return func(ctx context.Context, in Invocation) (Output, error) {
		out, err := run(ctx, in)
		tagProject(p.Projects, out, "cwd")
		return out, err
	}
}
func (p *Processes) Stop() {
	p.mu.Lock()
	for _, v := range p.items {
		v.cancel()
	}
	p.mu.Unlock()
}
func (p *Processes) Register(r *Registry) {
	r.Register(Tool{Spec: Spec{Name: "exec_command", Category: "terminal", Description: "Run a shell command on the host (NOT an OS sandbox). cwd is project-relative or a permitted absolute path. A non-zero exit code is returned as data (exit_code), not as a tool error. Output is returned as text, cut to the start and end when longer than 30 KiB; the full output is then saved and its path reported as stdout_path/stderr_path for read_file. If the command is still running after yield_time_ms the result has a session_id: poll or answer it with write_stdin. Set background=true for long jobs (servers, builds, test runs): it returns at once and a task_finished notification is sent when the job ends. login_shell=true loads your shell profile first (needed for Homebrew, nvm, pyenv tools that are not on the default PATH). There is no PTY. For file work prefer read_file, edit_file, write_file, search_files and glob over cat, sed, grep and find.", Mutating: true, Parallel: true, InputSchema: Schema(map[string]any{"command": Prop("string", "Shell command"), "project": projectProp, "cwd": Prop("string", "Project-relative or permitted absolute directory"), "timeout": limited("integer", "Seconds before the command is killed; default 60 (1800 with background), max 14400", 1, maxTimeout), "yield_time_ms": limited("integer", "How long to wait for output before returning a session_id, default 1000", 0, 10000), "background": Prop("boolean", "Return immediately and notify when the command finishes"), "login_shell": Prop("boolean", "Run through the user's login shell so profile PATH entries apply"), "env": Prop("object", "Additional environment variables")}, "command")}, Run: p.tagged(p.start)})
	r.Register(Tool{Spec: Spec{Name: "list_tasks", Category: "terminal", Description: "List the command sessions this session started, running and recently finished: session_id, command, elapsed time, bytes of output, how long since the last output, exit code, and saved-output paths. A quick way to see whether a background job is progressing, stuck, or done.", Parallel: true, InputSchema: Schema(map[string]any{})}, Run: p.listTasks})
	r.AddProgress(p.progress)
	r.Register(Tool{Spec: Spec{Name: "write_stdin", Category: "terminal", Description: "Write to or poll a running exec_command session. An empty chars value polls and returns only output not yet read; a running result reports elapsed_ms, output_bytes and idle_ms so you can tell progress from a hang. Use return_on=output with a long yield_time_ms to follow a job without busy polling. terminate=true kills the process group; close_stdin=true sends EOF.", Mutating: true, Parallel: true, InputSchema: Schema(map[string]any{"session_id": Prop("string", "Process session_id from exec_command"), "chars": Prop("string", "Input bytes"), "yield_time_ms": limited("integer", "Longest wait for output, default 1000, max 20000", 0, maxPollWait), "return_on": enum("timeout (default) waits the full time unless the process exits; output returns as soon as new output arrives, a long poll for following logs", "timeout", "output"), "terminate": Prop("boolean", "Cancel process"), "close_stdin": Prop("boolean", "Close stdin")}, "session_id")}, Run: p.tagged(p.input)})
}

// commandEnv keeps secrets out of children but gives them a usable PATH: apps
// launched from the desktop inherit only /usr/bin:/bin.
func commandEnv() []string {
	home := os.Getenv("HOME")
	path := os.Getenv("PATH")
	seen := map[string]bool{}
	for _, d := range filepath.SplitList(path) {
		seen[d] = true
	}
	extras := []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/local/sbin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	if home != "" {
		extras = append(extras, filepath.Join(home, ".local/bin"), filepath.Join(home, "go/bin"), filepath.Join(home, ".cargo/bin"), filepath.Join(home, ".bun/bin"), filepath.Join(home, ".volta/bin"))
	}
	for _, d := range extras {
		if seen[d] {
			continue
		}
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			if path != "" {
				path += string(os.PathListSeparator)
			}
			path += d
			seen[d] = true
		}
	}
	env := []string{"PATH=" + path, "HOME=" + home, "LANG=en_US.UTF-8", "DEBIAN_FRONTEND=noninteractive", "TERM=dumb"}
	for _, k := range []string{"USER", "LOGNAME", "SHELL", "TMPDIR"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// loginShell picks a POSIX-compatible login shell.
func loginShell() string {
	shell := os.Getenv("SHELL")
	switch filepath.Base(shell) {
	case "bash", "zsh", "sh", "dash", "ksh":
		if info, err := os.Stat(shell); err == nil && !info.IsDir() {
			return shell
		}
	}
	for _, s := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(s); err == nil {
			return s
		}
	}
	return "/bin/sh"
}

// pruneSpill removes saved output older than spillLifetime.
func pruneSpill(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > spillLifetime && spillName.MatchString(e.Name()) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
func (p *Processes) start(ctx context.Context, in Invocation) (Output, error) {
	var a struct {
		Command, Cwd, Project string
		Timeout               int
		Yield                 int `json:"yield_time_ms"`
		Background            bool
		LoginShell            bool `json:"login_shell"`
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
		if a.Background {
			a.Timeout = 1800
		}
	}
	if a.Timeout < 1 || a.Timeout > maxTimeout || a.Yield < 0 || a.Yield > 10000 {
		return Output{}, errors.New("invalid timeout or yield_time_ms")
	}
	if a.Cwd == "" {
		a.Cwd = "."
	}
	var cwd string
	var err error
	if p.Projects != nil {
		cwd, err = p.Projects.Directory(in.Session, a.Project, a.Cwd)
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
	spillDir := ""
	if p.SpillDir != "" && os.MkdirAll(p.SpillDir, 0700) == nil {
		spillDir = p.SpillDir
		pruneSpill(spillDir)
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
	id := ID()
	processCtx, cancel := context.WithTimeout(context.Background(), time.Duration(a.Timeout)*time.Second)
	pr := &process{done: make(chan struct{}), cancel: cancel, session: in.Session, cwd: cwd, timeout: a.Timeout, started: time.Now(), command: clipCommand(a.Command), activity: make(chan struct{}, 1), stdout: newStreamBuf(spillDir, id+".stdout"), stderr: newStreamBuf(spillDir, id+".stderr")}
	pr.stdout.signal, pr.stderr.signal = pr.activity, pr.activity
	shell, flags := "/bin/sh", []string{"-c"}
	if a.LoginShell {
		shell, flags = loginShell(), []string{"-l", "-c"}
	}
	cmd := exec.CommandContext(processCtx, shell, append(flags, a.Command)...)
	cmd.Dir = cwd
	cmd.Env = commandEnv()
	for k, v := range a.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") {
			cancel()
			return Output{}, errors.New("invalid environment key")
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	configureProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout = pr.stdout
	cmd.Stderr = pr.stderr
	pr.stdin, err = cmd.StdinPipe()
	if err != nil {
		cancel()
		return Output{}, err
	}
	if err = cmd.Start(); err != nil {
		cancel()
		return Output{}, err
	}
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
		pr.stdout.Close()
		pr.stderr.Close()
		cancel()
		close(pr.done)
	}()
	// Do not hold the manager lock while waiting: the local emergency stop must stay responsive.
	p.mu.Unlock()
	yield := a.Yield
	if a.Background {
		yield = -1
	}
	out, err := poll(ctx, pr, id, yield, false)
	if err == nil && out.Value.(map[string]any)["running"] == true {
		out.Cancel = pr.cancel
		out.Snapshot = func() any { return processSnapshot(pr, id) }
		out.Completion = func() (any, error) {
			<-pr.done
			value := processSnapshot(pr, id)
			if pr.cancelled {
				return value, context.Canceled
			}
			if pr.timedOut {
				return value, fmt.Errorf("command timed out after %d s", pr.timeout)
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
		Yield     int    `json:"yield_time_ms"`
		ReturnOn  string `json:"return_on"`
		Terminate bool
		Close     bool `json:"close_stdin"`
	}
	if err := json.Unmarshal(in.Arguments, &a); err != nil {
		return Output{}, err
	}
	if a.Yield < 0 || a.Yield > maxPollWait {
		return Output{}, errors.New("invalid yield_time_ms")
	}
	p.mu.Lock()
	pr, ok := p.items[a.ID]
	p.mu.Unlock()
	if !ok || pr.session != in.Session {
		return Output{}, &ToolError{"session_not_found", "process session not found"}
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
	return poll(ctx, pr, a.ID, a.Yield, a.ReturnOn == "output")
}

// poll waits up to yield ms (default 1000; negative returns at once) and
// returns the unread output.
func poll(ctx context.Context, pr *process, id string, yield int, onOutput bool) (Output, error) {
	switch {
	case yield == 0:
		yield = 1000
	case yield < 0:
		yield = 20
	}
	timer := time.NewTimer(time.Duration(yield) * time.Millisecond)
	defer timer.Stop()
	finished := false
	// With onOutput, output that is already waiting is returned at once, and
	// otherwise the wait ends when the next output arrives.
	var wake <-chan struct{}
	if onOutput {
		if pr.stdout.unreadLen()+pr.stderr.unreadLen() > 0 {
			timer.Reset(0)
		} else {
			wake = pr.activity
		}
	}
	select {
	case <-pr.done:
		finished = true
	case <-timer.C:
	case <-wake:
		// Let a burst of lines arrive together.
		select {
		case <-time.After(50 * time.Millisecond):
		case <-pr.done:
		case <-ctx.Done():
			pr.cancel()
			return Output{}, ctx.Err()
		}
	case <-ctx.Done():
		pr.cancel()
		return Output{}, ctx.Err()
	}
	if !finished {
		select {
		case <-pr.done:
			finished = true
		default:
		}
	}
	stdout, a := pr.stdout.Drain()
	stderr, b := pr.stderr.Drain()
	// Output just drained must not wake the next long poll.
	select {
	case <-pr.activity:
	default:
	}
	value := map[string]any{"cwd": pr.cwd, "stdout": stdout, "stderr": stderr, "truncated": a || b, "session_id": id, "running": !finished}
	addPaths(value, pr)
	addProgress(value, pr, finished)
	out := Output{Value: value, TextKeys: []string{"stdout", "stderr"}}
	var err error
	if finished {
		value["exit_code"] = pr.code
		value["timed_out"] = pr.timedOut
		switch {
		case pr.cancelled:
			err = context.Canceled
		case pr.timedOut:
			err = &ToolError{"timeout", fmt.Sprintf("command timed out after %d s", pr.timeout)}
		case pr.code != 0:
			out.Failure = fmt.Sprintf("command exited with code %d", pr.code)
		}
	}
	out.Text = processText(value)
	return out, err
}

// addProgress reports how long the process has run, how much it printed and
// how long it has been silent, so an agent can tell a busy job from a hung one.
func addProgress(value map[string]any, pr *process, finished bool) {
	end := time.Now()
	if finished {
		end = pr.ended
	}
	value["elapsed_ms"] = end.Sub(pr.started).Milliseconds()
	value["output_bytes"] = pr.stdout.totalBytes() + pr.stderr.totalBytes()
	if !finished {
		value["idle_ms"] = time.Since(pr.lastActivity()).Milliseconds()
	}
}
func (pr *process) lastActivity() time.Time {
	last := pr.started
	for _, b := range []*streamBuf{pr.stdout, pr.stderr} {
		if t := b.lastWrite(); t.After(last) {
			last = t
		}
	}
	return last
}
func addPaths(value map[string]any, pr *process) {
	if path := pr.stdout.path(); path != "" {
		value["stdout_path"] = path
	}
	if path := pr.stderr.path(); path != "" {
		value["stderr_path"] = path
	}
}

// processText renders a result the way a terminal would show it.
func processText(v map[string]any) string {
	var b strings.Builder
	stdout, _ := v["stdout"].(string)
	stderr, _ := v["stderr"].(string)
	b.WriteString(stdout)
	if stdout != "" && !strings.HasSuffix(stdout, "\n") {
		b.WriteByte('\n')
	}
	if stderr != "" {
		b.WriteString("[stderr]\n" + stderr)
		if !strings.HasSuffix(stderr, "\n") {
			b.WriteByte('\n')
		}
	}
	if v["truncated"] == true {
		b.WriteString("[output cut to its start and end")
		if p, ok := v["stdout_path"].(string); ok {
			b.WriteString("; stdout: read_file path " + p)
		}
		if p, ok := v["stderr_path"].(string); ok {
			b.WriteString("; stderr: read_file path " + p)
		}
		b.WriteString("]\n")
	}
	if v["running"] == true {
		fmt.Fprintf(&b, "[running %s; %s output so far, last output %s ago; session_id=%v; poll or answer it with write_stdin]\n", humanDuration(asInt64(v["elapsed_ms"])), humanSize(asInt64(v["output_bytes"])), humanDuration(asInt64(v["idle_ms"])), v["session_id"])
	} else if code, ok := v["exit_code"]; ok {
		if v["timed_out"] == true {
			b.WriteString("[timed out]\n")
		}
		fmt.Fprintf(&b, "[exit code %v]\n", code)
	}
	return b.String()
}

// Snapshot never drains the stream consumed by a remote agent's write_stdin calls.
func processSnapshot(pr *process, id string) map[string]any {
	stdout, a := pr.stdout.Snapshot()
	stderr, b := pr.stderr.Snapshot()
	value := map[string]any{"cwd": pr.cwd, "stdout": stdout, "stderr": stderr, "truncated": a || b, "session_id": id, "running": true}
	addPaths(value, pr)
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

func asInt64(v any) int64 {
	n, _ := v.(int64)
	return n
}

// humanDuration renders milliseconds as 45s, 2m10s or 1h05m.
func humanDuration(ms int64) string {
	s := ms / 1000
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
}
func clipCommand(c string) string {
	c = strings.Join(strings.Fields(c), " ")
	if len(c) > 160 {
		c = strings.ToValidUTF8(c[:160], "") + "…"
	}
	return c
}

// taskInfo is the status of one command session.
func taskInfo(id string, pr *process) map[string]any {
	finished := false
	select {
	case <-pr.done:
		finished = true
	default:
	}
	v := map[string]any{"session_id": id, "command": pr.command, "cwd": pr.cwd, "running": !finished}
	addProgress(v, pr, finished)
	addPaths(v, pr)
	if finished {
		v["exit_code"] = pr.code
		v["timed_out"] = pr.timedOut
		v["cancelled"] = pr.cancelled
		v["ended_ago_ms"] = time.Since(pr.ended).Milliseconds()
	}
	return v
}

// tasks lists the command sessions a client session started, oldest first.
func (p *Processes) tasks(session string) []map[string]any {
	p.mu.Lock()
	type entry struct {
		id string
		pr *process
	}
	var mine []entry
	for id, pr := range p.items {
		if pr.session == session {
			mine = append(mine, entry{id, pr})
		}
	}
	p.mu.Unlock()
	sort.Slice(mine, func(i, j int) bool { return mine[i].pr.started.Before(mine[j].pr.started) })
	out := make([]map[string]any, 0, len(mine))
	for _, e := range mine {
		out = append(out, taskInfo(e.id, e.pr))
	}
	return out
}

// taskLine renders one task for text results.
func taskLine(t map[string]any) string {
	state := "running " + humanDuration(asInt64(t["elapsed_ms"]))
	detail := fmt.Sprintf("%s output, last output %s ago", humanSize(asInt64(t["output_bytes"])), humanDuration(asInt64(t["idle_ms"])))
	if t["running"] != true {
		state = fmt.Sprintf("exited %v after %s", t["exit_code"], humanDuration(asInt64(t["elapsed_ms"])))
		if t["timed_out"] == true {
			state = "timed out after " + humanDuration(asInt64(t["elapsed_ms"]))
		}
		detail = fmt.Sprintf("%s output, finished %s ago", humanSize(asInt64(t["output_bytes"])), humanDuration(asInt64(t["ended_ago_ms"])))
	}
	line := fmt.Sprintf("%v  %s  (%s)  %v", t["session_id"], state, detail, t["command"])
	if path, ok := t["stdout_path"].(string); ok {
		line += "  stdout: " + path
	}
	return line
}

// progress reports this session's long-running commands as a status line, for
// ProgressFor. Commands younger than five seconds are skipped: their own
// result is still in the agent's hands.
// progressMinAge keeps a job from being reported in the call that started it.
var progressMinAge = 5 * time.Second

func (p *Processes) progress(session string) []Event {
	var out []Event
	for _, t := range p.tasks(session) {
		if t["running"] != true || asInt64(t["elapsed_ms"]) < progressMinAge.Milliseconds() {
			continue
		}
		data := map[string]any{"text": "exec_command " + taskLine(t)}
		for k, v := range t {
			data[k] = v
		}
		out = append(out, Event{Session: session, Kind: "task_progress", Data: data})
	}
	return out
}
func (p *Processes) listTasks(ctx context.Context, in Invocation) (Output, error) {
	var a struct{}
	if err := Decode(in.Arguments, &a); err != nil {
		return Output{}, err
	}
	tasks := p.tasks(in.Session)
	var text strings.Builder
	for _, t := range tasks {
		text.WriteString(taskLine(t) + "\n")
	}
	if text.Len() == 0 {
		text.WriteString("[no command sessions]")
	}
	return Output{Value: map[string]any{"tasks": tasks}, Text: text.String(), TextKeys: []string{"tasks"}}, ctx.Err()
}

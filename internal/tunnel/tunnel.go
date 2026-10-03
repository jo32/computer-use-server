// Package tunnel manages opt-in Cloudflare quick and named tunnels.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Status struct {
	Mode       string   `json:"mode"`
	State      string   `json:"state"`
	Message    string   `json:"message"`
	URL        string   `json:"url,omitempty"`
	Error      string   `json:"error,omitempty"`
	Executable string   `json:"executable,omitempty"`
	Logs       []string `json:"logs,omitempty"`
}

type Options struct {
	Dir            string
	Command        string
	Changed        func()
	RedactSecrets  func(...string)
	ProbeClient    *http.Client
	StartupTimeout time.Duration
	// HealthInterval is how often a quick tunnel checks its public address.
	HealthInterval time.Duration
	// Lookup reports whether the internet is reachable (tests replace it).
	Lookup func(host string) error
}

type operation struct {
	verified          bool
	connectionVersion uint64
	fixed             fixedConfig
	ctx               context.Context
	cancel            context.CancelFunc
	done              chan struct{}
	ready             chan struct{}
	registered        bool
	host              string
	announced         bool
	target            string
	// unhealthy: the public address stopped answering, so the URL stays withdrawn.
	unhealthy bool
	// recycle: replace this tunnel once it exits. A user stop clears it.
	recycle  bool
	recycles int
}

type Manager struct {
	mu         sync.Mutex
	opts       Options
	status     Status
	run        *operation
	closed     bool
	fixed      fixedConfig
	fixedError string
	quickPath  string
}

func New(opts Options) *Manager {
	if opts.StartupTimeout == 0 {
		opts.StartupTimeout = 90 * time.Second
	}
	m := &Manager{opts: opts, status: Status{Mode: "quick", State: "stopped", Message: "公网分享未开启"}}
	m.loadFixed()
	if m.fixed.Token != "" {
		m.status.Mode = "fixed"
	}
	if opts.RedactSecrets != nil {
		opts.RedactSecrets(m.fixed.Token, m.fixed.AccessPath)
	}
	return m
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	s.Logs = append([]string(nil), s.Logs...)
	return s
}

// Start returns promptly. Discovery, optional installation and connection run in
// the background. Repeated starts share the current operation.
func (m *Manager) Start(target string) error { return m.StartMode(target, "quick") }

func (m *Manager) StartMode(target, mode string) error {
	if mode != "quick" && mode != "fixed" {
		return errors.New("未知的公网链接类型")
	}
	if err := validateTarget(target); err != nil {
		return err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("隧道服务已关闭")
	}
	if m.run != nil {
		if m.status.Mode != mode {
			m.mu.Unlock()
			return errors.New("请先关闭当前分享，再切换链接类型")
		}
		stopping := m.status.State == "stopping"
		m.mu.Unlock()
		if stopping {
			return errors.New("正在关闭分享，请稍后重试")
		}
		return nil
	}
	if mode == "fixed" && (m.fixed.Token == "" || m.fixedError != "") {
		m.mu.Unlock()
		return errors.New("请先保存固定域名和 Tunnel Token")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &operation{ctx: ctx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}), target: target}
	message := "正在创建免登录隧道…"
	if mode == "fixed" {
		r.fixed = m.fixed
		r.host = m.fixed.URL
		message = "正在连接固定域名隧道…"
	}
	m.run = r
	m.status = Status{Mode: mode, State: "starting", Message: message}
	m.mu.Unlock()
	m.changed()
	go m.execute(r, target)
	return nil
}

// Stop revokes the displayed URLs immediately and waits for the process (or
// download) to exit. A new start cannot overlap a stopping operation.
func (m *Manager) Stop() error {
	m.mu.Lock()
	r := m.run
	if r == nil {
		m.status.State, m.status.Message = "stopped", "公网分享已关闭"
		m.status.URL, m.status.Error = "", ""
		m.mu.Unlock()
		m.changed()
		return nil
	}
	m.status.State, m.status.Message, m.status.URL = "stopping", "正在关闭公网分享…", ""
	r.recycle = false // a user stop always wins over an automatic replacement
	m.mu.Unlock()
	r.cancel()
	m.changed()
	select {
	case <-r.done:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("关闭隧道超时，正在等待进程退出")
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	_ = m.Stop()
}

func (m *Manager) changed() {
	if m.opts.Changed != nil {
		m.opts.Changed()
	}
}

func (m *Manager) setState(r *operation, state, message, executable string) {
	m.mu.Lock()
	if m.run == r && r.ctx.Err() == nil && m.status.State != "stopping" {
		m.status.State, m.status.Message = state, message
		m.status.Executable = executable
	}
	m.mu.Unlock()
	m.changed()
}

func (m *Manager) finish(r *operation, err error) {
	m.mu.Lock()
	if m.run == r {
		m.status.URL = ""
		if r.ctx.Err() != nil && r.recycle && !m.closed {
			// The public address stopped answering. Replace the tunnel in the same
			// locked step, so a user stop cannot slip in between and be overridden.
			ctx, cancel := context.WithCancel(context.Background())
			next := &operation{ctx: ctx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}), target: r.target, recycles: r.recycles + 1}
			m.status.State, m.status.Message, m.status.Error = "starting", "公网链接已失效，正在创建新的免登录隧道…", ""
			m.run = next
			go m.execute(next, r.target)
		} else if r.ctx.Err() != nil {
			m.status.State, m.status.Message, m.status.Error = "stopped", "公网分享已关闭", ""
		} else {
			if err == nil {
				err = errors.New("cloudflared 意外退出")
			}
			m.status.State, m.status.Message, m.status.Error = "error", "分享未能保持连接，请重试", err.Error()
		}
		if m.run == r {
			m.run = nil
		}
	}
	close(r.done)
	m.mu.Unlock()
	r.cancel()
	m.changed()
}

func (m *Manager) execute(r *operation, target string) {
	var runErr error
	defer func() { m.finish(r, runErr) }()
	command := discover(m.opts.Dir, m.opts.Command)
	if command == "" {
		if m.opts.Command != "" {
			runErr = errors.New("找不到指定的 cloudflared 程序")
			return
		}
		m.setState(r, "installing", "首次使用，正在下载 Cloudflare 官方程序…", "")
		command, runErr = install(r.ctx, m.opts.Dir)
		if runErr != nil {
			return
		}
	}
	if err := r.ctx.Err(); err != nil {
		runErr = err
		return
	}
	message := "正在创建免登录隧道…"
	if r.fixed.Token != "" {
		message = "正在连接固定域名隧道…"
	}
	m.setState(r, "starting", message, command)
	// Ignore the user's named-tunnel configuration and credential environment.
	// Nothing is written to ~/.cloudflared and no account login is performed.
	dir, err := os.MkdirTemp("", "readyrig-quick-tunnel-")
	if err != nil {
		runErr = err
		return
	}
	defer os.RemoveAll(dir)
	config := filepath.Join(dir, "config.yml")
	if err = os.WriteFile(config, []byte("{}\n"), 0600); err != nil {
		runErr = err
		return
	}
	args := []string{"tunnel", "--config", config, "--no-autoupdate", "--metrics", "127.0.0.1:0", "--protocol", "http2", "--loglevel", "info", "--grace-period", "1s"}
	if r.fixed.Token != "" {
		tokenFile := filepath.Join(dir, "token")
		if err = os.WriteFile(tokenFile, []byte(r.fixed.Token), 0600); err != nil {
			runErr = errors.New("无法准备隧道凭证")
			return
		}
		args = append(args, "run", "--token-file", tokenFile)
	} else {
		args = append(args, "--url", target)
	}
	cmd := exec.CommandContext(r.ctx, command, args...)
	cmd.Dir = dir
	cmd.Env = cleanEnv(os.Environ())
	cmd.WaitDelay = 2 * time.Second
	configureProcess(cmd)
	output := &lineWriter{emit: func(line string) { m.observe(r, line) }}
	cmd.Stdout, cmd.Stderr = output, output
	if err = cmd.Start(); err != nil {
		runErr = fmt.Errorf("启动 cloudflared: %w", err)
		return
	}
	if r.fixed.Token != "" {
		go m.monitorFixed(r, target)
	} else {
		go m.monitorQuick(r, target)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	timer := time.NewTimer(m.opts.StartupTimeout)
	defer timer.Stop()
	select {
	case runErr = <-wait:
	case <-r.ready:
		timer.Stop()
		runErr = <-wait
	case <-timer.C:
		// Keep a timeout distinguishable from an explicit stop.
		runErr = errors.New("连接 Cloudflare 超时，请检查网络后重试")
		if r.fixed.Token != "" {
			runErr = errors.New("固定链接连接超时：请检查 Tunnel Token、域名 DNS，以及应用路由是否指向 " + target)
		}
		_ = cmd.Cancel()
		<-wait
	}
	output.flush()
}

var quickURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com\b`)

func (m *Manager) observe(r *operation, line string) {
	if r.fixed.Token != "" {
		line = strings.ReplaceAll(line, r.fixed.Token, "[REDACTED]")
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	m.mu.Lock()
	if m.run != r || r.ctx.Err() != nil || m.status.State == "stopping" {
		m.mu.Unlock()
		return
	}
	m.status.Logs = append(m.status.Logs, line)
	if len(m.status.Logs) > 80 {
		m.status.Logs = m.status.Logs[len(m.status.Logs)-80:]
	}
	if host := quickURL.FindString(line); host != "" && r.fixed.Token == "" {
		r.host = host
	}
	if strings.Contains(line, "Registered tunnel connection") {
		r.registered = true
	}
	if strings.Contains(line, "Unregistered tunnel connection") || strings.Contains(line, "Failed to serve tunnel connection") {
		r.registered = false
		r.verified = false
		r.connectionVersion++
		m.status.State, m.status.Message, m.status.URL = "starting", "连接中断，正在重新连接…", ""
	}
	if r.host != "" && r.registered && !r.unhealthy {
		if r.fixed.Token == "" || r.verified {
			m.publishReady(r)
		} else {
			m.status.Message = "隧道已连接，正在验证固定域名路由…"
		}
	}
	m.mu.Unlock()
	m.changed()
}

func validateTarget(target string) error {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("隧道目标必须是本机 Agent 接口地址")
	}
	host, port, err := net.SplitHostPort(u.Host)
	ip := net.ParseIP(host)
	if err != nil || port == "" || host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("隧道只能连接本机 Agent 接口")
	}
	return nil
}

func discover(dir, override string) string {
	if override != "" {
		return resolveCommand(override)
	}
	name := "cloudflared"
	if filepath.Separator == '\\' {
		name += ".exe"
	}
	for _, path := range []string{filepath.Join(dir, name), "/opt/homebrew/bin/cloudflared", "/usr/local/bin/cloudflared", "/usr/bin/cloudflared"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && (info.Mode()&0111 != 0 || filepath.Separator == '\\') {
			return path
		}
	}
	return resolveCommand(name)
}

func resolveCommand(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	// The subprocess has its own temporary working directory. Resolve an
	// explicitly supplied relative path before switching to that directory.
	path, err = filepath.Abs(path)
	if err != nil {
		return ""
	}
	return path
}

func cleanEnv(env []string) []string {
	clean := make([]string, 0, len(env))
	for _, value := range env {
		key, _, _ := strings.Cut(value, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "TUNNEL_") || strings.HasPrefix(key, "CLOUDFLARED_") || key == "NO_AUTOUPDATE" {
			continue
		}
		clean = append(clean, value)
	}
	return clean
}

// exec serializes writes when stdout and stderr share this writer. The mutex
// also protects a final flush, and the line cap bounds malformed diagnostics.
type lineWriter struct {
	mu   sync.Mutex
	line []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			w.emit(string(w.line))
			w.line = w.line[:0]
		} else if len(w.line) < 4096 {
			w.line = append(w.line, b)
		}
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.line) > 0 {
		w.emit(string(w.line))
		w.line = nil
	}
}

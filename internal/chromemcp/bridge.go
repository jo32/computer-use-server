package chromemcp

import (
	"computer-use-server/internal/harness"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

type Status struct {
	State   string `json:"state"`
	Message string `json:"message"`
	Tools   int    `json:"tools"`
}
type Bridge struct {
	registry   *harness.Registry
	opts       Options
	mu         sync.Mutex
	status     Status
	client     *client
	key        string
	cancel     context.CancelFunc
	done       chan struct{}
	wake       chan struct{}
	startOnce  sync.Once
	retryAfter time.Time
	// Dependencies remain injectable for deterministic tests without controlling a browser.
	detect  func(context.Context, Options) (target, error)
	launch  func(context.Context, string, []string, []string) (*client, error)
	resolve func(Options, target) (string, []string, []string, error)
}

func New(r *harness.Registry) *Bridge {
	return &Bridge{registry: r, status: Status{State: "waiting", Message: "等待检测 Chrome 远程调试"}, wake: make(chan struct{}, 1), detect: discover, launch: startClient, resolve: command}
}
func (b *Bridge) Start(opts Options) error {
	if opts.BrowserURL != "" {
		if _, err := localURL(opts.BrowserURL); err != nil {
			return err
		}
	}
	b.startOnce.Do(func() {
		// A startup opt-out can still be changed explicitly in the local console.
		if opts.Disabled {
			_ = b.registry.Enable("browser", false)
		}
		ctx, cancel := context.WithCancel(context.Background())
		b.mu.Lock()
		b.opts = opts
		b.cancel = cancel
		b.done = make(chan struct{})
		b.mu.Unlock()
		go b.run(ctx)
	})
	return nil
}
func (b *Bridge) Status() Status { b.mu.Lock(); defer b.mu.Unlock(); return b.status }
func (b *Bridge) Refresh() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
func (b *Bridge) Close() {
	b.mu.Lock()
	cancel, done := b.cancel, b.done
	b.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
func (b *Bridge) setStatus(s Status) {
	b.mu.Lock()
	changed := b.status != s
	b.status = s
	b.mu.Unlock()
	if changed {
		b.registry.Signal()
	}
}
func (b *Bridge) disconnect() {
	b.mu.Lock()
	c := b.client
	b.client = nil
	b.key = ""
	b.mu.Unlock()
	if c != nil {
		b.registry.ReplaceCategory("browser", nil)
		c.Close()
		<-c.exited
	}
}
func (b *Bridge) run(ctx context.Context) {
	defer close(b.done)
	defer b.disconnect()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		b.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-b.wake:
			b.retryAfter = time.Time{}
		}
	}
}
func (b *Bridge) reconcile(ctx context.Context) {
	paused, enabled := b.registry.State()
	if !enabled["browser"] {
		b.disconnect()
		b.setStatus(Status{State: "disabled", Message: "Chrome DevTools MCP 已关闭"})
		return
	}
	if paused {
		return
	}
	if time.Now().Before(b.retryAfter) {
		return
	}
	t, err := b.detect(ctx, b.opts)
	if err != nil {
		b.disconnect()
		b.setStatus(Status{State: "waiting", Message: err.Error()})
		return
	}
	b.mu.Lock()
	c, key := b.client, b.key
	b.mu.Unlock()
	if c != nil && key == t.Key {
		select {
		case <-c.done:
		default:
			return
		}
	}
	b.disconnect()
	path, args, env, err := b.resolve(b.opts, t)
	if err != nil {
		b.setStatus(Status{State: "unavailable", Message: err.Error()})
		return
	}
	b.setStatus(Status{State: "connecting", Message: "正在启动官方 Chrome DevTools MCP，首次准备可能需要下载组件…"})
	c, err = b.launch(ctx, path, args, env)
	if err == nil {
		initCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err = c.initialize(initCtx)
		var tools []harness.Tool
		if err == nil {
			tools, err = b.loadTools(initCtx, c)
		}
		cancel()
		if err == nil {
			// Do not publish a connection enabled before a user paused or disabled it.
			paused, enabled = b.registry.State()
			if ctx.Err() != nil || paused || !enabled["browser"] {
				c.Close()
				<-c.exited
				return
			}
			b.mu.Lock()
			b.client = c
			b.key = t.Key
			b.mu.Unlock()
			b.registry.ReplaceCategory("browser", tools)
			b.setStatus(Status{State: "ready", Message: "工具已接入；首次使用时请允许 Chrome 的连接请求。", Tools: len(tools)})
			return
		}
		c.Close()
		<-c.exited
	}
	b.setStatus(Status{State: "error", Message: fmt.Sprintf("Chrome MCP 连接失败：%v", err)})
	b.retryAfter = time.Now().Add(30 * time.Second)
}

var toolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,57}$`)

func (b *Bridge) loadTools(ctx context.Context, c *client) ([]harness.Tool, error) {
	var out []harness.Tool
	seen := map[string]bool{}
	cursor := ""
	cursors := map[string]bool{}
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools []struct {
				Name         string         `json:"name"`
				Description  string         `json:"description"`
				InputSchema  map[string]any `json:"inputSchema"`
				OutputSchema map[string]any `json:"outputSchema"`
				Annotations  map[string]any `json:"annotations"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &page) != nil {
			return nil, errors.New("Chrome MCP 工具清单无效")
		}
		for _, t := range page.Tools {
			if !toolName.MatchString(t.Name) || seen[t.Name] || t.InputSchema["type"] != "object" {
				return nil, errors.New("Chrome MCP 工具定义无效或重复")
			}
			seen[t.Name] = true
			name := t.Name
			readOnly, _ := t.Annotations["readOnlyHint"].(bool)
			out = append(out, harness.Tool{Spec: harness.Spec{Name: "chrome_" + name, Description: t.Description, Category: "browser", InputSchema: t.InputSchema, OutputSchema: t.OutputSchema, Annotations: t.Annotations, Mutating: !readOnly, Parallel: false}, External: true, Run: func(ctx context.Context, in harness.Invocation) (harness.Output, error) {
				// A stale tool reference must never reconnect and silently repeat an action.
				callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				raw, err := c.call(callCtx, "tools/call", map[string]any{"name": name, "arguments": in.Arguments})
				if err != nil {
					b.Refresh()
					return harness.Output{}, err
				}
				var result map[string]any
				if json.Unmarshal(raw, &result) != nil || result == nil {
					return harness.Output{}, errors.New("Chrome MCP 返回了无效结果")
				}
				out := harness.Output{Value: result, MCPResult: result}
				if failed, _ := result["isError"].(bool); failed {
					return out, errors.New("Chrome DevTools 工具执行失败，请查看返回内容")
				}
				return out, nil
			}})
		}
		if len(out) > 256 {
			return nil, errors.New("Chrome MCP 工具数超出上限")
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
		if cursors[cursor] {
			return nil, errors.New("Chrome MCP 工具分页重复")
		}
		cursors[cursor] = true
		if len(cursors) > 32 {
			return nil, errors.New("Chrome MCP 工具分页超出上限")
		}
	}
	if len(out) == 0 {
		return nil, errors.New("Chrome MCP 未提供工具")
	}
	return out, nil
}

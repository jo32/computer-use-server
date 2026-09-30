// Package harness separates model-visible specifications from execution and auditing.
package harness

import (
	"bytes"
	"computer-use-server/internal/store"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

type Spec struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Category     string         `json:"category"`
	InputSchema  map[string]any `json:"inputSchema"`
	Mutating     bool           `json:"mutating"`
	Parallel     bool           `json:"parallel"`
	Annotations  map[string]any `json:"annotations,omitempty"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}
type Invocation struct {
	ID, Session, Client string
	Arguments           json.RawMessage
}
type Output struct {
	Value      any
	Screenshot string
	Completion func() (any, error)
	Snapshot   func() any
	Cancel     context.CancelFunc
	MCPResult  map[string]any
}
type Handler func(context.Context, Invocation) (Output, error)
type Tool struct {
	Spec Spec
	Run  Handler
	// Upstream MCP servers validate their complete JSON Schema themselves.
	External bool
	// Informational tools can describe blocked capabilities while control is paused.
	AvailableWhenPaused bool
}
type activeCall struct {
	cancel   context.CancelFunc
	category string
}

type Registry struct {
	background  sync.WaitGroup
	foreground  sync.WaitGroup
	mu          sync.Mutex
	tools       map[string]Tool
	order       []string
	active      map[string]activeCall
	paused      bool
	lastStarted time.Time
	enabled     map[string]bool
	serial      chan struct{}
	store       *store.Store
	notify      chan struct{}
	secretsMu   sync.RWMutex
	secrets     []string
	OnPause     func()
}

func New(s *store.Store, secrets ...string) *Registry {
	return &Registry{tools: map[string]Tool{}, active: map[string]activeCall{}, enabled: map[string]bool{"system": true, "files": true, "terminal": false, "computer": false, "browser": true}, serial: make(chan struct{}, 1), store: s, notify: make(chan struct{}), secrets: secrets}
}
func ID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[t.Spec.Name]; ok {
		panic("duplicate tool " + t.Spec.Name)
	}
	r.tools[t.Spec.Name] = t
	r.order = append(r.order, t.Spec.Name)
}
func (r *Registry) Specs() []Spec {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Spec{}
	for _, n := range r.order {
		out = append(out, r.tools[n].Spec)
	}
	return out
}

// ReplaceCategory atomically publishes (or withdraws) discovered tools.
func (r *Registry) ReplaceCategory(category string, tools []Tool) {
	r.mu.Lock()
	order := make([]string, 0, len(r.order)+len(tools))
	for _, name := range r.order {
		if r.tools[name].Spec.Category == category {
			delete(r.tools, name)
		} else {
			order = append(order, name)
		}
	}
	for _, t := range tools {
		if t.Spec.Category != category {
			r.mu.Unlock()
			panic("tool category mismatch")
		}
		if _, exists := r.tools[t.Spec.Name]; exists {
			r.mu.Unlock()
			panic("duplicate tool")
		}
		r.tools[t.Spec.Name] = t
		order = append(order, t.Spec.Name)
	}
	r.order = order
	r.mu.Unlock()
	r.Signal()
}
func (r *Registry) State() (bool, map[string]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := map[string]bool{}
	for k, v := range r.enabled {
		e[k] = v
	}
	return r.paused, e
}

// Activity is a lightweight snapshot for the native menu bar; it avoids database polling.
func (r *Registry) Activity() (paused bool, running int, lastStarted time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused, len(r.active), r.lastStarted
}
func (r *Registry) Changed() <-chan struct{} { r.mu.Lock(); defer r.mu.Unlock(); return r.notify }
func (r *Registry) Signal() {
	r.mu.Lock()
	close(r.notify)
	r.notify = make(chan struct{})
	r.mu.Unlock()
}
func (r *Registry) SetPaused(p bool) {
	r.mu.Lock()
	r.paused = p
	if p {
		for _, active := range r.active {
			active.cancel()
		}
		if r.OnPause != nil {
			r.OnPause()
		}
	}
	r.mu.Unlock()
	r.Signal()
}
func (r *Registry) Enable(category string, v bool) error {
	if category == "system" {
		return errors.New("system help is always enabled")
	}
	r.mu.Lock()
	if _, ok := r.enabled[category]; !ok {
		r.mu.Unlock()
		return errors.New("unknown category")
	}
	r.enabled[category] = v
	if !v {
		for _, active := range r.active {
			if active.category == category {
				active.cancel()
			}
		}
	}
	r.mu.Unlock()
	r.Signal()
	return nil
}

// Cancel affects only this invocation, including a yielded command.
func (r *Registry) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	active, ok := r.active[id]
	if ok {
		active.cancel()
	}
	return ok
}
func (r *Registry) finishActive(id string) {
	r.mu.Lock()
	if active, ok := r.active[id]; ok {
		active.cancel()
		delete(r.active, id)
	}
	r.mu.Unlock()
}
func (r *Registry) Invoke(ctx context.Context, name string, in Invocation) (out Output, call store.Call, err error) {
	r.foreground.Add(1)
	defer r.foreground.Done()
	if in.ID == "" {
		in.ID = ID()
	}
	if in.Session == "" {
		in.Session = "default"
	}
	if in.Client == "" {
		in.Client = "Remote agent"
	}
	r.mu.Lock()
	t, ok := r.tools[name]
	r.mu.Unlock()
	call = store.Call{ID: in.ID, Session: in.Session, Client: in.Client, Tool: name, Category: t.Spec.Category, Status: "running", Started: time.Now(), Arguments: r.redact(in.Arguments)}
	if err = r.store.Save(call); err != nil {
		return out, call, fmt.Errorf("audit log unavailable: %w", err)
	}
	r.Signal()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("tool panic: %v", p)
		}
		call.Duration = time.Since(call.Started).Milliseconds()
		if err != nil {
			if call.Status != "denied" {
				call.Status = "error"
				if errors.Is(err, context.Canceled) {
					call.Status = "cancelled"
				}
			}
			call.Error = r.redactText(err.Error())
		} else {
			call.Status = "success"
			if out.Completion != nil {
				call.Status = "running"
			}
		}
		b, e := json.Marshal(out.Value)
		if e != nil {
			err = e
			call.Status = "error"
			call.Error = e.Error()
		}
		call.Result = r.redact(b)
		call.Screenshot = out.Screenshot
		if e := r.store.Save(call); e != nil {
			err = fmt.Errorf("persist result: %w", e)
		}
		if out.Completion != nil && err == nil {
			r.mu.Lock()
			// Cancellation may arrive between the handler yielding and this handover.
			if out.Cancel != nil {
				if ctx.Err() != nil || r.paused || !r.enabled[t.Spec.Category] {
					out.Cancel()
				}
				r.active[in.ID] = activeCall{out.Cancel, t.Spec.Category}
			}
			r.mu.Unlock()
			r.background.Add(1)
			go r.follow(call, out)
		} else {
			if out.Cancel != nil {
				out.Cancel()
			}
			r.finishActive(in.ID)
		}
		r.Signal()
	}()
	if !ok {
		err = errors.New("unknown tool")
		return
	}
	if t.External {
		var args map[string]any
		if json.Unmarshal(in.Arguments, &args) != nil || args == nil {
			err = errors.New("arguments must be a JSON object")
		}
	} else {
		err = validate(in.Arguments, t.Spec.InputSchema)
	}
	if err != nil {
		return
	}
	r.mu.Lock()
	if (r.paused && !t.AvailableWhenPaused) || !r.enabled[t.Spec.Category] {
		r.mu.Unlock()
		call.Status = "denied"
		err = errors.New("控制已暂停或该能力未启用，请在本地控制台启用")
		return
	}
	r.active[in.ID] = activeCall{cancel, t.Spec.Category}
	r.lastStarted = time.Now()
	r.mu.Unlock()
	if !t.Spec.Parallel {
		select {
		case r.serial <- struct{}{}:
			defer func() { <-r.serial }()
		case <-ctx.Done():
			err = ctx.Err()
			return
		}
	}
	if err = ctx.Err(); err != nil {
		return
	}
	out, err = t.Run(ctx, in)
	return
}
func (r *Registry) follow(saved store.Call, out Output) {
	defer r.background.Done()
	defer r.finishActive(saved.ID)
	type result struct {
		value any
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var res result
		defer func() {
			if p := recover(); p != nil {
				res.err = fmt.Errorf("completion panic: %v", p)
			}
			done <- res
		}()
		res.value, res.err = out.Completion()
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if out.Snapshot == nil {
				continue
			}
			b, err := json.Marshal(out.Snapshot())
			if err != nil {
				continue
			}
			if bytes.Equal(saved.Result, r.redact(b)) {
				continue
			}
			saved.Result = r.redact(b)
			saved.Duration = time.Since(saved.Started).Milliseconds()
		case res := <-done:
			saved.Duration = time.Since(saved.Started).Milliseconds()
			saved.Status = "success"
			if res.err != nil {
				saved.Status = "error"
				saved.Error = r.redactText(res.err.Error())
				if errors.Is(res.err, context.Canceled) {
					saved.Status = "cancelled"
				}
			}
			b, err := json.Marshal(res.value)
			if err != nil {
				saved.Status = "error"
				saved.Error = err.Error()
			}
			saved.Result = r.redact(b)
		}
		if err := r.store.Save(saved); err != nil {
			log.Printf("persist background result: %v", err)
		}
		r.Signal()
		if saved.Status != "running" {
			return
		}
	}
}
func (r *Registry) AddSecrets(secrets ...string) {
	r.secretsMu.Lock()
	defer r.secretsMu.Unlock()
	r.secrets = append(r.secrets, secrets...)
}

func (r *Registry) redactText(s string) string {
	r.secretsMu.RLock()
	defer r.secretsMu.RUnlock()
	for _, secret := range r.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	return s
}
func (r *Registry) redact(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(`null`)
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return json.RawMessage(`null`)
	}
	var clean func(any) any
	clean = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				lower := strings.ToLower(k)
				if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || lower == "authorization" || lower == "api_key" {
					x[k] = "[REDACTED]"
				} else if k == "screenshot" && strings.HasPrefix(fmt.Sprint(val), "data:") {
					x[k] = "[stored as screenshot]"
				} else {
					x[k] = clean(val)
				}
			}
			return x
		case []any:
			for i := range x {
				x[i] = clean(x[i])
			}
			return x
		case string:
			return r.redactText(x)
		default:
			return v
		}
	}
	b, _ = json.Marshal(clean(v))
	return b
}
func Decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected a single JSON object")
	}
	return nil
}
func Schema(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func Prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}
func validate(raw []byte, schema map[string]any) error {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return errors.New("arguments must be a JSON object")
	}
	for _, key := range schema["required"].([]string) {
		if _, ok := value[key]; !ok {
			return fmt.Errorf("missing argument: %s", key)
		}
	}
	props := schema["properties"].(map[string]any)
	for key, v := range value {
		p, ok := props[key]
		if !ok {
			return fmt.Errorf("unknown argument: %s", key)
		}
		kind := p.(map[string]any)["type"]
		valid := false
		switch kind {
		case "string":
			_, valid = v.(string)
		case "number", "integer":
			n, ok := v.(float64)
			valid = ok && (kind != "integer" || n == float64(int64(n)))
		case "boolean":
			_, valid = v.(bool)
		case "array":
			_, valid = v.([]any)
		case "object":
			_, valid = v.(map[string]any)
		}
		if !valid {
			return fmt.Errorf("%s must be %s", key, kind)
		}
	}
	return nil
}

// WaitBackground is called after stopping the gateway and all processes.
func (r *Registry) WaitBackground() { r.foreground.Wait(); r.background.Wait() }

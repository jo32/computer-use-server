package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ToolHelp is a live definition plus policy status, not a cached tool catalogue.
type ToolHelp struct {
	Spec
	// InputSchema shadows Spec.InputSchema so compact listings can omit it.
	InputSchema       map[string]any `json:"inputSchema,omitempty"`
	Enabled           bool           `json:"enabled"`
	Available         bool           `json:"available"`
	UnavailableReason string         `json:"unavailable_reason,omitempty"`
}
type HelpResult struct {
	Paused bool       `json:"paused"`
	Total  int        `json:"total"`
	Note   string     `json:"note,omitempty"`
	Tools  []ToolHelp `json:"tools"`
}

// firstSentence keeps compact listings short.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 0 && i < 220 {
		return s[:i+1]
	}
	if len(s) > 220 {
		return s[:220] + "…"
	}
	return s
}

func (r *Registry) RegisterHelp() {
	r.Register(Tool{
		Spec:                Spec{Name: "help", Category: "system", Description: "List currently available tools and their complete definitions (description, inputSchema, outputSchema when provided, category, group, annotations and execution flags). Call with {} for available tools, with compact=true for just names and one-line descriptions (then fetch one schema with name), or with include_disabled=true to also see tools blocked by capability settings or pause. Tools with group \"advanced\" are not advertised by tools/list; call them with use_tool. Chrome tools are discovered dynamically and disappear when disconnected. This read-only tool remains available while control is paused. Availability describes ReadyRig policy, not a guarantee of OS permissions or upstream Chrome authorization.", Parallel: true, InputSchema: Schema(map[string]any{"name": Prop("string", "Exact tool name to inspect; omit to list tools"), "include_disabled": Prop("boolean", "Include registered tools unavailable due to capability settings or pause; default false"), "compact": Prop("boolean", "List names and one-line descriptions without schemas")}), Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}},
		AvailableWhenPaused: true,
		Run: func(ctx context.Context, in Invocation) (Output, error) {
			var args struct {
				Name            string `json:"name"`
				IncludeDisabled bool   `json:"include_disabled"`
				Compact         bool   `json:"compact"`
			}
			if err := Decode(in.Arguments, &args); err != nil {
				return Output{}, err
			}
			if err := ctx.Err(); err != nil {
				return Output{}, err
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			if args.Name != "" {
				if _, ok := r.tools[args.Name]; !ok {
					return Output{}, fmt.Errorf("tool %q is not currently registered; call help with {} for the live list", args.Name)
				}
			}
			result := HelpResult{Paused: r.paused, Tools: []ToolHelp{}}
			hidden := false
			for _, name := range r.order {
				if args.Name != "" && args.Name != name {
					continue
				}
				t := r.tools[name]
				enabled := r.enabled[t.Spec.Category]
				h := ToolHelp{Spec: t.Spec, InputSchema: t.Spec.InputSchema, Enabled: enabled, Available: enabled && (!r.paused || t.AvailableWhenPaused)}
				if !enabled {
					h.UnavailableReason = "capability_disabled"
				} else if !h.Available {
					h.UnavailableReason = "control_paused"
				}
				if args.Name == "" && !args.IncludeDisabled && !h.Available {
					continue
				}
				if t.Spec.Group == "advanced" && !r.ExposeAll {
					hidden = true
				}
				if args.Compact && args.Name == "" {
					h.InputSchema = nil
					h.Spec.InputSchema = nil
					h.Spec.Description = firstSentence(h.Spec.Description)
				}
				result.Tools = append(result.Tools, h)
			}
			result.Total = len(result.Tools)
			if hidden {
				result.Note = "Tools in group advanced are not listed by tools/list; run them with use_tool {name, arguments}."
			}
			return Output{Value: result}, nil
		},
	})
	r.Register(Tool{
		Spec: Spec{Name: "use_tool", Category: "system", Description: "Run any tool by name, including advanced tools that tools/list does not advertise (see help). arguments are passed to that tool unchanged and its own capability and pause checks apply.", Mutating: true, Parallel: true, InputSchema: Schema(map[string]any{"name": Prop("string", "Tool name, as listed by help"), "arguments": Prop("object", "Arguments for that tool")}, "name")},
		Run: func(ctx context.Context, in Invocation) (Output, error) {
			var args struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := Decode(in.Arguments, &args); err != nil {
				return Output{}, err
			}
			if args.Name == "use_tool" {
				return Output{}, errors.New("use_tool cannot call itself")
			}
			if len(args.Arguments) == 0 {
				args.Arguments = json.RawMessage(`{}`)
			}
			out, _, err := r.Invoke(ctx, args.Name, Invocation{Session: in.Session, Client: in.Client, Arguments: args.Arguments})
			// The inner call owns its background completion; the wrapper must not repeat it.
			out.Completion, out.Snapshot, out.Cancel = nil, nil, nil
			return out, err
		},
	})
}

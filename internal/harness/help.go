package harness

import (
	"context"
	"fmt"
)

// ToolHelp is a live definition plus policy status, not a cached tool catalogue.
type ToolHelp struct {
	Spec
	Enabled           bool   `json:"enabled"`
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}
type HelpResult struct {
	Paused bool       `json:"paused"`
	Total  int        `json:"total"`
	Tools  []ToolHelp `json:"tools"`
}

func (r *Registry) RegisterHelp() {
	r.Register(Tool{
		Spec:                Spec{Name: "help", Category: "system", Description: "List currently available tools and their complete definitions (description, inputSchema, outputSchema when provided, category, annotations and execution flags). Call with {} for available tools; include_disabled=true also shows registered tools blocked by capability settings or pause. Set name to inspect one tool, including disabled tools. Chrome tools are discovered dynamically and disappear when disconnected. This read-only tool remains available while control is paused. Availability describes Relay policy, not a guarantee of OS permissions or upstream Chrome authorization.", Parallel: true, InputSchema: Schema(map[string]any{"name": Prop("string", "Exact tool name to inspect; omit to list tools"), "include_disabled": Prop("boolean", "Include registered tools unavailable due to capability settings or pause; default false")}), Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}},
		AvailableWhenPaused: true,
		Run: func(ctx context.Context, in Invocation) (Output, error) {
			var args struct {
				Name            string `json:"name"`
				IncludeDisabled bool   `json:"include_disabled"`
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
			for _, name := range r.order {
				if args.Name != "" && args.Name != name {
					continue
				}
				t := r.tools[name]
				enabled := r.enabled[t.Spec.Category]
				h := ToolHelp{Spec: t.Spec, Enabled: enabled, Available: enabled && (!r.paused || t.AvailableWhenPaused)}
				if !enabled {
					h.UnavailableReason = "capability_disabled"
				} else if !h.Available {
					h.UnavailableReason = "control_paused"
				}
				if args.Name == "" && !args.IncludeDisabled && !h.Available {
					continue
				}
				result.Tools = append(result.Tools, h)
			}
			result.Total = len(result.Tools)
			return Output{Value: result}, nil
		},
	})
}

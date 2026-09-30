package harness

import (
	"context"
	"encoding/json"
	"testing"
)

func helpResult(t *testing.T, r *Registry, args any) HelpResult {
	t.Helper()
	out, err := invoke(t, r, "help", args)
	if err != nil {
		t.Fatal(err)
	}
	return out.Value.(HelpResult)
}
func TestHelpLiveDefinitionsAndDisabledTools(t *testing.T) {
	r := registryForTest(t)
	r.RegisterHelp()
	r.Register(Tool{Spec: Spec{Name: "terminal_test", Category: "terminal", InputSchema: Schema(map[string]any{"cmd": Prop("string", "command")}, "cmd")}})
	got := helpResult(t, r, map[string]any{})
	if got.Total != 1 || got.Tools[0].Name != "help" || !got.Tools[0].Available {
		t.Fatal(got)
	}
	got = helpResult(t, r, map[string]any{"name": "terminal_test"})
	if got.Total != 1 || got.Tools[0].Available || got.Tools[0].Enabled || got.Tools[0].UnavailableReason != "capability_disabled" {
		t.Fatal(got)
	}
	if got.Tools[0].InputSchema["required"].([]string)[0] != "cmd" {
		t.Fatal("schema missing")
	}
	if got = helpResult(t, r, map[string]any{"include_disabled": true}); got.Total != 2 {
		t.Fatal(got)
	}
	r.Enable("terminal", true)
	if got = helpResult(t, r, map[string]any{}); got.Total != 2 || !got.Tools[1].Available {
		t.Fatal(got)
	}
	// External schemas use decoded JSON types and may contain arbitrary nested constraints.
	var schema map[string]any
	json.Unmarshal([]byte(`{"type":"object","properties":{"pageId":{"type":"number"}},"required":["pageId"]}`), &schema)
	r.ReplaceCategory("browser", []Tool{{Spec: Spec{Name: "chrome_test", Category: "browser", InputSchema: schema, OutputSchema: map[string]any{"type": "object"}, Annotations: map[string]any{"readOnlyHint": true}}, External: true}})
	got = helpResult(t, r, map[string]any{"name": "chrome_test"})
	if !got.Tools[0].Available || got.Tools[0].OutputSchema == nil || got.Tools[0].Annotations["readOnlyHint"] != true {
		t.Fatal(got)
	}
	r.ReplaceCategory("browser", nil)
	if _, err := invoke(t, r, "help", map[string]any{"name": "chrome_test"}); err == nil {
		t.Fatal("removed Chrome tool still present")
	}
}
func TestHelpWhilePausedAndAudit(t *testing.T) {
	r := registryForTest(t)
	r.RegisterHelp()
	r.Register(Tool{Spec: Spec{Name: "file_test", Category: "files", InputSchema: Schema(map[string]any{})}, Run: func(context.Context, Invocation) (Output, error) {
		t.Fatal("paused operation executed")
		return Output{}, nil
	}})
	r.SetPaused(true)
	got := helpResult(t, r, map[string]any{})
	if !got.Paused || got.Total != 1 || got.Tools[0].Name != "help" {
		t.Fatal(got)
	}
	got = helpResult(t, r, map[string]any{"include_disabled": true})
	if got.Total != 2 || !got.Tools[1].Enabled || got.Tools[1].Available || got.Tools[1].UnavailableReason != "control_paused" {
		t.Fatal(got)
	}
	if _, err := invoke(t, r, "file_test", map[string]any{}); err == nil {
		t.Fatal("pause bypassed")
	}
	if err := r.Enable("system", false); err == nil {
		t.Fatal("help can be disabled")
	}
	_, call, err := r.Invoke(context.Background(), "help", Invocation{Arguments: json.RawMessage(`{"name":"help"}`)})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := r.store.Get(call.ID)
	if err != nil || saved.Status != "success" || saved.Category != "system" {
		t.Fatal(saved, err)
	}
	for _, args := range []any{map[string]any{"name": 123}, map[string]any{"include_disabled": "yes"}, map[string]any{"unknown": true}} {
		if _, err := invoke(t, r, "help", args); err == nil {
			t.Fatal("invalid args accepted", args)
		}
	}
}

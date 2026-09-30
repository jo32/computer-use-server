package server

import (
	"computer-use-server/internal/cloud"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCloudCommandAllowlistAndPublicIsolation(t *testing.T) {
	s := fixture(t)
	if err := s.StartCloud(""); err != nil {
		t.Fatal(err)
	}
	defer s.Cloud.Close()
	for _, cmd := range []cloud.Command{
		{Kind: "control.pause", Payload: json.RawMessage(`{"paused":true}`)},
		{Kind: "capability.set", Payload: json.RawMessage(`{"category":"terminal","enabled":true}`)},
	} {
		if err := s.executeCloudCommand(cmd); err != nil {
			t.Fatal(err)
		}
	}
	paused, enabled := s.Registry.State()
	if !paused || !enabled["terminal"] {
		t.Fatal(paused, enabled)
	}
	for _, cmd := range []cloud.Command{{Kind: "shell.exec", Payload: json.RawMessage(`{}`)}, {Kind: "capability.set", Payload: json.RawMessage(`{"category":"full_access","enabled":true}`)}, {Kind: "control.pause", Payload: json.RawMessage(`{}`)}} {
		if err := s.executeCloudCommand(cmd); err == nil {
			t.Fatal("accepted", cmd.Kind)
		}
	}
	for _, path := range []string{"/api/cloud/login", "/api/cloud/disconnect", "/api/window/open-url"} {
		if w := request(s.Gateway(), "POST", "/app"+path, "{}", s.AccessPath); w.Code != http.StatusForbidden {
			t.Fatal(path, w.Code)
		}
	}
	if w := request(s.Gateway(), "GET", "/app/api/cloud", "", s.AccessPath); w.Code != 404 {
		t.Fatal("cloud account published", w.Code)
	}
	w := request(s.Gateway(), "GET", "/app/api/state", "", s.AccessPath)
	var state map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &state)
	if state["cloud"] != nil {
		t.Fatal("cloud account included in public status")
	}
}

package server

import (
	"computer-use-server/internal/tunnel"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPublicConsoleRoutesAndOriginBoundary(t *testing.T) {
	s := fixture(t)
	h := s.Gateway()
	for _, path := range []string{"/app/", "/app/app.js", "/app/app.css", "/app/icon.svg", "/app/api/state", "/app/api/connection", "/app/api/calls", "/app/api/export"} {
		w := request(h, "GET", path, "", s.AccessPath)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		if w := request(h, "GET", path, "", "invalid0"); w.Code != 404 {
			t.Fatal("missing access path accepted", path, w.Code)
		}
	}
	w := request(h, "GET", "/app", "", s.AccessPath)
	if w.Code != 307 || w.Header().Get("Location") != "/"+s.AccessPath+"/app/" {
		t.Fatal("redirect dropped secret prefix", w.Code, w.Header())
	}
	for _, origin := range []string{"https://share.trycloudflare.com", "https://evil.example", "null", "https://user@share.trycloudflare.com", "https://share.trycloudflare.com/path"} {
		r := httptest.NewRequest("GET", "https://share.trycloudflare.com/"+s.AccessPath+"/app/api/state", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 403
		if origin == "https://share.trycloudflare.com" {
			want = 200
		}
		if w.Code != want {
			t.Fatal(origin, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "https://share.trycloudflare.com/"+s.AccessPath+"/api/v1/fs/list", strings.NewReader(`{"path":"."}`))
	r.Header.Set("Origin", "https://share.trycloudflare.com")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("console origin can invoke Agent endpoints", w.Code)
	}
}

func TestPublicConsoleCannotManageHost(t *testing.T) {
	s := fixture(t)
	h := s.Gateway()
	for _, path := range []string{"/api/pause", "/api/capability", "/api/projects", "/api/access", "/api/access/system-settings", "/api/chrome/refresh", "/api/update/check", "/api/update/restart", "/api/tunnel/start", "/api/tunnel/stop", "/api/tools/list_directory", "/api/calls/example/cancel", "/api/login"} {
		if w := request(h, "POST", "/app"+path, "{}", s.AccessPath); w.Code != http.StatusForbidden {
			t.Fatal("public mutation allowed", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/app/api/directories", "/app/api/events", "/app/api/update", "/app/api/tunnel", "/app/wails/runtime.js"} {
		if w := request(h, "GET", path, "", s.AccessPath); w.Code != 404 {
			t.Fatal("private route published", path, w.Code)
		}
	}
	w := request(h, "GET", "https://share.trycloudflare.com/"+s.AccessPath+"/app/api/state", "", "")
	var state map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state["public"] != true || state["gateway"] != "https://share.trycloudflare.com/"+s.AccessPath || state["update"] != nil {
		t.Fatal(state)
	}
	if strings.Contains(w.Body.String(), s.UIKey) {
		t.Fatal("dashboard credential published")
	}
	w = request(s.BrowserGuard(s.UI()), "GET", "http://127.0.0.1:7331/api/state", "", "")
	if w.Code != 401 {
		t.Fatal("local dashboard authentication changed", w.Code)
	}
}

// Use a harmless connector process and an in-memory public route. No real DNS,
// credentials or public tunnel are required to exercise persistent path access.
func TestFixedAccessSurvivesManagerRestartAndRevokesOnStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell fixture")
	}
	s := fixture(t)
	s.Registry.RegisterHelp()
	dir := t.TempDir()
	command := filepath.Join(dir, "connector")
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'INF Registered tunnel connection'\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: fixedRouteTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		s.Gateway().ServeHTTP(w, r)
		return w.Result(), nil
	})}
	opts := tunnel.Options{Dir: dir, Command: command, ProbeClient: client, StartupTimeout: 4 * time.Second}
	s.Tunnel = tunnel.New(opts)
	defer func() { s.Tunnel.Close() }()
	token := "testTokenABCDEFGHIJ0123456789"
	config := request(s.UI(), "POST", "/api/tunnel/fixed", `{"url":"https://readyrig.example.com","token":"`+token+`"}`, "")
	if config.Code != 200 || strings.Contains(config.Body.String(), token) {
		t.Fatal(config.Code, config.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, "fixed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		AccessPath string `json:"access_path"`
	}
	json.Unmarshal(b, &saved)
	h := s.Gateway()
	if w := request(h, "POST", "/api/v1/tools/help", "{}", saved.AccessPath); w.Code != 404 {
		t.Fatal("inactive fixed key accepted")
	}
	start := func() {
		t.Helper()
		if err := s.startSharingMode("fixed"); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if s.Tunnel.Status().State == "ready" {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal(s.Tunnel.Status())
	}
	start()
	for _, path := range []string{"/api/v1/tools", "/api/v1/openapi.json", "/app/", "/app/api/connection"} {
		w := request(h, "GET", path, "", saved.AccessPath)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		if path == "/app/api/connection" && (!strings.Contains(w.Body.String(), "https://readyrig.example.com/"+saved.AccessPath) || strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), "has_token")) {
			t.Fatal("public connection leaks settings or loses stable URL", w.Body.String())
		}
	}
	w := request(h, "GET", "/app", "", saved.AccessPath)
	if w.Header().Get("Location") != "/"+saved.AccessPath+"/app/" {
		t.Fatal("redirect changed fixed prefix", w.Header())
	}
	for _, path := range []string{"/api/tunnel/fixed", "/api/tunnel/start", "/api/tunnel/stop"} {
		if w := request(h, "POST", "/app"+path, `{}`, saved.AccessPath); w.Code != 403 {
			t.Fatal("remote configuration mutation", w.Code)
		}
	}
	for _, path := range []string{"/app/api/tunnel", "/app/api/tunnel/fixed"} {
		if w := request(h, "GET", path, "", saved.AccessPath); w.Code != 404 {
			t.Fatal("remote configuration read", w.Code)
		}
	}
	if err = s.Tunnel.Stop(); err != nil {
		t.Fatal(err)
	}
	if w := request(h, "POST", "/api/v1/tools/help", "{}", saved.AccessPath); w.Code != 404 {
		t.Fatal("stopped fixed path accepted")
	}
	s.Tunnel.Close()
	s.Tunnel = tunnel.New(opts)
	s.AccessPath = "newRun12"
	start()
	if w := request(h, "POST", "/api/v1/tools/help", "{}", saved.AccessPath); w.Code != 200 {
		t.Fatal("stable path failed after restart", w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/api/v1/tools/help", "{}", "aSsxba11"); w.Code != 404 {
		t.Fatal("old local path remained accepted")
	}
}

type fixedRouteTransport func(*http.Request) (*http.Response, error)

func (f fixedRouteTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

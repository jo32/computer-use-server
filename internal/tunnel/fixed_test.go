package tunnel

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type probeTransport func(*http.Request) (*http.Response, error)

func (f probeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func probeResponse(id string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"X-Readyrig-Instance": []string{id}}, Body: io.NopCloser(strings.NewReader("[]"))}
}

const testFixedToken = "testOnlyToken0123456789ABCDEFGHIJ"

func TestFixedPersistenceAndValidation(t *testing.T) {
	dir := t.TempDir()
	m := New(Options{Dir: dir})
	defer m.Close()
	for _, bad := range []string{"http://agent.example.com", "https://127.0.0.1", "https://agent.example.com/path", "https://name.trycloudflare.com", "https://user@agent.example.com", "https://agent.example.com:443", "https://agent.example.com?token=bad", ""} {
		if m.SaveFixed(bad, testFixedToken) == nil {
			t.Errorf("accepted URL %q", bad)
		}
	}
	if m.SaveFixed("readyrig.example.com", "cloudflared tunnel run --token abc") == nil {
		t.Fatal("accepted shell command")
	}
	if err := m.SaveFixed("ReadyRig.Example.com/", testFixedToken); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "fixed.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private config permissions", info, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "fixed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var original fixedConfig
	if err = json.Unmarshal(b, &original); err != nil {
		t.Fatal(err)
	}
	var redacted []string
	reload := New(Options{Dir: dir, RedactSecrets: func(s ...string) { redacted = append(redacted, s...) }})
	defer reload.Close()
	if reload.fixed.AccessPath != original.AccessPath || reload.FixedAccessPath() != "" || reload.Status().Mode != "fixed" {
		t.Fatal("path changed or exposed before start")
	}
	if len(redacted) != 2 || redacted[0] != testFixedToken || redacted[1] != original.AccessPath {
		t.Fatal("secrets were not registered for redaction")
	}
	if err = reload.SaveFixed("https://readyrig.example.com", ""); err != nil {
		t.Fatal(err)
	}
	if reload.fixed.Token != testFixedToken || reload.fixed.AccessPath != original.AccessPath {
		t.Fatal("empty token did not preserve config")
	}
	safe, _ := json.Marshal(reload.FixedSettings())
	if strings.Contains(string(safe), testFixedToken) || strings.Contains(string(safe), original.AccessPath) {
		t.Fatal("settings disclosed credentials")
	}
}
func TestFixedLaunchTokenIsolationAndStop(t *testing.T) {
	m, capture := helper(t, "ready")
	m.opts.ProbeClient = &http.Client{Transport: probeTransport(func(r *http.Request) (*http.Response, error) { return probeResponse("same-instance"), nil })}
	if err := m.SaveFixed("readyrig.example.com", testFixedToken); err != nil {
		t.Fatal(err)
	}
	if err := m.StartMode("http://127.0.0.1:7332", "fixed"); err != nil {
		t.Fatal(err)
	}
	s := awaitState(t, m, "ready")
	if s.URL != "https://readyrig.example.com" || s.Mode != "fixed" || m.FixedAccessPath() == "" {
		t.Fatal(s)
	}
	if m.StartMode("http://127.0.0.1:7332", "quick") == nil || m.SaveFixed("other.example.com", "") == nil {
		t.Fatal("changed live configuration")
	}
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Args  []string `json:"args"`
		Token string   `json:"token"`
	}
	if err = json.Unmarshal([]byte(strings.TrimSpace(string(b))), &recorded); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), testFixedToken) || recorded.Token != "" {
		t.Fatal("token leaked to command or environment")
	}
	tokenFile := ""
	for i, arg := range recorded.Args {
		if arg == "--url" {
			t.Fatal("fixed run uses quick tunnel")
		}
		if arg == "--token-file" {
			tokenFile = recorded.Args[i+1]
		}
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil || string(token) != testFixedToken {
		t.Fatal("token file not prepared", err)
	}
	m.mu.Lock()
	r := m.run
	m.mu.Unlock()
	m.observe(r, "credential="+testFixedToken)
	if strings.Contains(strings.Join(m.Status().Logs, "\n"), testFixedToken) {
		t.Fatal("secret leaked to diagnostic log")
	}
	if err = m.Stop(); err != nil {
		t.Fatal(err)
	}
	if m.FixedAccessPath() != "" || m.Status().URL != "" {
		t.Fatal("stop did not revoke fixed access")
	}
	if _, err = os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatal("temporary token remains", err)
	}
}
func TestFixedRequiresCorrectPublishedRoute(t *testing.T) {
	m, _ := helper(t, "ready")
	m.opts.StartupTimeout = 1500 * time.Millisecond
	m.opts.ProbeClient = &http.Client{Transport: probeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme == "http" {
			return probeResponse("local-instance"), nil
		}
		return probeResponse("wrong-instance"), nil
	})}
	if err := m.SaveFixed("readyrig.example.com", testFixedToken); err != nil {
		t.Fatal(err)
	}
	if err := m.StartMode("http://127.0.0.1:7332", "fixed"); err != nil {
		t.Fatal(err)
	}
	s := awaitState(t, m, "error")
	if s.URL != "" || !strings.Contains(s.Error, "应用路由") {
		t.Fatal("unverified public route offered", s)
	}
}

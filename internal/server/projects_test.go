package server

import (
	"computer-use-server/internal/harness"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectGrantsAreLocalOnly(t *testing.T) {
	s := fixture(t)
	p, err := harness.NewProjects(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	s.Projects = p
	p.Register(s.Registry)
	for _, endpoint := range []string{"/api/projects", "/api/access", "/api/access/system-settings"} {
		w := request(s.Gateway(), "POST", endpoint, `{"full_access":true}`, s.AccessPath)
		if w.Code != 404 {
			t.Fatal(endpoint, w.Code)
		}
		w = request(s.BrowserGuard(s.UI()), "POST", "http://127.0.0.1:7331"+endpoint, `{}`, "")
		if w.Code != 401 {
			t.Fatal(endpoint, w.Code)
		}
	}
	login := request(s.BrowserGuard(s.UI()), "POST", "http://127.0.0.1:7331/api/login", `{"key":"dashboard-secret"}`, "")
	cookie := login.Result().Cookies()[0]
	local := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7331"+path, strings.NewReader(body))
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.BrowserGuard(s.UI()).ServeHTTP(w, r)
		return w
	}
	b, _ := json.Marshal(map[string]string{"action": "add", "name": "Extra", "path": t.TempDir()})
	if w := local("POST", "/api/projects", string(b)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	second := p.Snapshot().Projects[1]
	b, _ = json.Marshal(map[string]string{"action": "activate", "id": second.ID})
	if w := local("POST", "/api/projects", string(b)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := local("POST", "/api/access", `{"full_access":true}`); w.Code != 200 || !p.Snapshot().FullAccess {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := local("GET", "/api/state", ""); w.Code != 200 || !strings.Contains(w.Body.String(), second.Path) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := local("GET", "/api/directories", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(s.Gateway(), "POST", "/api/v1/tools/list_projects", `{}`, s.AccessPath); w.Code != 200 || !strings.Contains(w.Body.String(), second.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
}

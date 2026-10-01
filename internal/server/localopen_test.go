package server

import (
	"computer-use-server/internal/harness"
	"computer-use-server/internal/localopen"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalOpenFilesAndFolders(t *testing.T) {
	s := fixture(t)
	projects, err := harness.NewProjects(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	s.Projects = projects
	project := projects.Snapshot().Projects[0]
	file := filepath.Join(project.Path, "中文 notes ' $().txt")
	if err := os.WriteFile(file, []byte("notes"), 0600); err != nil {
		t.Fatal(err)
	}
	ui := s.UI()
	for _, tc := range []struct {
		path, want  string
		application string
	}{{".", project.Path, ""}, {filepath.Base(file), file, "system-app-id"}} {
		called := false
		s.openLocalPath = func(ctx context.Context, path, application string) error {
			called = true
			if path != tc.want || application != tc.application {
				t.Fatalf("open(%q, %q), want (%q, %q)", path, application, tc.want, tc.application)
			}
			return nil
		}
		body, _ := json.Marshal(map[string]any{"project": project.ID, "path": tc.path, "application": tc.application})
		w := request(ui, "POST", "/api/files/open", string(body), "")
		if w.Code != 200 || !called || !strings.Contains(w.Body.String(), `"opened":true`) {
			t.Fatal(w.Code, w.Body.String(), called)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{nil, 200}, {localopen.ErrApplicationUnavailable, 400}, {localopen.ErrUnsupported, 400}, {errors.New("launch failed"), 500}} {
		s.openLocalPath = func(context.Context, string, string) error { return tc.err }
		w := request(ui, "POST", "/api/files/open", `{"path":".","application":"system-app-id"}`, "")
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestLocalOpenBoundaries(t *testing.T) {
	s := fixture(t)
	projects, err := harness.NewProjects(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	s.Projects = projects
	project := projects.Snapshot().Projects[0]
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project.Path, "outside")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	called := 0
	s.openLocalPath = func(context.Context, string, string) error { called++; return nil }
	ui := s.UI()
	for _, body := range []map[string]string{
		{"path": ".", "project": "missing-project"},
		{"path": "../"}, {"path": outside}, {"path": "outside"}, {"path": "missing-file"}, {"path": ""},
	} {
		data, _ := json.Marshal(body)
		if w := request(ui, "POST", "/api/files/open", string(data), ""); w.Code != 400 {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
	endpoint := "/api/files/open"
	if w := request(s.BrowserGuard(ui), "POST", "http://127.0.0.1:7331"+endpoint, `{"path":"."}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(s.Gateway(), "POST", endpoint, `{"path":"."}`, s.AccessPath); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(s.Gateway(), "POST", "/app"+endpoint, `{"path":"."}`, s.AccessPath); w.Code != http.StatusForbidden {
		t.Fatal(w.Code, w.Body.String())
	}
	appsEndpoint := "/api/files/applications?path=."
	if w := request(s.BrowserGuard(ui), "GET", "http://127.0.0.1:7331"+appsEndpoint, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, prefix := range []string{"", "/app"} {
		if w := request(s.Gateway(), "GET", prefix+appsEndpoint, "", s.AccessPath); w.Code != 404 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(ui, "GET", "/api/files/applications?path=outside", "", ""); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if called != 0 {
		t.Fatal("invalid or remote request launched an application")
	}
	projects.SetFullAccess(true)
	data, _ := json.Marshal(map[string]string{"path": outside})
	if w := request(ui, "POST", endpoint, string(data), ""); w.Code != 200 || called != 1 {
		t.Fatal("Full Access opening failed", w.Code, w.Body.String())
	}
}

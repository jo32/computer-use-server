package server

import (
	"computer-use-server/internal/harness"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

func (s *Server) projectState() harness.ProjectState {
	if s.Projects != nil {
		return s.Projects.Snapshot()
	}
	return harness.ProjectState{Projects: []harness.Project{}}
}
func (s *Server) activeWorkspace() string {
	state := s.projectState()
	for _, p := range state.Projects {
		if p.ID == state.Active {
			return p.Path
		}
	}
	return s.Workspace
}

// Project grants and directory browsing exist only behind local dashboard auth.
func (s *Server) projectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/projects", func(w http.ResponseWriter, r *http.Request) {
		if s.Projects == nil {
			problem(w, 503, fmt.Errorf("projects unavailable"))
			return
		}
		var v struct{ Action, ID, Name, Path string }
		if !decode(w, r, &v) {
			return
		}
		if err := s.Projects.Change(v.Action, v.ID, v.Name, v.Path); err != nil {
			problem(w, 400, err)
			return
		}
		write(w, s.Projects.Snapshot())
	})
	mux.HandleFunc("POST /api/access", func(w http.ResponseWriter, r *http.Request) {
		if s.Projects == nil {
			problem(w, 503, fmt.Errorf("projects unavailable"))
			return
		}
		var v struct {
			FullAccess bool `json:"full_access"`
		}
		if !decode(w, r, &v) {
			return
		}
		s.Projects.SetFullAccess(v.FullAccess)
		write(w, s.Projects.Snapshot())
	})
	mux.HandleFunc("GET /api/directories", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			path, _ = os.UserHomeDir()
		}
		path, err := harness.ProjectDirectory(path)
		if err != nil {
			problem(w, 400, err)
			return
		}
		f, err := os.Open(path)
		if err != nil {
			problem(w, 400, err)
			return
		}
		defer f.Close()
		entries, err := f.ReadDir(5001)
		if err != nil && err != io.EOF {
			problem(w, 400, err)
			return
		}
		truncated := len(entries) > 5000
		if truncated {
			entries = entries[:5000]
		}
		dirs := []map[string]string{}
		for _, entry := range entries {
			directory := entry.IsDir()
			if entry.Type()&os.ModeSymlink != 0 {
				info, e := os.Stat(filepath.Join(path, entry.Name()))
				directory = e == nil && info.IsDir()
			}
			if directory {
				dirs = append(dirs, map[string]string{"name": entry.Name(), "path": filepath.Join(path, entry.Name())})
			}
		}
		sort.Slice(dirs, func(i, j int) bool { return dirs[i]["name"] < dirs[j]["name"] })
		write(w, map[string]any{"path": path, "parent": filepath.Dir(path), "directories": dirs, "truncated": truncated})
	})
}

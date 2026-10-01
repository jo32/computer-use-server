package server

import (
	"computer-use-server/internal/localopen"
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
)

// Local file opening is shared by files and project folders, and is deliberately
// absent from the agent registry and the public console.
func (s *Server) localOpenRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/files/applications", func(w http.ResponseWriter, r *http.Request) {
		if s.Projects == nil {
			problem(w, 503, fmt.Errorf("projects unavailable"))
			return
		}
		path, err := s.localOpenPath(r.URL.Query().Get("project"), r.URL.Query().Get("path"))
		if err != nil {
			problem(w, 400, err)
			return
		}
		apps, err := localopen.Applications(r.Context(), path)
		if err != nil {
			problem(w, 400, err)
			return
		}
		write(w, map[string]any{"applications": apps})
	})
	mux.HandleFunc("POST /api/files/open", func(w http.ResponseWriter, r *http.Request) {
		if s.Projects == nil {
			problem(w, 503, fmt.Errorf("projects unavailable"))
			return
		}
		var in struct {
			Project     string `json:"project"`
			Path        string `json:"path"`
			Application string `json:"application"`
		}
		if !decode(w, r, &in) {
			return
		}
		path, err := s.localOpenPath(in.Project, in.Path)
		if err != nil {
			problem(w, 400, err)
			return
		}
		opener := s.openLocalPath
		if opener == nil {
			opener = localopen.Open
		}
		err = opener(r.Context(), path, in.Application)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, localopen.ErrApplicationUnavailable) || errors.Is(err, localopen.ErrUnsupported) {
				status = http.StatusBadRequest
			}
			problem(w, status, err)
			return
		}
		write(w, map[string]bool{"opened": true})
	})
}

func (s *Server) localOpenPath(project, path string) (string, error) {
	root, relative, close, err := s.Projects.Resolve(project, path)
	if err != nil {
		return "", err
	}
	defer close()
	if _, err := root.Stat(relative); err != nil {
		return "", err
	}
	// Hand the OS a canonical path, never an unchecked symlink or relative path.
	canonical, err := filepath.EvalSymlinks(filepath.Join(root.Name(), relative))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root.Name(), canonical)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("path is outside the selected project")
	}
	return canonical, nil
}

type localPathOpener func(context.Context, string, string) error

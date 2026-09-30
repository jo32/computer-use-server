package server

import (
	"computer-use-server/internal/buildinfo"
	"computer-use-server/internal/update"
	"errors"
	"net/http"
)

func (s *Server) updateStatus() update.Status {
	if s.Updates == nil {
		return update.Status{State: "disabled", Current: buildinfo.Version, Reason: "更新服务未启动"}
	}
	return s.Updates.Status()
}

// These routes belong exclusively to the authenticated local dashboard.
func (s *Server) updateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) { write(w, s.updateStatus()) })
	mux.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if s.Updates != nil {
			s.Updates.Check()
		}
		write(w, s.updateStatus())
	})
	mux.HandleFunc("POST /api/update/restart", func(w http.ResponseWriter, r *http.Request) {
		if s.Updates == nil {
			problem(w, 409, errors.New("更新服务未启动"))
			return
		}
		if err := s.Updates.RequestRestart(); err != nil {
			problem(w, 409, err)
			return
		}
		write(w, map[string]bool{"restarting": true})
	})
}

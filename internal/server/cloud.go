package server

import (
	"computer-use-server/internal/buildinfo"
	"computer-use-server/internal/cloud"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"runtime"
)

func (s *Server) StartCloud(baseURL string) error {
	s.Cloud = cloud.New(cloud.Options{Dir: filepath.Join(s.Store.Dir, "cloud"), URL: baseURL, Changed: s.Registry.Signal, RedactSecrets: s.Registry.AddSecrets, Snapshot: s.cloudSnapshot, Execute: s.executeCloudCommand})
	return s.Cloud.Start()
}
func (s *Server) cloudSnapshot() any {
	paused, enabled := s.Registry.State()
	return map[string]any{"version": buildinfo.Version, "platform": runtime.GOOS, "paused": paused, "enabled": enabled, "tunnel": s.tunnelStatus(true)}
}
func (s *Server) executeCloudCommand(cmd cloud.Command) error {
	switch cmd.Kind {
	case "tunnel.start":
		var in struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(cmd.Payload, &in); err != nil {
			return err
		}
		if in.Mode == "" {
			in.Mode = "quick"
		}
		return s.startSharingMode(in.Mode)
	case "tunnel.stop":
		if s.Tunnel != nil {
			return s.Tunnel.Stop()
		}
		return nil
	case "control.pause":
		var in struct {
			Paused *bool `json:"paused"`
		}
		if err := json.Unmarshal(cmd.Payload, &in); err != nil {
			return err
		}
		if in.Paused == nil {
			return errors.New("缺少 paused 设置")
		}
		s.Registry.SetPaused(*in.Paused)
		if s.Chrome != nil {
			s.Chrome.Refresh()
		}
		return nil
	case "capability.set":
		var in struct {
			Category string `json:"category"`
			Enabled  *bool  `json:"enabled"`
		}
		if err := json.Unmarshal(cmd.Payload, &in); err != nil {
			return err
		}
		if in.Enabled == nil {
			return errors.New("缺少 enabled 设置")
		}
		if !capabilityCategory(in.Category) {
			return errors.New("未知的能力配置")
		}
		return s.setCapability(in.Category, *in.Enabled)
	default:
		return errors.New("不支持的云端命令")
	}
}
func (s *Server) cloudStatus() any {
	if s.Cloud == nil {
		return nil
	}
	return s.Cloud.Status()
}
func (s *Server) cloudRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cloud", func(w http.ResponseWriter, r *http.Request) { write(w, s.cloudStatus()) })
	mux.HandleFunc("POST /api/cloud/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		}
		if !decode(w, r, &in) {
			return
		}
		if s.Cloud == nil {
			problem(w, 409, errors.New("云端连接尚未初始化"))
			return
		}
		status, err := s.Cloud.Login(in.URL, in.Name)
		if err != nil {
			problem(w, 400, err)
			return
		}
		write(w, status)
	})
	mux.HandleFunc("POST /api/cloud/disconnect", func(w http.ResponseWriter, r *http.Request) {
		if s.Cloud != nil {
			if err := s.Cloud.Disconnect(); err != nil {
				problem(w, 502, err)
				return
			}
		}
		write(w, s.cloudStatus())
	})
}

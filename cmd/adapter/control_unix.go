//go:build !windows

package main

import (
	"bytes"
	"computer-use-server/internal/server"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// OS permissions authenticate local administration. The runtime file contains
// only a socket path: file tools cannot read a reusable dashboard credential.
func startLocalControl(s *server.Server, info runtimeInfo, stop func()) (func(), error) {
	dir, err := os.MkdirTemp("", "rr-control-")
	if err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			_ = os.RemoveAll(dir)
		}
	}()
	socket := filepath.Join(dir, "api.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("local CLI socket (use a shorter TMPDIR if needed): %w", err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	ui := s.UI()
	control := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browser origins cannot manage the CLI socket", 403)
			return
		}
		if r.URL.Path == "/api/runtime" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(info)
			return
		}
		if r.URL.Path == "/api/daemon/stop" && r.Method == http.MethodPost {
			if stop == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"quit the desktop app to stop this instance"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"stopping":true}`))
			stop()
			return
		}
		ui.ServeHTTP(w, r)
	}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go control.Serve(ln)
	path := filepath.Join(s.Store.Dir, "control.json")
	if err := writePrivateJSON(path, controlEndpoint{Socket: socket}); err != nil {
		server.Shutdown(control)
		return nil, err
	}
	ready = true
	return sync.OnceFunc(func() {
		_ = os.Remove(path)
		server.Shutdown(control)
		_ = os.RemoveAll(dir)
	}), nil
}

func newControlClient(dir string) (*controlClient, error) {
	return controlClientWithTimeout(dir, 3*time.Minute)
}

func controlClientWithTimeout(dir string, timeout time.Duration) (*controlClient, error) {
	b, err := os.ReadFile(filepath.Join(dir, "control.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("ReadyRig is not running in this data directory; start 'readyrig serve' first")
	}
	if err != nil {
		return nil, err
	}
	var endpoint controlEndpoint
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&endpoint); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(endpoint.Socket) {
		return nil, errors.New("invalid local CLI socket path")
	}
	info, err := os.Stat(endpoint.Socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return nil, errors.New("ReadyRig local CLI socket is unavailable; start or restart the app")
	}
	c := &controlClient{client: &http.Client{
		Timeout: timeout,
		// Always use this Unix socket, never TCP, redirects, or an environment proxy.
		Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", endpoint.Socket)
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if _, err := c.request(http.MethodGet, "/api/connection", nil); err != nil {
		c.close()
		return nil, fmt.Errorf("cannot reach the running ReadyRig service: %w", err)
	}
	return c, nil
}

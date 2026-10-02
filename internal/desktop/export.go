package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type exportProgress struct {
	State     string `json:"state"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Bytes     int64  `json:"bytes"`
	Path      string `json:"path,omitempty"`
	Error     string `json:"error,omitempty"`
}
type exportJob struct {
	mu       sync.Mutex
	cond     *sync.Cond
	progress exportProgress
	cancel   context.CancelFunc
	file     *os.File
	header   http.Header
	code     int
}

func (j *exportJob) Header() http.Header { return j.header }
func (j *exportJob) WriteHeader(code int) {
	j.code = code
	j.mu.Lock()
	defer j.mu.Unlock()
	j.progress.Total, _ = strconv.Atoi(j.header.Get("X-Export-Total"))
}
func (j *exportJob) Write(p []byte) (int, error) {
	if j.code >= 400 {
		return 0, errors.New("export request failed")
	}
	written := 0
	for len(p) > 0 {
		j.mu.Lock()
		for j.progress.State == "paused" {
			j.cond.Wait()
		}
		if j.progress.State == "cancelling" {
			j.mu.Unlock()
			return written, context.Canceled
		}
		size := min(len(p), 64*1024)
		n, err := j.file.Write(p[:size])
		j.progress.Bytes += int64(n)
		j.progress.Completed += strings.Count(string(p[:n]), "\n")
		j.mu.Unlock()
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
		if n != size {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}
func (j *exportJob) control(action string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch action {
	case "pause":
		if j.progress.State == "running" {
			j.progress.State = "paused"
		}
	case "resume":
		if j.progress.State == "paused" {
			j.progress.State = "running"
		}
	case "cancel":
		if j.progress.State == "running" || j.progress.State == "paused" {
			j.progress.State = "cancelling"
			j.cancel()
		}
	}
	j.cond.Broadcast()
}
func (j *exportJob) run(handler http.Handler, r *http.Request) {
	defer os.Remove(j.file.Name())
	handler.ServeHTTP(j, r)
	j.mu.Lock()
	defer j.mu.Unlock()
	err := j.file.Sync()
	if closeErr := j.file.Close(); err == nil {
		err = closeErr
	}
	if j.progress.State == "cancelling" {
		j.progress.State = "cancelled"
		return
	}
	if j.code != 200 || j.header.Get("X-Export-Error") != "" || j.progress.Completed != j.progress.Total {
		err = errors.New("export incomplete")
	}
	if err == nil {
		err = os.Rename(j.file.Name(), j.progress.Path)
	}
	if err != nil {
		j.progress.State = "error"
		j.progress.Error = err.Error()
	} else {
		j.progress.State = "done"
	}
	j.cancel()
}

type exportManager struct {
	mu       sync.Mutex
	job      *exportJob
	choosing bool
	choose   func() (string, error)
	handler  http.Handler
}

func (m *exportManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job != nil {
		m.job.control("cancel")
	}
}
func (m *exportManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Method == http.MethodPost && r.URL.Query().Get("action") == "start" {
		if m.choosing {
			http.Error(w, "save dialog already open", 409)
			return
		}
		if m.job != nil {
			m.job.mu.Lock()
			state := m.job.progress.State
			m.job.mu.Unlock()
			if state == "running" || state == "paused" || state == "cancelling" {
				http.Error(w, "export already running", 409)
				return
			}
		}
		m.choosing = true
		m.mu.Unlock()
		path, err := m.choose()
		m.mu.Lock()
		m.choosing = false
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if path == "" {
			json.NewEncoder(w).Encode(exportProgress{State: "cancelled"})
			return
		}
		file, err := os.CreateTemp(filepath.Dir(path), ".readyrig-export-*")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		j := &exportJob{progress: exportProgress{State: "running", Path: path}, cancel: cancel, file: file, header: make(http.Header)}
		j.cond = sync.NewCond(&j.mu)
		m.job = j
		req := r.Clone(ctx)
		req.Method = http.MethodGet
		req.URL.Path = "/api/export"
		go j.run(m.handler, req)
	} else if r.Method == http.MethodPost {
		action := r.URL.Query().Get("action")
		if action != "pause" && action != "resume" && action != "cancel" {
			http.Error(w, "invalid export action", 400)
			return
		}
		if m.job != nil {
			m.job.control(action)
		}
	} else if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	if m.job == nil {
		json.NewEncoder(w).Encode(exportProgress{State: "idle"})
		return
	}
	m.job.mu.Lock()
	defer m.job.mu.Unlock()
	json.NewEncoder(w).Encode(m.job.progress)
}

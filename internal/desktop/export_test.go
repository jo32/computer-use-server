package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testExportJob(t *testing.T) *exportJob {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs.ndjson")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".export-*")
	if err != nil {
		t.Fatal(err)
	}
	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	j := &exportJob{file: f, cancel: cancel, header: make(http.Header), progress: exportProgress{State: "running", Path: path}}
	j.cond = sync.NewCond(&j.mu)
	return j
}
func TestExportPauseResumeAndCommit(t *testing.T) {
	j := testExportJob(t)
	j.control("pause")
	entered, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		j.run(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Export-Total", "1")
			w.WriteHeader(200)
			close(entered)
			w.Write([]byte("{}\n"))
		}), httptest.NewRequest("GET", "/api/export", nil))
	}()
	<-entered
	select {
	case <-done:
		t.Fatal("paused export completed")
	case <-time.After(30 * time.Millisecond):
	}
	b, _ := os.ReadFile(j.file.Name())
	if len(b) != 0 {
		t.Fatal("paused export wrote data")
	}
	j.control("resume")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("resume hung")
	}
	if j.progress.State != "done" || j.progress.Completed != 1 {
		t.Fatalf("%+v", j.progress)
	}
	b, _ = os.ReadFile(j.progress.Path)
	if string(b) != "{}\n" {
		t.Fatalf("unexpected output %q", b)
	}
}
func TestExportCancelAndFailurePreserveDestination(t *testing.T) {
	for _, action := range []string{"cancel", "fail"} {
		t.Run(action, func(t *testing.T) {
			j := testExportJob(t)
			if action == "cancel" {
				j.control("pause")
				j.control("cancel")
			}
			j.run(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Export-Total", "2")
				w.WriteHeader(200)
				w.Write([]byte("{}\n"))
			}), httptest.NewRequest("GET", "/api/export", nil))
			want := "error"
			if action == "cancel" {
				want = "cancelled"
			}
			if j.progress.State != want {
				t.Fatalf("%+v", j.progress)
			}
			b, _ := os.ReadFile(j.progress.Path)
			if string(b) != "original" {
				t.Fatal("overwrote destination")
			}
			if _, err := os.Stat(j.file.Name()); !os.IsNotExist(err) {
				t.Fatal("temporary file not removed")
			}
		})
	}
}
func TestExportSaveDialogCancelled(t *testing.T) {
	m := &exportManager{choose: func() (string, error) { return "", nil }}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("POST", "/api/window/export?action=start", nil))
	if w.Code != 200 || m.job != nil {
		t.Fatalf("cancelled picker started job: %s", w.Body.String())
	}
}

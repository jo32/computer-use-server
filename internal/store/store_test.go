package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPersistenceRecoveryAndFiltering(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := Call{ID: "one", Session: "s", Client: "test", Tool: "read_file", Category: "files", Status: "running", Started: time.Now(), Arguments: json.RawMessage(`{"path":"hello"}`)}
	if err = s.Save(c); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, n, err := s.List(Filter{Query: "hello", Category: "files"})
	if err != nil || n != 1 || rows[0].Status != "interrupted" {
		t.Fatalf("recovery: %v %d %v", rows, n, err)
	}
	_, n, err = s.List(Filter{Query: "not-there"})
	if err != nil || n != 0 {
		t.Fatal("filter failed")
	}
}

func TestSummaryListKeepsLargeBodiesInDetail(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := Call{ID: "large", Session: "s", Tool: "write_file", Status: "success", Arguments: json.RawMessage(`{"path":"note","content":"large body","env":{"secret":"hidden"}}`), Result: json.RawMessage(`{"stdout":"large output","frame_id":"frame","image_size":[1280,720]}`)}
	if err = s.Save(c); err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.List(Filter{Lightweight: true})
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	if strings.Contains(string(rows[0].Arguments), "large body") || strings.Contains(string(rows[0].Result), "large output") {
		t.Fatal("summary contains full bodies")
	}
	if !strings.Contains(string(rows[0].Result), "1280") {
		t.Fatal("summary lost replay dimensions")
	}
	full, err := s.Get("large")
	if err != nil || !strings.Contains(string(full.Result), "large output") {
		t.Fatal("full output lost", err)
	}
}

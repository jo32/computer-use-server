package store

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

type exportSink func([]byte) (int, error)

func (f exportSink) Write(p []byte) (int, error) { return f(p) }
func TestExportSnapshotAndFullResults(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"a", "b", "other"} {
		category := "terminal"
		if id == "other" {
			category = "files"
		}
		if err = s.Save(Call{ID: id, Category: category, Started: time.Now(), Arguments: json.RawMessage(`{"content":"full input"}`), Result: json.RawMessage(`{"text":"full result"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	total := 0
	first := true
	err = s.Export(context.Background(), Filter{Category: "terminal", Lightweight: true, Limit: 1, Offset: 1}, func(n int) { total = n }, exportSink(func(p []byte) (int, error) {
		if first {
			first = false
			// A paused reader must allow new calls and updates, without changing its snapshot.
			if e := s.Save(Call{ID: "new", Category: "terminal", Started: time.Now()}); e != nil {
				t.Fatal(e)
			}
			if e := s.Save(Call{ID: "a", Status: "changed", Started: time.Now()}); e != nil {
				t.Fatal(e)
			}
		}
		return out.Write(p)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || bytes.Count(out.Bytes(), []byte("\n")) != 2 {
		t.Fatalf("unexpected export: total=%d %s", total, out.String())
	}
	if bytes.Contains(out.Bytes(), []byte(`"id":"new"`)) || bytes.Contains(out.Bytes(), []byte(`changed`)) {
		t.Fatal("export did not retain snapshot")
	}
	if bytes.Count(out.Bytes(), []byte("full result")) != 2 || bytes.Count(out.Bytes(), []byte("full input")) != 2 {
		t.Fatal("export lost full payloads")
	}
}
func TestExportCancellation(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.Export(ctx, Filter{}, func(int) {}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected cancellation")
	}
}

package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDataIsPrivateAndSingleOwner(t *testing.T) {
	workspace := t.TempDir()
	if a, err := New(workspace, filepath.Join(workspace, "logs")); err == nil {
		a.Close()
		t.Fatal("data inside workspace accepted")
	}
	dir := t.TempDir()
	a, err := New(workspace, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if other, err := New(workspace, dir); err == nil {
		other.Close()
		t.Fatal("two writers acquired the same data directory")
	}
}

func TestAccessPathRotatesOnRestartAndIgnoresLegacyToken(t *testing.T) {
	workspace, dir := t.TempDir(), t.TempDir()
	legacy := filepath.Join(dir, "agent-token")
	// Even a malformed legacy token is no longer loaded.
	if err := os.WriteFile(legacy, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := New(workspace, dir)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := first.Server.AccessPath
	first.Close()
	second, err := New(workspace, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if oldPath == second.Server.AccessPath || len(second.Server.AccessPath) != 8 {
		t.Fatal("path did not rotate")
	}
	for _, pair := range []struct {
		Path string
		Code int
	}{{oldPath, 404}, {second.Server.AccessPath, 200}} {
		r := httptest.NewRequest("GET", "/"+pair.Path+"/api/v1/tools", nil)
		w := httptest.NewRecorder()
		second.Server.Gateway().ServeHTTP(w, r)
		if w.Code != pair.Code {
			t.Fatal(pair.Path, w.Code)
		}
	}
	b, err := os.ReadFile(legacy)
	if err != nil || string(b) != "old" {
		t.Fatal("legacy data unexpectedly changed")
	}
}
func TestFreshAppDoesNotCreateTokenFile(t *testing.T) {
	dir := t.TempDir()
	a, err := New(t.TempDir(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := os.Stat(filepath.Join(dir, "agent-token")); !os.IsNotExist(err) {
		t.Fatal("token file still created")
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirectoryPreservesExistingInstallation(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".local", "share", "relay")
	current := filepath.Join(home, ".local", "share", "readyrig")
	if got := defaultDataDir(home); got != current {
		t.Fatal(got)
	}
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "projects.json"), []byte(`{"projects":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := defaultDataDir(home); got != legacy {
		t.Fatalf("existing projects would be lost: %s", got)
	}
	previous := filepath.Join(home, ".local", "share", "readrig")
	if err := os.MkdirAll(previous, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "projects.json"), []byte(`{"projects":[{"name":"kept"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := defaultDataDir(home); got != previous {
		t.Fatalf("previous Readrig installation would be ignored: %s", got)
	}
	if err := os.MkdirAll(current, 0700); err != nil {
		t.Fatal(err)
	}
	if got := defaultDataDir(home); got != current {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(legacy, "projects.json")); err != nil {
		t.Fatal("legacy data changed", err)
	}
	if content, err := os.ReadFile(filepath.Join(previous, "projects.json")); err != nil || string(content) != `{"projects":[{"name":"kept"}]}` {
		t.Fatal("previous data changed", err)
	}
}

func TestEnvironmentPreservesLegacySettings(t *testing.T) {
	t.Setenv("RELAY_NO_UPDATE", "1")
	t.Setenv("READYRIG_NO_UPDATE", "0")
	if got := environment("NO_UPDATE"); got != "0" {
		t.Fatalf("current setting must take priority: %q", got)
	}
	if err := os.Unsetenv("READYRIG_NO_UPDATE"); err != nil {
		t.Fatal(err)
	}
	if got := environment("NO_UPDATE"); got != "1" {
		t.Fatalf("legacy setting lost: %q", got)
	}
}

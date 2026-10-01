package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupGuideSavesSelectedSettings(t *testing.T) {
	home := t.TempDir()
	data, workspace := filepath.Join(home, "private"), filepath.Join(home, "work space")
	f, o, err := startupFlags(home, data, false)
	if err != nil {
		t.Fatal(err)
	}
	f.Bool("if-needed", true, "installer option")
	var output bytes.Buffer
	start, err := setupGuide(f, o, strings.NewReader(workspace+"\nyes\nno\nno\n"), &output)
	if err != nil || start {
		t.Fatalf("start=%v error=%v\n%s", start, err, output.String())
	}
	values, err := readConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	var savedWorkspace string
	_ = json.Unmarshal(values["workspace"], &savedWorkspace)
	if savedWorkspace != workspace || string(values["allow-shell"]) != "true" || string(values["no-chrome"]) != "true" {
		t.Fatal(values)
	}
	for _, name := range []string{"full-access", "foreground", "if-needed", "data-dir"} {
		if _, ok := values[name]; ok {
			t.Fatalf("transient setting %s was saved", name)
		}
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		t.Fatal("workspace was not created", err)
	}
	for _, arg := range daemonArgs(f) {
		if strings.HasPrefix(arg, "--if-needed") {
			t.Fatal("installer option forwarded to daemon")
		}
	}
}

func TestSetupCancellationKeepsExistingConfiguration(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "private")
	f, o, err := startupFlags(home, data, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(data, f); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(data, configFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, answers := range []string{"", "\n", "\nyes\nno\n"} {
		_, err := setupGuide(f, o, strings.NewReader(answers), &bytes.Buffer{})
		if err == nil {
			t.Fatal("incomplete guide succeeded")
		}
		current, err := os.ReadFile(filepath.Join(data, configFile))
		if err != nil || !bytes.Equal(original, current) {
			t.Fatal("cancellation changed settings", err)
		}
	}
}

func TestSetupInvalidWorkspaceDoesNotSave(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "private")
	f, o, err := startupFlags(home, data, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = setupGuide(f, o, strings.NewReader(home+"\nno\nno\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("workspace containing private data was accepted")
	}
	if _, err := os.Stat(filepath.Join(data, configFile)); !os.IsNotExist(err) {
		t.Fatal("invalid settings were saved")
	}
}

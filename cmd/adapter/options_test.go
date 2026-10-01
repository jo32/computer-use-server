package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCapabilitySettingsPersistWithoutOverwritingOtherOptions(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	wantWorkspace := filepath.Join(home, "saved project")
	if err := writePrivateJSON(filepath.Join(dir, configFile), map[string]any{"workspace": wantWorkspace, "gateway": "127.0.0.1:9876", "chrome-user-data-dir": "custom profile"}); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		for _, category := range []string{"files", "terminal", "computer", "browser"} {
			if err := saveCapabilityConfig(dir, category, enabled); err != nil {
				t.Fatal(err)
			}
		}
		_, opts, err := startupFlags(home, dir, true)
		if err != nil {
			t.Fatal(err)
		}
		if opts.Shell != enabled || opts.Computer != enabled || opts.NoFiles == enabled || opts.NoChrome == enabled {
			t.Fatalf("switches lost on reload: %+v", opts)
		}
		if opts.Workspace != wantWorkspace || opts.Gateway != "127.0.0.1:9876" || opts.ChromeProfile != "custom profile" || opts.FullAccess {
			t.Fatalf("other launch settings changed: %+v", opts)
		}
		info, err := os.Stat(filepath.Join(dir, configFile))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("saved settings are not private", info, err)
		}
	}
}

func TestCapabilitySettingCreatesMinimalConfigAndPreservesMalformedConfig(t *testing.T) {
	dir := t.TempDir()
	if err := saveCapabilityConfig(dir, "computer", true); err != nil {
		t.Fatal(err)
	}
	values, err := readConfig(dir)
	if err != nil || len(values) != 1 || string(values["allow-computer"]) != "true" {
		t.Fatal("first switch saved unrelated defaults", values, err)
	}
	for _, category := range []string{"system", "full_access", "unknown"} {
		if err := saveCapabilityConfig(dir, category, true); err == nil {
			t.Fatal("saved unsupported category", category)
		}
	}
	malformed := []byte(`{"allow-shell":`)
	if err := os.WriteFile(filepath.Join(dir, configFile), malformed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveCapabilityConfig(dir, "computer", false); err == nil {
		t.Fatal("overwrote malformed configuration")
	}
	got, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil || !reflect.DeepEqual(got, malformed) {
		t.Fatal("configuration changed after save failure", string(got), err)
	}
}

func TestCapabilityRestartArgsUseCurrentSwitchesAndPreserveLaunchOptions(t *testing.T) {
	home := t.TempDir()
	enabled := map[string]bool{"files": false, "terminal": false, "computer": true, "browser": true}
	original := []string{"serve", "--foreground", "--workspace", filepath.Join(home, "项目 with spaces"), "--data-dir=/private data", "--allow-shell", "-allow-shell=true", "--allow-computer=false", "--no-chrome", "--no-files=false", "--full-access", "--chrome-mcp-command", "--allow-shell", "--"}
	before := append([]string(nil), original...)
	flags, opts, err := startupFlags(home, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	got := capabilityRestartArgs(original, flags, enabled)
	if !reflect.DeepEqual(original, before) {
		t.Fatal("changed original launch arguments")
	}
	args, dir, err := extractDataDir(got[1:], home)
	if err != nil || dir != "/private data" {
		t.Fatal("lost the original instance data directory", got, err)
	}
	if err := flags.Parse(args); err != nil {
		t.Fatal(err)
	}
	if !opts.Computer || opts.Shell || !opts.NoFiles || opts.NoChrome || !opts.FullAccess || !opts.Foreground || opts.Workspace != original[3] || opts.ChromeCommand != "--allow-shell" || flags.NArg() != 0 {
		t.Fatalf("stale capability flags survived update restart: %+v, args=%q", opts, got)
	}
}

package cliinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func appFixture(t *testing.T, root, name, version string) string {
	t.Helper()
	contents := filepath.Join(root, name+".app", "Contents")
	for _, dir := range []string{"MacOS", "Helpers"} {
		if err := os.MkdirAll(filepath.Join(contents, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	helper := filepath.Join(contents, "Helpers", "readyrig")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf 'ReadyRig "+version+"\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(contents, "MacOS", "readyrig")
}

func TestAppCLIInstallsUpdatesAndRepairsMovedApp(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	exe := appFixture(t, root, "ReadyRig", "1.0.0")
	o := Options{Executable: exe, Home: home, Shell: "/bin/zsh"}
	status := Install(o)
	if status.State != "installed" {
		t.Fatal(status)
	}
	run := func(want string) {
		t.Helper()
		output, err := exec.Command(status.Path, "version").CombinedOutput()
		if err != nil || string(output) != "ReadyRig "+want+"\n" {
			t.Fatalf("CLI failed: %v %s", err, output)
		}
	}
	run("1.0.0")
	// Replacing the bundled helper updates the registered command without copying.
	appFixture(t, root, "ReadyRig", "1.1.0")
	run("1.1.0")
	if status := Install(o); status.State != "installed" {
		t.Fatal(status)
	}
	for _, name := range []string{".zprofile", ".zshrc"} {
		body, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || strings.Count(string(body), pathMarker) != 1 {
			t.Fatalf("duplicated PATH settings in %s: %v", name, err)
		}
	}
	newRoot := filepath.Join(root, "Applications with spaces")
	if err := os.Mkdir(newRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "ReadyRig.app"), filepath.Join(newRoot, "ReadyRig.app")); err != nil {
		t.Fatal(err)
	}
	o.Executable = filepath.Join(newRoot, "ReadyRig.app", "Contents", "MacOS", "readyrig")
	if status := Install(o); status.State != "installed" {
		t.Fatal(status)
	}
	run("1.1.0")
	if target, err := os.Readlink(status.Path); err != nil || !strings.HasPrefix(target, newRoot) {
		t.Fatal("moved app link not repaired", target, err)
	}
}

func TestIndependentCLIAndShellSettingsArePreserved(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "binary", true: "symlink"}[symlink], func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			exe := appFixture(t, root, "ReadyRig", "1.0.0")
			cli := filepath.Join(home, ".local", "bin", "readyrig")
			if err := os.MkdirAll(filepath.Dir(cli), 0755); err != nil {
				t.Fatal(err)
			}
			body := []byte("#!/bin/sh\necho independently installed CLI\n")
			if symlink {
				other := filepath.Join(root, "other CLI")
				if err := os.WriteFile(other, body, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, cli); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(cli, body, 0755); err != nil {
				t.Fatal(err)
			}
			original := "# User settings without a final newline\nexport PERSONAL_SETTING='keep this'"
			if err := os.WriteFile(filepath.Join(home, ".profile"), []byte(original), 0640); err != nil {
				t.Fatal(err)
			}
			o := Options{Executable: exe, Home: home, Shell: "/bin/bash"}
			for i := 0; i < 2; i++ {
				if status := Install(o); status.State != "existing" {
					t.Fatal(status)
				}
			}
			if current, err := os.ReadFile(cli); err != nil || string(current) != string(body) {
				t.Fatal("existing command was replaced", err)
			}
			profile, err := os.ReadFile(filepath.Join(home, ".profile"))
			if err != nil || !strings.HasPrefix(string(profile), original) || strings.Count(string(profile), pathMarker) != 1 {
				t.Fatal("user profile changed or PATH duplicated", err)
			}
			if info, err := os.Stat(filepath.Join(home, ".profile")); err != nil || info.Mode().Perm() != 0640 {
				t.Fatal("profile permissions changed", err)
			}
			if _, err := os.Stat(filepath.Join(home, ".bash_profile")); !os.IsNotExist(err) {
				t.Fatal("new bash profile would hide user's .profile")
			}
		})
	}
}

func TestFreshShellFindsBundledCLI(t *testing.T) {
	for _, shell := range []string{"zsh", "bash", "fish"} {
		t.Run(shell, func(t *testing.T) {
			interpreter, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell is not installed")
			}
			root, home := t.TempDir(), t.TempDir()
			exe := appFixture(t, root, "ReadyRig", "1.0.0")
			o := Options{Executable: exe, Home: home, Shell: interpreter}
			if status := Install(o); status.State != "installed" {
				t.Fatal(status)
			}
			profiles := shellProfiles(o)
			var cmd *exec.Cmd
			switch shell {
			case "zsh":
				cmd = exec.Command(interpreter, "-d", "-i", "-c", "readyrig version")
			case "bash":
				cmd = exec.Command(interpreter, "--noprofile", "--norc", "-c", `. "$1"; readyrig version`, "--", profiles[0])
			case "fish":
				cmd = exec.Command(interpreter, "--no-config", "-c", `source $argv[1]; readyrig version`, profiles[0])
			}
			cmd.Env = append(os.Environ(), "HOME="+home, "PATH=/usr/bin:/bin", "ZDOTDIR="+home)
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "ReadyRig 1.0.0") {
				t.Fatalf("fresh shell cannot find CLI: %v %s", err, out)
			}
		})
	}
}

func TestSourceBuildTranslocationAndMissingHelperDoNotRegisterCLI(t *testing.T) {
	for _, kind := range []string{"source", "translocated", "missing"} {
		t.Run(kind, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			exe := filepath.Join(root, "readyrig")
			want := ""
			if kind == "translocated" {
				exe = appFixture(t, filepath.Join(root, "AppTranslocation", "random"), "ReadyRig", "1.0.0")
				want = "relocate"
			}
			if kind == "missing" {
				exe = filepath.Join(root, "ReadyRig.app", "Contents", "MacOS", "readyrig")
				want = "error"
			}
			status := Install(Options{Executable: exe, Home: home, Shell: "/bin/zsh"})
			if status.State != want {
				t.Fatal(status)
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatal("CLI registration changed home before it was possible", entries, err)
			}
		})
	}
}

func TestLegacyAppLinkAndCustomShellDirectories(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	exe := appFixture(t, root, "ReadyRig", "1.0.0")
	old := appFixture(t, root, "Relay", "0.5.0")
	helper, _ := HelperPath(old)
	cli := filepath.Join(home, ".local", "bin", "readyrig")
	if err := os.MkdirAll(filepath.Dir(cli), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(helper, cli); err != nil {
		t.Fatal(err)
	}
	zdir := filepath.Join(home, "zsh configuration")
	if status := Install(Options{Executable: exe, Home: home, Shell: "/bin/zsh", ZDotDir: zdir}); status.State != "installed" {
		t.Fatal(status)
	}
	if _, err := os.Stat(filepath.Join(zdir, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatal("ZDOTDIR was ignored")
	}
	config := filepath.Join(home, "configuration")
	if status := Install(Options{Executable: exe, Home: home, Shell: "/usr/bin/fish", ConfigHome: config}); status.State != "installed" {
		t.Fatal(status)
	}
	if _, err := os.Stat(filepath.Join(config, "fish", "conf.d", "readyrig.fish")); err != nil {
		t.Fatal(err)
	}
}

func TestOccupiedInstallPathIsReported(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	exe := appFixture(t, root, "ReadyRig", "1.0.0")
	path := filepath.Join(home, ".local", "bin", "readyrig")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if status := Install(Options{Executable: exe, Home: home}); status.State != "error" || status.Error == "" {
		t.Fatal(status)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("occupied path was overwritten", err)
	}
}

func TestMissingShellDefaultsToZsh(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	exe := appFixture(t, root, "ReadyRig", "1.0.0")
	if status := Install(Options{Executable: exe, Home: home}); status.State != "installed" {
		t.Fatal(status)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal("default shell was not configured", err)
	}
}

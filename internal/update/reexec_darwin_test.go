package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

type restartProbe struct {
	Before, After int
	Arguments     []string
}

// Run the restart in a subprocess so the CLI exec path cannot replace go test.
func TestReexecProbeProcess(t *testing.T) {
	args := os.Args
	for len(args) > 0 && args[0] != "--restart-probe" {
		args = args[1:]
	}
	if len(args) == 0 {
		return
	}
	args = args[1:]
	if args[0] == "parent" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		next := []string{"-test.run=^TestReexecProbeProcess$", "--", "--restart-probe", "child", args[1], strconv.Itoa(os.Getpid())}
		if err := Reexec(exe, append(next, args[2:]...)); err != nil {
			t.Fatal(err)
		}
		return
	}
	before, err := strconv.Atoi(args[2])
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(restartProbe{Before: before, After: os.Getpid(), Arguments: args[3:]})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(args[1], data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReexecMacAppAndCLI(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, bundled := range []bool{false, true} {
		t.Run(fmt.Sprintf("bundle=%t", bundled), func(t *testing.T) {
			root := t.TempDir()
			exe := self
			if bundled {
				app := filepath.Join(root, "Restart Fixture.app")
				contents := filepath.Join(app, "Contents")
				exe = filepath.Join(contents, "MacOS", "probe")
				if err := os.MkdirAll(filepath.Dir(exe), 0755); err != nil {
					t.Fatal(err)
				}
				binary, err := os.ReadFile(self)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(exe, binary, 0755); err != nil {
					t.Fatal(err)
				}
				// A background fixture exercises LaunchServices without opening UI.
				plist := fmt.Sprintf(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>dev.readyrig.restart-probe.%d</string><key>CFBundleExecutable</key><string>probe</string><key>CFBundlePackageType</key><string>APPL</string><key>LSBackgroundOnly</key><true/></dict></plist>`, time.Now().UnixNano())
				if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0644); err != nil {
					t.Fatal(err)
				}
				if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", app).CombinedOutput(); err != nil {
					t.Fatalf("sign fixture: %v: %s", err, output)
				}
			}
			result := filepath.Join(root, "result.json")
			want := []string{"--workspace", filepath.Join(root, "项目 with spaces"), "--no-chrome", "literal-$value"}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			args := []string{"-test.run=^TestReexecProbeProcess$", "--", "--restart-probe", "parent", result}
			if output, err := exec.CommandContext(ctx, exe, append(args, want...)...).CombinedOutput(); err != nil {
				t.Fatalf("restart: %v: %s", err, output)
			}
			for {
				data, err := os.ReadFile(result)
				if err == nil {
					var probe restartProbe
					if err := json.Unmarshal(data, &probe); err != nil {
						t.Fatal(err)
					}
					if (probe.Before != probe.After) != bundled {
						t.Fatalf("app must get a fresh PID; CLI must retain its PID: %+v", probe)
					}
					if !slices.Equal(probe.Arguments, want) {
						t.Fatalf("launch arguments changed: %q", probe.Arguments)
					}
					return
				}
				select {
				case <-ctx.Done():
					t.Fatal("restarted app did not report readiness")
				case <-time.After(20 * time.Millisecond):
				}
			}
		})
	}
}

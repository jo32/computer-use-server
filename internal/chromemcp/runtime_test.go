package chromemcp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSupportedNodeVersions(t *testing.T) {
	for _, v := range []string{"v20.19.0", "v20.20.1", "v22.12.0", "v22.20.0", "v23.0.0", "v25.2.1"} {
		if !supportedNode(v) {
			t.Fatalf("rejected supported Node %q", v)
		}
	}
	for _, v := range []string{"v18.2.0", "v20.18.9", "v21.7.3", "v22.11.9", "v25.x.1", "v25.0", "v25.0.0-preview", "v25.-1.0", ""} {
		if supportedNode(v) {
			t.Fatalf("accepted unsupported Node %q", v)
		}
	}
}

func TestFinderSkipsLegacyNodeAndUsesMatchingNPX(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "legacy")
	modern := filepath.Join(t.TempDir(), "modern")
	oldNode, newNode := filepath.Join(legacy, "node"), filepath.Join(modern, "node")
	oldNPX, newNPX := filepath.Join(legacy, "npx"), filepath.Join(modern, "npx")
	executables := map[string]string{"node": oldNode, "npx": oldNPX, oldNode: oldNode, newNode: newNode, oldNPX: oldNPX, newNPX: newNPX}
	search := runtimeSearch{
		dirs: []string{modern}, env: []string{"PATH=" + legacy, "KEEP=value"},
		lookPath: func(name string) (string, error) {
			if p, ok := executables[name]; ok {
				return p, nil
			}
			return "", os.ErrNotExist
		},
		nodeVersion: func(path string) (string, error) {
			if path == oldNode {
				return "v18.2.0", nil
			}
			return "v25.2.1", nil
		},
	}
	target := target{Args: []string{"--autoConnect", "--channel=stable"}}
	p, args, env, err := search.command(Options{}, target)
	if err != nil || p != newNPX || !reflect.DeepEqual(args, []string{"--yes", Package, "--autoConnect", "--channel=stable", "--no-usage-statistics", "--no-performance-crux"}) {
		t.Fatalf("wrong runtime: %s %v, %v", p, args, err)
	}
	if !reflect.DeepEqual(env, []string{"KEEP=value", "PATH=" + modern + string(os.PathListSeparator) + legacy}) {
		t.Fatalf("child may still use legacy Node: %v", env)
	}
	// Installed and explicitly configured MCP scripts need the same Node PATH.
	installed := filepath.Join(legacy, "chrome-devtools-mcp")
	executables["chrome-devtools-mcp"], executables[installed] = installed, installed
	for _, opts := range []Options{{}, {Command: installed}} {
		p, args, gotEnv, err := search.command(opts, target)
		if err != nil || p != installed || args[0] != "--autoConnect" || !reflect.DeepEqual(gotEnv, env) {
			t.Fatalf("installed MCP used wrong runtime: %s %v %v, %v", p, args, gotEnv, err)
		}
	}
	// A working PATH installation remains preferred over fallback installations.
	search.nodeVersion = func(string) (string, error) { return "v22.12.0", nil }
	delete(executables, "chrome-devtools-mcp")
	p, _, _, err = search.command(Options{}, target)
	if err != nil || p != oldNPX {
		t.Fatalf("ignored compatible PATH: %s, %v", p, err)
	}
}

func TestUnsupportedRuntimeReportsDetectedVersion(t *testing.T) {
	search := runtimeSearch{
		lookPath: func(name string) (string, error) {
			if name == "node" || name == "/legacy/node" {
				return "/legacy/node", nil
			}
			if name == "/custom-mcp" {
				return name, nil
			}
			return "", os.ErrNotExist
		},
		nodeVersion: func(string) (string, error) { return "v18.2.0", nil },
	}
	if _, _, _, err := search.command(Options{}, target{}); err == nil || !strings.Contains(err.Error(), "v18.2.0 (/legacy/node)") {
		t.Fatalf("runtime error is not actionable: %v", err)
	}
	if p, _, _, err := search.command(Options{Command: "/custom-mcp"}, target{}); err != nil || p != "/custom-mcp" {
		t.Fatalf("self-contained command requires Node: %s, %v", p, err)
	}
	if _, _, _, err := search.command(Options{Command: "/missing-mcp"}, target{}); err == nil {
		t.Fatal("ignored an invalid explicit command")
	}
}

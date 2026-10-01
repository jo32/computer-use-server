package chromemcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const nodeRequirement = "需要 Node.js（20.19+、22.12+ 或更新版）和 npx 来运行官方 Chrome DevTools MCP"

type runtimeSearch struct {
	dirs        []string
	env         []string
	lookPath    func(string) (string, error)
	nodeVersion func(string) (string, error)
}

func command(opts Options, t target) (string, []string, []string, error) {
	home, _ := os.UserHomeDir()
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".volta/bin"), filepath.Join(home, ".local/share/mise/shims")}
	nvm, _ := filepath.Glob(filepath.Join(home, ".nvm/versions/node/*/bin"))
	dirs = append(dirs, nvm...)
	return (runtimeSearch{dirs: dirs, env: os.Environ(), lookPath: exec.LookPath, nodeVersion: installedNodeVersion}).command(opts, t)
}

func installedNodeVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, path, "--version").Output()
	return strings.TrimSpace(string(b)), err
}

func supportedNode(version string) bool {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	patch, err := strconv.Atoi(parts[2])
	return err == nil && patch >= 0 && minor >= 0 && (major == 20 && minor >= 19 || major == 22 && minor >= 12 || major >= 23)
}

func (s runtimeSearch) command(opts Options, t target) (string, []string, []string, error) {
	args := append(append([]string{}, t.Args...), "--no-usage-statistics", "--no-performance-crux")
	var direct string
	if opts.Command != "" {
		var err error
		direct, err = s.lookPath(opts.Command)
		if err != nil {
			return "", nil, nil, err
		}
	} else {
		direct, _ = s.lookPath("chrome-devtools-mcp")
	}

	// Finder's PATH can contain a legacy Node even when Homebrew or nvm has
	// a supported version. Check all candidates rather than trusting PATH alone.
	var candidates []string
	if p, err := s.lookPath("node"); err == nil {
		candidates = append(candidates, p)
	}
	if p, err := s.lookPath("npx"); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(p), "node"))
	}
	for _, dir := range s.dirs {
		candidates = append(candidates, filepath.Join(dir, "node"))
	}
	seen := map[string]bool{}
	var node, rejected string
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		path, err := s.lookPath(candidate)
		if err != nil {
			continue
		}
		version, err := s.nodeVersion(path)
		if err != nil {
			continue
		}
		if supportedNode(version) {
			node = path
			break
		}
		if rejected == "" {
			rejected = fmt.Sprintf("%s (%s)", version, path)
		}
	}
	if node == "" {
		// An explicit command can be a self-contained wrapper with its own runtime.
		if opts.Command != "" {
			return direct, args, s.env, nil
		}
		if rejected != "" {
			return "", nil, nil, fmt.Errorf("检测到的 Node.js 不兼容：%s；%s", rejected, nodeRequirement)
		}
		return "", nil, nil, fmt.Errorf("%s", nodeRequirement)
	}

	dir := filepath.Dir(node)
	env := prependRuntimePath(s.env, dir)
	if direct != "" {
		return direct, args, env, nil
	}
	// Prefer the selected Node installation's npm/npx, keeping its shebang and
	// any nested npm subprocesses on the same validated Node runtime.
	for _, name := range append([]string{filepath.Join(dir, "npx"), "npx"}, runtimeNPXPaths(s.dirs)...) {
		if p, err := s.lookPath(name); err == nil {
			return p, append([]string{"--yes", Package}, args...), env, nil
		}
	}
	return "", nil, nil, fmt.Errorf("%s", nodeRequirement)
}

func runtimeNPXPaths(dirs []string) []string {
	paths := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		paths = append(paths, filepath.Join(dir, "npx"))
	}
	return paths
}

func prependRuntimePath(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	var previous string
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "PATH") {
			previous = value
		} else {
			out = append(out, entry)
		}
	}
	if previous != "" {
		dir += string(os.PathListSeparator) + previous
	}
	return append(out, "PATH="+dir)
}

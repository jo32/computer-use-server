// Package chromemcp bridges the official Chrome DevTools MCP over stdio.
package chromemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const Package = "chrome-devtools-mcp@1.10.1"

type Options struct {
	Disabled    bool
	BrowserURL  string
	UserDataDir string
	// Command is an optional installed chrome-devtools-mcp executable (never a shell string).
	Command string
}
type target struct {
	Key  string
	Args []string
}

func localURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("Chrome 调试地址必须是本机 HTTP 地址，例如 http://127.0.0.1:9222")
	}
	host := u.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf("Chrome 调试地址只允许 localhost 或回环 IP")
		}
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("Chrome 调试地址需要有效端口")
	}
	return strings.TrimRight(raw, "/"), nil
}

func chromePaths() (binaries, profiles []string) {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		binaries = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome")}
		profiles = []string{filepath.Join(home, "Library/Application Support/Google/Chrome")}
	case "windows":
		for _, base := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if base != "" {
				binaries = append(binaries, filepath.Join(base, "Google/Chrome/Application/chrome.exe"))
			}
		}
		profiles = []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Google/Chrome/User Data")}
	default:
		for _, name := range []string{"google-chrome", "google-chrome-stable"} {
			if p, err := exec.LookPath(name); err == nil {
				binaries = append(binaries, p)
			}
		}
		config := os.Getenv("XDG_CONFIG_HOME")
		if config == "" {
			config = filepath.Join(home, ".config")
		}
		profiles = []string{filepath.Join(config, "google-chrome")}
	}
	return
}
func discover(ctx context.Context, opts Options) (target, error) {
	bins, profiles := chromePaths()
	return discoverAt(ctx, opts, bins, profiles)
}

func discoverAt(ctx context.Context, opts Options, bins, profiles []string) (target, error) {
	installed := false
	for _, p := range bins {
		if s, e := os.Stat(p); e == nil && !s.IsDir() {
			installed = true
			break
		}
	}
	if !installed {
		return target{}, fmt.Errorf("未检测到本地 Google Chrome")
	}
	if opts.BrowserURL != "" {
		return probeURL(ctx, opts.BrowserURL)
	}
	if opts.UserDataDir != "" {
		profiles = []string{opts.UserDataDir}
	}
	var readErr error
	for _, dir := range profiles {
		// This file contains only a port and WebSocket path; never read browsing data.
		f, err := os.Open(filepath.Join(dir, "DevToolsActivePort"))
		if err != nil {
			if !os.IsNotExist(err) {
				readErr = err
			}
			continue
		}
		b, err := io.ReadAll(io.LimitReader(f, 4097))
		f.Close()
		if err != nil || len(b) > 4096 {
			continue
		}
		lines := strings.Fields(string(b))
		if len(lines) != 2 {
			continue
		}
		port, err := strconv.Atoi(lines[0])
		if err != nil || port < 1 || port > 65535 || !strings.HasPrefix(lines[1], "/devtools/browser/") {
			continue
		}
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		conn, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext(ctx, "tcp", addr)
		if err != nil {
			continue
		}
		conn.Close()
		return target{Key: dir + "|" + string(b), Args: []string{"--autoConnect", "--user-data-dir=" + dir}}, nil
	}
	// Explicit profiles must not silently attach to a different browser.
	if opts.UserDataDir == "" {
		if t, err := probeURL(ctx, "http://127.0.0.1:9222"); err == nil {
			return t, nil
		}
	}
	if readErr != nil {
		return target{}, fmt.Errorf("无法读取 Chrome 调试入口；请检查 Relay 对 Chrome 数据目录的访问权限，或指定 --chrome-browser-url")
	}
	return target{}, fmt.Errorf("等待 Chrome 开启远程调试：chrome://inspect/#remote-debugging")
}
func probeURL(ctx context.Context, raw string) (target, error) {
	base, err := localURL(raw)
	if err != nil {
		return target{}, err
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/json/version", nil)
	if err != nil {
		return target{}, err
	}
	res, err := client.Do(req)
	if err != nil {
		return target{}, fmt.Errorf("Chrome 调试入口不可用：%w", err)
	}
	defer res.Body.Close()
	var version struct {
		Browser              string
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&version) != nil || !strings.Contains(version.Browser, "Chrome/") {
		return target{}, fmt.Errorf("此端口没有提供 Chrome 调试服务")
	}
	ws, err := url.Parse(version.WebSocketDebuggerURL)
	if err != nil || ws.Scheme != "ws" || ws.User != nil || ws.RawQuery != "" || ws.Fragment != "" || !strings.HasPrefix(ws.Path, "/devtools/browser/") {
		return target{}, fmt.Errorf("无效的 Chrome WebSocket 调试地址")
	}
	if _, err = localURL("http://" + ws.Host); err != nil {
		return target{}, err
	}
	// Use the validated endpoint, avoiding another discovery/redirect in the child.
	return target{Key: version.WebSocketDebuggerURL, Args: []string{"--ws-endpoint=" + version.WebSocketDebuggerURL}}, nil
}

func command(opts Options, t target) (string, []string, []string, error) {
	args := append([]string{}, t.Args...)
	args = append(args, "--no-usage-statistics", "--no-performance-crux")
	if opts.Command != "" {
		p, err := exec.LookPath(opts.Command)
		return p, args, os.Environ(), err
	}
	if p, err := exec.LookPath("chrome-devtools-mcp"); err == nil {
		return p, args, os.Environ(), nil
	}
	// Finder-launched apps have a minimal PATH. Locate common Node installations too.
	home, _ := os.UserHomeDir()
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".volta/bin"), filepath.Join(home, ".local/share/mise/shims")}
	nvm, _ := filepath.Glob(filepath.Join(home, ".nvm/versions/node/*/bin"))
	dirs = append(dirs, nvm...)
	p, err := exec.LookPath("npx")
	if err != nil {
		for _, dir := range dirs {
			candidate := filepath.Join(dir, "npx")
			if s, e := os.Stat(candidate); e == nil && !s.IsDir() {
				p = candidate
				break
			}
		}
	}
	if p == "" {
		return "", nil, nil, fmt.Errorf("需要 Node.js（20.19+、22.12+ 或更新版）和 npx 来运行官方 Chrome DevTools MCP")
	}
	env := append(os.Environ(), "PATH="+filepath.Dir(p)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p, append([]string{"--yes", Package}, args...), env, nil
}

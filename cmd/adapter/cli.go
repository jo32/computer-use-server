package main

import (
	"bytes"
	"computer-use-server/internal/app"
	"computer-use-server/internal/buildinfo"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const cliHelp = `ReadyRig — configure and use your local agent service

Usage: readyrig [command] [options]

  serve / web                 Run without a desktop window
  desktop                     Open the desktop app (desktop builds only)
  init [startup options]      Save settings for serve/web; create workspace
  config show                 Show saved startup settings
  config set <key> <value>    Change a startup setting while the service is stopped
  status                      Show the running service state as JSON
  connection                  Show agent, MCP and sharing connection information
  tools                       List live tool definitions
  call [--session ID] <tool> [JSON|-]
                              Invoke a tool; '-' reads JSON from stdin
  projects list|add <path>|remove <id>|use <id>
                              Manage authorized project folders
  capability <category> on|off
                              Set files, terminal, browser or computer for this run
  pause / resume              Pause or resume tool execution
  share start [quick|fixed]|stop|status
                              Manage public connections for this run
  share configure --url <https://domain> [--token-stdin]
                              Save fixed tunnel settings; read secret from stdin
  cloud login [--name NAME] [--url URL]|status|logout
                              Bind this computer from a browser on another machine
  service install|start|stop|restart|status|uninstall|print
                              Manage a Linux systemd user service
  update / version            Update the binary or print its version
  help                        Show this help

Global option: --data-dir PATH (before or after a command)
With no command, CLI builds run the service; desktop builds open the app.
Local control commands support macOS/Linux and require a running instance with the same data directory.
Full Access is never saved. Runtime switches reset on restart; projects are saved.

Startup options:
`

type controlEndpoint struct {
	Socket string `json:"socket"`
}

type controlClient struct {
	client  *http.Client
	session string
}

func (c *controlClient) request(method, path string, input any) (json.RawMessage, error) {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, "http://readyrig.local"+path, body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Client-Name", "ReadyRig CLI")
	if c.session != "" {
		r.Header.Set("X-Session-ID", c.session)
	}
	res, err := c.client.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 8<<20 {
		return nil, errors.New("CLI response exceeds 8 MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &problem)
		if problem.Error == "" {
			problem.Error = res.Status
		}
		return b, errors.New(problem.Error)
	}
	if !json.Valid(b) {
		return nil, errors.New("service returned invalid JSON")
	}
	return b, nil
}

func manageCLI(mode string, args []string, flags *flag.FlagSet, o *startupOptions) (bool, error) {
	switch mode {
	case "version":
		if len(args) != 0 {
			return true, errors.New("usage: readyrig version")
		}
		fmt.Println("ReadyRig", buildinfo.Version)
		return true, nil
	case "help":
		flags.SetOutput(os.Stdout)
		flags.Usage()
		return true, nil
	case "config":
		return true, configureCLI(args, flags, o)
	case "service":
		return true, manageService(args, o.DataDir)
	case "status", "connection", "tools", "call", "projects", "capability", "pause", "resume", "share", "cloud":
		method, path, input, session, err := controlAction(mode, args, o)
		if err != nil {
			return true, err
		}
		client, err := newControlClient(o.DataDir)
		if err != nil {
			return true, err
		}
		client.session = session
		b, err := client.request(method, path, input)
		if err != nil {
			if mode == "call" && json.Valid(b) {
				_ = printJSON(os.Stdout, b)
			}
			return true, err
		}
		if mode == "tools" || mode == "projects" && path == "/api/state" {
			var state map[string]json.RawMessage
			if err := json.Unmarshal(b, &state); err != nil {
				return true, err
			}
			if mode == "tools" {
				b = state["tools"]
			} else {
				b = state["project_access"]
			}
		}
		if mode == "connection" {
			var connection map[string]any
			if err := json.Unmarshal(b, &connection); err != nil {
				return true, err
			}
			if gateway, ok := connection["gateway"].(string); ok {
				connection["mcp"] = gateway + "/mcp"
			}
			return true, printJSON(os.Stdout, connection)
		}
		return true, printJSON(os.Stdout, b)
	}
	return false, nil
}

func configurationLock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return app.LockData(dir)
}

func configureCLI(args []string, flags *flag.FlagSet, o *startupOptions) error {
	if len(args) == 1 && args[0] == "show" {
		values, err := readConfig(o.DataDir)
		if err != nil {
			return err
		}
		return printJSON(os.Stdout, values)
	}
	if len(args) != 3 || args[0] != "set" {
		return errors.New("usage: readyrig config show | config set <startup-option> <value>")
	}
	name := strings.ReplaceAll(strings.TrimPrefix(args[1], "--"), "_", "-")
	if name == "full-access" {
		return errors.New("Full Access is session-only; use 'serve --full-access' for this run")
	}
	if flags.Lookup(name) == nil || !persistentFlag(name) {
		return fmt.Errorf("unknown startup setting %q; see 'readyrig help'", name)
	}
	unlock, err := configurationLock(o.DataDir)
	if err != nil {
		return err
	}
	defer unlock()
	// Reload under the same lock used by the server; avoid racing startup or another editor.
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	flags, o, err = startupFlags(home, o.DataDir, true)
	if err != nil {
		return err
	}
	if err := flags.Set(name, args[2]); err != nil {
		return err
	}
	if err := validateOptions(o); err != nil {
		return err
	}
	if err := flags.Set("workspace", o.Workspace); err != nil {
		return err
	}
	if err := saveConfig(o.DataDir, flags); err != nil {
		return err
	}
	fmt.Println("Saved", name+". It will apply to the next serve/web launch.")
	return nil
}

func controlAction(mode string, args []string, o *startupOptions) (method, path string, input any, session string, err error) {
	method = http.MethodPost
	get := func(p string) { method, path = http.MethodGet, p }
	bad := func(usage string) { err = errors.New("usage: readyrig " + usage) }
	switch mode {
	case "status", "connection", "tools", "pause", "resume":
		if len(args) != 0 {
			bad(mode)
			break
		}
		switch mode {
		case "status", "tools":
			get("/api/state")
		case "connection":
			get("/api/connection")
		default:
			path, input = "/api/pause", map[string]bool{"paused": mode == "pause"}
		}
	case "call":
		f := flag.NewFlagSet("call", flag.ContinueOnError)
		f.StringVar(&session, "session", "cli", "Stable session ID for related tool calls")
		if err = f.Parse(args); err != nil {
			break
		}
		args = f.Args()
		if len(args) < 1 || len(args) > 2 {
			bad("call [--session ID] <tool> [JSON|-]")
			break
		}
		if len(session) > 128 {
			err = errors.New("session ID exceeds 128 characters")
			break
		}
		raw := []byte(`{}`)
		if len(args) == 2 {
			if args[1] == "-" {
				raw, err = io.ReadAll(io.LimitReader(os.Stdin, (2<<20)+1))
			} else {
				raw = []byte(args[1])
			}
		}
		if err != nil {
			break
		}
		if len(raw) > 2<<20 {
			err = errors.New("tool arguments exceed 2 MiB")
			break
		}
		if !json.Valid(raw) {
			err = errors.New("tool arguments must be valid JSON")
			break
		}
		path, input = "/api/tools/"+url.PathEscape(args[0]), json.RawMessage(raw)
	case "projects":
		if len(args) == 1 && args[0] == "list" {
			get("/api/state")
			break
		}
		if len(args) != 2 {
			bad("projects list | add <path> | remove <id> | use <id>")
			break
		}
		v := map[string]string{"action": args[0]}
		switch args[0] {
		case "add":
			home, e := os.UserHomeDir()
			if e != nil {
				err = e
				break
			}
			v["path"], err = absolutePath(home, args[1])
		case "use":
			v["action"], v["id"] = "activate", args[1]
		case "remove":
			v["id"] = args[1]
		default:
			bad("projects list | add <path> | remove <id> | use <id>")
		}
		path, input = "/api/projects", v
	case "capability":
		if len(args) != 2 || (args[1] != "on" && args[1] != "off") {
			bad("capability <files|terminal|browser|computer> <on|off>")
			break
		}
		if args[0] != "files" && args[0] != "terminal" && args[0] != "browser" && args[0] != "computer" {
			err = errors.New("unknown capability")
			break
		}
		path, input = "/api/capability", map[string]any{"category": args[0], "enabled": args[1] == "on"}
	case "share":
		if len(args) == 1 && args[0] == "status" {
			get("/api/tunnel")
			break
		}
		if len(args) == 1 && args[0] == "stop" {
			path, input = "/api/tunnel/stop", map[string]any{}
			break
		}
		if len(args) >= 1 && args[0] == "configure" {
			f := flag.NewFlagSet("share configure", flag.ContinueOnError)
			var fixedURL string
			var tokenStdin bool
			f.StringVar(&fixedURL, "url", "", "Fixed tunnel HTTPS URL")
			f.BoolVar(&tokenStdin, "token-stdin", false, "Read Cloudflare Tunnel Token from stdin")
			if err = f.Parse(args[1:]); err != nil {
				break
			}
			if fixedURL == "" || f.NArg() != 0 {
				bad("share configure --url <https://domain> [--token-stdin]")
				break
			}
			var token []byte
			if tokenStdin {
				token, err = io.ReadAll(io.LimitReader(os.Stdin, 65537))
				if len(token) > 65536 {
					err = errors.New("tunnel token exceeds 64 KiB")
				}
			}
			path, input = "/api/tunnel/fixed", map[string]string{"url": fixedURL, "token": strings.TrimSpace(string(token))}
			break
		}
		if (len(args) == 1 || len(args) == 2) && args[0] == "start" {
			shareMode := "quick"
			if len(args) == 2 {
				shareMode = args[1]
			}
			if shareMode != "quick" && shareMode != "fixed" {
				bad("share start [quick|fixed]")
				break
			}
			path, input = "/api/tunnel/start", map[string]string{"mode": shareMode}
			break
		}
		bad("share start [quick|fixed] | stop | status | configure --url URL --token-stdin")
	case "cloud":
		if len(args) == 1 && args[0] == "status" {
			get("/api/cloud")
			break
		}
		if len(args) == 1 && args[0] == "logout" {
			path, input = "/api/cloud/disconnect", map[string]any{}
			break
		}
		if len(args) >= 1 && args[0] == "login" {
			f := flag.NewFlagSet("cloud login", flag.ContinueOnError)
			name, _ := os.Hostname()
			cloudURL := o.CloudURL
			f.StringVar(&name, "name", name, "Computer name")
			f.StringVar(&cloudURL, "url", cloudURL, "Cloud console URL")
			if err = f.Parse(args[1:]); err != nil {
				break
			}
			if f.NArg() != 0 {
				bad("cloud login [--name NAME] [--url URL]")
				break
			}
			path, input = "/api/cloud/login", map[string]string{"name": name, "url": cloudURL}
			break
		}
		bad("cloud login [--name NAME] [--url URL] | status | logout")
	}
	return
}

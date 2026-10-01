package main

import (
	"bufio"
	"computer-use-server/internal/buildinfo"
	"computer-use-server/internal/harness"
	"computer-use-server/internal/store"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	tuiReset  = "\x1b[0m"
	tuiDim    = "\x1b[2m"
	tuiAccent = "\x1b[36m"
)

type tuiState struct {
	Paused    bool                 `json:"paused"`
	Enabled   map[string]bool      `json:"enabled"`
	Workspace string               `json:"workspace"`
	Gateway   string               `json:"gateway"`
	Projects  harness.ProjectState `json:"project_access"`
	Tools     []harness.Spec       `json:"tools"`
	Chrome    struct {
		State string `json:"state"`
	} `json:"chrome"`
	Tunnel struct{ State, URL string } `json:"tunnel"`
}

type terminalUI struct {
	flags                  *flag.FlagSet
	opts                   *startupOptions
	client                 *controlClient
	state                  tuiState
	info                   runtimeInfo
	calls                  []store.Call
	tab, selected          int
	message, prompt, input string
	busy                   bool
}

func runTUI(f *flag.FlagSet, o *startupOptions) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) || os.Getenv("TERM") == "dumb" {
		fmt.Fprint(os.Stdout, "The terminal dashboard needs an interactive terminal. Use a command below for scripts.\n\n")
		fmt.Fprint(os.Stdout, cliHelp)
		return nil
	}
	client, probeErr := controlClientWithTimeout(o.DataDir, time.Second)
	if probeErr == nil {
		client.close()
	} else {
		if _, err := os.Stat(filepath.Join(o.DataDir, configFile)); errors.Is(err, os.ErrNotExist) {
			if err := runSetup(f, o); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			fmt.Fprintln(os.Stdout, "Starting ReadyRig...")
			// A startup error remains visible in the dashboard and can be retried.
			if err := startDaemon(f, o, os.Stdout); err != nil {
				fmt.Fprintln(os.Stdout, err)
			}
		}
	}
	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l")
	defer func() {
		fmt.Fprint(os.Stdout, tuiReset+"\x1b[?25h\x1b[?1049l")
		_ = term.Restore(int(os.Stdin.Fd()), old)
	}()
	u := &terminalUI{flags: f, opts: o, message: "Ready. Quitting this dashboard keeps the service running."}
	defer func() { u.client.close() }()
	u.refresh()
	keys := make(chan string, 32)
	done := make(chan struct{})
	defer close(done)
	go readTerminalKeys(os.Stdin, keys, done)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	results := make(chan error, 1)
	var pending []string
	for {
		width, height, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			width, height = 80, 24
		}
		fmt.Fprint(os.Stdout, u.render(width, height))
		input := (<-chan string)(keys)
		fromPending := !u.busy && len(pending) > 0
		if fromPending {
			queued := make(chan string, 1)
			queued <- pending[0]
			input = queued
		}
		select {
		case <-stop:
			return nil
		case <-tick.C:
			if !u.busy {
				u.refresh()
			}
		case err := <-results:
			u.busy = false
			if err != nil {
				u.message = err.Error()
			} else {
				u.message = "Done."
			}
			u.refresh()
		case key := <-input:
			if fromPending {
				pending = pending[1:]
			}
			if key == "eof" || key == "\x03" || key == "q" && u.prompt == "" {
				return nil
			}
			if u.busy {
				if len(pending) < 32 {
					pending = append(pending, key)
				}
				continue
			}
			if u.prompt != "" {
				u.editPrompt(key, results)
				continue
			}
			switch key {
			case "1", "2", "3", "4":
				u.tab, u.selected = int(key[0]-'1'), 0
				u.refresh()
			case "\t":
				u.tab, u.selected = (u.tab+1)%4, 0
				u.refresh()
			case "up", "k":
				if u.selected > 0 {
					u.selected--
				}
			case "down", "j":
				if u.selected+1 < u.itemCount() {
					u.selected++
				}
			case "r":
				u.refresh()
			case "s":
				if u.client == nil {
					u.perform("Starting service...", results, func() error { return startDaemon(f, o, io.Discard) })
				} else {
					u.perform("Stopping service...", results, func() error { return stopDaemon(o.DataDir, io.Discard) })
				}
			case "p":
				u.request("/api/pause", map[string]bool{"paused": !u.state.Paused}, results)
			case "f", "t", "b", "c":
				category := map[string]string{"f": "files", "t": "terminal", "b": "browser", "c": "computer"}[key]
				u.request("/api/capability", map[string]any{"category": category, "enabled": !u.state.Enabled[category]}, results)
			case "h":
				if u.state.Tunnel.State == "stopped" || u.state.Tunnel.State == "error" || u.state.Tunnel.State == "" {
					u.request("/api/tunnel/start", map[string]string{"mode": "quick"}, results)
				} else {
					u.request("/api/tunnel/stop", map[string]any{}, results)
				}
			case "a":
				if u.tab == 1 && u.client != nil {
					u.prompt, u.input = "Project folder", ""
				}
			case "\r", "\n":
				if u.tab == 1 && u.selected < len(u.state.Projects.Projects) {
					u.request("/api/projects", map[string]string{"action": "activate", "id": u.state.Projects.Projects[u.selected].ID}, results)
				}
			case "d":
				if u.tab == 1 && u.selected < len(u.state.Projects.Projects) {
					u.prompt, u.input = "Remove access to selected project? Type yes", ""
				}
			}
		}
	}
}

func (u *terminalUI) perform(message string, results chan<- error, action func() error) {
	u.busy, u.message = true, message
	go func() { results <- action() }()
}

func (u *terminalUI) request(path string, input any, results chan<- error) {
	if u.client == nil {
		u.message = "Start the service with s first."
		return
	}
	client := u.client
	u.perform("Applying change...", results, func() error {
		_, err := client.request(http.MethodPost, path, input)
		return err
	})
}

func (u *terminalUI) editPrompt(key string, results chan<- error) {
	switch key {
	case "esc":
		u.prompt, u.input = "", ""
	case "\x7f", "\b":
		runes := []rune(u.input)
		if len(runes) > 0 {
			u.input = string(runes[:len(runes)-1])
		}
	case "\r", "\n":
		prompt, input := u.prompt, strings.TrimSpace(u.input)
		u.prompt, u.input = "", ""
		if prompt == "Project folder" && input != "" {
			home, err := os.UserHomeDir()
			if err != nil {
				u.message = err.Error()
				return
			}
			path, err := absolutePath(home, input)
			if err != nil {
				u.message = err.Error()
				return
			}
			u.request("/api/projects", map[string]string{"action": "add", "path": path}, results)
		} else if input == "yes" && u.selected < len(u.state.Projects.Projects) {
			u.request("/api/projects", map[string]string{"action": "remove", "id": u.state.Projects.Projects[u.selected].ID}, results)
		}
	default:
		if len([]rune(key)) == 1 && key >= " " && len(u.input) < 4096 {
			u.input += key
		}
	}
}

func (u *terminalUI) refresh() {
	u.client.close()
	client, err := controlClientWithTimeout(u.opts.DataDir, time.Second)
	if err != nil {
		u.client, u.state, u.info, u.calls = nil, tuiState{}, runtimeInfo{}, nil
		return
	}
	raw, err := client.request(http.MethodGet, "/api/state", nil)
	if err != nil {
		u.message = err.Error()
		return
	}
	if err := json.Unmarshal(raw, &u.state); err != nil {
		u.message = err.Error()
		return
	}
	u.client = client
	if raw, err := client.request(http.MethodGet, "/api/runtime", nil); err == nil {
		_ = json.Unmarshal(raw, &u.info)
	}
	if u.tab == 3 {
		if raw, err := client.request(http.MethodGet, "/api/calls?limit=50&view=summary", nil); err == nil {
			var activity struct {
				Calls []store.Call `json:"calls"`
			}
			if json.Unmarshal(raw, &activity) == nil {
				u.calls = activity.Calls
			}
		}
	}
	if u.selected >= u.itemCount() {
		u.selected = max(0, u.itemCount()-1)
	}
}

func (u *terminalUI) itemCount() int {
	switch u.tab {
	case 1:
		return len(u.state.Projects.Projects)
	case 2:
		return len(u.state.Tools)
	case 3:
		return len(u.calls)
	}
	return 0
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func (u *terminalUI) render(width, height int) string {
	width, height = max(1, width), max(1, height)
	status := "stopped"
	if u.client != nil {
		status = fmt.Sprintf("running  PID %d", u.info.PID)
		if u.state.Paused {
			status += "  PAUSED"
		}
	}
	lines := []string{tuiAccent + "  ReadyRig" + tuiReset + "  " + terminalText(buildinfo.Version) + "  |  " + status, ""}
	nav := "  "
	for i, name := range []string{"Overview", "Projects", "Tools", "Activity"} {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if i == u.tab {
			nav += tuiAccent + "\x1b[7m" + label + tuiReset
		} else {
			nav += tuiDim + label + tuiReset
		}
		nav += " "
	}
	lines = append(lines, nav, "")
	available := max(1, height-9)
	if u.client == nil {
		lines = append(lines, "  Service stopped. Press s to start in the background.", "", "  Configure startup settings with: readyrig setup", "  Logs: "+terminalText(filepath.Join(u.opts.DataDir, "daemon.log")))
	} else {
		switch u.tab {
		case 0:
			lines = append(lines, "  Workspace   "+terminalText(u.state.Workspace), "  Agent API   "+terminalText(u.state.Gateway), "  MCP         "+terminalText(u.state.Gateway+"/mcp"), "  Dashboard   "+terminalText(u.info.Dashboard), "", "  Tools       files "+onOff(u.state.Enabled["files"])+"   terminal "+onOff(u.state.Enabled["terminal"])+"   browser "+onOff(u.state.Enabled["browser"])+"   computer "+onOff(u.state.Enabled["computer"]), "  Chrome      "+terminalText(u.state.Chrome.State), "  Sharing     "+terminalText(u.state.Tunnel.State+" "+u.state.Tunnel.URL), "", "  f files   t terminal   b browser   c computer", "  h sharing   p pause/resume   s start/stop")
		case 1:
			lines = append(lines, "  a add folder   Enter use selected   d remove access", "")
			start := max(0, u.selected-available+3)
			for i := start; i < len(u.state.Projects.Projects) && i < start+max(1, available-2); i++ {
				p := u.state.Projects.Projects[i]
				active := ""
				if p.ID == u.state.Projects.Active {
					active = "  [active]"
				}
				lines = append(lines, u.row(i, terminalText(p.Name)+active+"  "+terminalText(p.Path)))
			}
		case 2:
			start := max(0, u.selected-available+1)
			for i := start; i < len(u.state.Tools) && i < start+available; i++ {
				spec := u.state.Tools[i]
				lines = append(lines, u.row(i, fmt.Sprintf("%-32s %-10s %s", terminalText(spec.Name), terminalText(spec.Category), onOff(u.state.Enabled[spec.Category]))))
			}
		case 3:
			if len(u.calls) == 0 {
				lines = append(lines, "  No tool calls yet.")
			}
			start := max(0, u.selected-available+1)
			for i := start; i < len(u.calls) && i < start+available; i++ {
				call := u.calls[i]
				lines = append(lines, u.row(i, terminalText(call.Tool+"  "+call.Status+"  "+call.Session)))
			}
		}
	}
	if len(lines) > height-4 {
		lines = lines[:max(0, height-4)]
	}
	for len(lines) < height-4 {
		lines = append(lines, "")
	}
	lines = append(lines, tuiDim+"  Tab / 1-4 views   arrows / j k select   r refresh   q quit"+tuiReset,
		"  "+terminalText(u.message), "  "+terminalText(u.prompt+" "+u.input))
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, line := range lines {
		if i >= height {
			break
		}
		b.WriteString(clipTerminalLine(line, max(0, width-1)))
		b.WriteString(tuiReset + "\x1b[K")
		if i+1 < len(lines) && i+1 < height {
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\x1b[J")
	return b.String()
}

func (u *terminalUI) row(i int, value string) string {
	if i == u.selected {
		return tuiAccent + " > " + value + tuiReset
	}
	return "   " + value
}

// Strip terminal control bytes from paths, tool names, and service errors.
func terminalText(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r >= 127 && r < 160 {
			return ' '
		}
		return r
	}, value)
}

func clipTerminalLine(value string, width int) string {
	runes := []rune(value)
	var b strings.Builder
	columns := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			for ; i < len(runes); i++ {
				b.WriteRune(runes[i])
				if runes[i] == 'm' {
					break
				}
			}
			continue
		}
		size := 1
		if runes[i] >= 0x1100 {
			size = 2
		}
		if columns+size > width {
			break
		}
		b.WriteRune(runes[i])
		columns += size
	}
	return b.String()
}

func readTerminalKeys(in io.Reader, keys chan<- string, done <-chan struct{}) {
	reader := bufio.NewReader(in)
	escape := 0
	send := func(key string) bool {
		select {
		case keys <- key:
			return true
		case <-done:
			return false
		}
	}
	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			send("eof")
			return
		}
		if escape == 1 && r == '[' {
			escape = 2
			continue
		}
		if escape == 2 {
			if r < '@' || r > '~' {
				continue
			}
			escape = 0
			switch r {
			case 'A':
				if !send("up") {
					return
				}
			case 'B':
				if !send("down") {
					return
				}
			}
			continue
		}
		escape = 0
		if r == '\x1b' {
			escape = 1
			if !send("esc") {
				return
			}
			continue
		}
		if !send(string(r)) {
			return
		}
	}
}

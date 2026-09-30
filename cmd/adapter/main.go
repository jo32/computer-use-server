package main

import (
	"computer-use-server/internal/app"
	"computer-use-server/internal/buildinfo"
	"computer-use-server/internal/chromemcp"
	"computer-use-server/internal/desktop"
	"computer-use-server/internal/server"
	"computer-use-server/internal/update"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() (runErr error) {
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("Relay", buildinfo.Version)
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	args := os.Args[1:]
	mode := "desktop"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("relay", flag.ContinueOnError)
	workspace := flags.String("workspace", filepath.Join(home, "agent_workspace"), "Workspace exposed to file tools")
	data := flags.String("data-dir", filepath.Join(home, ".local", "share", "relay"), "Private logs, screenshots and application data directory (outside workspace)")
	gateway := flags.String("gateway", "127.0.0.1:7332", "Agent API listener")
	ui := flags.String("ui", "127.0.0.1:7331", "Local browser dashboard listener")
	shell := flags.Bool("allow-shell", false, "Enable host shell execution (not sandboxed)")
	computer := flags.Bool("allow-computer", false, "Enable native computer use")
	allowIP := flags.String("allow-ip", "", "Comma-separated peer IP CIDRs for gateway")
	noChrome := flags.Bool("no-chrome", false, "Disable automatic Chrome DevTools MCP bridge")
	chromeURL := flags.String("chrome-browser-url", "", "Existing Chrome debugging HTTP URL on loopback (auto-detected by default)")
	chromeProfile := flags.String("chrome-user-data-dir", "", "Chrome user data directory containing DevToolsActivePort")
	chromeCommand := flags.String("chrome-mcp-command", "", "Installed chrome-devtools-mcp executable (defaults to installed command or npx)")
	repoDefault := buildinfo.ReleaseRepo
	if v := os.Getenv("RELAY_UPDATE_REPO"); v != "" {
		repoDefault = v
	}
	feedDefault := buildinfo.UpdateFeed
	if v := os.Getenv("RELAY_UPDATE_FEED"); v != "" {
		feedDefault = v
	}
	updateRepo := flags.String("update-repo", repoDefault, "GitHub repository used for release updates (owner/repo)")
	updateFeed := flags.String("update-feed", feedDefault, "Override release metadata URL (HTTPS or loopback)")
	noUpdate := flags.Bool("no-update", os.Getenv("RELAY_NO_UPDATE") == "1", "Disable release checks and automatic updates")
	if err = flags.Parse(args); err != nil {
		return err
	}
	if mode != "desktop" && mode != "web" && mode != "update" {
		return fmt.Errorf("usage: relay [desktop|web|update|version] [--workspace path]")
	}
	feed := *updateFeed
	if feed == "" && *updateRepo != "" {
		feed, err = update.GitHubFeed(*updateRepo)
		if err != nil {
			return err
		}
	}
	updates := update.New(update.Options{Version: buildinfo.Version, Feed: feed, GUI: desktop.Available, Disabled: *noUpdate})
	// Registered before App.Close and listener shutdown: install only after all
	// tools, data files and ports have been released.
	defer func() {
		if err := updates.Finish(runErr == nil); err != nil {
			runErr = err
		}
	}()
	if mode == "update" {
		updates.Check()
		for {
			s := updates.Status()
			if s.State != "checking" && s.State != "downloading" {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		s := updates.Status()
		if s.Error != "" {
			return fmt.Errorf("update: %s", s.Error)
		}
		if s.State == "ready" {
			if err := updates.Finish(true); err != nil {
				return err
			}
			fmt.Println("Updated Relay to", s.Latest)
			return nil
		}
		if s.State == "latest" {
			fmt.Println("Relay", s.Current, "is up to date")
			return nil
		}
		return fmt.Errorf("update: %s", s.Reason)
	}
	a, err := app.New(*workspace, *data)
	if err != nil {
		return err
	}
	closeApp := sync.OnceFunc(a.Close)
	defer closeApp()
	a.Server.Updates = updates
	if err = a.Chrome.Start(chromemcp.Options{Disabled: *noChrome, BrowserURL: *chromeURL, UserDataDir: *chromeProfile, Command: *chromeCommand}); err != nil {
		return err
	}
	if *shell {
		a.Server.Registry.Enable("terminal", true)
	}
	if *computer {
		a.Server.Registry.Enable("computer", true)
	}
	if *allowIP != "" {
		for _, v := range strings.Split(*allowIP, ",") {
			_, cidr, e := net.ParseCIDR(strings.TrimSpace(v))
			if e != nil {
				return e
			}
			a.Server.AllowedIPs = append(a.Server.AllowedIPs, cidr)
		}
	}
	gw, ln, err := server.Listen(*gateway, a.Server.Gateway())
	if err != nil {
		return err
	}
	closeGateway := sync.OnceFunc(func() { server.Shutdown(gw) })
	defer closeGateway()
	a.Server.GatewayAddr = "http://" + ln.Addr().String()
	fmt.Println("Relay Agent API:", a.Server.GatewayURL())
	fmt.Println("Workspace:", a.Server.Workspace)
	if mode == "desktop" {
		updates.Start()
		return desktop.Run(a.Server.UI(), a.Server.Registry, updates, func() {
			// AppKit terminates without returning from Run; ordinary Go defers do not
			// run. Use Wails' shutdown hook for both service cleanup and installation.
			closeGateway()
			closeApp()
			if err := updates.Finish(true); err != nil {
				log.Printf("update: %v", err)
			}
		})
	}
	host, _, err := net.SplitHostPort(*ui)
	if err != nil {
		return err
	}
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		return fmt.Errorf("dashboard must listen on loopback")
	}
	web, uiln, err := server.Listen(*ui, a.Server.BrowserGuard(a.Server.UI()))
	if err != nil {
		return err
	}
	defer server.Shutdown(web)
	fmt.Printf("Dashboard: http://%s/#key=%s\n", uiln.Addr(), a.Server.UIKey)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	updates.Start()
	select {
	case <-stop:
	case <-updates.RestartSignal():
	}
	return nil
}

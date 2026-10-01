package main

import (
	"computer-use-server/internal/app"
	"computer-use-server/internal/buildinfo"
	"computer-use-server/internal/chromemcp"
	"computer-use-server/internal/desktop"
	"computer-use-server/internal/server"
	"computer-use-server/internal/tunnel"
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
	if err := run(); err != nil && err != flag.ErrHelp {
		log.Fatal(err)
	}
}
func run() (runErr error) {
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("ReadyRig", buildinfo.Version)
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	args, dataDir, err := extractDataDir(os.Args[1:], home)
	if err != nil {
		return err
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("ReadyRig", buildinfo.Version)
		return nil
	}
	mode := "desktop"
	if !desktop.Available {
		mode = "web"
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode, args = args[0], args[1:]
	}
	if mode == "serve" {
		mode = "web"
	}
	flags, opts, err := startupFlags(home, dataDir, mode != "desktop" && mode != "help" && mode != "version")
	if err != nil {
		return err
	}
	if handled, err := manageCLI(mode, args, flags, opts); handled {
		return err
	}
	if err = flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if mode != "desktop" && mode != "web" && mode != "update" && mode != "init" {
		return fmt.Errorf("unknown command %q; see 'readyrig help'", mode)
	}
	if err := validateOptions(opts); err != nil {
		return err
	}
	if mode == "init" {
		if opts.FullAccess {
			return fmt.Errorf("Full Access is session-only; use 'serve --full-access'")
		}
		unlock, err := configurationLock(opts.DataDir)
		if err != nil {
			return err
		}
		defer unlock()
		if err := os.MkdirAll(opts.Workspace, 0755); err != nil {
			return err
		}
		if err := saveConfig(opts.DataDir, flags); err != nil {
			return err
		}
		fmt.Println("Saved CLI configuration:", filepath.Join(opts.DataDir, configFile))
		fmt.Println("Start ReadyRig with: readyrig serve --data-dir", opts.DataDir)
		return nil
	}
	feed := opts.UpdateFeed
	if feed == "" && opts.UpdateRepo != "" {
		feed, err = update.GitHubFeed(opts.UpdateRepo)
		if err != nil {
			return err
		}
	}
	updates := update.New(update.Options{Version: buildinfo.Version, Feed: feed, GUI: desktop.Available, Disabled: opts.NoUpdate})
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
			fmt.Println("Updated ReadyRig to", s.Latest)
			return nil
		}
		if s.State == "latest" {
			fmt.Println("ReadyRig", s.Current, "is up to date")
			return nil
		}
		return fmt.Errorf("update: %s", s.Reason)
	}
	a, err := app.New(opts.Workspace, opts.DataDir)
	if err != nil {
		return err
	}
	closeApp := sync.OnceFunc(a.Close)
	defer closeApp()
	a.Server.Updates = updates
	if opts.Cloudflared != "" {
		a.Server.Tunnel = tunnel.New(tunnel.Options{Dir: filepath.Join(a.Server.Store.Dir, "cloudflared"), Command: opts.Cloudflared, Changed: a.Server.Registry.Signal, RedactSecrets: a.Server.Registry.AddSecrets})
	}
	a.Server.Projects.SetFullAccess(opts.FullAccess)
	if err = a.Chrome.Start(chromemcp.Options{Disabled: opts.NoChrome, BrowserURL: opts.ChromeURL, UserDataDir: opts.ChromeProfile, Command: opts.ChromeCommand}); err != nil {
		return err
	}
	if opts.Shell {
		a.Server.Registry.Enable("terminal", true)
	}
	if opts.Computer {
		a.Server.Registry.Enable("computer", true)
	}
	if opts.AllowIP != "" {
		for _, v := range strings.Split(opts.AllowIP, ",") {
			_, cidr, e := net.ParseCIDR(strings.TrimSpace(v))
			if e != nil {
				return e
			}
			a.Server.AllowedIPs = append(a.Server.AllowedIPs, cidr)
		}
	}
	gw, ln, err := server.Listen(opts.Gateway, a.Server.Gateway())
	if err != nil {
		return err
	}
	closeGateway := sync.OnceFunc(func() { server.Shutdown(gw) })
	defer closeGateway()
	a.Server.GatewayAddr = "http://" + ln.Addr().String()
	if err = a.Server.StartCloud(opts.CloudURL); err != nil {
		return err
	}
	fmt.Println("ReadyRig Agent API:", a.Server.GatewayURL())
	fmt.Println("Workspace:", a.Server.Workspace)
	if opts.Share {
		if err = a.Server.StartSharing(); err != nil {
			return err
		}
		go func() {
			for {
				status := a.Server.Tunnel.Status()
				if status.State == "ready" {
					fmt.Println("Public Agent API:", status.URL+"/"+a.Server.AccessPath)
					fmt.Println("Public console:", status.URL+"/"+a.Server.AccessPath+"/app/")
					return
				}
				if status.State == "error" {
					log.Printf("Public sharing: %s", status.Error)
					return
				}
				if status.State == "stopped" {
					return
				}
				time.Sleep(200 * time.Millisecond)
			}
		}()
	}
	closeControl, err := startLocalControl(a.Server)
	if err != nil {
		return err
	}
	defer closeControl()
	if mode == "desktop" {
		updates.Start()
		return desktop.Run(a.Server.UI(), a.Server.Registry, updates, func() {
			// AppKit can terminate without running Go defers.
			closeControl()
			closeGateway()
			closeApp()
			if err := updates.Finish(true); err != nil {
				log.Printf("update: %v", err)
			}
		}, filepath.Join(opts.DataDir, "language.json"))
	}
	web, uiln, err := server.Listen(opts.UI, a.Server.BrowserGuard(a.Server.UI()))
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

// Reuse existing installations without moving their private logs or projects.
func defaultDataDir(home string) string {
	current := filepath.Join(home, ".local", "share", "readyrig")
	if _, err := os.Stat(current); os.IsNotExist(err) {
		// Preserve both earlier names without moving logs, projects or secrets.
		for _, name := range []string{"readrig", "relay"} {
			legacy := filepath.Join(home, ".local", "share", name)
			if info, err := os.Stat(legacy); err == nil && info.IsDir() {
				return legacy
			}
		}
	}
	return current
}

// Prefer the current name while keeping existing launch configurations usable.
func environment(name string) string {
	if value, ok := os.LookupEnv("READYRIG_" + name); ok {
		return value
	}
	return os.Getenv("RELAY_" + name)
}

package app

import (
	"computer-use-server/internal/chromemcp"
	"computer-use-server/internal/computer"
	"computer-use-server/internal/harness"
	"computer-use-server/internal/server"
	"computer-use-server/internal/store"
	"fmt"
	"os"
	"path/filepath"
)

type App struct {
	unlock    func()
	Server    *server.Server
	Processes *harness.Processes
	Files     *harness.Files
	Chrome    *chromemcp.Bridge
}

func New(workspace, dataDir string) (*App, error) {
	var err error
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(workspace, 0755); err != nil {
		return nil, err
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, err
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	dataDir, err = filepath.EvalSymlinks(dataDir)
	if err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(workspace, dataDir)
	if filepath.IsLocal(rel) {
		return nil, fmt.Errorf("data directory must be outside workspace to keep private application data outside file tools")
	}
	unlock, err := lockData(dataDir)
	if err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			unlock()
		}
	}()
	s, err := store.Open(dataDir)
	if err != nil {
		return nil, err
	}
	accessPath, err := server.NewAccessPath()
	if err != nil {
		s.Close()
		return nil, err
	}
	uiKey := harness.ID() + harness.ID()
	registry := harness.New(s, accessPath, uiKey)
	registry.RegisterHelp()
	files, err := harness.NewFiles(workspace)
	if err != nil {
		s.Close()
		return nil, err
	}
	processes := harness.NewProcesses(workspace)
	c := computer.New(filepath.Join(dataDir, "screenshots"))
	files.Register(registry)
	processes.Register(registry)
	c.Register(registry)
	registry.OnPause = processes.Stop
	chrome := chromemcp.New(registry)
	ready = true
	return &App{unlock: unlock, Server: &server.Server{Registry: registry, Store: s, Computer: c, Chrome: chrome, Workspace: workspace, AccessPath: accessPath, UIKey: uiKey}, Processes: processes, Files: files, Chrome: chrome}, nil
}
func (a *App) Close() {
	a.Server.Registry.SetPaused(true)
	a.Chrome.Close()
	a.Processes.Stop()
	a.Server.Registry.WaitBackground()
	a.Files.Close()
	a.Server.Store.Close()
	a.unlock()
}

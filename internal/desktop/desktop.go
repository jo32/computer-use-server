//go:build !nogui

package desktop

import (
	"computer-use-server/internal/harness"
	"computer-use-server/internal/update"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"net/http"
	"runtime"
	"sync"
	"time"
)

const Available = true

func Run(handler http.Handler, registry *harness.Registry, updates *update.Manager, shutdown func()) error {
	done := make(chan struct{})
	stop := sync.OnceFunc(func() { close(done) })
	defer stop()
	app := application.New(application.Options{Name: "Relay", Description: "Local Agent Adapter", Assets: application.AssetOptions{Handler: handler}, PostShutdown: func() { stop(); shutdown() }})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Relay · 本地 Agent 控制台", Width: 1320, Height: 860, MinWidth: 860, MinHeight: 600,
		URL: "/?shell=" + runtime.GOOS,
		Mac: application.MacWindow{
			// Keep the native traffic lights inside the page's single header row.
			// Dragging is controlled by CSS so navigation remains clickable.
			TitleBar: application.MacTitleBarHiddenInset,
		},
	})
	menu := app.NewMenu()
	menu.Add("打开 Relay").OnClick(func(*application.Context) { window.Show(); window.Focus() })
	menu.AddSeparator()
	menu.Add("紧急暂停所有控制").OnClick(func(*application.Context) { registry.SetPaused(true) })
	menu.Add("恢复控制").OnClick(func(*application.Context) { registry.SetPaused(false) })
	menu.AddSeparator()
	checkItem := menu.Add("检查更新").OnClick(func(*application.Context) { updates.Check(); window.Show(); window.Focus() })
	restartItem := menu.Add("重启并更新").SetEnabled(false).OnClick(func(*application.Context) { _ = updates.RequestRestart() })
	menu.AddSeparator()
	menu.Add("退出 Relay").OnClick(func(*application.Context) { app.Quit() })
	tray := app.SystemTray.New()
	tray.SetLabel("↗")
	tray.SetTooltip("Relay · 本地 Agent 控制台")
	tray.SetMenu(menu)
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { window.Hide(); e.Cancel() })
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-updates.RestartSignal():
				application.InvokeAsync(func() { app.Quit() })
				return
			case <-ticker.C:
				status := updates.Status()
				application.InvokeAsync(func() {
					checkItem.SetEnabled(status.CanCheck && status.State != "checking" && status.State != "downloading")
					restartItem.SetEnabled(status.CanRestart)
					label := "重启并更新"
					if status.CanRestart {
						label += " · " + status.Latest
					}
					restartItem.SetLabel(label)
				})
			}
		}
	}()
	return app.Run()
}

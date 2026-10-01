package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
)

var errSystemSettingsUnsupported = errors.New("此入口仅适用于 macOS")

// System permission shortcuts are available only in the local dashboard.
func (s *Server) permissionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/access/system-settings", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Permission string `json:"permission"`
		}
		if !decode(w, r, &in) {
			return
		}
		var pane string
		switch in.Permission {
		case "", "files": // Keep the existing Full Disk Access shortcut compatible.
			pane = "Privacy_AllFiles"
		case "screen":
			pane = "Privacy_ScreenCapture"
		case "accessibility":
			pane = "Privacy_Accessibility"
		default:
			problem(w, http.StatusBadRequest, fmt.Errorf("未知的系统权限类型"))
			return
		}
		opener := s.openSystemSettings
		if opener == nil {
			opener = openMacOSSettings
		}
		if err := opener(r.Context(), "x-apple.systempreferences:com.apple.preference.security?"+pane); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errSystemSettingsUnsupported) {
				status = http.StatusBadRequest
			}
			problem(w, status, err)
			return
		}
		write(w, map[string]bool{"ok": true})
	})
}

type systemSettingsOpener func(context.Context, string) error

func openMacOSSettings(ctx context.Context, url string) error {
	if runtime.GOOS != "darwin" {
		return errSystemSettingsUnsupported
	}
	return exec.CommandContext(ctx, "/usr/bin/open", url).Run()
}

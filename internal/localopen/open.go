// Package localopen hands local files and folders to the operating system.
// Callers are responsible for checking access before invoking Open.
package localopen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

var ErrUnsupported = errors.New("此系统不支持列出打开方式")
var ErrApplicationUnavailable = errors.New("应用不可用，请重新选择打开方式")

type Capabilities struct {
	Applications bool `json:"applications"`
}

type Application struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Icon    string `json:"icon,omitempty"`
	Default bool   `json:"default"`
}

// Open uses the default application, or one returned by Applications for this
// path. It never changes the default association.
func Open(ctx context.Context, path, application string) error {
	if err := validate(ctx, path); err != nil {
		return err
	}
	if application != "" {
		apps, err := Applications(ctx, path)
		if err != nil {
			return err
		}
		found := false
		for _, app := range apps {
			if app.ID == application {
				found = true
				break
			}
		}
		if !found {
			return ErrApplicationUnavailable
		}
	}
	return open(ctx, filepath.Clean(path), application)
}

// Applications asks the OS which installed apps accept this file or folder.
// IDs are opaque to callers, and are checked again when opening.
func Applications(ctx context.Context, path string) ([]Application, error) {
	if err := validate(ctx, path); err != nil {
		return nil, err
	}
	if !Supported().Applications {
		return nil, ErrUnsupported
	}
	apps, err := applications(ctx, filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].Default != apps[j].Default {
			return apps[i].Default
		}
		if apps[i].Name != apps[j].Name {
			return apps[i].Name < apps[j].Name
		}
		return apps[i].ID < apps[j].ID
	})
	unique := make([]Application, 0, len(apps))
	seen := map[string]bool{}
	for _, app := range apps {
		if app.ID != "" && app.Name != "" && !seen[app.ID] {
			seen[app.ID] = true
			unique = append(unique, app)
		}
	}
	return unique, nil
}

func validate(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		return errors.New("打开路径必须为绝对路径")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return errors.New("仅支持打开文件或文件夹")
	}
	return nil
}

package localopen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemApplicationsForFilesAndFolders(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "中文 notes ' $().txt")
	if err := os.WriteFile(file, []byte("notes"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, file} {
		apps, err := Applications(context.Background(), path)
		if err != nil || len(apps) == 0 {
			t.Fatal(path, apps, err)
		}
		seen, defaults := map[string]bool{}, 0
		for _, app := range apps {
			if seen[app.ID] || app.Name == "" || !filepath.IsAbs(app.ID) {
				t.Fatalf("invalid application: %#v", app)
			}
			seen[app.ID] = true
			if app.Default {
				defaults++
			}
		}
		if defaults != 1 || !apps[0].Default {
			t.Fatalf("missing default application: %#v", apps)
		}
		if err := Open(context.Background(), path, "/tmp/unregistered-app.app"); !errors.Is(err, ErrApplicationUnavailable) {
			t.Fatal("unregistered application accepted:", err)
		}
	}
}

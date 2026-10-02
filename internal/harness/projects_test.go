package harness

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestProjectsFileAccessAndPersistence(t *testing.T) {
	a, b, outside := t.TempDir(), t.TempDir(), t.TempDir()
	catalog := filepath.Join(t.TempDir(), "projects.json")
	p, err := NewProjects(a, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Change("add", "", "Second", b); err != nil {
		t.Fatal(err)
	}
	second := p.Snapshot().Projects[1]
	if err = p.Change("add", "", "duplicate", b); err == nil {
		t.Fatal("duplicate accepted")
	}
	f, err := NewFiles(a)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Projects = p
	r := registryForTest(t)
	f.Register(r)
	p.Register(r)
	if _, err = invoke(t, r, "write_file", map[string]any{"project": second.ID, "path": "nested/note", "content": "second"}); err != nil {
		t.Fatal(err)
	}
	if _, err = invoke(t, r, "read_file", map[string]any{"path": "nested/note"}); err == nil {
		t.Fatal("default unexpectedly changed")
	}
	if err = p.Change("activate", second.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"nested/note", filepath.Join(second.Path, "nested/note")} {
		o, e := invokeAs(t, r, "later-session", "read_file", map[string]any{"path": path})
		if e != nil || o.Value.(map[string]any)["content"] != "second" {
			t.Fatal(o, e)
		}
	}
	if _, err = invokeAs(t, r, "later-session", "read_file", map[string]any{"project": p.Snapshot().Projects[0].ID, "path": filepath.Join(second.Path, "nested/note")}); err == nil {
		t.Fatal("explicit project escaped")
	}
	secret := filepath.Join(outside, "private")
	if err = os.WriteFile(secret, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(b, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{secret, "../private", "escape/private"} {
		if _, err = invokeAs(t, r, "later-session", "read_file", map[string]any{"path": path}); err == nil {
			t.Fatal("restricted read escaped", path)
		}
	}
	p.SetFullAccess(true)
	for _, path := range []string{secret, "escape/private"} {
		o, e := invokeAs(t, r, "later-session", "read_file", map[string]any{"path": path})
		if e != nil || o.Value.(map[string]any)["content"] != "outside" {
			t.Fatal(o, e)
		}
	}
	if _, err = invokeAs(t, r, "later-session", "write_file", map[string]any{"path": secret, "content": "updated"}); err != nil {
		t.Fatal(err)
	}
	if _, err = invokeAs(t, r, "later-session", "list_directory", map[string]any{"path": outside}); err != nil {
		t.Fatal(err)
	}
	if o, e := invokeAs(t, r, "later-session", "search_files", map[string]any{"path": outside, "query": "updated"}); e != nil || len(o.Value.(map[string]any)["matches"].([]map[string]any)) != 1 {
		t.Fatal(o, e)
	}
	p.SetFullAccess(false)
	if _, err = invokeAs(t, r, "later-session", "read_file", map[string]any{"path": secret}); err == nil {
		t.Fatal("Full Access revocation failed")
	}
	p.SetFullAccess(true)
	if err = p.Change("rename", second.ID, "Renamed", ""); err != nil {
		t.Fatal(err)
	}
	restored, err := NewProjects(a, catalog)
	if err != nil {
		t.Fatal(err)
	}
	state := restored.Snapshot()
	if state.FullAccess || state.Active != second.ID || state.Projects[1].Name != "Renamed" {
		t.Fatal(state)
	}
	r.SetPaused(true)
	if _, err = invokeAs(t, r, "later-session", "list_projects", map[string]any{}); err != nil {
		t.Fatal("list_projects blocked while paused", err)
	}
	r.SetPaused(false)
	p.SetFullAccess(false)
	if err = p.Change("remove", second.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = invokeAs(t, r, "later-session", "read_file", map[string]any{"project": second.ID, "path": "nested/note"}); err == nil {
		t.Fatal("removed project accessible")
	}
	if _, err = invokeAs(t, r, "later-session", "read_file", map[string]any{"path": filepath.Join(second.Path, "nested/note")}); err == nil {
		t.Fatal("removed absolute path accessible")
	}
	if _, err = os.Stat(filepath.Join(b, "nested/note")); err != nil {
		t.Fatal("removal deleted file", err)
	}
	if err = p.Change("remove", p.Snapshot().Active, "", ""); err == nil {
		t.Fatal("removed last project")
	}
}
func TestProjectsTerminalDirectories(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	p, err := NewProjects(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Change("add", "", "Second", b); err != nil {
		t.Fatal(err)
	}
	second := p.Snapshot().Projects[1]
	procs := NewProcesses(a)
	procs.Projects = p
	defer procs.Stop()
	r := registryForTest(t)
	procs.Register(r)
	r.Enable("terminal", true)
	out, err := invoke(t, r, "exec_command", map[string]any{"project": second.ID, "command": "pwd"})
	if err != nil || strings.TrimSpace(out.Value.(map[string]any)["stdout"].(string)) != second.Path {
		t.Fatal(out, err)
	}
	outside := t.TempDir()
	if _, err = invoke(t, r, "exec_command", map[string]any{"cwd": outside, "command": "pwd"}); err == nil {
		t.Fatal("outside cwd accepted")
	}
	p.SetFullAccess(true)
	if _, err = invoke(t, r, "exec_command", map[string]any{"cwd": outside, "command": "pwd"}); err != nil {
		t.Fatal(err)
	}
	p.SetFullAccess(false)
	if _, err = invoke(t, r, "exec_command", map[string]any{"cwd": outside, "command": "pwd"}); err == nil {
		t.Fatal("cwd restriction not restored")
	}
}
func TestProjectsConcurrentSelectionAndAccess(t *testing.T) {
	p, err := NewProjects(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 30; n++ {
				p.SetFullAccess(n%2 == 0)
				_ = p.Snapshot()
				_, _, close, e := p.Resolve("", "", ".")
				if e != nil {
					t.Error(e)
				} else {
					close()
				}
			}
		}()
	}
	wg.Wait()
}
func TestProjectsReplacedRootRejected(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "project")
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := NewProjects(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(base, base+"-original"); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(t.TempDir(), base); err != nil {
		t.Fatal(err)
	}
	if _, _, close, err := p.Resolve("", "", "."); err == nil {
		close()
		t.Fatal("replaced project root accepted")
	}
}

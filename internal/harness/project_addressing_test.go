package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func invokeAs(t *testing.T, r *Registry, session, name string, args any) (Output, error) {
	t.Helper()
	b, _ := json.Marshal(args)
	out, _, err := r.Invoke(context.Background(), name, Invocation{Session: session, Arguments: b})
	return out, err
}

// twoProjects approves two folders that each hold x.txt.
func twoProjects(t *testing.T) (*Registry, *Projects, Project, Project) {
	t.Helper()
	a, b := t.TempDir(), t.TempDir()
	for dir, text := range map[string]string{a: "from A", b: "from B"} {
		if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := NewProjects(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Change("add", "", "", b); err != nil {
		t.Fatal(err)
	}
	f, err := NewFiles(a)
	if err != nil {
		t.Fatal(err)
	}
	f.Projects = p
	t.Cleanup(func() { f.Close() })
	procs := NewProcesses(a)
	procs.Projects = p
	t.Cleanup(procs.Stop)
	r := registryForTest(t)
	f.Register(r)
	p.Register(r)
	procs.Register(r)
	r.Enable("terminal", true)
	snap := p.Snapshot()
	return r, p, snap.Projects[0], snap.Projects[1]
}
func readX(t *testing.T, r *Registry, session string, extra map[string]any) (string, error) {
	t.Helper()
	args := map[string]any{"path": "x.txt"}
	for k, v := range extra {
		args[k] = v
	}
	out, err := invokeAs(t, r, session, "read_file", args)
	if err != nil {
		return "", err
	}
	return asMap(t, out)["content"].(string), nil
}

func TestRelativePathsStayWithTheSessionsProject(t *testing.T) {
	r, p, first, second := twoProjects(t)
	if got, err := readX(t, r, "s1", nil); err != nil || got != "from A" {
		t.Fatalf("first read: %q %v", got, err)
	}
	if err := p.Change("activate", second.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := readX(t, r, "s1", nil); got != "from A" {
		t.Fatalf("switching the default moved a running session: %q", got)
	}
	if got, _ := readX(t, r, "s2", nil); got != "from B" {
		t.Fatalf("a new session ignored the new default: %q", got)
	}
	for session, want := range map[string]string{"s1": first.ID, "s2": second.ID, "fresh": second.ID} {
		out, err := invokeAs(t, r, session, "list_projects", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		list := out.Value.(projectList)
		if list.SessionDefault == nil || list.SessionDefault.ID != want || list.Active != second.ID || list.Pinned != nil {
			t.Fatalf("%s: %+v", session, list)
		}
	}
	pinned := p.Snapshot().Pinned
	if pinned[first.ID] != 1 || pinned[second.ID] != 1 || len(pinned) != 2 {
		t.Fatalf("pinned counts: %v (list_projects must not pin)", pinned)
	}
}
func TestSessionsWithoutAnIDFollowTheDefault(t *testing.T) {
	r, p, _, second := twoProjects(t)
	if got, _ := readX(t, r, "", nil); got != "from A" {
		t.Fatal(got)
	}
	if err := p.Change("activate", second.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := readX(t, r, "", nil); got != "from B" {
		t.Fatalf("the shared default session was pinned: %q", got)
	}
	if n := len(p.Snapshot().Pinned); n != 0 {
		t.Fatalf("%d pins for sessions without an ID", n)
	}
}
func TestProjectsAreAddressableByName(t *testing.T) {
	r, p, first, second := twoProjects(t)
	p.Change("rename", first.ID, "Alpha", "")
	p.Change("rename", second.ID, "Beta Site", "")
	if got, err := readX(t, r, "s", map[string]any{"project": "beta site"}); err != nil || got != "from B" {
		t.Fatalf("by name: %q %v", got, err)
	}
	if got, err := readX(t, r, "s", map[string]any{"project": second.ID}); err != nil || got != "from B" {
		t.Fatalf("by id: %q %v", got, err)
	}
	_, err := readX(t, r, "s", map[string]any{"project": "gamma"})
	if err == nil || !strings.Contains(err.Error(), "Alpha, Beta Site") {
		t.Fatalf("unknown project should list the known ones: %v", err)
	}
	if n := len(p.Snapshot().Pinned); n != 0 {
		t.Fatal("an explicit project pinned the session")
	}
	if err = p.Change("add", "", "ALPHA", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err = readX(t, r, "s", map[string]any{"project": "alpha"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("duplicate names: %v", err)
	}
	if got, err := readX(t, r, "s", map[string]any{"project": first.ID}); err != nil || got != "from A" {
		t.Fatalf("the id still disambiguates: %q %v", got, err)
	}
}
func TestResultsNameTheirProject(t *testing.T) {
	r, p, first, second := twoProjects(t)
	p.Change("rename", first.ID, "Alpha", "")
	p.Change("rename", second.ID, "Beta", "")
	name := func(o Output) string {
		m, ok := asMap(t, o)["project"].(map[string]any)
		if !ok {
			t.Fatalf("no project in %v", o.Value)
		}
		return m["name"].(string)
	}
	for tool, args := range map[string]map[string]any{
		"read_file":      {"path": "x.txt"},
		"list_directory": {},
		"glob":           {"pattern": "*"},
		"search_files":   {"query": "from"},
		"write_file":     {"path": "y.txt", "content": "y"},
		"edit_file":      {"path": "x.txt", "old_string": "from", "new_string": "FROM"},
	} {
		out, err := invokeAs(t, r, "s", tool, args)
		if err != nil || name(out) != "Alpha" {
			t.Errorf("%s: %v %v", tool, out.Value, err)
		}
	}
	out, err := invokeAs(t, r, "s", "exec_command", map[string]any{"command": "pwd", "project": "Beta"})
	if err != nil || name(out) != "Beta" {
		t.Fatalf("exec: %v %v", out.Value, err)
	}
	out, err = invokeAs(t, r, "s", "read_file", map[string]any{"path": filepath.Join(second.Path, "x.txt")})
	if err != nil || name(out) != "Beta" {
		t.Fatalf("absolute path: %v %v", out.Value, err)
	}
}
func TestRemovingAProjectReleasesItsPins(t *testing.T) {
	r, p, first, second := twoProjects(t)
	if err := p.Change("activate", second.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := readX(t, r, "s1", nil); got != "from B" {
		t.Fatal(got)
	}
	if err := p.Change("remove", second.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if n := p.Snapshot().Pinned[second.ID]; n != 0 {
		t.Fatalf("%d sessions still pinned to a removed project", n)
	}
	if got, err := readX(t, r, "s1", nil); err != nil || got != "from A" {
		t.Fatalf("session did not fall back to the remaining project: %q %v", got, err)
	}
	if p.Snapshot().Pinned[first.ID] != 1 {
		t.Fatal("fallback was not pinned")
	}
}
func TestAbsolutePathsNeedNoProjectAndDoNotPin(t *testing.T) {
	r, p, _, second := twoProjects(t)
	if got, err := readX(t, r, "abs", nil); err != nil || got != "from A" {
		t.Fatal(got, err)
	}
	p.dropPins(p.Snapshot().Active)
	out, err := invokeAs(t, r, "abs", "read_file", map[string]any{"path": filepath.Join(second.Path, "x.txt")})
	if err != nil || asMap(t, out)["content"] != "from B" {
		t.Fatalf("%v %v", out.Value, err)
	}
	if n := len(p.Snapshot().Pinned); n != 0 {
		t.Fatalf("an absolute path pinned the session: %v", p.Snapshot().Pinned)
	}
}
func TestPinsAreBounded(t *testing.T) {
	r, p, _, _ := twoProjects(t)
	for i := 0; i < maxPins+80; i++ {
		if _, err := readX(t, r, fmt.Sprint("session-", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	p.pinMu.Lock()
	n := len(p.pins)
	p.pinMu.Unlock()
	if n > maxPins {
		t.Fatalf("%d pins", n)
	}
}

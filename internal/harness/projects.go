package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}
type ProjectState struct {
	Projects   []Project `json:"projects"`
	Active     string    `json:"active"`
	FullAccess bool      `json:"full_access"`
	// Pinned counts the agent sessions that currently resolve relative paths to each project.
	Pinned map[string]int `json:"pinned,omitempty"`
}

// Projects owns local grants. Full Access is deliberately session-only.
// File operations hold a read lease so removing a grant waits for current I/O.
type Projects struct {
	mu    sync.RWMutex
	state ProjectState
	file  string
	// OnChange runs, without any lock held, after the project list or Full Access changes.
	OnChange func()
	// pins remembers which project each agent session's relative paths mean,
	// so changing the default in the dashboard never moves a running session.
	pinMu sync.Mutex
	pins  map[string]string
}

func NewProjects(workspace, file string) (*Projects, error) {
	p := &Projects{file: file, pins: map[string]string{}}
	if file != "" {
		b, err := os.ReadFile(file)
		if err == nil {
			if err = json.Unmarshal(b, &p.state); err != nil {
				return nil, fmt.Errorf("projects: %w", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	p.state.FullAccess = false
	if len(p.state.Projects) == 0 {
		path, err := ProjectDirectory(workspace)
		if err != nil {
			return nil, err
		}
		p.state.Projects = []Project{{ID: ID(), Name: filepath.Base(path), Path: path}}
		p.state.Active = p.state.Projects[0].ID
	}
	found := false
	ids := map[string]bool{}
	for _, v := range p.state.Projects {
		if v.ID == "" || ids[v.ID] || !filepath.IsAbs(v.Path) {
			return nil, errors.New("invalid projects configuration")
		}
		ids[v.ID] = true
		if v.ID == p.state.Active {
			found = true
		}
	}
	if !found {
		p.state.Active = p.state.Projects[0].ID
	}
	if err := p.save(); err != nil {
		return nil, err
	}
	return p, nil
}
func ProjectDirectory(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("请输入绝对目录路径（支持 ~/）")
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("请选择目录")
	}
	return filepath.Clean(path), nil
}
func (p *Projects) Snapshot() ProjectState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.state
	s.Projects = append([]Project{}, s.Projects...)
	p.pinMu.Lock()
	defer p.pinMu.Unlock()
	for _, id := range p.pins {
		if s.Pinned == nil {
			s.Pinned = map[string]int{}
		}
		s.Pinned[id]++
	}
	return s
}
func (p *Projects) SetFullAccess(enabled bool) {
	p.mu.Lock()
	p.state.FullAccess = enabled
	p.mu.Unlock()
	if p.OnChange != nil {
		p.OnChange()
	}
}
func (p *Projects) Change(action, id, name, path string) error {
	err := p.change(action, id, name, path)
	if err == nil && p.OnChange != nil {
		p.OnChange()
	}
	return err
}
func (p *Projects) change(action, id, name, path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.state
	p.state.Projects = append([]Project{}, old.Projects...)
	switch action {
	case "add":
		canonical, err := ProjectDirectory(path)
		if err != nil {
			return err
		}
		for _, v := range p.state.Projects {
			if v.Path == canonical {
				return errors.New("这个目录已添加")
			}
		}
		name = strings.TrimSpace(name)
		if name == "" {
			name = filepath.Base(canonical)
		}
		if len(name) > 128 {
			return errors.New("项目名称过长")
		}
		p.state.Projects = append(p.state.Projects, Project{ID: ID(), Name: name, Path: canonical})
	case "activate", "remove", "rename":
		index := -1
		for i, v := range p.state.Projects {
			if v.ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("project not found")
		}
		switch action {
		case "activate":
			p.state.Active = id
		case "rename":
			name = strings.TrimSpace(name)
			if name == "" || len(name) > 128 {
				return errors.New("项目名称需为 1–128 字节")
			}
			p.state.Projects[index].Name = name
		case "remove":
			if len(p.state.Projects) == 1 {
				return errors.New("请至少保留一个项目")
			}
			p.state.Projects = append(p.state.Projects[:index], p.state.Projects[index+1:]...)
			p.dropPins(id)
			if p.state.Active == id {
				p.state.Active = p.state.Projects[0].ID
			}
		}
	default:
		return errors.New("unknown project action")
	}
	if err := p.save(); err != nil {
		p.state = old
		return err
	}
	return nil
}
func (p *Projects) save() error {
	if p.file == "" {
		return nil
	}
	// Persist only the catalog and selection, never an elevated access mode.
	b, err := json.MarshalIndent(struct {
		Projects []Project `json:"projects"`
		Active   string    `json:"active"`
	}{p.state.Projects, p.state.Active}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p.file), ".projects-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), p.file)
}

// Resolve returns an open root and a relative path. close releases both root and lease.
// Resolve maps a path to an open project root. project is an ID or a project
// name (empty: absolute paths find their containing project, relative paths use
// the session's project). session may be empty.
func (p *Projects) Resolve(session, project, path string) (root *os.Root, relative string, close func(), err error) {
	p.mu.RLock()
	close = p.mu.RUnlock
	fail := func(e error) (*os.Root, string, func(), error) { p.mu.RUnlock(); return nil, "", nil, e }
	if path == "" {
		return fail(errors.New("path is required"))
	}
	selected, pickErr := p.pick(session, project, !filepath.IsAbs(path))
	if pickErr != nil {
		return fail(pickErr)
	}
	base := selected.Path
	if p.state.FullAccess {
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		path, err = unrestrictedPath(path)
		if err != nil {
			return fail(err)
		}
		base = filepath.VolumeName(path) + string(filepath.Separator)
		relative, err = filepath.Rel(base, path)
	} else if filepath.IsAbs(path) {
		// An explicit project confines even absolute paths to that project.
		if project == "" {
			bestLength := -1
			for _, v := range p.state.Projects {
				rel, e := filepath.Rel(v.Path, path)
				if e == nil && filepath.IsLocal(rel) {
					if len(v.Path) > bestLength {
						bestLength = len(v.Path)
						base = v.Path
					}
				}
			}
		}
		relative, err = filepath.Rel(base, path)
	} else {
		relative = path
	}
	if err != nil {
		return fail(err)
	}
	if err = pathOK(relative); err != nil {
		return fail(err)
	}
	if !p.state.FullAccess {
		actual, e := filepath.EvalSymlinks(base)
		if e != nil {
			return fail(e)
		}
		if actual != base {
			return fail(errors.New("project directory changed; remove and add it again"))
		}
	}
	root, err = os.OpenRoot(base)
	if err != nil {
		return fail(err)
	}
	close = func() { root.Close(); p.mu.RUnlock() }
	return root, relative, close, nil
}

// Resolve existing symlinks before using the volume root. os.Root rejects
// absolute symlinks even when the root is /; Full Access intentionally permits
// these links. Missing suffixes are retained for atomic file creation.
func unrestrictedPath(path string) (string, error) {
	path = filepath.Clean(path)
	prefix := path
	suffix := []string{}
	for {
		_, err := os.Lstat(prefix)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			return "", err
		}
		suffix = append(suffix, filepath.Base(prefix))
		prefix = parent
	}
	resolved, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	return resolved, nil
}

func (p *Projects) Directory(session, project, path string) (string, error) {
	root, rel, close, err := p.Resolve(session, project, path)
	if err != nil {
		return "", err
	}
	defer close()
	// OpenRoot validates the directory with the same symlink boundary as file tools.
	dir, err := root.OpenRoot(rel)
	if err != nil {
		return "", err
	}
	dir.Close()
	return filepath.EvalSymlinks(filepath.Join(root.Name(), rel))
}

const maxPins = 1024

// pick chooses the project for a call. An explicit reference (ID, or a name
// that matches one project) always wins. Otherwise the session's pinned project
// is used; the first relative path pins the active project for that session.
// The shared "default" session, used by clients that send no session ID, is
// never pinned: it follows the dashboard's default.
func (p *Projects) pick(session, ref string, pin bool) (Project, error) {
	if ref != "" {
		var byName []Project
		for _, v := range p.state.Projects {
			if v.ID == ref {
				return v, nil
			}
			if strings.EqualFold(v.Name, ref) {
				byName = append(byName, v)
			}
		}
		switch len(byName) {
		case 1:
			return byName[0], nil
		case 0:
			return Project{}, &ToolError{"project_not_found", fmt.Sprintf("project %q not found; known projects: %s", ref, p.names())}
		}
		return Project{}, &ToolError{"ambiguous_project", fmt.Sprintf("project name %q is ambiguous; use its ID from list_projects", ref)}
	}
	pinnable := session != "" && session != "default"
	p.pinMu.Lock()
	defer p.pinMu.Unlock()
	if pinnable {
		if id, ok := p.pins[session]; ok {
			for _, v := range p.state.Projects {
				if v.ID == id {
					return v, nil
				}
			}
			delete(p.pins, session)
		}
	}
	for _, v := range p.state.Projects {
		if v.ID == p.state.Active {
			if pin && pinnable {
				if len(p.pins) >= maxPins {
					for k := range p.pins {
						delete(p.pins, k)
						break
					}
				}
				p.pins[session] = v.ID
			}
			return v, nil
		}
	}
	return Project{}, errors.New("no active project; use list_projects")
}
func (p *Projects) names() string {
	names := make([]string, 0, len(p.state.Projects))
	for _, v := range p.state.Projects {
		names = append(names, v.Name)
	}
	return strings.Join(names, ", ")
}
func (p *Projects) dropPins(id string) {
	p.pinMu.Lock()
	defer p.pinMu.Unlock()
	for session, pinned := range p.pins {
		if pinned == id {
			delete(p.pins, session)
		}
	}
}

// SessionDefault is the project a relative path means to this session right now.
func (p *Projects) SessionDefault(session string) (Project, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	p.pinMu.Lock()
	defer p.pinMu.Unlock()
	id := p.state.Active
	if pinned, ok := p.pins[session]; ok && session != "default" {
		id = pinned
	}
	for _, v := range p.state.Projects {
		if v.ID == id {
			return v, true
		}
	}
	return Project{}, false
}

// Count is the number of approved projects.
func (p *Projects) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.state.Projects)
}

// ProjectFor finds the approved project that contains an absolute path.
func (p *Projects) ProjectFor(abs string) (Project, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	best, found := -1, Project{}
	for _, v := range p.state.Projects {
		if rel, err := filepath.Rel(v.Path, abs); err == nil && filepath.IsLocal(rel) && len(v.Path) > best {
			best, found = len(v.Path), v
		}
	}
	return found, best >= 0
}

// tagProject names the project a result belongs to, found from the absolute
// path in key, so the agent can see what its relative path resolved to.
func tagProject(p *Projects, out Output, key string) {
	// With a single project there is nothing to disambiguate.
	if p == nil || p.Count() < 2 {
		return
	}
	if m, ok := out.Value.(map[string]any); ok {
		if path, ok := m[key].(string); ok {
			if pr, found := p.ProjectFor(path); found {
				m["project"] = map[string]any{"id": pr.ID, "name": pr.Name}
			}
		}
	}
}

type projectList struct {
	ProjectState
	// SessionDefault is what relative paths mean to the calling session.
	SessionDefault *Project `json:"session_default,omitempty"`
}

func (p *Projects) Register(r *Registry) {
	r.Register(Tool{Spec: Spec{Name: "list_projects", Category: "system", Description: "List approved projects (id, name, path), the active default, session_default (what your relative paths mean; fixed when the session first uses one) and full_access. Absolute paths need no project argument.", Parallel: true, InputSchema: Schema(map[string]any{})}, AvailableWhenPaused: true, Run: func(ctx context.Context, in Invocation) (Output, error) {
		var a struct{}
		if err := Decode(in.Arguments, &a); err != nil {
			return Output{}, err
		}
		list := projectList{ProjectState: p.Snapshot()}
		list.Pinned = nil
		if d, ok := p.SessionDefault(in.Session); ok {
			list.SessionDefault = &d
		}
		return Output{Value: list}, ctx.Err()
	}})
}

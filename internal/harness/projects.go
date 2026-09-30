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
}

// Projects owns local grants. Full Access is deliberately session-only.
// File operations hold a read lease so removing a grant waits for current I/O.
type Projects struct {
	mu    sync.RWMutex
	state ProjectState
	file  string
}

func NewProjects(workspace, file string) (*Projects, error) {
	p := &Projects{file: file}
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
	return s
}
func (p *Projects) SetFullAccess(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.FullAccess = enabled
}
func (p *Projects) Change(action, id, name, path string) error {
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
func (p *Projects) Resolve(project, path string) (root *os.Root, relative string, close func(), err error) {
	p.mu.RLock()
	close = p.mu.RUnlock
	fail := func(e error) (*os.Root, string, func(), error) { p.mu.RUnlock(); return nil, "", nil, e }
	if path == "" {
		return fail(errors.New("path is required"))
	}
	selected := Project{}
	selectedID := project
	if selectedID == "" {
		selectedID = p.state.Active
	}
	for _, v := range p.state.Projects {
		if v.ID == selectedID {
			selected = v
			break
		}
	}
	if selected.ID == "" {
		return fail(errors.New("project not found; use list_projects"))
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

func (p *Projects) Directory(project, path string) (string, error) {
	root, rel, close, err := p.Resolve(project, path)
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
func (p *Projects) Register(r *Registry) {
	r.Register(Tool{Spec: Spec{Name: "list_projects", Category: "system", Description: "List locally approved projects, their IDs and absolute paths, the active default project and Full Access status. Pass project=<id> to file tools or exec_command. Relative paths default to the active project; absolute paths must belong to an approved project unless Full Access is enabled locally. This tool cannot grant access or change projects.", Parallel: true, InputSchema: Schema(map[string]any{})}, AvailableWhenPaused: true, Run: func(ctx context.Context, in Invocation) (Output, error) {
		var a struct{}
		if err := Decode(in.Arguments, &a); err != nil {
			return Output{}, err
		}
		return Output{Value: p.Snapshot()}, ctx.Err()
	}})
}

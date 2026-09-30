package harness

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const MaxFileBytes = 1024 * 1024

// os.Root resolves every operation beneath an open directory handle, including symlinks.
type Files struct {
	root     *os.Root
	Projects *Projects
}

func NewFiles(path string) (*Files, error) {
	root, err := os.OpenRoot(path)
	return &Files{root: root}, err
}
func (f *Files) Close() error { return f.root.Close() }
func (f *Files) resolve(project, path string) (*os.Root, string, func(), error) {
	if f.Projects != nil {
		return f.Projects.Resolve(project, path)
	}
	if project != "" {
		return nil, "", nil, errors.New("projects unavailable")
	}
	if err := pathOK(path); err != nil {
		return nil, "", nil, err
	}
	return f.root, path, func() {}, nil
}
func (f *Files) Register(r *Registry) {
	r.Register(Tool{Spec: Spec{Name: "read_file", Category: "files", Description: "Read a project-relative file, optionally by 1-based inclusive line range. Set encoding=base64 for binary data. At most 1 MiB is returned.", Parallel: true, InputSchema: Schema(map[string]any{"project": Prop("string", "Project ID from list_projects; defaults to active project"), "path": Prop("string", "Project-relative or permitted absolute path"), "start_line": Prop("integer", "First line, default 1"), "end_line": Prop("integer", "Last line, inclusive"), "encoding": Prop("string", "utf8 or base64")}, "path")}, Run: f.read})
	r.Register(Tool{Spec: Spec{Name: "write_file", Category: "files", Description: "Atomically write a file in an approved project, or any current-user accessible directory with Full Access. Creates parent directories.", Mutating: true, InputSchema: Schema(map[string]any{"project": Prop("string", "Project ID from list_projects; defaults to active project"), "path": Prop("string", "Project-relative or permitted absolute path"), "content": Prop("string", "Text or base64 content"), "encoding": Prop("string", "utf8 or base64")}, "path", "content")}, Run: f.write})
	r.Register(Tool{Spec: Spec{Name: "list_directory", Category: "files", Description: "List up to 1000 immediate children of a permitted directory, including size and modification time.", Parallel: true, InputSchema: Schema(map[string]any{"project": Prop("string", "Project ID from list_projects; defaults to active project"), "path": Prop("string", "Project-relative or permitted absolute directory, default .")})}, Run: f.list})
	r.Register(Tool{Spec: Spec{Name: "search_files", Category: "files", Description: "Search text literally in permitted files. Skips symlinks, .git, node_modules and binary files. Returns at most 200 matching lines.", Parallel: true, InputSchema: Schema(map[string]any{"project": Prop("string", "Project ID from list_projects; defaults to active project"), "path": Prop("string", "Project-relative or permitted absolute directory, default ."), "query": Prop("string", "Literal text to find")}, "query")}, Run: f.search})
}
func pathOK(p string) error {
	if p == "" || filepath.IsAbs(p) || !filepath.IsLocal(p) {
		return errors.New("path must stay inside the workspace")
	}
	return nil
}
func (f *Files) read(ctx context.Context, in Invocation) (Output, error) {
	var a struct {
		Path     string `json:"path"`
		Project  string `json:"project"`
		Start    int    `json:"start_line"`
		End      int    `json:"end_line"`
		Encoding string `json:"encoding"`
	}
	if err := Decode(in.Arguments, &a); err != nil {
		return Output{}, err
	}
	root, path, release, err := f.resolve(a.Project, a.Path)
	if err != nil {
		return Output{}, err
	}
	defer release()
	a.Path = path
	if a.Start < 0 || a.End < 0 || a.End > 0 && a.End < a.Start {
		return Output{}, errors.New("invalid line range")
	}
	if a.Encoding != "" && a.Encoding != "utf8" && a.Encoding != "base64" {
		return Output{}, errors.New("encoding must be utf8 or base64")
	}
	file, err := openRead(root, a.Path)
	if err != nil {
		return Output{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Output{}, err
	}
	if !info.Mode().IsRegular() {
		return Output{}, errors.New("only regular files can be read")
	}
	b, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return Output{}, err
	}
	truncated := len(b) > MaxFileBytes
	if truncated {
		b = b[:MaxFileBytes]
	}
	content := ""
	if a.Encoding == "base64" {
		content = base64.StdEncoding.EncodeToString(b)
	} else {
		if truncated {
			for len(b) > 0 && !utf8.Valid(b) {
				b = b[:len(b)-1]
				if len(b) < MaxFileBytes-4 {
					break
				}
			}
		}
		if !utf8.Valid(b) {
			return Output{}, errors.New("binary file: use encoding=base64")
		}
		lines := strings.Split(string(b), "\n")
		start := max(a.Start, 1) - 1
		end := len(lines)
		if a.End > 0 {
			end = min(end, a.End)
		}
		if start > end {
			content = ""
		} else {
			content = strings.Join(lines[start:end], "\n")
		}
	}
	return Output{Value: map[string]any{"resolved_path": filepath.Join(root.Name(), a.Path), "content": content, "size": info.Size(), "truncated": truncated, "encoding": a.Encoding}}, ctx.Err()
}
func (f *Files) write(ctx context.Context, in Invocation) (Output, error) {
	var a struct{ Path, Project, Content, Encoding string }
	if err := json.Unmarshal(in.Arguments, &a); err != nil {
		return Output{}, err
	}
	root, path, release, err := f.resolve(a.Project, a.Path)
	if err != nil {
		return Output{}, err
	}
	defer release()
	a.Path = path
	if a.Encoding != "" && a.Encoding != "utf8" && a.Encoding != "base64" {
		return Output{}, errors.New("invalid encoding")
	}
	b := []byte(a.Content)
	if a.Encoding == "base64" {
		var err error
		b, err = base64.StdEncoding.DecodeString(a.Content)
		if err != nil {
			return Output{}, err
		}
	}
	if len(b) > MaxFileBytes {
		return Output{}, errors.New("file exceeds 1 MiB")
	}
	dir := filepath.Dir(a.Path)
	if err := root.MkdirAll(dir, 0755); err != nil {
		return Output{}, err
	}
	temp := filepath.Join(dir, ".adapter-"+ID())
	out, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Output{}, err
	}
	defer root.Remove(temp)
	_, err = out.Write(b)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return Output{}, err
	}
	if closeErr != nil {
		return Output{}, closeErr
	}
	if err = ctx.Err(); err != nil {
		return Output{}, err
	}
	if err = root.Rename(temp, a.Path); err != nil {
		return Output{}, err
	}
	return Output{Value: map[string]any{"path": a.Path, "resolved_path": filepath.Join(root.Name(), a.Path), "bytes_written": len(b)}}, nil
}
func (f *Files) list(ctx context.Context, in Invocation) (Output, error) {
	var a struct{ Path, Project string }
	if err := json.Unmarshal(in.Arguments, &a); err != nil {
		return Output{}, err
	}
	if a.Path == "" {
		a.Path = "."
	}
	root, path, release, err := f.resolve(a.Project, a.Path)
	if err != nil {
		return Output{}, err
	}
	defer release()
	a.Path = path
	file, err := root.Open(a.Path)
	if err != nil {
		return Output{}, err
	}
	defer file.Close()
	entries, err := file.ReadDir(1001)
	if err != nil && err != io.EOF {
		return Output{}, err
	}
	cut := len(entries) > 1000
	if cut {
		entries = entries[:1000]
	}
	out := []map[string]any{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, map[string]any{"name": entry.Name(), "directory": entry.IsDir(), "size": info.Size(), "modified": info.ModTime(), "symlink": entry.Type()&os.ModeSymlink != 0})
	}
	return Output{Value: map[string]any{"resolved_path": filepath.Join(root.Name(), a.Path), "entries": out, "truncated": cut}}, ctx.Err()
}
func (f *Files) search(ctx context.Context, in Invocation) (Output, error) {
	var a struct{ Path, Project, Query string }
	if err := json.Unmarshal(in.Arguments, &a); err != nil {
		return Output{}, err
	}
	if a.Path == "" {
		a.Path = "."
	}
	root, path, release, err := f.resolve(a.Project, a.Path)
	if err != nil {
		return Output{}, err
	}
	defer release()
	a.Path = path
	if a.Query == "" {
		return Output{}, errors.New("query is required")
	}
	out := []map[string]any{}
	scanned := 0
	cut := false
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 20 || scanned >= 5000 || len(out) >= 200 {
			cut = true
			return nil
		}
		d, err := root.Open(dir)
		if err != nil {
			return err
		}
		entries, err := d.ReadDir(5001)
		d.Close()
		if err != nil && err != io.EOF {
			return err
		}
		if len(entries) > 5000 {
			entries = entries[:5000]
			cut = true
		}
		for _, entry := range entries {
			if scanned >= 5000 || len(out) >= 200 {
				cut = true
				break
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			scanned++
			name := entry.Name()
			if entry.Type()&os.ModeSymlink != 0 || name == ".git" || name == "node_modules" {
				continue
			}
			path := filepath.Join(dir, name)
			if entry.IsDir() {
				if err := walk(path, depth+1); err != nil {
					return err
				}
				continue
			}
			info, e := entry.Info()
			if e != nil || !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
				continue
			}
			file, e := openRead(root, path)
			if e != nil {
				continue
			}
			b, e := io.ReadAll(io.LimitReader(file, MaxFileBytes))
			file.Close()
			if e != nil || !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
				continue
			}
			for i, line := range strings.Split(string(b), "\n") {
				if strings.Contains(line, a.Query) {
					out = append(out, map[string]any{"path": path, "line": i + 1, "text": line[:min(len(line), 1000)]})
					if len(out) >= 200 {
						cut = true
						break
					}
				}
			}
		}
		return nil
	}
	if err := walk(a.Path, 0); err != nil {
		return Output{}, fmt.Errorf("search: %w", err)
	}
	return Output{Value: map[string]any{"resolved_path": filepath.Join(root.Name(), a.Path), "matches": out, "truncated": cut, "scanned": scanned}}, nil
}

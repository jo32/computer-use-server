package harness

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ignoreRule is one parsed .gitignore line. base is the project-relative
// directory that holds the file ("" for the project root).
type ignoreRule struct {
	base     string
	segs     []string
	negate   bool
	dirOnly  bool
	anchored bool
}

func parseIgnore(base string, r io.Reader) []ignoreRule {
	var rules []ignoreRule
	sc := bufio.NewScanner(io.LimitReader(r, 256<<10))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{base: base}
		if strings.HasPrefix(line, "!") {
			rule.negate = true
			line = line[1:]
		} else if strings.HasPrefix(line, `\`) {
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly = true
			line = strings.TrimRight(line, "/")
		}
		if line == "" {
			continue
		}
		if strings.Contains(line, "/") {
			rule.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		rule.segs = strings.Split(line, "/")
		rules = append(rules, rule)
	}
	return rules
}

// ignored reports whether the project-relative path is excluded; the last
// matching rule wins, as in git.
func ignored(rules []ignoreRule, rel string, isDir bool) bool {
	result := false
	for _, r := range rules {
		if r.dirOnly && !isDir {
			continue
		}
		sub := rel
		if r.base != "" {
			if !strings.HasPrefix(rel, r.base+"/") {
				continue
			}
			sub = rel[len(r.base)+1:]
		}
		var ok bool
		if r.anchored {
			ok = matchSegs(r.segs, strings.Split(sub, "/"))
		} else {
			ok = matchSegs(r.segs, []string{path.Base(sub)})
		}
		if ok {
			result = !r.negate
		}
	}
	return result
}

// matchSegs matches slash-separated segments; "**" spans zero or more of them.
func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for len(pat) > 1 && pat[1] == "**" {
				pat = pat[1:]
			}
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegs(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// expandBraces turns "*.{go,md}" into "*.go" and "*.md" (at most 64 results).
func expandBraces(p string) []string {
	i := strings.IndexByte(p, '{')
	if i < 0 {
		return []string{p}
	}
	depth, j := 0, -1
	for k := i; k < len(p) && j < 0; k++ {
		switch p[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				j = k
			}
		}
	}
	if j < 0 {
		return []string{p}
	}
	var parts []string
	start, d := i+1, 0
	for k := i + 1; k < j; k++ {
		switch p[k] {
		case '{':
			d++
		case '}':
			d--
		case ',':
			if d == 0 {
				parts = append(parts, p[start:k])
				start = k + 1
			}
		}
	}
	parts = append(parts, p[start:j])
	var out []string
	for _, part := range parts {
		out = append(out, expandBraces(p[:i]+part+p[j+1:])...)
		if len(out) >= 64 {
			return out[:64]
		}
	}
	return out
}

// glob is a compiled pattern list. A pattern without a slash matches the base
// name at any depth when anywhere is set (like grep --include).
type glob [][]string

func compileGlob(p string, anywhere bool) glob {
	var g glob
	for _, one := range expandBraces(strings.TrimPrefix(p, "./")) {
		one = strings.TrimPrefix(one, "/")
		if one == "" {
			continue
		}
		segs := strings.Split(one, "/")
		if anywhere && len(segs) == 1 {
			segs = []string{"**", one}
		}
		g = append(g, segs)
	}
	return g
}
func (g glob) match(rel string) bool {
	name := strings.Split(rel, "/")
	for _, p := range g {
		if matchSegs(p, name) {
			return true
		}
	}
	return false
}

var errWalkBudget = errors.New("walk budget exhausted")

type walkItem struct {
	Rel   string // relative to the walk start, slash separated
	Path  string // relative to the project root
	Entry os.DirEntry
	Depth int
}

const (
	walkBudget   = 100000
	maxIgnoreRaw = 256 << 10
)

func loadIgnore(root *os.Root, dir string) []ignoreRule {
	f, err := root.Open(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > maxIgnoreRaw {
		return nil
	}
	base := filepath.ToSlash(dir)
	if base == "." {
		base = ""
	}
	return parseIgnore(base, f)
}

// walkTree visits entries beneath start in name order. With useIgnore it skips
// .git, node_modules and anything a .gitignore on the way excludes. visit
// reports whether to descend into a directory. Symlinks are never followed.
func walkTree(ctx context.Context, root *os.Root, start string, maxDepth int, useIgnore bool, visit func(walkItem) (bool, error)) error {
	budget := walkBudget
	var inherited []ignoreRule
	if useIgnore && start != "." {
		dir := "."
		inherited = append(inherited, loadIgnore(root, dir)...)
		parts := strings.Split(filepath.ToSlash(filepath.Dir(start)), "/")
		for _, p := range parts {
			if p == "." || p == "" {
				continue
			}
			dir = filepath.Join(dir, p)
			inherited = append(inherited, loadIgnore(root, dir)...)
		}
	}
	prefix := ""
	var rec func(dir string, depth int, rules []ignoreRule) error
	rec = func(dir string, depth int, rules []ignoreRule) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		d, err := root.Open(dir)
		if err != nil {
			if depth == 1 {
				return err
			}
			return nil
		}
		entries, err := d.ReadDir(-1)
		d.Close()
		if err != nil && len(entries) == 0 {
			if depth == 1 {
				return err
			}
			return nil
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		if useIgnore {
			rules = append(rules[:len(rules):len(rules)], loadIgnore(root, dir)...)
		}
		for _, e := range entries {
			if budget--; budget < 0 {
				return errWalkBudget
			}
			name := e.Name()
			p := filepath.Join(dir, name)
			slash := filepath.ToSlash(p)
			isDir := e.IsDir()
			if useIgnore && (name == ".git" || name == "node_modules" || name == ".DS_Store" || ignored(rules, slash, isDir)) {
				continue
			}
			rel := slash
			if prefix != "" {
				rel = strings.TrimPrefix(slash, prefix)
			}
			descend, err := visit(walkItem{Rel: rel, Path: p, Entry: e, Depth: depth})
			if err != nil {
				return err
			}
			if isDir && descend && depth < maxDepth {
				if err := rec(p, depth+1, rules); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if start != "." {
		prefix = filepath.ToSlash(start) + "/"
	}
	return rec(start, 1, inherited)
}

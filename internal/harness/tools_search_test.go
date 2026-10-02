package harness

import (
	"fmt"
	"strings"
	"testing"
)

func tree(t *testing.T, root string) {
	t.Helper()
	put(t, root, ".gitignore", "# build output\nbuild/\n*.log\n!keep.log\n/secret.txt\n")
	put(t, root, "main.go", "package main\n\nfunc Main() {}\nfunc helper() {}\n")
	put(t, root, "README.md", "# Title\nHello World\n")
	put(t, root, "secret.txt", "Main secret\n")
	put(t, root, "app.log", "Main log\n")
	put(t, root, "keep.log", "Main kept\n")
	put(t, root, "build/out.go", "package build\nfunc Main() {}\n")
	put(t, root, "src/lib.go", "package src\nfunc Main() {}\n")
	put(t, root, "src/deep/util.ts", "export const Main = 1\n")
	put(t, root, "src/sub/.gitignore", "*.tmp\n")
	put(t, root, "src/sub/x.tmp", "Main tmp\n")
	put(t, root, "src/sub/y.go", "package sub\nfunc Main() {}\n")
	put(t, root, "src/secret.txt", "Main nested secret\n")
	put(t, root, "node_modules/pkg/index.js", "Main module\n")
	put(t, root, ".git/config", "Main git\n")
}
func names(t *testing.T, o Output) []string {
	t.Helper()
	var out []string
	for _, e := range asMap(t, o)["entries"].([]map[string]any) {
		out = append(out, e["name"].(string))
	}
	return out
}
func TestGitignoreMatching(t *testing.T) {
	rules := parseIgnore("", strings.NewReader("build/\n*.log\n!keep.log\n/top.txt\ndocs/**/*.gen\n\\#hash\n"))
	for path, want := range map[string]bool{"build": true, "a/build": true, "x.log": true, "keep.log": false, "d/keep.log": false, "top.txt": true, "sub/top.txt": false, "docs/a/b/c.gen": true, "docs/c.gen": true, "other/c.gen": false, "#hash": true, "main.go": false} {
		isDir := path == "build" || path == "a/build"
		if got := ignored(rules, path, isDir); got != want {
			t.Errorf("ignored(%q) = %v, want %v", path, got, want)
		}
	}
	if ignored(rules, "build", false) {
		t.Error("directory-only rule matched a file")
	}
}
func TestGlobBraces(t *testing.T) {
	for pattern, want := range map[string]map[string]bool{
		"*.{go,ts}":    {"a.go": true, "d/e.ts": true, "x.md": false},
		"src/**/*.go":  {"src/a.go": true, "src/a/b/c.go": true, "lib/a.go": false},
		"**/test_*.py": {"test_a.py": true, "x/y/test_b.py": true, "x/a.py": false},
	} {
		g := compileGlob(pattern, false)
		any := compileGlob(pattern, true)
		for path, ok := range want {
			if strings.Contains(pattern, "/") && g.match(path) != ok {
				t.Errorf("%s on %s = %v", pattern, path, !ok)
			}
			if !strings.Contains(pattern, "/") && any.match(path) != ok {
				t.Errorf("anywhere %s on %s = %v", pattern, path, !ok)
			}
		}
	}
	if g := compileGlob("*.go", false); g.match("sub/a.go") || !g.match("a.go") {
		t.Error("strict glob must stay on one level")
	}
}
func TestListDirectoryDepthPatternAndIgnore(t *testing.T) {
	r, _, root := filesForTest(t)
	tree(t, root)
	out, err := invoke(t, r, "list_directory", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	flat := strings.Join(names(t, out), ",")
	for _, want := range []string{".git", "build", "node_modules", "app.log", "src"} {
		if !strings.Contains(","+flat+",", ","+want+",") {
			t.Fatalf("a plain listing must show %s: %s", want, flat)
		}
	}
	if !strings.Contains(out.Text, "src/") || !strings.Contains(out.Text, "main.go") {
		t.Fatal(out.Text)
	}
	out, err = invoke(t, r, "list_directory", map[string]any{"depth": 4, "type": "file"})
	if err != nil {
		t.Fatal(err)
	}
	got := "," + strings.Join(names(t, out), ",") + ","
	for _, want := range []string{"main.go", "keep.log", "src/lib.go", "src/deep/util.ts", "src/sub/y.go", "src/secret.txt"} {
		if !strings.Contains(got, ","+want+",") {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	for _, hidden := range []string{"app.log", "build/out.go", "src/sub/x.tmp", "node_modules", ".git/config", ",secret.txt,"} {
		if strings.Contains(got, hidden) || strings.Contains(got, ","+hidden) {
			t.Errorf("%s should be ignored: %s", hidden, got)
		}
	}
	out, _ = invoke(t, r, "list_directory", map[string]any{"depth": 4, "no_ignore": true, "pattern": "*.log"})
	if got := strings.Join(names(t, out), ","); got != "app.log,keep.log" {
		t.Fatalf("no_ignore pattern: %s", got)
	}
	out, _ = invoke(t, r, "list_directory", map[string]any{"path": "src", "depth": 3, "pattern": "*.go", "sort": "size"})
	if len(names(t, out)) != 2 {
		t.Fatalf("scoped pattern: %v", names(t, out))
	}
	for _, bad := range []map[string]any{{"depth": 9}, {"limit": 0, "depth": -1}, {"sort": "random"}, {"type": "socket"}} {
		if _, err = invoke(t, r, "list_directory", bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	out, _ = invoke(t, r, "list_directory", map[string]any{"limit": 2})
	if len(names(t, out)) != 2 || asMap(t, out)["truncated"] != true || !strings.Contains(out.Text, "truncated") {
		t.Fatal(out.Value)
	}
}
func TestGlobTool(t *testing.T) {
	r, _, root := filesForTest(t)
	tree(t, root)
	out, err := invoke(t, r, "glob", map[string]any{"pattern": "**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Text; got != "main.go\nsrc/lib.go\nsrc/sub/y.go\n" {
		t.Fatalf("glob: %q", got)
	}
	out, _ = invoke(t, r, "glob", map[string]any{"pattern": "*.go"})
	if out.Text != "main.go\n" {
		t.Fatalf("top-level glob: %q", out.Text)
	}
	out, _ = invoke(t, r, "glob", map[string]any{"pattern": "*", "type": "dir", "path": "src"})
	if out.Text != "deep/\nsub/\n" {
		t.Fatalf("dirs: %q", out.Text)
	}
	out, _ = invoke(t, r, "glob", map[string]any{"pattern": "**/*.zzz"})
	if out.Text != "[no matches]" {
		t.Fatal(out.Text)
	}
	out, _ = invoke(t, r, "glob", map[string]any{"pattern": "**/*.{log,md}", "no_ignore": true})
	if out.Text != "README.md\napp.log\nkeep.log\n" {
		t.Fatalf("no_ignore: %q", out.Text)
	}
	if _, err = invoke(t, r, "glob", map[string]any{"pattern": ""}); err == nil {
		t.Fatal("empty pattern accepted")
	}
}
func TestSearchFilesModes(t *testing.T) {
	r, _, root := filesForTest(t)
	tree(t, root)
	out, err := invoke(t, r, "search_files", map[string]any{"query": "func Main"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "main.go:3:func Main() {}\nsrc/lib.go:2:func Main() {}\nsrc/sub/y.go:2:func Main() {}\n"; out.Text != want {
		t.Fatalf("literal search (ignored paths must be skipped):\n%q", out.Text)
	}
	out, _ = invoke(t, r, "search_files", map[string]any{"query": "SECRET", "case_insensitive": true, "no_ignore": true})
	if !strings.Contains(out.Text, "secret.txt:1:Main secret") || !strings.Contains(out.Text, "src/secret.txt:1:Main nested secret") {
		t.Fatalf("case-insensitive: %q", out.Text)
	}
	out, err = invoke(t, r, "search_files", map[string]any{"query": `^func (\w+)\(\) \{\}$`, "regex": true, "include": "*.go", "output": "files"})
	if err != nil || out.Text != "main.go\nsrc/lib.go\nsrc/sub/y.go\n" {
		t.Fatalf("regex files: %q %v", out.Text, err)
	}
	out, _ = invoke(t, r, "search_files", map[string]any{"query": "func Main", "path": "main.go", "context": 1})
	if want := "main.go-2-\nmain.go:3:func Main() {}\nmain.go-4-func helper() {}\n"; out.Text != want {
		t.Fatalf("context on a single file: %q", out.Text)
	}
	if _, err = invoke(t, r, "search_files", map[string]any{"query": "(", "regex": true}); err == nil || !strings.Contains(err.Error(), "invalid regular expression") {
		t.Fatalf("bad regex: %v", err)
	}
	out, _ = invoke(t, r, "search_files", map[string]any{"query": "nothing-matches-this"})
	if out.Text != "[no matches]" {
		t.Fatal(out.Text)
	}
}
func TestSearchFilesPagination(t *testing.T) {
	r, _, root := filesForTest(t)
	var b strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&b, "hit %d\n", i)
	}
	put(t, root, "hits.txt", b.String())
	seen := 0
	offset := 0
	for page := 0; page < 10; page++ {
		args := map[string]any{"query": "hit", "max_results": 10, "offset": offset}
		out, err := invoke(t, r, "search_files", args)
		if err != nil {
			t.Fatal(err)
		}
		v := asMap(t, out)
		seen += len(v["matches"].([]map[string]any))
		next, more := v["next_offset"].(int)
		if !more {
			break
		}
		if v["truncated"] != true || !strings.Contains(out.Text, fmt.Sprintf("offset=%d", next)) {
			t.Fatalf("continuation hint: %v", out.Text)
		}
		offset = next
	}
	if seen != 25 {
		t.Fatalf("paged through %d of 25 matches", seen)
	}
}

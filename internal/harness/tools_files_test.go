package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func filesForTest(t *testing.T) (*Registry, *Files, string) {
	t.Helper()
	root := t.TempDir()
	f, err := NewFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	r := registryForTest(t)
	f.Register(r)
	return r, f, root
}
func put(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func asMap(t *testing.T, o Output) map[string]any {
	t.Helper()
	m, ok := o.Value.(map[string]any)
	if !ok {
		t.Fatalf("value is %T", o.Value)
	}
	return m
}

func TestReadFilePagesWithLineNumbers(t *testing.T) {
	r, _, root := filesForTest(t)
	var b strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	put(t, root, "big.txt", b.String())
	out, err := invoke(t, r, "read_file", map[string]any{"path": "big.txt"})
	if err != nil {
		t.Fatal(err)
	}
	v := asMap(t, out)
	if v["next_start_line"] != 2001 || v["end_line"] != 2000 || v["total_lines"] != 3000 || v["truncated"] != true {
		t.Fatalf("paging metadata: %v", v)
	}
	if !strings.HasPrefix(out.Text, "     1\tline 1\n") || !strings.Contains(out.Text, "continue with start_line=2001") || len(out.TextKeys) == 0 {
		t.Fatalf("text tail: %q", out.Text[len(out.Text)-120:])
	}
	out, err = invoke(t, r, "read_file", map[string]any{"path": "big.txt", "start_line": 2001})
	v = asMap(t, out)
	if err != nil || v["truncated"] != false || v["end_line"] != 3000 || !strings.Contains(out.Text, "  3000\tline 3000") {
		t.Fatalf("second page: %v %v", v, err)
	}
	out, _ = invoke(t, r, "read_file", map[string]any{"path": "big.txt", "start_line": 10, "limit": 2})
	if asMap(t, out)["content"] != "line 10\nline 11" || asMap(t, out)["next_start_line"] != 12 {
		t.Fatalf("limit: %v", out.Value)
	}
	out, _ = invoke(t, r, "read_file", map[string]any{"path": "big.txt", "start_line": 5000})
	if !strings.Contains(out.Text, "past the end") {
		t.Fatal(out.Text)
	}
}
func TestReadFileByteCapLongLinesEmptyAndBinary(t *testing.T) {
	r, _, root := filesForTest(t)
	put(t, root, "wide.txt", strings.Repeat(strings.Repeat("w", 1500)+"\n", 200))
	out, err := invoke(t, r, "read_file", map[string]any{"path": "wide.txt"})
	v := asMap(t, out)
	if err != nil || v["truncated"] != true || len(out.Text) > maxReadBytes+400 || v["next_start_line"] == nil {
		t.Fatalf("byte cap: %v %d", err, len(out.Text))
	}
	put(t, root, "long.txt", strings.Repeat("z", 5000)+"\nshort\n")
	out, _ = invoke(t, r, "read_file", map[string]any{"path": "long.txt"})
	if !strings.Contains(out.Text, "[line truncated]") || !strings.Contains(out.Text, "short") {
		t.Fatal("long line not cut")
	}
	put(t, root, "empty.txt", "")
	out, _ = invoke(t, r, "read_file", map[string]any{"path": "empty.txt"})
	if out.Text != "[empty file]" {
		t.Fatal(out.Text)
	}
	put(t, root, "bin.dat", "ab\x00cd")
	if _, err = invoke(t, r, "read_file", map[string]any{"path": "bin.dat"}); err == nil {
		t.Fatal("binary read as text")
	}
	out, err = invoke(t, r, "read_file", map[string]any{"path": "bin.dat", "encoding": "base64"})
	if err != nil || asMap(t, out)["content"] != "YWIAY2Q=" {
		t.Fatal(out.Value, err)
	}
	put(t, root, "pic.png", "\x89PNG fake")
	out, err = invoke(t, r, "read_file", map[string]any{"path": "pic.png"})
	if err != nil || len(out.Images) != 1 || out.Images[0].MIME != "image/png" || asMap(t, out)["image"] != true {
		t.Fatalf("image: %v %v", out, err)
	}
	if _, err = invoke(t, r, "read_file", map[string]any{"path": "pic.png", "limit": -1}); err == nil {
		t.Fatal("negative limit accepted")
	}
}
func TestReadSpillFileOnlyByName(t *testing.T) {
	r, f, _ := filesForTest(t)
	f.SpillDir = t.TempDir()
	name := "0123456789abcdef01234567.stdout"
	if err := os.WriteFile(filepath.Join(f.SpillDir, name), []byte("saved\noutput\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := invoke(t, r, "read_file", map[string]any{"path": "spill:" + name, "start_line": 2})
	if err != nil || asMap(t, out)["content"] != "output" {
		t.Fatalf("spill read: %v %v", out.Value, err)
	}
	for _, bad := range []string{"spill:../x", "spill:other.txt", "spill:" + name + "/../..", "spill:"} {
		if _, err = invoke(t, r, "read_file", map[string]any{"path": bad}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	f.SpillDir = ""
	if _, err = invoke(t, r, "read_file", map[string]any{"path": "spill:" + name}); err == nil {
		t.Fatal("spill read without a spill dir")
	}
}
func TestEditFile(t *testing.T) {
	r, _, root := filesForTest(t)
	put(t, root, "a.sh", "one\ntwo\nthree\ntwo\n")
	os.Chmod(filepath.Join(root, "a.sh"), 0755)
	if _, err := invoke(t, r, "edit_file", map[string]any{"path": "a.sh", "old_string": "two", "new_string": "2"}); err == nil || !strings.Contains(err.Error(), "2 times (lines 2, 4)") {
		t.Fatalf("ambiguous edit: %v", err)
	}
	out, err := invoke(t, r, "edit_file", map[string]any{"path": "a.sh", "old_string": "one\ntwo", "new_string": "1\n2", "description": "renumber"})
	if err != nil {
		t.Fatal(err)
	}
	v := asMap(t, out)
	if v["replacements"] != 1 || v["first_line"] != 1 || !strings.Contains(v["snippet"].(string), "1\t1") {
		t.Fatalf("result: %v", v)
	}
	out, err = invoke(t, r, "edit_file", map[string]any{"path": "a.sh", "old_string": "two", "new_string": "II", "replace_all": true})
	if err != nil || asMap(t, out)["replacements"] != 1 {
		t.Fatalf("replace_all: %v %v", out.Value, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "a.sh"))
	if string(b) != "1\n2\nthree\nII\n" {
		t.Fatalf("content %q", b)
	}
	if info, _ := os.Stat(filepath.Join(root, "a.sh")); info.Mode().Perm() != 0755 {
		t.Fatalf("mode lost: %v", info.Mode())
	}
	if _, err = invoke(t, r, "edit_file", map[string]any{"path": "a.sh", "old_string": "absent", "new_string": "x"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatal(err)
	}
	put(t, root, "idx.go", "func f() {\n\treturn\n}\n")
	if _, err = invoke(t, r, "edit_file", map[string]any{"path": "idx.go", "old_string": "  return", "new_string": "x"}); err != nil && !strings.Contains(err.Error(), "not found") {
		t.Fatal(err)
	}
	if _, err = invoke(t, r, "edit_file", map[string]any{"path": "idx.go", "old_string": "return \n", "new_string": "x"}); err == nil || !strings.Contains(err.Error(), "whitespace") {
		t.Fatalf("no whitespace hint: %v", err)
	}
	put(t, root, "crlf.txt", "a\r\nb\r\nc\r\n")
	if _, err = invoke(t, r, "edit_file", map[string]any{"path": "crlf.txt", "old_string": "a\nb", "new_string": "A\nB"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(root, "crlf.txt"))
	if string(b) != "A\r\nB\r\nc\r\n" {
		t.Fatalf("CRLF not preserved: %q", b)
	}
	for _, args := range []map[string]any{{"path": "a.sh", "old_string": "", "new_string": "x"}, {"path": "a.sh", "old_string": "3", "new_string": "3"}, {"path": "missing", "old_string": "a", "new_string": "b"}, {"path": "../x", "old_string": "a", "new_string": "b"}} {
		if _, err = invoke(t, r, "edit_file", args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	put(t, root, "bin", "a\x00b")
	if _, err = invoke(t, r, "edit_file", map[string]any{"path": "bin", "old_string": "a", "new_string": "b"}); err == nil {
		t.Fatal("binary edited")
	}
}
func TestWriteFileModeAndCreateOnly(t *testing.T) {
	r, _, root := filesForTest(t)
	out, err := invoke(t, r, "write_file", map[string]any{"path": "d/new.txt", "content": "x", "create_only": true})
	if err != nil || asMap(t, out)["created"] != true {
		t.Fatal(out.Value, err)
	}
	if info, _ := os.Stat(filepath.Join(root, "d/new.txt")); info.Mode().Perm() != 0644 {
		t.Fatalf("new file mode %v", info.Mode().Perm())
	}
	if _, err = invoke(t, r, "write_file", map[string]any{"path": "d/new.txt", "content": "y", "create_only": true}); err == nil {
		t.Fatal("create_only overwrote")
	}
	os.Chmod(filepath.Join(root, "d/new.txt"), 0750)
	if _, err = invoke(t, r, "write_file", map[string]any{"path": "d/new.txt", "content": "z"}); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Join(root, "d/new.txt")); info.Mode().Perm() != 0750 {
		t.Fatalf("mode not kept: %v", info.Mode().Perm())
	}
	if _, err = invoke(t, r, "write_file", map[string]any{"path": "d", "content": "z"}); err == nil {
		t.Fatal("wrote over a directory")
	}
}

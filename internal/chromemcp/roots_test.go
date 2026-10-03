package chromemcp

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func childRecords(path string) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var v map[string]any
		if json.Unmarshal(scanner.Bytes(), &v) == nil {
			out = append(out, v)
		}
	}
	return out
}

// find returns the first record of a kind whose id matches (id is ignored when nil).
func find(records []map[string]any, kind string, id any) map[string]any {
	for _, v := range records {
		if v["kind"] == kind && (id == nil || v["id"] == id) {
			return v
		}
	}
	return nil
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

// The Chrome DevTools server only lets its tools write inside the roots the client
// declares. Without them a screenshot could only be saved to the OS temp folder.
func TestBridgeDeclaresRootsAndAnswersTheServer(t *testing.T) {
	out := filepath.Join(t.TempDir(), "child.jsonl")
	t.Setenv("READYRIG_MCP_TEST_ROOTS", "1")
	t.Setenv("READYRIG_MCP_TEST_OUT", out)
	project, extra := t.TempDir(), t.TempDir()
	var mu sync.Mutex
	paths := []string{project, extra, project, "relative/dir", ""}
	_, r, _ := testBridgeWith(t, func(b *Bridge) {
		b.SetRoots(func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), paths...)
		})
	})
	// The server's request used id 2, the id of the pending tools/list call.
	// The tools must still load, so it was not mistaken for that call's reply.
	if n := len(r.Specs()); n != 3 {
		t.Fatalf("a server request was taken for a reply: %d tools", n)
	}
	waitFor(t, func() bool {
		rec := childRecords(out)
		return find(rec, "capabilities", nil) != nil && find(rec, "response", float64(2)) != nil && find(rec, "response", "s-3") != nil
	})
	rec := childRecords(out)
	caps, _ := find(rec, "capabilities", nil)["value"].(map[string]any)
	if roots, _ := caps["roots"].(map[string]any); roots == nil || roots["listChanged"] != true {
		t.Fatalf("the roots capability was not declared: %v", caps)
	}
	result, _ := find(rec, "response", float64(2))["result"].(map[string]any)
	list, _ := result["roots"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected the two absolute, distinct folders, got %v", list)
	}
	for i, dir := range []string{project, extra} {
		root, _ := list[i].(map[string]any)
		if root["uri"] != fileURI(dir) || root["name"] != filepath.Base(dir) {
			t.Fatalf("root %d is %v, want %s", i, root, fileURI(dir))
		}
	}
	unknown, _ := find(rec, "response", "s-3")["error"].(map[string]any)
	if unknown == nil || unknown["code"] != float64(-32601) {
		t.Fatalf("an unknown request needs a method-not-found error, got %v", unknown)
	}
}

func TestRootsChangedNotifiesTheServer(t *testing.T) {
	out := filepath.Join(t.TempDir(), "child.jsonl")
	t.Setenv("READYRIG_MCP_TEST_OUT", out)
	b, _, _ := testBridgeWith(t, func(b *Bridge) { b.SetRoots(func() []string { return []string{t.TempDir()} }) })
	if find(childRecords(out), "notification", nil) != nil {
		t.Fatal("notified before anything changed")
	}
	b.RootsChanged()
	waitFor(t, func() bool { return find(childRecords(out), "notification", nil) != nil })
}

// Without a folder list the client behaves as before: nothing is declared.
func TestBridgeWithoutRootsDeclaresNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "child.jsonl")
	t.Setenv("READYRIG_MCP_TEST_OUT", out)
	b, _, _ := testBridge(t)
	caps, _ := find(childRecords(out), "capabilities", nil)["value"].(map[string]any)
	if caps == nil || len(caps) != 0 {
		t.Fatalf("expected empty capabilities, got %v", caps)
	}
	b.RootsChanged() // must be a harmless no-op
	if find(childRecords(out), "notification", nil) != nil {
		t.Fatal("sent a roots notification without declaring roots")
	}
}

func TestPathHintOnlyForRefusedPaths(t *testing.T) {
	text := func(s string) map[string]any {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}}
	}
	refused := text("Access denied: path /x (canonical: /x) is not within any of the configured workspace roots.")
	if pathHint(refused) == "" {
		t.Fatal("the refusal got no hint")
	}
	if h := pathHint(text("Page crashed")); h != "" {
		t.Fatalf("unrelated error got a hint: %q", h)
	}
	if h := pathHint(map[string]any{}); h != "" {
		t.Fatalf("empty result got a hint: %q", h)
	}
}

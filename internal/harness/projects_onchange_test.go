package harness

import "testing"

// The browser tools must learn about folder changes, but never while a lock is held.
func TestProjectsOnChange(t *testing.T) {
	p, err := NewProjects(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	p.OnChange = func() {
		calls++
		_ = p.Snapshot() // would deadlock if the project lock were still held
	}
	dir := t.TempDir()
	if err := p.Change("add", "", "extra", dir); err != nil || calls != 1 {
		t.Fatalf("add: err=%v calls=%d", err, calls)
	}
	if err := p.Change("add", "", "extra", dir); err == nil || calls != 1 {
		t.Fatalf("a rejected change must not notify: err=%v calls=%d", err, calls)
	}
	if err := p.Change("remove", "missing", "", ""); err == nil || calls != 1 {
		t.Fatalf("an unknown project must not notify: err=%v calls=%d", err, calls)
	}
	p.SetFullAccess(true)
	if calls != 2 || !p.Snapshot().FullAccess {
		t.Fatalf("full access: calls=%d", calls)
	}
}

package main

import (
	"computer-use-server/internal/cloud"
	"strings"
	"testing"
)

func TestAccountLinkWrapAndScroll(t *testing.T) {
	url := "https://readyrig.getmegaportal.com/console?pair=" + strings.Repeat("x", 80)
	u := &terminalUI{tab: 4, client: &controlClient{}, cloud: cloud.Status{
		State: "signing_in", LoginURL: url, Code: "123456", Name: "host\x1b[2J",
	}}
	lines := u.renderAccount(40)
	var joined strings.Builder
	for _, line := range lines {
		if len([]rune(line)) > 39 {
			t.Fatalf("line exceeds available columns: %q", line)
		}
		if strings.ContainsRune(line, '\x1b') {
			t.Fatalf("account data contains terminal escape: %q", line)
		}
		joined.WriteString(strings.TrimSpace(line))
	}
	if !strings.Contains(joined.String(), url) {
		t.Fatal("wrapped login URL lost characters")
	}
	u.render(40, 16)
	u.selected = u.itemCount() - 1
	if !strings.Contains(u.render(40, 16), "automatically.") {
		t.Fatal("cannot scroll to the end of login instructions in a small terminal")
	}
}

func TestAccountExpiredLoginShowsRecovery(t *testing.T) {
	u := &terminalUI{cloud: cloud.Status{
		State: "error", Message: "Login expired", LoginURL: "https://example.test/console?pair=old",
	}}
	screen := strings.Join(u.renderAccount(80), "\n")
	if !strings.Contains(screen, "Login expired") || !strings.Contains(screen, "then l to try again") {
		t.Fatal("expired login must show the error and recovery action")
	}
}

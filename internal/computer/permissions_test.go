package computer

import (
	"strings"
	"testing"
)

// permDriver reports fixed permissions on top of the fake driver.
type permDriver struct {
	*fakeDriver
	perms Permissions
}

func (d permDriver) Permissions() Permissions { return d.perms }

func TestMissingPermissionsAreNamedPerTool(t *testing.T) {
	none := newComputer(t, permDriver{newFake(), Permissions{Supported: true}})
	for tool, want := range map[string]string{
		"computer_screenshot": "Screen & System Audio Recording",
		"computer_action":     "Accessibility",
		"computer_ui_tree":    "Accessibility",
		"computer_app":        "",
		"computer_clipboard":  "",
	} {
		got := none.Missing(tool)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%s: %q, want mention of %q", tool, got, want)
		}
	}
	screenOnly := newComputer(t, permDriver{newFake(), Permissions{Supported: true, Screen: true}})
	if screenOnly.Missing("computer_screenshot") != "" || screenOnly.Missing("computer_action") == "" {
		t.Error("permissions are checked per tool")
	}
	all := newComputer(t, permDriver{newFake(), Permissions{Supported: true, Screen: true, Accessibility: true}})
	for _, tool := range []string{"computer_screenshot", "computer_action", "computer_ui_tree"} {
		if all.Missing(tool) != "" {
			t.Errorf("%s blocked although everything is granted", tool)
		}
	}
	unsupported := newComputer(t, permDriver{newFake(), Permissions{}})
	if unsupported.Missing("computer_action") != "" {
		t.Error("on a platform without desktop control the tool's own error is clearer than a permission hint")
	}
}

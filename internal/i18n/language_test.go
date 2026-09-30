package i18n

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectionPersistsAndTranslatesNativeCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "language.json")
	language := New(path)
	if err := language.Set(Selection{Preference: "en", Locale: "zh-CN"}); err != nil {
		t.Fatal(err)
	}
	if got := language.Text("正在执行 %d 个任务", 3); got != "Running 3 tasks" {
		t.Fatalf("got %q", got)
	}
	if got := New(path).Selection(); got != (Selection{Preference: "en", Locale: "en"}) {
		t.Fatalf("restored %#v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("preference permissions %o", info.Mode().Perm())
	}
	if err := language.Set(Selection{Preference: "zh-CN", Locale: "en"}); err != nil {
		t.Fatal(err)
	}
	if got := language.Text("打开 ReadyRig"); got != "打开 ReadyRig" {
		t.Fatalf("got %q", got)
	}
	if got := language.Text("unknown"); got != "unknown" {
		t.Fatalf("fallback %q", got)
	}
}

func TestAutomaticAndInvalidSelections(t *testing.T) {
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	path := filepath.Join(t.TempDir(), "language.json")
	language := New(path)
	if got := language.Selection(); got != (Selection{Preference: "auto", Locale: "zh-CN"}) {
		t.Fatalf("system selection %#v", got)
	}
	if err := language.Set(Selection{Preference: "auto", Locale: "en"}); err != nil {
		t.Fatal(err)
	}
	// Auto follows the current system on restart rather than a stale resolved locale.
	if got := New(path).Selection().Locale; got != "zh-CN" {
		t.Fatalf("automatic locale %q", got)
	}
	before := language.Selection()
	for _, bad := range []Selection{{Preference: "xx", Locale: "en"}, {Preference: "en", Locale: "xx"}} {
		if err := language.Set(bad); err == nil {
			t.Fatalf("accepted %#v", bad)
		}
		if language.Selection() != before {
			t.Fatal("invalid selection changed the preference")
		}
	}
	if err := os.WriteFile(path, []byte(`{"preference":"unknown","locale":"en"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := New(path).Selection().Preference; got != "auto" {
		t.Fatalf("invalid saved preference %q", got)
	}
}

func TestFailedSaveKeepsCurrentSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	language := New(filepath.Join(path, "language.json"))
	before := language.Selection()
	preference := "en"
	if before.Locale == "en" {
		preference = "zh-CN"
	}
	if err := language.Set(Selection{Preference: preference, Locale: preference}); err == nil {
		t.Fatal("expected a save failure")
	}
	if got := language.Selection(); got != before {
		t.Fatalf("failed save changed selection to %#v", got)
	}
}

package server

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"testing"
)

func TestSystemPermissionSettingsDestinations(t *testing.T) {
	s := fixture(t)
	ui := s.UI()
	for _, tc := range []struct{ body, pane string }{
		{`{}`, "Privacy_AllFiles"},
		{`{"permission":"files"}`, "Privacy_AllFiles"},
		{`{"permission":"screen"}`, "Privacy_ScreenCapture"},
		{`{"permission":"accessibility"}`, "Privacy_Accessibility"},
	} {
		t.Run(tc.pane+tc.body, func(t *testing.T) {
			var opened string
			s.openSystemSettings = func(ctx context.Context, url string) error {
				opened = url
				return ctx.Err()
			}
			w := request(ui, "POST", "/api/access/system-settings", tc.body, "")
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
				t.Fatal(w.Code, w.Body.String())
			}
			if want := "x-apple.systempreferences:com.apple.preference.security?" + tc.pane; opened != want {
				t.Fatalf("opened %q, want %q", opened, want)
			}
		})
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{errors.New("settings launch failed"), 500}, {errSystemSettingsUnsupported, 400}} {
		s.openSystemSettings = func(context.Context, string) error { return tc.err }
		w := request(ui, "POST", "/api/access/system-settings", `{"permission":"screen"}`, "")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.err.Error()) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if runtime.GOOS != "darwin" {
		s.openSystemSettings = nil
		if w := request(ui, "POST", "/api/access/system-settings", `{}`, ""); w.Code != 400 {
			t.Fatal("non-macOS launch was not rejected", w.Code, w.Body.String())
		}
	}
}

func TestSystemPermissionSettingsBoundaries(t *testing.T) {
	s := fixture(t)
	called := 0
	s.openSystemSettings = func(context.Context, string) error { called++; return nil }
	ui := s.UI()
	endpoint := "/api/access/system-settings"
	for _, body := range []string{
		`{"permission":"https://example.com"}`, `{"permission":"Privacy_ScreenCapture"}`,
		`{"permission":"screen; open /tmp"}`, `{"permission":true}`, `{"permission":`,
		`{"permission":"screen","url":"https://example.com"}`,
	} {
		if w := request(ui, "POST", endpoint, body, ""); w.Code != 400 {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
	for _, permission := range []string{"screen", "accessibility"} {
		body := `{"permission":"` + permission + `"}`
		if w := request(s.BrowserGuard(ui), "POST", "http://127.0.0.1:7331"+endpoint, body, ""); w.Code != http.StatusUnauthorized {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := request(s.Gateway(), "POST", endpoint, body, s.AccessPath); w.Code != http.StatusNotFound {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := request(s.Gateway(), "POST", "/app"+endpoint, body, s.AccessPath); w.Code != http.StatusForbidden {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if called != 0 {
		t.Fatal("invalid or remote request opened System Settings")
	}
}

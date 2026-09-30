package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateRoutesAreLocalAndAuthenticated(t *testing.T) {
	s := fixture(t)
	for _, route := range []struct{ method, path string }{{"GET", "/api/update"}, {"POST", "/api/update/check"}, {"POST", "/api/update/restart"}} {
		if w := request(s.Gateway(), route.method, route.path, "{}", s.AccessPath); w.Code != 404 {
			t.Fatalf("agent reaches updater: %s %d", route.path, w.Code)
		}
		url := "http://127.0.0.1:7331" + route.path
		if w := request(s.BrowserGuard(s.UI()), route.method, url, "{}", ""); w.Code != 401 {
			t.Fatalf("unauthenticated updater: %s %d", route.path, w.Code)
		}
		r := httptest.NewRequest(route.method, url, nil)
		r.AddCookie(&http.Cookie{Name: dashboardCookieName("127.0.0.1:7331"), Value: s.UIKey})
		r.Header.Set("Origin", "https://evil.test")
		w := httptest.NewRecorder()
		s.BrowserGuard(s.UI()).ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-origin updater accepted: %s", route.path)
		}
	}
	if w := request(s.UI(), "POST", "/api/update/restart", "{}", ""); w.Code != 409 {
		t.Fatal("restart accepted without staged release")
	}
	if w := request(s.UI(), "GET", "/api/update", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

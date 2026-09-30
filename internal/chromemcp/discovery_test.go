package chromemcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestModernChromeWithoutHTTPDiscovery(t *testing.T) {
	// Chrome's permission-based debugging server deliberately returns 404 here.
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	u, _ := url.Parse(server.URL)
	dir := t.TempDir()
	binary := filepath.Join(dir, "chrome")
	if err := os.WriteFile(binary, []byte("installed"), 0700); err != nil {
		t.Fatal(err)
	}
	reader := func(string) ([]byte, error) {
		return []byte(u.Port() + "\n/devtools/browser/permission-session\n"), nil
	}
	for _, browserURL := range []string{"", server.URL} {
		got, err := discoverWithReader(context.Background(), Options{UserDataDir: dir, BrowserURL: browserURL}, []string{binary}, nil, reader)
		expected := "ws://" + u.Host + "/devtools/browser/permission-session"
		if err != nil || got.Key != expected || len(got.Args) != 1 || got.Args[0] != "--ws-endpoint="+expected {
			t.Fatalf("got %+v, %v", got, err)
		}
	}
	// An explicit port must not silently attach to another Chrome instance.
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	if _, err := discoverWithReader(context.Background(), Options{UserDataDir: dir, BrowserURL: other.URL}, []string{binary}, nil, reader); err == nil {
		t.Fatal("accepted different browser port")
	}
}

func TestDebugPermissionIsActionable(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "chrome")
	if err := os.WriteFile(binary, []byte("installed"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{os.ErrPermission, syscall.EPERM, syscall.EACCES} {
		reader := func(path string) ([]byte, error) { return nil, &os.PathError{Op: "open", Path: path, Err: cause} }
		_, err := discoverWithReader(context.Background(), Options{UserDataDir: dir}, []string{binary}, nil, reader)
		if !errors.Is(err, ErrDebugPermission) {
			t.Fatalf("permission confused with debugging disabled: %v", err)
		}
	}
	reader := func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	_, err := discoverWithReader(context.Background(), Options{UserDataDir: dir}, []string{binary}, nil, reader)
	if errors.Is(err, ErrDebugPermission) || err == nil || !strings.Contains(err.Error(), "开启远程调试") {
		t.Fatalf("missing file confused with permission: %v", err)
	}
}

func TestBridgeReportsPermissionInsteadOfMissingChrome(t *testing.T) {
	b, r, _ := testBridge(t)
	b.Close()
	t.Cleanup(b.disconnect)
	b.detect = func(context.Context, Options) (target, error) { return target{}, ErrDebugPermission }
	b.reconcile(context.Background())
	if b.Status().State != "permission_required" || len(r.Specs()) != 3 {
		t.Fatalf("bad permission status: %+v", b.Status())
	}
}

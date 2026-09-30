package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v1.10.0", "1.9.0", true}, {"1.0.0", "1.0.0-rc.2", true}, {"1.0.0-rc.10", "1.0.0-rc.9", true}, {"1.0.0-2", "1.0.0-alpha", false}, {"1.0.0+new", "1.0.0+old", false}, {"0.9.0", "1.0.0", false}, {"2.0.0", "dev", false},
	} {
		if Newer(c.a, c.b) != c.want {
			t.Errorf("Newer(%q,%q)", c.a, c.b)
		}
	}
	for _, v := range []string{"dev", "1.2", "1.02.3", "1.2.3-", "1.2.3-01", "v1.2.3-3-gabcdef", "1.2.3-dirty", "1.2.3-a..b"} {
		if Released(v) {
			t.Errorf("accepted %q", v)
		}
	}
}

func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func testTarget(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "relay")
	if err := os.WriteFile(path, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}
func awaitCheck(t *testing.T, u *Manager) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s := u.Status()
		if s.State != "checking" && s.State != "downloading" {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("update check timed out")
	return Status{}
}

func TestBackgroundStageAndInstall(t *testing.T) {
	data := []byte("new binary")
	var requests atomic.Int32
	var fail atomic.Bool
	var base string
	name := AssetName(runtime.GOOS, runtime.GOARCH, false, false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			w.Write(data)
			return
		}
		requests.Add(1)
		if fail.Load() {
			http.Error(w, "offline", 503)
			return
		}
		json.NewEncoder(w).Encode(Release{Version: "1.1.0", Assets: map[string]Asset{name: {URL: base + "/asset", Size: int64(len(data)), SHA256: digest(data)}}})
	}))
	defer srv.Close()
	base = srv.URL
	target := testTarget(t)
	u := newAt(Options{Version: "1.0.0", Feed: base}, target)
	defer u.Finish(false)
	u.Check()
	s := awaitCheck(t, u)
	if s.State != "ready" || !s.CanRestart || s.Done != int64(len(data)) {
		t.Fatalf("%+v", s)
	}
	if b, _ := os.ReadFile(target); string(b) != "old" {
		t.Fatal("background check installed an update")
	}
	fail.Store(true)
	u.Check()
	s = awaitCheck(t, u)
	if !s.CanRestart || s.Latest != "1.1.0" || s.Error == "" {
		t.Fatalf("lost verified download after feed failure: %+v", s)
	}
	if err := u.Finish(true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); !bytes.Equal(b, data) {
		t.Fatal("quit did not install update")
	}
	if err := u.Finish(true); err != nil {
		t.Fatal("Finish must be idempotent")
	}
}

func TestCoalesceCancelAndInstallationLock(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		entered <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()
	target := testTarget(t)
	u := newAt(Options{Version: "1.0.0", Feed: srv.URL}, target)
	other := newAt(Options{Version: "1.0.0", Feed: srv.URL}, target)
	defer other.Finish(false)
	if other.status.Reason == "" {
		t.Fatal("second updater can install concurrently")
	}
	u.Check()
	<-entered
	for range 20 {
		u.Check()
	}
	start := time.Now()
	if err := u.Finish(false); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second || requests.Load() != 1 {
		t.Fatal("checks not coalesced or shutdown did not cancel")
	}
	if b, _ := os.ReadFile(target); string(b) != "old" {
		t.Fatal("cancel changed original")
	}
	u.Check()
	if requests.Load() != 1 {
		t.Fatal("check after close")
	}
	next := newAt(Options{Version: "1.0.0", Feed: srv.URL}, target)
	defer next.Finish(false)
	if next.status.Reason != "" {
		t.Fatal("installation lock not released", next.status.Reason)
	}
}

func TestDownloadRejectsCorruptionAndSize(t *testing.T) {
	body := []byte("new binary")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	u := newAt(Options{Version: "dev"}, testTarget(t))
	defer u.Finish(false)
	for _, a := range []Asset{
		{URL: srv.URL, Size: int64(len(body)), SHA256: strings.Repeat("0", 64)},
		{URL: srv.URL, Size: 1, SHA256: digest(body)},
		{URL: srv.URL, Size: 100, SHA256: digest(body)},
	} {
		path := filepath.Join(t.TempDir(), "download")
		if err := u.download(context.Background(), a, path); err == nil {
			t.Fatal("accepted damaged download")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("partial download retained")
		}
	}
}

func TestInvalidAssetAndDevelopmentNeverInstall(t *testing.T) {
	for _, o := range []Options{{Version: "dev", Feed: "http://127.0.0.1:1"}, {Version: "1.0.0"}, {Version: "1.0.0", Feed: "http://example.com/feed"}, {Version: "1.0.0", Feed: "https://example.com", Disabled: true}} {
		u := newAt(o, testTarget(t))
		u.Check()
		if u.Status().CanCheck {
			t.Fatal("unexpected network-enabled updater")
		}
		if err := u.RequestRestart(); err == nil {
			t.Fatal("restart without an update")
		}
		u.Finish(false)
	}
	u := newAt(Options{Version: "dev"}, testTarget(t))
	defer u.Finish(false)
	for _, a := range []Asset{{URL: "http://127.0.0.1:1", Size: 10}, {URL: "http://127.0.0.1:1", Size: maxDownload + 1, SHA256: strings.Repeat("0", 64)}} {
		if _, _, err := u.stageRelease(context.Background(), &Release{Assets: map[string]Asset{u.asset: a}}); err == nil {
			t.Fatal("invalid asset accepted")
		}
	}
}

func TestSwapRollsBack(t *testing.T) {
	target := testTarget(t)
	if err := swap(filepath.Join(t.TempDir(), "missing"), target); err == nil {
		t.Fatal("missing update installed")
	}
	if b, _ := os.ReadFile(target); string(b) != "old" {
		t.Fatal("original lost")
	}
}

func TestChangedInstallationNotOverwritten(t *testing.T) {
	target := testTarget(t)
	u := newAt(Options{Version: "1.0.0", Feed: "https://example.com"}, target)
	u.stageDir = t.TempDir()
	u.stage = filepath.Join(u.stageDir, "new")
	os.WriteFile(u.stage, []byte("new"), 0755)
	os.Rename(target, target+".saved")
	os.WriteFile(target, []byte("installed elsewhere"), 0755)
	if err := u.Finish(true); err == nil {
		t.Fatal("overwrote another installation")
	}
	if b, _ := os.ReadFile(target); string(b) != "installed elsewhere" {
		t.Fatal("concurrent installation lost")
	}
}

func TestArchiveRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "Relay.app/../../escape", `Relay.app\evil`, "Relay.app/link"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.zip")
			f, _ := os.Create(path)
			z := zip.NewWriter(f)
			h := &zip.FileHeader{Name: name}
			if strings.HasSuffix(name, "link") {
				h.SetMode(os.ModeSymlink | 0777)
			}
			w, _ := z.CreateHeader(h)
			io.WriteString(w, "payload")
			z.Close()
			f.Close()
			if err := unzip(path, t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestGitHubPrivateAssetsAndChecksums(t *testing.T) {
	u := newAt(Options{Version: "dev"}, testTarget(t))
	defer u.Finish(false)
	sum := strings.Repeat("a", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "%s  %s\n", sum, u.asset) }))
	defer srv.Close()
	b, _ := json.Marshal(map[string]any{"tag_name": "v1.2.3", "html_url": "https://github.com/owner/repo/releases/tag/v1.2.3", "assets": []any{
		map[string]any{"name": u.asset, "url": "https://api.github.com/repos/owner/repo/releases/assets/1", "browser_download_url": "https://github.com/wrong", "size": 123},
		map[string]any{"name": "SHA256SUMS", "url": srv.URL, "size": 100},
	}})
	r, err := u.decodeRelease(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "1.2.3" || r.Assets[u.asset].SHA256 != sum || !strings.Contains(r.Assets[u.asset].URL, "api.github.com") {
		t.Fatalf("%+v", r)
	}
	t.Setenv("RELAY_UPDATE_TOKEN", "test-private-token")
	for _, raw := range []string{"https://api.github.com/repos/o/r/releases/assets/1", "https://github.com/o/r", "http://127.0.0.1/file", "https://api.github.com.evil.test/"} {
		req, _ := http.NewRequest("GET", raw, nil)
		u.authorize(req)
		if (req.Header.Get("Authorization") != "") != (req.URL.Host == "api.github.com") {
			t.Fatal("credential leaked", raw)
		}
		if req.URL.Host == "api.github.com" && req.Header.Get("Accept") != "application/octet-stream" {
			t.Fatal("private asset request uses wrong Accept")
		}
	}
	redirect, _ := http.NewRequest("GET", "https://objects.githubusercontent.com/file", nil)
	redirect.Header.Set("Authorization", "secret")
	if err := u.client.CheckRedirect(redirect, []*http.Request{{}}); err != nil {
		t.Fatal(err)
	}
	if redirect.Header.Get("Authorization") != "" {
		t.Fatal("redirect leaked credentials")
	}
}

func TestMacBundleVerification(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS codesign")
	}
	makeApp := func(name, id string) string {
		dir := filepath.Join(t.TempDir(), name+".app")
		exe := filepath.Join(dir, "Contents", "MacOS", "relay")
		os.MkdirAll(filepath.Dir(exe), 0755)
		// A real Mach-O executable gives codesign something meaningful to verify.
		body, err := os.ReadFile("/usr/bin/true")
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(exe, body, 0755)
		plist := fmt.Sprintf(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>%s</string><key>CFBundleExecutable</key><string>relay</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>`, id)
		os.WriteFile(filepath.Join(dir, "Contents", "Info.plist"), []byte(plist), 0644)
		if out, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", dir).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return dir
	}
	old := makeApp("Relay", "dev.local.relay")
	fresh := makeApp("Relay", "dev.local.relay")
	if err := verifyBundle(context.Background(), old, fresh); err != nil {
		t.Fatal(err)
	}
	wrong := makeApp("Relay", "dev.other.app")
	if err := verifyBundle(context.Background(), old, wrong); err == nil {
		t.Fatal("wrong bundle accepted")
	}
	exe := filepath.Join(fresh, "Contents", "MacOS", "relay")
	f, _ := os.OpenFile(exe, os.O_WRONLY|os.O_APPEND, 0)
	f.WriteString("tampered")
	f.Close()
	if err := verifyBundle(context.Background(), old, fresh); err == nil {
		t.Fatal("tampered signature accepted")
	}
}

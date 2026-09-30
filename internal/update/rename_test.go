package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestRenamedReleasePreservesLegacyFeeds(t *testing.T) {
	for _, prefix := range []string{"readrig-", "relay-"} {
		t.Run(prefix, func(t *testing.T) {
			data := []byte("verified legacy feed binary")
			canonical := AssetName(runtime.GOOS, runtime.GOARCH, false, false)
			legacy := strings.Replace(canonical, "readyrig-", prefix, 1)
			var base string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/asset" {
					w.Write(data)
					return
				}
				json.NewEncoder(w).Encode(Release{Version: "1.1.0", Assets: map[string]Asset{legacy: {URL: base + "/asset", Size: int64(len(data)), SHA256: digest(data)}}})
			}))
			defer srv.Close()
			base = srv.URL
			target := testTarget(t)
			u := newAt(Options{Version: "1.0.0", Feed: base}, target)
			defer u.Finish(false)
			u.Check()
			if status := awaitCheck(t, u); status.State != "ready" {
				t.Fatalf("legacy release became unavailable: %+v", status)
			}
			if err := u.Finish(true); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(target); err != nil || string(got) != string(data) {
				t.Fatal("legacy release did not install", err)
			}
		})
	}
}

func TestLegacyGitHubChecksumsUseSelectedAsset(t *testing.T) {
	u := newAt(Options{Version: "dev"}, testTarget(t))
	defer u.Finish(false)
	legacy := strings.Replace(u.asset, "readyrig-", "relay-", 1)
	sum := strings.Repeat("b", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "%s  %s\n", sum, legacy) }))
	defer srv.Close()
	body, _ := json.Marshal(map[string]any{"tag_name": "v1.2.3", "assets": []any{
		map[string]any{"name": legacy, "browser_download_url": srv.URL + "/asset", "size": 123},
		map[string]any{"name": "SHA256SUMS", "browser_download_url": srv.URL, "size": 100},
	}})
	rel, err := u.decodeRelease(context.Background(), body)
	if err != nil || rel.Assets[legacy].SHA256 != sum {
		t.Fatal("legacy checksum was not verified", err)
	}
	rel.Assets[u.asset] = Asset{URL: "https://example.com/current"}
	if name, _, ok := u.releaseAsset(rel); !ok || name != u.asset {
		t.Fatal("current release name did not take priority")
	}
}

func TestUpdateTokenNameCompatibility(t *testing.T) {
	t.Setenv("RELAY_UPDATE_TOKEN", "legacy-test-token")
	t.Setenv("READYRIG_UPDATE_TOKEN", "current-test-token")
	if got := githubToken(context.Background()); got != "current-test-token" {
		t.Fatal("current token setting did not take priority")
	}
	if err := os.Unsetenv("READYRIG_UPDATE_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if got := githubToken(context.Background()); got != "legacy-test-token" {
		t.Fatal("legacy token setting no longer works")
	}
}

package tunnel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testArchive(t *testing.T, name string, kind byte, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: kind, Size: int64(len(content)), Mode: 0755}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOfficialDownloadDigestAndAtomicInstall(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "tampered"}[tamper], func(t *testing.T) {
			binary := []byte("verified official executable")
			asset := testArchive(t, "cloudflared", tar.TypeReg, binary)
			digest := sha256.Sum256(asset)
			metadata, _ := json.Marshal(map[string]any{"assets": []releaseAsset{{Name: "cloudflared-darwin-arm64.tgz", URL: "https://github.com/cloudflare/cloudflared/releases/download/2026.9.3/cloudflared-darwin-arm64.tgz", Size: int64(len(asset)), Digest: "sha256:" + hex.EncodeToString(digest[:])}}})
			if tamper {
				asset[len(asset)-1] ^= 1
			}
			client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				body := asset
				if r.URL.String() == releaseURL {
					body = metadata
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
			})}
			dir := t.TempDir()
			path, err := installWithClient(context.Background(), dir, client, "darwin", "arm64")
			if tamper {
				if err == nil {
					t.Fatal("tampered binary installed", path)
				}
				entries, _ := os.ReadDir(dir)
				if len(entries) != 0 {
					t.Fatal("failed download left files", entries)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, binary) {
				t.Fatal(err, string(actual))
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0700 {
				t.Fatal(info.Mode())
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatal("staging files left behind", entries)
			}
		})
	}
}

func TestArchiveDoesNotFollowLinksOrTraversal(t *testing.T) {
	for _, item := range []struct {
		name string
		kind byte
	}{{"../cloudflared", tar.TypeReg}, {"other/cloudflared", tar.TypeReg}, {"cloudflared", tar.TypeSymlink}} {
		dir := t.TempDir()
		archive := filepath.Join(dir, "asset.tgz")
		content := []byte("payload")
		if item.kind == tar.TypeSymlink {
			content = nil
		}
		if err := os.WriteFile(archive, testArchive(t, item.name, item.kind, content), 0600); err != nil {
			t.Fatal(err)
		}
		if err := extractBinary(archive, filepath.Join(dir, "binary")); err == nil {
			t.Fatal("accepted unsafe archive", item)
		}
	}
}

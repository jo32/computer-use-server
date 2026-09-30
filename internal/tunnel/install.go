package tunnel

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releaseURL = "https://api.github.com/repos/cloudflare/cloudflared/releases/latest"
const maxDownload = 100 * 1024 * 1024

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// install keeps a SHA-256 verified official binary in ReadyRig's private data
// directory. It never replaces an existing system/Homebrew installation.
func install(ctx context.Context, dir string) (string, error) {
	client := &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) > 10 {
			return errors.New("无效的官方下载重定向")
		}
		return nil
	}}
	return installWithClient(ctx, dir, client, runtime.GOOS, runtime.GOARCH)
}

func installWithClient(ctx context.Context, dir string, client *http.Client, platform, arch string) (string, error) {
	name, err := assetName(platform, arch)
	if err != nil {
		return "", err
	}
	resp, err := get(ctx, client, releaseURL)
	if err != nil {
		return "", fmt.Errorf("获取 Cloudflare 官方版本: %w", err)
	}
	var release struct {
		Assets []releaseAsset `json:"assets"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&release)
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	var asset releaseAsset
	for _, item := range release.Assets {
		if item.Name == name {
			asset = item
			break
		}
	}
	if asset.Name == "" || !strings.HasPrefix(asset.URL, "https://github.com/cloudflare/cloudflared/releases/download/") || asset.Size <= 0 || asset.Size > maxDownload {
		return "", errors.New("Cloudflare 官方发布中没有适用的程序")
	}
	expected, err := hex.DecodeString(strings.TrimPrefix(asset.Digest, "sha256:"))
	if err != nil || !strings.HasPrefix(asset.Digest, "sha256:") || len(expected) != sha256.Size {
		return "", errors.New("Cloudflare 官方发布缺少 SHA-256 校验值，请先安装 cloudflared")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(dir, "download-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	resp, err = get(ctx, client, asset.URL)
	if err != nil {
		return "", fmt.Errorf("下载 cloudflared: %w", err)
	}
	defer resp.Body.Close()
	archive := filepath.Join(stage, "asset")
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, maxDownload+1))
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n != asset.Size || hex.EncodeToString(hash.Sum(nil)) != hex.EncodeToString(expected) {
		return "", errors.New("cloudflared 下载校验失败，请重试")
	}
	binaryName := "cloudflared"
	if platform == "windows" {
		binaryName += ".exe"
	}
	binary := archive
	if strings.HasSuffix(asset.Name, ".tgz") {
		binary = filepath.Join(stage, binaryName)
		if err = extractBinary(archive, binary); err != nil {
			return "", err
		}
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Chmod(binary, 0700); err != nil {
		return "", err
	}
	destination := filepath.Join(dir, binaryName)
	if err = os.Rename(binary, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func get(ctx context.Context, client *http.Client, address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ReadyRig-Quick-Tunnel")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("官方服务器返回 HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func assetName(platform, arch string) (string, error) {
	if arch != "amd64" && arch != "arm64" {
		return "", errors.New("当前架构需要自行安装 cloudflared")
	}
	switch platform {
	case "darwin":
		return "cloudflared-darwin-" + arch + ".tgz", nil
	case "linux":
		return "cloudflared-linux-" + arch, nil
	case "windows":
		return "cloudflared-windows-" + arch + ".exe", nil
	default:
		return "", errors.New("当前系统需要自行安装 cloudflared")
	}
}

func extractBinary(archive, destination string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return errors.New("Cloudflare 下载包中缺少 cloudflared")
		}
		if err != nil {
			return err
		}
		if header.Name != "cloudflared" && header.Name != "./cloudflared" {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxDownload {
			return errors.New("cloudflared 下载包格式无效")
		}
		out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(tr, maxDownload+1))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != header.Size {
			return errors.New("cloudflared 下载包不完整")
		}
		return nil
	}
}

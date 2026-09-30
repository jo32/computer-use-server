package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (u *Manager) stageRelease(ctx context.Context, rel *Release) (staged, dir string, err error) {
	_, a, ok := u.releaseAsset(rel)
	if !ok {
		return "", "", fmt.Errorf("release %s has no %s", rel.Version, u.asset)
	}
	digest, e := hex.DecodeString(a.SHA256)
	if e != nil || len(digest) != sha256.Size {
		return "", "", errors.New("release asset has no valid SHA-256 checksum")
	}
	if a.Size <= 0 || a.Size > maxDownload {
		return "", "", errors.New("release asset size is invalid or exceeds 512 MiB")
	}
	dir, err = os.MkdirTemp(filepath.Dir(u.target), ".readyrig-update-*")
	if err != nil {
		return "", "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	path := filepath.Join(dir, "download")
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", dir, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		err = u.download(ctx, a, path)
		if err == nil || ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return "", dir, err
	}
	if !u.bundle {
		err = os.Chmod(path, 0755)
		return path, dir, err
	}
	out := filepath.Join(dir, "app")
	if err = unzip(path, out); err != nil {
		return "", dir, err
	}
	for _, name := range []string{"ReadyRig.app", "Readrig.app", "Relay.app"} {
		candidate := filepath.Join(out, name)
		if info, e := os.Stat(candidate); e == nil && info.IsDir() {
			staged = candidate
			break
		}
	}
	if staged == "" {
		return "", dir, errors.New("update contains no ReadyRig app bundle")
	}
	if err = verifyBundle(ctx, u.target, staged); err != nil {
		return "", dir, err
	}
	return staged, dir, nil
}

func (u *Manager) download(ctx context.Context, a Asset, path string) (err error) {
	r, err := u.get(ctx, a.URL)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.ContentLength > 0 && r.ContentLength != a.Size {
		return errors.New("download size differs from release metadata")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	h := sha256.New()
	done := int64(0)
	u.mu.Lock()
	u.status.Done = 0
	u.status.Total = a.Size
	u.mu.Unlock()
	reader := io.LimitReader(r.Body, a.Size+1)
	buffer := make([]byte, 64<<10)
	for {
		n, e := reader.Read(buffer)
		if n > 0 {
			done += int64(n)
			if done > a.Size {
				return errors.New("download exceeds expected size")
			}
			if _, err = io.MultiWriter(f, h).Write(buffer[:n]); err != nil {
				return err
			}
			u.mu.Lock()
			u.status.Done = done
			u.mu.Unlock()
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	if done != a.Size {
		return errors.New("download is incomplete")
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		return errors.New("download SHA-256 checksum mismatch")
	}
	if err = f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

// Reject traversal, symlinks and special files before writing any archive entry.
// ReadyRig's self-contained app bundle contains only directories and regular files.
func unzip(src, dst string) error {
	z, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer z.Close()
	var total uint64
	if len(z.File) > 10000 {
		return errors.New("update archive has too many files")
	}
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !filepath.IsLocal(name) || strings.Contains(name, "\\") || (!f.Mode().IsRegular() && !f.FileInfo().IsDir()) {
			return fmt.Errorf("unsafe update archive entry: %s", f.Name)
		}
		if f.UncompressedSize64 > 1<<30 || total > 1<<30-f.UncompressedSize64 {
			return errors.New("expanded update exceeds 1 GiB")
		}
		total += f.UncompressedSize64
	}
	for _, f := range z.File {
		// ditto may store AppleDouble metadata alongside bundle files. These
		// are not actual resources; materialising them breaks the sealed bundle.
		if strings.HasPrefix(filepath.Base(f.Name), "._") || strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		path := filepath.Join(dst, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if err = os.MkdirAll(path, 0755); err != nil {
				return err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600|f.Mode().Perm()&0155)
		if err != nil {
			r.Close()
			return err
		}
		_, err = io.Copy(w, io.LimitReader(r, int64(f.UncompressedSize64)+1))
		closeErr := w.Close()
		r.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func verifyBundle(ctx context.Context, installed, staged string) error {
	if _, err := bundleExecutable(staged); err != nil {
		return err
	}
	if b, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", staged).CombinedOutput(); err != nil {
		return fmt.Errorf("update signature verification failed: %s", strings.TrimSpace(string(b)))
	}
	identity := func(path string) (string, string, error) {
		b, err := exec.CommandContext(ctx, "/usr/bin/codesign", "-dv", path).CombinedOutput()
		if err != nil {
			return "", "", err
		}
		team, id := "", ""
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "TeamIdentifier="); ok && v != "not set" {
				team = v
			}
			if v, ok := strings.CutPrefix(line, "Identifier="); ok {
				id = v
			}
		}
		return team, id, nil
	}
	wantTeam, wantID, err := identity(installed)
	if err != nil {
		return err
	}
	gotTeam, gotID, err := identity(staged)
	if err != nil {
		return err
	}
	if wantID == "" || wantTeam != gotTeam || wantID != gotID {
		return errors.New("update must have the same signing team and bundle identifier as the installed app")
	}
	return nil
}

// Only known executable names are accepted; archive and signature checks still
// apply equally to current and legacy bundles.
func bundleExecutable(bundle string) (string, error) {
	for _, name := range []string{"readyrig", "readrig", "relay"} {
		path := filepath.Join(bundle, "Contents", "MacOS", name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", errors.New("update contains no executable ReadyRig app")
}

// Move the original aside and roll it back if the second rename fails. Both
// paths live on the same volume. Never delete a backup when rollback fails.
func swap(staged, target string) error {
	dir, err := os.MkdirTemp(filepath.Dir(target), ".readyrig-backup-*")
	if err != nil {
		return err
	}
	old := filepath.Join(dir, filepath.Base(target))
	if err = os.Rename(target, old); err != nil {
		_ = os.Remove(dir)
		return err
	}
	if err = os.Rename(staged, target); err != nil {
		if rollback := os.Rename(old, target); rollback != nil {
			return fmt.Errorf("install: %v; rollback: %v; original retained at %s", err, rollback, old)
		}
		_ = os.Remove(dir)
		return err
	}
	// Windows may keep the old executable open until this process exits.
	_ = os.RemoveAll(dir)
	return nil
}

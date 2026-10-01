// Package update stages verified releases in the background and installs them
// on orderly shutdown. The feed format and lifecycle follow yetone/magpie
// (MIT, 575a8f5fe3bba6ca22f8ec0509eb3af88100aac0); see MAGPIE-LICENSE.txt.
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const maxDownload = 512 << 20

type Asset struct {
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Release struct {
	Version string           `json:"version"`
	Notes   string           `json:"notes"`
	URL     string           `json:"url"`
	Assets  map[string]Asset `json:"assets"`
}
type Status struct {
	State      string `json:"state"`
	Current    string `json:"current"`
	Latest     string `json:"latest,omitempty"`
	Notes      string `json:"notes,omitempty"`
	URL        string `json:"url,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Error      string `json:"error,omitempty"`
	Done       int64  `json:"done"`
	Total      int64  `json:"total"`
	CanCheck   bool   `json:"can_check"`
	CanRestart bool   `json:"can_restart"`
}
type Options struct {
	Version, Feed string
	GUI, Disabled bool
}

type Manager struct {
	mu                                        sync.Mutex
	status                                    Status
	feed, exe, target, asset, stage, stageDir string
	bundle                                    bool
	self                                      os.FileInfo
	lockFile                                  *os.File
	ctx                                       context.Context
	cancel                                    context.CancelFunc
	wg                                        sync.WaitGroup
	busy, closed, restart                     bool
	restartCh                                 chan struct{}
	client                                    *http.Client
	tokenOnce                                 sync.Once
	token                                     string
}

func New(o Options) *Manager {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	u := newAt(o, exe)
	if err != nil {
		u.status.State = "disabled"
		u.status.Reason = err.Error()
		u.status.CanCheck = false
	}
	return u
}

func newAt(o Options, exe string) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	u := &Manager{feed: o.Feed, exe: exe, target: exe, ctx: ctx, cancel: cancel, restartCh: make(chan struct{}), status: Status{State: "idle", Current: o.Version}}
	u.client = &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many update redirects")
		}
		if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" {
			r.Header.Del("Authorization")
		}
		return safeURL(r.URL.String())
	}}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if runtime.GOOS == "darwin" && filepath.Ext(app) == ".app" && filepath.Base(filepath.Dir(exe)) == "MacOS" && filepath.Base(filepath.Dir(filepath.Dir(exe))) == "Contents" {
		u.bundle = true
		u.target = app
	}
	u.asset = AssetName(runtime.GOOS, runtime.GOARCH, o.GUI, u.bundle)
	switch {
	case runtime.GOOS == "darwin" && filepath.Ext(app) == ".app" && filepath.Base(filepath.Dir(exe)) == "Helpers" && filepath.Base(filepath.Dir(filepath.Dir(exe))) == "Contents":
		// Replacing one helper would invalidate the containing app's signature.
		// The bundled CLI is updated atomically with its desktop app instead.
		u.status.State = "managed"
		u.status.Reason = "此 CLI 随 ReadyRig App 更新，请在 App 中检查更新。"
	case o.Disabled:
		u.status.State = "disabled"
		u.status.Reason = "自动更新已关闭"
	case !Released(o.Version):
		u.status.State = "source"
		u.status.Reason = "开发构建，不会自动覆盖"
	case o.Feed == "":
		u.status.State = "disabled"
		u.status.Reason = "尚未配置更新源"
	default:
		if err := safeURL(o.Feed); err != nil {
			u.status.State = "disabled"
			u.status.Reason = err.Error()
			break
		}
		u.status.CanCheck = true
		if strings.Contains(filepath.ToSlash(exe), "/Cellar/") {
			u.status.Reason = "此版本由 Homebrew 管理，请使用 brew upgrade"
			break
		}
		if strings.Contains(u.target, "/AppTranslocation/") {
			u.status.Reason = "请先将 ReadyRig 移到应用程序文件夹"
			break
		}
		f, err := os.OpenFile(filepath.Join(filepath.Dir(u.target), "."+filepath.Base(u.target)+".update.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			u.status.Reason = "安装目录不可写，请手动安装更新"
			break
		}
		if err = lock(f); err != nil {
			f.Close()
			u.status.Reason = "另一个 ReadyRig 进程正在管理更新，请在该进程中操作"
			break
		}
		u.lockFile = f
		u.self, err = os.Stat(u.target)
		if err != nil {
			u.status.Reason = err.Error()
		}
	}
	return u
}

func AssetName(goos, arch string, gui, bundle bool) string {
	if bundle {
		return "readyrig-darwin-" + arch + ".zip"
	}
	name := "readyrig-web-"
	if gui {
		name = "readyrig-"
	}
	name += goos + "-" + arch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Older feeds used the previous product names. Always prefer ReadyRig assets.
func (u *Manager) releaseAsset(rel *Release) (string, Asset, bool) {
	for _, name := range []string{u.asset, strings.Replace(u.asset, "readyrig-", "readrig-", 1), strings.Replace(u.asset, "readyrig-", "relay-", 1)} {
		if asset, ok := rel.Assets[name]; ok {
			return name, asset, true
		}
	}
	return "", Asset{}, false
}

// HTTPS is required except for loopback feeds used in local release tests.
func safeURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return errors.New("invalid update URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
		return nil
	}
	return errors.New("update URLs require HTTPS (HTTP is allowed only on loopback)")
}

func (u *Manager) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.status
	s.CanRestart = u.stage != "" && !u.busy && !u.closed
	return s
}
func (u *Manager) RestartSignal() <-chan struct{} { return u.restartCh }
func (u *Manager) RequestRestart() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.restart {
		return nil
	}
	if u.stage == "" || u.busy || u.closed {
		return errors.New("更新尚未准备好")
	}
	u.restart = true
	u.status.State = "restarting"
	close(u.restartCh)
	return nil
}

func (u *Manager) Start() {
	u.mu.Lock()
	if u.closed || !u.status.CanCheck {
		u.mu.Unlock()
		return
	}
	u.wg.Add(1)
	u.mu.Unlock()
	go func() {
		defer u.wg.Done()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-u.ctx.Done():
				return
			case <-timer.C:
				u.Check()
				timer.Reset(6 * time.Hour)
			}
		}
	}()
}

// Check coalesces concurrent requests, and never holds the status lock over I/O.
func (u *Manager) Check() {
	u.mu.Lock()
	if u.closed || u.busy || u.restart || !u.status.CanCheck {
		u.mu.Unlock()
		return
	}
	u.busy = true
	u.status.State = "checking"
	u.status.Error = ""
	u.wg.Add(1)
	u.mu.Unlock()
	go func() { defer u.wg.Done(); u.check() }()
}

func (u *Manager) check() {
	ctx, cancel := context.WithTimeout(u.ctx, 10*time.Minute)
	defer cancel()
	rel, err := u.latest(ctx)
	u.mu.Lock()
	if err != nil {
		u.failLocked(err)
		u.mu.Unlock()
		return
	}
	// An already verified download remains usable if the feed fails or rolls back.
	if u.stage != "" && !Newer(rel.Version, u.status.Latest) {
		u.status.State = "ready"
		u.busy = false
		u.mu.Unlock()
		return
	}
	if !Newer(rel.Version, u.status.Current) {
		u.status.State = "latest"
		u.busy = false
		u.mu.Unlock()
		return
	}
	if u.status.Reason != "" {
		u.status.Latest = rel.Version
		u.status.Notes = rel.Notes
		u.status.URL = rel.URL
		u.status.State = "available"
		u.busy = false
		u.mu.Unlock()
		return
	}
	u.status.State = "downloading"
	u.status.Done = 0
	u.status.Total = 0
	u.mu.Unlock()
	staged, dir, err := u.stageRelease(ctx, rel)
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		u.failLocked(err)
		return
	}
	if u.stageDir != "" {
		_ = os.RemoveAll(u.stageDir)
	}
	u.stage, u.stageDir = staged, dir
	u.status.Latest = rel.Version
	u.status.Notes = rel.Notes
	u.status.URL = rel.URL
	u.status.State = "ready"
	u.busy = false
}

func (u *Manager) failLocked(err error) {
	u.status.Error = err.Error()
	u.status.State = "error"
	if u.stage != "" {
		u.status.State = "ready"
	}
	u.busy = false
}

func (u *Manager) latest(ctx context.Context) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := u.get(ctx, u.feed)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, errors.New("update feed exceeds 1 MiB")
	}
	rel, err := u.decodeRelease(ctx, b)
	if err != nil {
		return nil, err
	}
	if !Released(rel.Version) {
		return nil, errors.New("update feed has no valid release version")
	}
	if rel.URL != "" {
		if err = safeURL(rel.URL); err != nil {
			return nil, err
		}
	}
	return rel, nil
}
func (u *Manager) get(ctx context.Context, raw string) (*http.Response, error) {
	if err := safeURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ReadyRig/"+u.status.Current)
	u.authorize(req)
	r, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	if r.StatusCode != http.StatusOK {
		r.Body.Close()
		if req.URL.Host == "api.github.com" && (r.StatusCode == 401 || r.StatusCode == 404) {
			return nil, errors.New("GitHub 更新不可用：请确认已发布 Release，且 gh auth login 或 READYRIG_UPDATE_TOKEN 有权读取仓库")
		}
		return nil, fmt.Errorf("update request: %s", r.Status)
	}
	return r, nil
}

// Finish cancels pending network work, then swaps a verified download only after
// the caller has stopped its listeners, tools and database. No password prompts
// or forced restarts happen in background checks.
func (u *Manager) Finish(install bool) (err error) {
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return nil
	}
	u.closed = true
	u.cancel()
	u.mu.Unlock()
	u.wg.Wait()
	cleanup := func() {
		if u.lockFile != nil {
			unlock(u.lockFile)
			u.lockFile = nil
		}
		if u.stageDir != "" {
			_ = os.RemoveAll(u.stageDir)
		}
	}
	defer cleanup()
	if !install || u.stage == "" {
		return nil
	}
	current, e := os.Stat(u.target)
	if e != nil {
		return e
	}
	if u.self == nil || !os.SameFile(u.self, current) || current.Size() != u.self.Size() || !current.ModTime().Equal(u.self.ModTime()) {
		return errors.New("installation changed while ReadyRig was running; restart before updating")
	}
	if err = swap(u.stage, u.target); err != nil {
		return err
	}
	if u.restart {
		cleanup()
		exe := u.exe
		if u.bundle {
			// An upgrade from Readrig/Relay changes the executable basename.
			exe, err = bundleExecutable(u.target)
			if err != nil {
				return err
			}
		}
		return Reexec(exe, os.Args[1:])
	}
	return nil
}

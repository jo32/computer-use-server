package tunnel

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FixedSettings is safe to return to the local UI. Credentials are never returned.
type FixedSettings struct {
	URL      string `json:"url"`
	HasToken bool   `json:"has_token"`
	Error    string `json:"error,omitempty"`
}
type fixedConfig struct {
	URL        string `json:"url"`
	Token      string `json:"token"`
	AccessPath string `json:"access_path"`
}

var fixedHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)
var fixedToken = regexp.MustCompile(`^[A-Za-z0-9_+/=-]+$`)
var fixedPath = regexp.MustCompile(`^[A-Za-z0-9]{8}$`)

func validFixedToken(token string) bool {
	return len(token) >= 20 && len(token) <= 8192 && fixedToken.MatchString(token)
}
func normalizeFixedURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Port() != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("请输入固定 HTTPS 域名，不要包含路径、端口或登录信息")
	}
	host := strings.ToLower(u.Hostname())
	if !fixedHost.MatchString(host) || net.ParseIP(host) != nil || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".trycloudflare.com") || len(host) > 253 {
		return "", errors.New("请填写 Cloudflare 中配置的固定域名，例如 readyrig.example.com")
	}
	return "https://" + host, nil
}
func (m *Manager) loadFixed() {
	b, err := os.ReadFile(filepath.Join(m.opts.Dir, "fixed.json"))
	if os.IsNotExist(err) {
		return
	}
	var c fixedConfig
	if err != nil || json.Unmarshal(b, &c) != nil {
		m.fixedError = "固定链接配置无法读取，请重新保存配置"
		return
	}
	u, err := normalizeFixedURL(c.URL)
	if err != nil || !validFixedToken(c.Token) || !fixedPath.MatchString(c.AccessPath) {
		m.fixedError = "固定链接配置无效，请重新保存配置"
		return
	}
	c.URL = u
	m.fixed = c
}
func (m *Manager) FixedSettings() FixedSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return FixedSettings{URL: m.fixed.URL, HasToken: m.fixed.Token != "", Error: m.fixedError}
}
func (m *Manager) SaveFixed(rawURL, token string) error {
	u, err := normalizeFixedURL(rawURL)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("隧道服务已关闭")
	}
	if m.run != nil {
		return errors.New("请先关闭公网分享，再修改固定链接配置")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		token = m.fixed.Token
	}
	if !validFixedToken(token) {
		return errors.New("请输入有效的 Tunnel Token，只粘贴令牌本身，不要粘贴安装命令")
	}
	c := fixedConfig{URL: u, Token: token, AccessPath: m.fixed.AccessPath}
	if c.AccessPath == "" {
		const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		for range 8 {
			n, e := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if e != nil {
				return e
			}
			c.AccessPath += string(alphabet[n.Int64()])
		}
	}
	if err = os.MkdirAll(m.opts.Dir, 0700); err != nil {
		return errors.New("无法创建固定链接配置目录")
	}
	b, _ := json.Marshal(c)
	f, err := os.CreateTemp(m.opts.Dir, ".fixed-*")
	if err != nil {
		return errors.New("无法保存固定链接配置")
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(m.opts.Dir, "fixed.json"))
	}
	if err != nil {
		return errors.New("无法保存固定链接配置")
	}
	m.fixed = c
	if m.opts.RedactSecrets != nil {
		m.opts.RedactSecrets(c.Token, c.AccessPath)
	}
	m.fixedError = ""
	return nil
}

// FixedAccessPath authorizes the persistent path only while its tunnel is running.
func (m *Manager) FixedAccessPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.run != nil && m.run.fixed.Token != "" && m.run.registered && m.run.ctx.Err() == nil && m.status.State != "stopping" {
		return m.run.fixed.AccessPath
	}
	return ""
}

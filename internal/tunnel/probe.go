package tunnel

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// A registered connector alone does not prove its published hostname points to
// this ReadyRig instance. Check the local and public routes before offering a URL.
func (m *Manager) monitorFixed(r *operation, target string) {
	client := m.opts.ProbeClient
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
		}
		m.mu.Lock()
		check := m.run == r && r.registered && !r.verified
		version := r.connectionVersion
		m.mu.Unlock()
		if !check {
			continue
		}
		suffix := "/" + r.fixed.AccessPath + "/api/v1/tools"
		local := instanceID(r.ctx, client, strings.TrimRight(target, "/")+suffix)
		if local == "" {
			continue
		}
		public := instanceID(r.ctx, client, r.fixed.URL+suffix)
		m.mu.Lock()
		if m.run == r && r.ctx.Err() == nil && r.registered && r.connectionVersion == version && m.status.State != "stopping" {
			if public == local {
				r.verified = true
				m.publishReady(r)
			} else {
				m.status.Message = "隧道已连接，正在检查固定域名；请确认 DNS 与 HTTP 应用路由指向 " + target
			}
		}
		m.mu.Unlock()
		m.changed()
	}
}
func instanceID(ctx context.Context, client *http.Client, address string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	return resp.Header.Get("X-Readyrig-Instance")
}

// Caller holds m.mu.
func (m *Manager) publishReady(r *operation) {
	m.status.State, m.status.Message, m.status.URL = "ready", "公网分享已开启", r.host
	if !r.announced {
		r.announced = true
		close(r.ready)
	}
}

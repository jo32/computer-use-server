package tunnel

import (
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	defaultHealthInterval = 20 * time.Second
	// Used after a wake-up, a failed check or a missing network, so recovery
	// does not wait a whole interval.
	settleInterval     = 5 * time.Second
	healthFailLimit    = 2
	maxHealthFailLimit = 10
	// The host cloudflared needs to create a quick tunnel. If it does not
	// resolve there is no internet yet, and replacing the tunnel cannot help.
	networkProbeHost = "api.trycloudflare.com"
)

// SetAccessPath tells the manager which path prefix the local gateway serves,
// so a quick tunnel can check that its public address reaches this instance.
func (m *Manager) SetAccessPath(path string) {
	m.mu.Lock()
	m.quickPath = path
	m.mu.Unlock()
}

// A quick tunnel only lives while Cloudflare keeps its hostname. After the
// computer sleeps, cloudflared reconnects and logs "Registered tunnel
// connection", but the old trycloudflare.com hostname may no longer point
// here, so the manager would keep reporting a dead URL as ready. Probe the
// public address like monitorFixed does, withdraw the URL while it does not
// answer, and replace the tunnel when it does not come back.
func (m *Manager) monitorQuick(r *operation, target string) {
	m.mu.Lock()
	path := m.quickPath
	m.mu.Unlock()
	if path == "" {
		return
	}
	client := m.opts.ProbeClient
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	lookup := m.opts.Lookup
	if lookup == nil {
		lookup = func(host string) error { _, err := net.LookupHost(host); return err }
	}
	interval := m.opts.HealthInterval
	if interval <= 0 {
		interval = defaultHealthInterval
	}
	settle := min(settleInterval, interval)
	suffix := "/" + path + "/api/v1/tools"
	localURL := strings.TrimRight(target, "/") + suffix
	timer := time.NewTimer(interval)
	defer timer.Stop()
	last := time.Now().Round(0)
	fails, offline := 0, false
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-timer.C:
		}
		// Round(0) drops the monotonic reading, which stops while the computer
		// sleeps. A wall-clock gap much longer than the interval means it slept.
		now := time.Now().Round(0)
		woke := now.Sub(last) > 3*interval
		last = now
		next := interval
		if woke || offline || fails > 0 {
			next = settle
		}
		m.mu.Lock()
		active := m.run == r && r.registered && r.host != "" && r.ctx.Err() == nil && m.status.State != "stopping"
		host := r.host
		limit := min(healthFailLimit+2*r.recycles, maxHealthFailLimit)
		m.mu.Unlock()
		if !active {
			timer.Reset(next)
			continue
		}
		// If this app does not answer locally, replacing the tunnel will not help.
		local := instanceID(r.ctx, client, localURL)
		if local == "" {
			timer.Reset(next)
			continue
		}
		if lookup(networkProbeHost) != nil {
			offline = true
			timer.Reset(settle)
			continue
		}
		offline = false
		if instanceID(r.ctx, client, host+suffix) == local {
			recovered := fails > 0
			m.mu.Lock()
			r.recycles = 0
			if recovered && m.run == r && r.ctx.Err() == nil && m.status.State != "stopping" {
				r.unhealthy = false
				m.publishReady(r)
			}
			m.mu.Unlock()
			if recovered {
				m.changed()
			}
			fails = 0
			timer.Reset(interval)
			continue
		}
		fails++
		recycle := fails >= limit
		m.mu.Lock()
		if m.run == r && r.ctx.Err() == nil && m.status.State != "stopping" {
			r.unhealthy = true
			m.status.State, m.status.Message, m.status.URL = "starting", "公网链接无响应，正在重新连接…", ""
			r.recycle = recycle
		} else {
			recycle = false
		}
		m.mu.Unlock()
		m.changed()
		if recycle {
			// finish() sees r.recycle and starts a replacement in the same step.
			r.cancel()
			return
		}
		timer.Reset(settle)
	}
}

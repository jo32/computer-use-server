package tunnel

import (
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeNetwork answers the local gateway and, unless dead, the public address.
// A dead public address behaves like a trycloudflare.com hostname that
// Cloudflare no longer routes here: a 404 without the instance header.
type fakeNetwork struct{ publicDead, offline atomic.Bool }

func (f *fakeNetwork) RoundTrip(req *http.Request) (*http.Response, error) {
	h := http.Header{}
	code := 200
	if strings.HasSuffix(req.URL.Hostname(), "trycloudflare.com") && f.publicDead.Load() {
		code = 404
	} else {
		h.Set("X-Readyrig-Instance", "instance-1")
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func healthManager(t *testing.T) (*Manager, *fakeNetwork, string, func() []Status) {
	t.Helper()
	m, capture := helper(t, "ready")
	net := &fakeNetwork{}
	m.opts.ProbeClient = &http.Client{Transport: net}
	m.opts.HealthInterval = 20 * time.Millisecond
	m.opts.Lookup = func(string) error {
		if net.offline.Load() {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	var mu sync.Mutex
	var seen []Status
	m.opts.Changed = func() {
		s := m.Status()
		mu.Lock()
		seen = append(seen, s)
		mu.Unlock()
	}
	m.SetAccessPath("abcd1234")
	return m, net, capture, func() []Status {
		mu.Lock()
		defer mu.Unlock()
		return append([]Status(nil), seen...)
	}
}

func spawns(capture string) int {
	b, _ := os.ReadFile(capture)
	return strings.Count(string(b), "\n")
}

func awaitSpawns(t *testing.T, capture string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if spawns(capture) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d tunnel processes, got %d", n, spawns(capture))
}

// After sleep the old hostname stops routing here. The URL must be withdrawn
// and a new tunnel started instead of reporting "ready" forever.
func TestQuickTunnelIsReplacedWhenPublicAddressDies(t *testing.T) {
	m, net, capture, seen := healthManager(t)
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, "ready")
	net.publicDead.Store(true)
	awaitSpawns(t, capture, 2)
	net.publicDead.Store(false)
	if s := awaitState(t, m, "ready"); s.URL == "" {
		t.Fatal(s)
	}
	withdrawn := false
	for _, s := range seen() {
		if s.State == "starting" && s.URL == "" && strings.Contains(s.Message, "公网链接") {
			withdrawn = true
		}
	}
	if !withdrawn {
		t.Fatal("the dead URL was never withdrawn")
	}
}

func TestHealthyQuickTunnelIsLeftAlone(t *testing.T) {
	m, _, capture, _ := healthManager(t)
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, "ready")
	time.Sleep(300 * time.Millisecond)
	if n := spawns(capture); n != 1 || m.Status().State != "ready" {
		t.Fatal("a healthy tunnel was restarted", n, m.Status())
	}
}

// Without internet a new tunnel cannot be created, so do not tear down anything.
func TestNoRestartWhileOffline(t *testing.T) {
	m, net, capture, _ := healthManager(t)
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, "ready")
	net.offline.Store(true)
	net.publicDead.Store(true)
	time.Sleep(400 * time.Millisecond)
	if n := spawns(capture); n != 1 {
		t.Fatal("restarted while offline", n)
	}
	// Back online with a dead hostname: now it is replaced.
	net.offline.Store(false)
	awaitSpawns(t, capture, 2)
}

// A recovered address brings the URL back without starting another process.
func TestTransientFailureRestoresURL(t *testing.T) {
	m, net, capture, _ := healthManager(t)
	m.opts.HealthInterval = 100 * time.Millisecond
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, "ready")
	net.publicDead.Store(true)
	awaitState(t, m, "starting")
	net.publicDead.Store(false)
	if s := awaitState(t, m, "ready"); s.URL == "" {
		t.Fatal(s)
	}
	if n := spawns(capture); n != 1 {
		t.Fatal("transient failure replaced the tunnel", n)
	}
}

// If the user stops sharing while a replacement is pending, it stays stopped.
func TestUserStopBeatsAutomaticReplacement(t *testing.T) {
	m, net, capture, _ := healthManager(t)
	if err := m.Start("http://127.0.0.1:7332"); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, "ready")
	net.publicDead.Store(true)
	awaitState(t, m, "starting")
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if s := m.Status(); s.State != "stopped" || s.URL != "" {
		t.Fatal("restarted after a user stop", s)
	}
	if n := spawns(capture); n > 2 {
		t.Fatal("extra tunnel processes after stop", n)
	}
}

//go:build integration

package test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

const (
	// readinessBound is the api's readinessTimeout, three seconds, and a second for the answer to arrive.
	readinessBound = 4 * time.Second
	// unansweredWait is how long a probe has waited when it ran into the timeout rather than a broken connection.
	unansweredWait = 2 * time.Second
	// outageProbes is more probes than the pool has idle connections to break.
	outageProbes = 50
	// readyAgainWait bounds the pool replacing the connections the outage broke.
	readyAgainWait = 30 * time.Second
	// readyPoll is how often a wait asks /readyz again.
	readyPoll = 200 * time.Millisecond
)

// pgProxy stands between a pool and the primary. Cut, it drops what it carries and holds every new connection
// without an answer, as a primary gone off the network does.
type pgProxy struct {
	l      net.Listener
	target string

	mu    sync.Mutex
	cut   bool
	conns map[net.Conn]struct{}
	held  []net.Conn
}

func newPGProxy(t *testing.T, target string) *pgProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &pgProxy{l: l, target: target, conns: map[net.Conn]struct{}{}}
	go p.serve()
	t.Cleanup(func() {
		l.Close()
		p.restore()
		p.mu.Lock()
		for c := range p.conns {
			c.Close()
		}
		p.mu.Unlock()
	})
	return p
}

func (p *pgProxy) serve() {
	for {
		in, err := p.l.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		if p.cut {
			p.held = append(p.held, in)
			p.mu.Unlock()
			continue
		}
		p.mu.Unlock()
		out, err := net.Dial("tcp", p.target)
		if err != nil {
			in.Close()
			continue
		}
		p.mu.Lock()
		p.conns[in], p.conns[out] = struct{}{}, struct{}{}
		p.mu.Unlock()
		pipe := func(dst, src net.Conn) {
			_, _ = io.Copy(dst, src)
			dst.Close()
			src.Close()
		}
		go pipe(out, in)
		go pipe(in, out)
	}
}

func (p *pgProxy) sever() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cut = true
	for c := range p.conns {
		c.Close()
		delete(p.conns, c)
	}
}

func (p *pgProxy) restore() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cut = false
	for _, c := range p.held {
		c.Close()
	}
	p.held = nil
}

// through points a database address at the proxy instead of its host.
func (p *pgProxy) through(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = p.l.Addr().String()
	return u.String()
}

func probe(t *testing.T, base, path string) (int, map[string]any, time.Duration) {
	t.Helper()
	start := time.Now()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body, time.Since(start)
}

// /readyz follows the primary: down, it says unavailable within its timeout while /healthz stays up, and back, it
// says ready again without a restart.
func TestReadinessFollowsThePrimary(t *testing.T) {
	h := newHarness(t)
	primary, err := url.Parse(h.cfg.DB.PrimaryURL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := newPGProxy(t, primary.Host)
	cfg := h.cfg.DB
	cfg.PrimaryURL, cfg.AdminURL, cfg.ReplicaURLs = proxy.through(t, cfg.PrimaryURL), proxy.through(t, cfg.AdminURL), nil
	cluster, err := db.Open(context.Background(), cfg, discard())
	if err != nil {
		t.Fatalf("open a cluster through the proxy: %v", err)
	}
	t.Cleanup(cluster.Close)
	api := httptest.NewServer((&httpapi.Server{DB: cluster, Log: discard()}).Routes(nil))
	t.Cleanup(api.Close)

	if status, body, _ := probe(t, api.URL, "/readyz"); status != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("/readyz with the primary up = %d %v, want 200 ok", status, body)
	}

	proxy.sever()
	// The pool's idle connections fail at once; once they are gone, a new one waits on a primary that never answers,
	// and that wait is what readinessTimeout bounds.
	waited := false
	for try := 1; try <= outageProbes && !waited; try++ {
		status, body, took := probe(t, api.URL, "/readyz")
		if status != http.StatusServiceUnavailable || len(body) != 1 || body["status"] != "unavailable" {
			t.Errorf("probe %d after the cut: /readyz = %d %v, want 503 with nothing but {\"status\":\"unavailable\"}", try, status, body)
		}
		if took > readinessBound {
			t.Errorf("probe %d after the cut: /readyz took %s, want at most %s", try, took, readinessBound)
		}
		waited = took >= unansweredWait
	}
	if !waited {
		t.Errorf("none of %d probes waited on an unanswered connection, so readinessTimeout was never reached", outageProbes)
	}
	if status, body, _ := probe(t, api.URL, "/healthz"); status != http.StatusOK || body["status"] != "ok" {
		t.Errorf("/healthz with the primary unreachable = %d %v, want 200: liveness must not follow the database", status, body)
	}

	proxy.restore()
	deadline := time.Now().Add(readyAgainWait)
	for {
		status, body, _ := probe(t, api.URL, "/readyz")
		if status == http.StatusOK && body["status"] == "ok" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("/readyz still answers %d %v %s after the primary came back", status, body, readyAgainWait)
		}
		time.Sleep(readyPoll)
	}
}

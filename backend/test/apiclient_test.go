//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// apiServer is the real router over the real database, signing callers in with real sessions.
type apiServer struct {
	srv      *httptest.Server
	accounts *auth.Service
	h        *harness

	mu         sync.Mutex
	lastWriter *client
}

// cheapPasswords keep the suite fast; the costs themselves are the unit tests' business.
func cheapPasswords() auth.PasswordParams {
	return auth.PasswordParams{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

// newAPIServer starts the api in this process, its every answer checked against the document.
func newAPIServer(t *testing.T, h *harness) *apiServer {
	t.Helper()
	accounts := auth.NewService(h.cluster, cheapPasswords(), time.Hour)
	a := &apiServer{accounts: accounts, h: h}
	server := &httpapi.Server{
		DB: h.cluster, Log: discard(), Auth: accounts, Accounts: accounts,
		Audit: audit.NewService(h.cluster), AuditRetention: config.DefaultRetainAudit,
		Fresh: h.freshness(t), CookieName: h.cfg.Auth.SessionCookie,
	}
	a.srv = httptest.NewServer(observed(t, server.Routes(nil)))
	t.Cleanup(a.srv.Close)
	return a
}

// handOver waits for the replica when somebody reads after another person
// wrote: only the writer's own reads are promised the write at once.
func (a *apiServer) handOver(t *testing.T, c *client, method string) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastWriter != nil && a.lastWriter != c {
		a.h.settle(t)
		a.lastWriter = nil
	}
	if method != http.MethodGet && method != http.MethodHead {
		a.lastWriter = c
	}
}

// client is somebody calling the API from a browser of their own, whose cookies it keeps.
type client struct {
	api  *apiServer
	user uuid.UUID
	http *http.Client
}

func cookieJarClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// as opens a session for a member in their organization, the row a sign-in
// writes, and hands its cookie to a browser of their own.
func (a *apiServer) as(t *testing.T, user, org uuid.UUID) *client {
	t.Helper()
	var token string
	_, err := a.h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		_, token, err = auth.OpenSession(ctx, tx, user, &org, auth.ProofPassword, time.Now().Add(time.Hour), "", "")
		return err
	})
	if err != nil {
		t.Fatalf("open a session: %v", err)
	}
	c := &client{api: a, user: user, http: cookieJarClient()}
	base, _ := url.Parse(a.srv.URL)
	c.http.Jar.SetCookies(base, []*http.Cookie{{Name: a.h.cfg.Auth.SessionCookie, Value: token, Path: "/"}})
	return c
}

func (a *apiServer) anonymous() *client { return &client{api: a, http: cookieJarClient()} }

type response struct {
	Status int
	Header http.Header
	Body   map[string]any
	Raw    []byte
}

func (c *client) call(t *testing.T, method, path string, body any) response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, c.api.srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.api.handOver(t, c, method)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := response{Status: resp.StatusCode, Header: resp.Header, Raw: raw}
	if len(raw) > 0 && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

func (c *client) get(t *testing.T, path string) response { return c.call(t, http.MethodGet, path, nil) }
func (c *client) post(t *testing.T, path string, body any) response {
	return c.call(t, http.MethodPost, path, body)
}
func (c *client) patch(t *testing.T, path string, body any) response {
	return c.call(t, http.MethodPatch, path, body)
}
func (c *client) delete(t *testing.T, path string) response {
	return c.call(t, http.MethodDelete, path, nil)
}

// want fails the test unless the answer has the status, and returns it.
func want(t *testing.T, got response, status int, what string) response {
	t.Helper()
	if got.Status != status {
		t.Fatalf("%s: got %d, want %d: %s", what, got.Status, status, got.Raw)
	}
	return got
}

// obj walks into the body by keys, failing when a step is not an object.
func obj(t *testing.T, r response, keys ...string) map[string]any {
	t.Helper()
	var cur any = r.Body
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("%v is not an object in %s", keys, r.Raw)
		}
		cur = m[k]
	}
	m, ok := cur.(map[string]any)
	if !ok {
		t.Fatalf("%v is not an object in %s", keys, r.Raw)
	}
	return m
}

func list(t *testing.T, r response, key string) []any {
	t.Helper()
	l, ok := r.Body[key].([]any)
	if !ok {
		t.Fatalf("%s is not a list in %s", key, r.Raw)
	}
	return l
}

// errorCode reads the envelope and checks it is a sentence, as every refusal is.
func errorCode(t *testing.T, r response) string {
	t.Helper()
	e, ok := r.Body["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope in %s", r.Raw)
	}
	if msg, _ := e["message"].(string); msg == "" || msg[len(msg)-1] != '.' {
		t.Errorf("the message %q is not a sentence", msg)
	}
	code, _ := e["code"].(string)
	return code
}

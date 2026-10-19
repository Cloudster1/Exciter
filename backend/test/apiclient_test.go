//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/rand"
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

	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/baseline"
	"github.com/cloudster1/exciter/backend/internal/codetrace"
	"github.com/cloudster1/exciter/backend/internal/comment"
	"github.com/cloudster1/exciter/backend/internal/config"
	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/dashboard"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/duplicates"
	"github.com/cloudster1/exciter/backend/internal/exporttemplate"
	"github.com/cloudster1/exciter/backend/internal/glossary"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/label"
	"github.com/cloudster1/exciter/backend/internal/notify"
	"github.com/cloudster1/exciter/backend/internal/oauth"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/quality"
	"github.com/cloudster1/exciter/backend/internal/render"
	"github.com/cloudster1/exciter/backend/internal/reqifio"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
	"github.com/cloudster1/exciter/backend/internal/risk"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
	"github.com/cloudster1/exciter/backend/internal/testexec"
	"github.com/cloudster1/exciter/backend/internal/testresult"
	"github.com/cloudster1/exciter/backend/internal/theme"
	"github.com/cloudster1/exciter/backend/internal/trace"
	"github.com/cloudster1/exciter/backend/internal/watch"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// apiServer is the real router over the real database and the real bucket, signing callers in with real sessions.
type apiServer struct {
	srv *httptest.Server
	// URL is where the server listens, and sso signs people in through the realm with it as the callback's home.
	URL      string
	accounts *auth.Service
	sso      *oidc.Service
	store    objectstore.Store
	themes   *theme.Service
	// files is the server's attachment service, whose reaper a test runs.
	files *attachment.Service
	h     *harness

	mu         sync.Mutex
	lastWriter *client
}

// testUploadLimit keeps the file that is too large small enough to send quickly.
const testUploadLimit = 64 << 10

// cheapPasswords keep the suite fast; the costs themselves are the unit tests' business.
func cheapPasswords() auth.PasswordParams {
	return auth.PasswordParams{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

// newAPIServer starts the api in this process, its every answer checked against the document.
func newAPIServer(t *testing.T, h *harness) *apiServer {
	t.Helper()
	return newAPIServerWith(t, h, nil)
}

// newAPIServerWith is newAPIServer with the server set up further by configure, when given, before it starts.
func newAPIServerWith(t *testing.T, h *harness, configure func(*apiServer, *httpapi.Server)) *apiServer {
	t.Helper()
	key := make([]byte, secret.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}
	// Unstarted, so the callback address can name the port before any request arrives.
	srv := httptest.NewUnstartedServer(nil)
	base := "http://" + srv.Listener.Addr().String()
	callback := base + httpapi.APIPrefix + "/auth/oidc/callback"
	accounts := auth.NewService(h.cluster, cheapPasswords(), time.Hour)
	store := h.bucket(t)
	requirements := requirement.NewService(h.cluster).WithCoverage(trace.CoverageJudge{})
	links, finder := trace.NewService(h.cluster), search.NewService(h.cluster)
	a := &apiServer{srv: srv, URL: base, accounts: accounts, sso: oidc.NewService(h.cluster, box, callback), store: store,
		themes: theme.NewService(h.cluster, store), files: attachment.NewService(h.cluster, store, requirements).WithMaxSize(testUploadLimit), h: h}
	server := &httpapi.Server{
		DB: h.cluster, Log: discard(), Auth: accounts, Accounts: accounts, OIDC: a.sso, OIDCCallbackURL: callback,
		Audit: audit.NewService(h.cluster), AuditRetention: config.DefaultRetainAudit, Installation: installation.NewService(h.cluster), Themes: a.themes, Projects: project.NewService(h.cluster).WithSeeder(risk.SeedProject), Perms: perm.NewService(h.cluster),
		Requirements: requirements, Search: finder,
		Attributes: attribute.NewService(h.cluster),
		Workflows:  workflow.NewService(h.cluster), Labels: label.NewService(h.cluster),
		Trace: links, Attachments: a.files, Imports: csvio.NewService(h.cluster, requirements, links, finder),
		TestResults:     testresult.NewService(h.cluster).WithCommits(h.armature(t)),
		Code:            codetrace.NewService(h.cluster),
		Quality:         quality.NewService(h.cluster, requirement.LocateForQuality),
		Glossary:        glossary.NewService(h.cluster),
		Duplicates:      duplicates.NewService(h.cluster),
		Dashboards:      dashboard.NewService(h.cluster, finder, links),
		ReqIF:           reqifio.NewService(h.cluster, requirements, attribute.NewService(h.cluster), links, a.files, store),
		Documents:       specdoc.NewService(h.cluster, requirements, baseline.NewService(h.cluster)),
		ExportTemplates: exporttemplate.NewService(h.cluster, store),
		Tests:           testexec.NewService(h.cluster, store).WithIssues(h.armature(t), "http://exciter.test"),
		Risks:           risk.NewService(h.cluster),
		Renderer:        h.renderer(),
		Armature:        h.armature(t),
		Stator:          h.stator(t),
		Webhooks:        h.webhooks(t),
		Comments:        comment.NewService(h.cluster),
		Watches:         watch.NewService(h.cluster),
		Notifications:   notify.NewService(h.cluster),
		Reviews:         review.NewService(h.cluster).WithPasswords(accounts),
		Baselines:       baseline.NewService(h.cluster).WithPasswords(accounts),
		OAuth:           oauth.NewService(h.cluster, "http://"+appHost, httpapi.APIPrefix, nil),
		Fresh:           h.freshness(t), CookieName: h.cfg.Auth.SessionCookie, AppBaseURL: "http://" + appHost,
	}
	if configure != nil {
		configure(a, server)
	}
	srv.Config.Handler = observed(t, server.Routes(nil))
	srv.Start()
	t.Cleanup(srv.Close)
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
	// bearer is the personal access token a script sends instead of a cookie; empty for a browser.
	bearer string
}

func cookieJarClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// as opens a session for a member in their organization, the row a sign-in
// writes, and hands its cookie to a browser of their own.
func (a *apiServer) as(t *testing.T, user uuid.UUID) *client {
	t.Helper()
	var token string
	_, err := a.h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		_, token, err = auth.OpenSession(ctx, tx, user, auth.ProofPassword, time.Now().Add(time.Hour), "", "")
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

// withToken is a script calling the API as user with a personal access token, without any cookie.
func (a *apiServer) withToken(user uuid.UUID, secret string) *client {
	return &client{api: a, user: user, http: &http.Client{}, bearer: secret}
}

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
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	c.api.handOver(t, c, method)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return decoded(resp, raw)
}

// decoded is an answer with its JSON body read, when it has one.
func decoded(resp *http.Response, raw []byte) response {
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
func (c *client) put(t *testing.T, path string, body any) response {
	return c.call(t, http.MethodPut, path, body)
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

// renderer is the stack's render service, or none when the suite runs without one.
func (h *harness) renderer() render.Renderer {
	if h.cfg.Render.URL == "" {
		return render.Unavailable{}
	}
	return render.New(h.cfg.Render.URL, render.Options{Timeout: h.cfg.Render.Timeout, Concurrency: h.cfg.Render.Concurrency, MaxSize: h.cfg.Render.MaxSize})
}

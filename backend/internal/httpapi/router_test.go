package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/observability"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }
func (f fakeDB) Stats() db.Stats            { return db.Stats{ReadsToPrimary: 7} }

func newServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		DB:          fakeDB{},
		Log:         slog.New(slog.DiscardHandler),
		CookieName:  "exciter_session",
		AppBaseURL:  "http://app.test",
		CheckOrigin: true,
	}
}

func serve(t *testing.T, h http.Handler, req *http.Request) (*http.Response, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	resp := rec.Result()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("%s %s answered something that is not JSON: %q", req.Method, req.URL.Path, raw)
		}
	}
	return resp, body
}

// errorOf reads the one error envelope and checks it is a sentence with an id.
func errorOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope in %v", body)
	}
	msg, _ := e["message"].(string)
	if msg == "" || !strings.HasSuffix(msg, ".") || strings.ToUpper(msg[:1]) != msg[:1] {
		t.Errorf("message %q is not a sentence", msg)
	}
	if id, _ := e["requestId"].(string); id == "" {
		t.Errorf("the envelope carries no request id: %v", e)
	}
	return e
}

func TestProbes(t *testing.T) {
	s := newServer(t)
	h := s.Routes(nil)

	resp, body := serve(t, h, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if resp.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("/healthz = %d %v", resp.StatusCode, body)
	}

	resp, body = serve(t, h, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if resp.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("/readyz = %d %v", resp.StatusCode, body)
	}
	if routing, _ := body["routing"].(map[string]any); routing["readsToPrimary"] != float64(7) {
		t.Errorf("/readyz does not report routing: %v", body)
	}

	s.DB = fakeDB{err: errors.New("connection refused")}
	resp, body = serve(t, s.Routes(nil), httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if resp.StatusCode != http.StatusServiceUnavailable || body["status"] != "unavailable" {
		t.Fatalf("/readyz with the database down = %d %v", resp.StatusCode, body)
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
		t.Error("a probe answered HTML")
	}
}

func TestOpenAPIIsServed(t *testing.T) {
	resp, body := serve(t, newServer(t).Routes(nil), httptest.NewRequest(http.MethodGet, APIPrefix+"/openapi.json", nil))
	if resp.StatusCode != http.StatusOK || body["openapi"] != "3.1.0" {
		t.Fatalf("openapi.json = %d %v", resp.StatusCode, body["openapi"])
	}
	if info, _ := body["info"].(map[string]any); info["title"] != ProductName {
		t.Errorf("the document is titled %v, want %s, which Armature and Stator check", info["title"], ProductName)
	}
}

// Armature and Stator tell Exciter apart from any other address by this answer.
func TestMetaNamesTheProductAndItsBuild(t *testing.T) {
	resp, body := serve(t, newServer(t).Routes(nil), httptest.NewRequest(http.MethodGet, APIPrefix+"/meta", nil))
	if resp.StatusCode != http.StatusOK || body["product"] != ProductName {
		t.Fatalf("meta = %d %v", resp.StatusCode, body)
	}
	if build, _ := body["build"].(map[string]any); build["version"] == "" || build["version"] == nil {
		t.Errorf("meta names no version: %v", body)
	}
}

func TestEveryAnswerCarriesTheEdgeHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	resp, _ := serve(t, newServer(t).Routes(nil), req)
	if got := resp.Header.Get("X-Request-Id"); got != "abc-123" {
		t.Errorf("request id = %q, want the caller's echoed", got)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", strings.Repeat("x", maxRequestIDLength+1))
	resp, _ = serve(t, newServer(t).Routes(nil), req)
	if _, err := uuid.Parse(resp.Header.Get("X-Request-Id")); err != nil {
		t.Errorf("an oversized request id was echoed rather than replaced: %q", resp.Header.Get("X-Request-Id"))
	}
}

func TestUnknownRoutesAnswerInTheEnvelope(t *testing.T) {
	h := newServer(t).Routes(nil)
	resp, body := serve(t, h, httptest.NewRequest(http.MethodGet, APIPrefix+"/nowhere", nil))
	if resp.StatusCode != http.StatusNotFound || errorOf(t, body)["code"] != "not_found" {
		t.Fatalf("unknown path = %d %v", resp.StatusCode, body)
	}
	resp, body = serve(t, h, httptest.NewRequest(http.MethodDelete, "/healthz", nil))
	if resp.StatusCode != http.StatusMethodNotAllowed || errorOf(t, body)["code"] != "method_not_allowed" {
		t.Fatalf("wrong method = %d %v", resp.StatusCode, body)
	}
}

func TestAPanicIsA500InTheEnvelope(t *testing.T) {
	h := requestID(logging(slog.New(slog.DiscardHandler))(recovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))))
	resp, body := serve(t, h, httptest.NewRequest(http.MethodGet, "/", nil))
	if resp.StatusCode != http.StatusInternalServerError || errorOf(t, body)["code"] != "internal_error" {
		t.Fatalf("panic = %d %v", resp.StatusCode, body)
	}
}

// A browser talked into a write from another site carries the cookie but not
// the right origin, and cannot send JSON from a plain form.
func TestSameSiteRefusesCrossSiteCookieWrites(t *testing.T) {
	h := newServer(t).Routes(nil)
	cookie := &http.Cookie{Name: "exciter_session", Value: "whatever"}

	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/openapi.json", nil)
	req.AddCookie(cookie)
	req.Header.Set("Origin", "http://evil.test")
	resp, body := serve(t, h, req)
	if resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "cross_site" {
		t.Fatalf("foreign origin = %d %v", resp.StatusCode, body)
	}

	req = httptest.NewRequest(http.MethodPost, APIPrefix+"/openapi.json", strings.NewReader("a=b"))
	req.AddCookie(cookie)
	req.Header.Set("Origin", "http://app.test")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, body = serve(t, h, req)
	if resp.StatusCode != http.StatusUnsupportedMediaType || errorOf(t, body)["code"] != "unsupported_media_type" {
		t.Fatalf("form post = %d %v", resp.StatusCode, body)
	}

	// A bearer token was never talked into anything, whatever the origin.
	req = httptest.NewRequest(http.MethodPost, APIPrefix+"/openapi.json", nil)
	req.Header.Set("Authorization", "Bearer x")
	req.Header.Set("Origin", "http://evil.test")
	resp, _ = serve(t, h, req)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("a token write was stopped at the edge: %d", resp.StatusCode)
	}
}

func TestRequestsAreCountedByRoutePattern(t *testing.T) {
	tel, err := observability.Setup(t.Context(), observability.Config{Service: "test"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(t)
	s.Telemetry = tel
	h := s.Routes(nil)
	serve(t, h, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	serve(t, h, httptest.NewRequest(http.MethodGet, "/made-up/path", nil))

	rec := httptest.NewRecorder()
	tel.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	scrape := rec.Body.String()
	for _, want := range []string{
		`exciter_http_requests_total{method="GET",route="/healthz",status="200"} 1`,
		`exciter_http_requests_total{method="GET",route="unmatched",status="404"} 1`,
	} {
		if !strings.Contains(scrape, want) {
			t.Errorf("the scrape lacks %s", want)
		}
	}
}

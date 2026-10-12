package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// spyFreshness is a tracker that also says which keys were asked for.
type spyFreshness struct {
	mu    sync.Mutex
	lsns  map[string]db.LSN
	asked []string
}

func (f *spyFreshness) Note(_ context.Context, key string, lsn db.LSN) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lsns[key] = max(f.lsns[key], lsn)
}

func (f *spyFreshness) Required(_ context.Context, key string) db.LSN {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, key)
	return f.lsns[key]
}

func freshServer(t *testing.T) (*Server, *spyFreshness) {
	t.Helper()
	s := newServer(t)
	f := &spyFreshness{lsns: map[string]db.LSN{}}
	s.Fresh = f
	return s, f
}

// writing notes a position, the way a handler does after a write.
func writing(lsn db.LSN) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noteWrite(r.Context(), lsn)
		w.WriteHeader(http.StatusNoContent)
	})
}

func clientCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == ClientCookie {
			return c
		}
	}
	return nil
}

func httptestRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestAWriteWithoutASessionIsKeyedByAClientCookie(t *testing.T) {
	s, f := freshServer(t)
	h := s.readYourWrites(writing(42))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil))
	cookie := clientCookie(rec.Result())
	if cookie == nil || !validClientID(cookie.Value) || !cookie.HttpOnly {
		t.Fatalf("a first write should be handed an http-only client cookie, got %+v", cookie)
	}
	if got := f.lsns["c:"+cookie.Value]; got != 42 {
		t.Fatalf("the write was noted as %v under the client cookie, want 42: %v", got, f.lsns)
	}

	read := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	read.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.readYourWrites(http.NotFoundHandler()).ServeHTTP(rec, read)
	if len(f.asked) != 2 || f.asked[1] != "c:"+cookie.Value {
		t.Fatalf("the next read did not ask for the client's position: %v", f.asked)
	}
	if clientCookie(rec.Result()) != nil {
		t.Error("a client that has a cookie was handed another")
	}
}

func TestAReadWithNoIdentityIsLeftAlone(t *testing.T) {
	s, f := freshServer(t)
	rec := httptest.NewRecorder()
	s.readYourWrites(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil))
	if len(f.asked) != 0 || clientCookie(rec.Result()) != nil {
		t.Fatalf("a read with nothing to key it by asked %v and set %v", f.asked, rec.Result().Cookies())
	}
}

func TestAForgedClientCookieIsReplaced(t *testing.T) {
	s, f := freshServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
	req.AddCookie(&http.Cookie{Name: ClientCookie, Value: "exciter:anything"})
	rec := httptest.NewRecorder()
	s.readYourWrites(writing(9)).ServeHTTP(rec, req)
	cookie := clientCookie(rec.Result())
	if cookie == nil || f.lsns["c:"+cookie.Value] != 9 {
		t.Fatalf("a malformed cookie should be replaced by a fresh one: %v, %v", cookie, f.lsns)
	}
	if _, ok := f.lsns["c:exciter:anything"]; ok {
		t.Fatal("a malformed cookie became a key in the store")
	}
}

func TestWithoutATrackerNothingIsKept(t *testing.T) {
	s := newServer(t)
	rec := httptest.NewRecorder()
	s.readYourWrites(writing(3)).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if clientCookie(rec.Result()) != nil {
		t.Fatal("a server without a tracker handed out a client cookie")
	}
}

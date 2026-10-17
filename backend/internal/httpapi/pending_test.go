package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// Every operation agreed and not built yet tells a member so in the one error
// envelope, and tells somebody who has not signed in nothing more than a built one would.
func TestEveryPendingOperationAnswersNotImplemented(t *testing.T) {
	s := newServer(t)
	s.Auth = fakeAuth{"member": {UserID: uuid.New(), Role: auth.RoleMember}}
	router := s.Routes(nil)
	pending := 0
	for _, op := range operations {
		if !op.pending {
			continue
		}
		pending++
		path := op.address(pathParamPattern.ReplaceAllString(op.path, uuid.NewString()))
		signedIn := httptestRequest(op.method, path, `{}`)
		signedIn.Header.Set("Authorization", "Bearer member")
		resp, body := serve(t, router, signedIn)
		if resp.StatusCode != http.StatusNotImplemented || errorOf(t, body)["code"] != "not_implemented" {
			t.Errorf("%s %s = %d %v, want 501 not_implemented", op.method, op.path, resp.StatusCode, body)
		}
		resp, _ = serve(t, router, httptestRequest(op.method, path, `{}`))
		if want := http.StatusUnauthorized; !op.public && resp.StatusCode != want {
			t.Errorf("%s %s answered %d to nobody, want %d", op.method, op.path, resp.StatusCode, want)
		}
	}
	if pending == 0 {
		t.Skip("nothing is pending")
	}
}

// address is where a row's path is served: under the API's prefix, or outside it for a root row.
func (op operation) address(path string) string {
	if op.root {
		return path
	}
	return APIPrefix + path
}

// A row outside the prefix is served there and described there, or a client of the document would call
// /api/v1/.well-known/... and find nothing.
func TestARootOperationIsServedAndDescribedOutsideThePrefix(t *testing.T) {
	doc := Spec()
	for _, op := range operations {
		described := doc.Paths[op.path][strings.ToLower(op.method)]
		outside := len(described.Servers) == 1 && described.Servers[0].URL == "/"
		if op.root != outside {
			t.Errorf("%s %s: root %v, but the document puts it at %v", op.method, op.path, op.root, described.Servers)
		}
	}
	resp, _ := serve(t, newServer(t).Routes(nil), httptestRequest(http.MethodGet, "/healthz", ""))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz = %d, want 200 at the root", resp.StatusCode)
	}
}

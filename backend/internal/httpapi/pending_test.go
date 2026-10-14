package httpapi

import (
	"net/http"
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
		path := APIPrefix + pathParamPattern.ReplaceAllString(op.path, uuid.NewString())
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

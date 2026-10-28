package tokenexchange

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An answer that is no bearer token, a redirect, or more than the bounded answer is the partner being away rather
// than a refusal, so the sign-in says it did not answer and a refresh is tried again later.
func TestWhatIsNoTokenAnswerIsThePartnerBeingAway(t *testing.T) {
	token := `{"access_token":"a","token_type":"Bearer","expires_in":3600,"refresh_token":"r","scope":"read"}`
	padded := `{"access_token":"a","token_type":"Bearer","expires_in":3600,"refresh_token":"r","scope":"read","padding":"` +
		strings.Repeat("x", maxAnswerBytes) + `"}`
	for _, tc := range []struct {
		name   string
		status int
		header map[string]string
		body   string
	}{
		{"a token of another type", http.StatusOK, nil, strings.Replace(token, "Bearer", "mac", 1)},
		{"a redirect", http.StatusFound, nil, token},
		{"a redirect to a sign-in page", http.StatusFound, map[string]string{"Location": "/sign-in"}, ""},
		{"an answer past the bound", http.StatusOK, nil, padded},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A redirect followed lands on a page, as a proxy's sign-in is.
			if r.URL.Path != TokenPath {
				_, _ = w.Write([]byte("<html>Sign in</html>"))
				return
			}
			for k, v := range tc.header {
				w.Header().Set(k, v)
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		tokens, err := Exchange(context.Background(), srv.Client(), App{BaseURL: srv.URL}, "kc")
		srv.Close()
		var refused *Refusal
		if !errors.Is(err, ErrUnreachable) || errors.As(err, &refused) || tokens != nil {
			t.Errorf("%s: %+v %v, want unreachable", tc.name, tokens, err)
		}
	}
	// The same answer within the bound is taken, so the bound and not the padding refused the one above.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(token)) }))
	defer srv.Close()
	if tokens, err := Exchange(context.Background(), srv.Client(), App{BaseURL: srv.URL}, "kc"); err != nil || tokens.Access != "a" {
		t.Errorf("a token answer reads %+v %v", tokens, err)
	}
}

// A partner that refuses without a reason still gets a sentence that ends.
func TestARefusalWithoutAReasonSaysSo(t *testing.T) {
	why := Why("Armature", &Refusal{Code: "unauthorized_client"})
	want := "Armature does not let Exciter connect people at sign-in yet: it gave no reason. Connect with a token below meanwhile."
	if why != want {
		t.Errorf("unauthorized_client without a description reads %q, want %q", why, want)
	}
	if why := Why("Stator", &Refusal{Code: "invalid_grant", Description: "  "}); !strings.Contains(why, "Stator did not connect you at sign-in: it gave no reason.") {
		t.Errorf("a blank description reads %q", why)
	}
}

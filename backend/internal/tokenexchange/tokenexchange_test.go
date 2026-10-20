package tokenexchange

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The exchange is posted as Armature's docs/oauth.md asks: a form, the app in HTTP Basic with both parts
// form-encoded first, and the subject as an access token.
func TestTheExchangeIsPostedAsTheContractAsks(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"armature_oat_a","token_type":"Bearer","expires_in":3600,"refresh_token":"armature_ort_a","scope":"read issue.write"}`))
	}))
	defer srv.Close()
	before := time.Now()
	tokens, err := Exchange(context.Background(), srv.Client(), App{BaseURL: srv.URL + "/", ClientID: "exciter app", ClientSecret: "s:e/cret"}, "kc-token")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != TokenPath || got.Method != http.MethodPost || got.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("posted %s %s as %s", got.Method, got.URL.Path, got.Header.Get("Content-Type"))
	}
	if id, secret, ok := got.BasicAuth(); !ok || id != "exciter+app" || secret != "s%3Ae%2Fcret" {
		t.Errorf("basic auth %q %q %v, want both form-encoded", id, secret, ok)
	}
	if got.PostForm.Get("grant_type") != GrantType || got.PostForm.Get("subject_token") != "kc-token" ||
		got.PostForm.Get("subject_token_type") != TokenTypeAccess || got.PostForm.Has("scope") || got.PostForm.Has("client_secret") {
		t.Errorf("form %v", got.PostForm)
	}
	if tokens.Access != "armature_oat_a" || tokens.Refresh != "armature_ort_a" || !tokens.Has("issue.write") {
		t.Errorf("tokens %+v", tokens)
	}
	if tokens.ExpiresAt.Before(before.Add(time.Hour)) || tokens.ExpiresAt.After(time.Now().Add(time.Hour)) {
		t.Errorf("expires at %v, want an hour from the request", tokens.ExpiresAt)
	}
}

func TestARefreshSendsTheRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != GrantRefresh || r.PostForm.Get("refresh_token") != "old" {
			t.Errorf("form %v", r.PostForm)
		}
		_, _ = w.Write([]byte(`{"access_token":"new","token_type":"bearer","expires_in":60,"refresh_token":"newer","scope":"read"}`))
	}))
	defer srv.Close()
	tokens, err := Refresh(context.Background(), srv.Client(), App{BaseURL: srv.URL}, "old")
	if err != nil || tokens.Refresh != "newer" || tokens.Has("issue.write") {
		t.Fatalf("%+v %v", tokens, err)
	}
}

// What the partner answers decides what Exciter does next: a refusal is the partner's sentence, an ended grant is
// a disconnection, and anything else is the partner being away, tried again later.
func TestAnswersAreToldApart(t *testing.T) {
	for _, tc := range []struct {
		status      int
		body        string
		ended       bool
		unreachable bool
		code        string
	}{
		{400, `{"error":"invalid_grant","error_description":"This connection has ended. Connect Armature again."}`, true, false, "invalid_grant"},
		{400, `{"error":"unauthorized_client","error_description":"No Keycloak client is named"}`, false, false, "unauthorized_client"},
		{401, `{"error":"invalid_client","error_description":"Wrong secret."}`, false, false, "invalid_client"},
		{404, `not json`, false, false, "refused"},
		{429, `{}`, false, true, ""},
		{502, `{}`, false, true, ""},
		{200, `{"access_token":"a","token_type":"Bearer","expires_in":3600,"scope":"read"}`, false, false, "no_refresh_token"},
		{200, `{"token_type":"Bearer"}`, false, true, ""},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := Exchange(context.Background(), srv.Client(), App{BaseURL: srv.URL}, "kc")
		srv.Close()
		var r *Refusal
		switch {
		case tc.unreachable:
			if !errors.Is(err, ErrUnreachable) {
				t.Errorf("%d %s: %v, want unreachable", tc.status, tc.body, err)
			}
		case !errors.As(err, &r) || r.Code != tc.code || r.Ended() != tc.ended:
			t.Errorf("%d %s: %v, want %s", tc.status, tc.body, err, tc.code)
		}
	}
}

func TestEveryReasonIsASentenceNamingThePartner(t *testing.T) {
	for _, err := range []error{
		ErrUnreachable,
		&Refusal{Code: "invalid_client"},
		&Refusal{Code: "unauthorized_client", Description: "Name the Keycloak client"},
		&Refusal{Code: "invalid_grant", Description: "Ada has no Armature account yet."},
		&Refusal{Code: "invalid_grant"},
	} {
		why := Why("Armature", err)
		if !strings.HasPrefix(why, "Armature ") || !strings.HasSuffix(why, ".") || strings.Contains(why, "..") {
			t.Errorf("%v reads %q", err, why)
		}
	}
}

func TestAGrantIsRefreshedAMinuteBeforeItRunsOut(t *testing.T) {
	now := time.Now()
	if Due(now.Add(2*RefreshMargin), now) || !Due(now.Add(RefreshMargin/2), now) || !Due(now.Add(-time.Hour), now) {
		t.Error("Due does not keep the margin")
	}
}

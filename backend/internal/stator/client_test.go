package stator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/secret"
)

// Stator itself is not here, so these stand-ins answer as it does; the
// integration suite runs the same client against the contract-tested stub.

func loopback() *Client { return NewClient(netguard.ParseAllow("127.0.0.0/8"), nil) }

func stator(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestMeReadsWhomATokenActsAs(t *testing.T) {
	user := uuid.New()
	srv := stator(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" || r.Header.Get("Authorization") != "Bearer stator_pat_good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"user":{"id":"` + user.String() + `","name":"Alice","email":"alice@example.com","locale":"en","showInReaders":true},
			"organization":{"name":"Stator","role":"member","guestSpace":null}}`))
	})
	me, err := loopback().As(srv.URL+"/", "stator_pat_good").Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.User.ID != user || me.User.Name != "Alice" || me.User.Email != "alice@example.com" {
		t.Errorf("me = %+v", me)
	}
	if _, err := loopback().As(srv.URL, "stator_pat_bad").Me(context.Background()); !errors.Is(err, ErrRejected) || StatusOf(err) != StatusRejected {
		t.Errorf("a 401 is %v, want ErrRejected", err)
	}
}

func TestStatorsAnswersBecomeStatusesAndRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   Status
		code   string
	}{
		{"a server error", 502, `{}`, StatusUnreachable, ""},
		{"too many requests", 429, `{}`, StatusUnreachable, ""},
		{"a refusal", 403, `{"error":{"code":"forbidden","message":"You may not edit this page."}}`, StatusUnreachable, "forbidden"},
		{"no such page", 404, `{"error":{"code":"not_found","message":"No such page."}}`, StatusUnreachable, "not_found"},
		{"not JSON", 200, `<html>`, StatusUnreachable, ""},
	} {
		srv := stator(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})
		var out map[string]any
		err := loopback().As(srv.URL, "stator_pat_x").Get(context.Background(), "/pages/x", nil, &out)
		if StatusOf(err) != tc.want {
			t.Errorf("%s: status %s, want %s (%v)", tc.name, StatusOf(err), tc.want, err)
		}
		var refused *RefusedError
		if tc.code != "" && (!errors.As(err, &refused) || refused.Code != tc.code || refused.Status != tc.status) {
			t.Errorf("%s: %v is not Stator's own refusal", tc.name, err)
		}
	}
}

func TestAnAnswerIsReadOnlyUpToItsBound(t *testing.T) {
	srv := stator(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`"` + strings.Repeat("x", MaxResponseBytes) + `"`))
	})
	var out string
	if err := loopback().As(srv.URL, "stator_pat_x").Get(context.Background(), "/pages/x", nil, &out); !errors.Is(err, ErrUnreachable) {
		t.Errorf("an answer past MaxResponseBytes was read: %v", err)
	}
}

// Every call passes the guard: without the operator's word, a Stator on the
// server's own network is not reached, and says so as unreachable.
func TestTheGuardStandsInFrontOfEveryCall(t *testing.T) {
	reached := false
	srv := stator(t, func(http.ResponseWriter, *http.Request) { reached = true })
	err := NewClient(netguard.ParseAllow(""), nil).As(srv.URL, "stator_pat_x").Get(context.Background(), "/auth/me", nil, nil)
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, netguard.ErrBlocked) || reached {
		t.Errorf("a call inside the network went out: %v, reached %v", err, reached)
	}
	if _, err := NewClient(netguard.ParseAllow(""), nil).Describe(context.Background(), srv.URL); !errors.Is(err, netguard.ErrBlocked) || reached {
		t.Errorf("describing an address inside the network went out: %v, reached %v", err, reached)
	}
}

// The backchannel reaches a public origin at another address, with the public
// Host header, as EXCITER_STATOR_BACKCHANNEL maps it.
func TestTheBackchannelRewritesThePublicOrigin(t *testing.T) {
	var host string
	srv := stator(t, func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		_, _ = w.Write([]byte(`{"product":"stator","build":{"version":"1"},"apiVersion":"1","capabilities":[]}`))
	})
	client := NewClient(netguard.ParseAllow("127.0.0.0/8"), map[string]string{"http://stator.example:8080": srv.URL})
	if _, err := client.Describe(context.Background(), "http://stator.example:8080"); err != nil {
		t.Fatal(err)
	}
	if host != "stator.example:8080" {
		t.Errorf("Host = %q, want the public name", host)
	}
}

// A token never follows a redirect to another host.
func TestATokenStaysWithItsHost(t *testing.T) {
	var leaked string
	elsewhere := stator(t, func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	})
	srv := stator(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(elsewhere.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	})
	client := NewClient(netguard.ParseAllow("127.0.0.0/8, ::1/128, localhost"), nil)
	var out map[string]any
	if err := client.As(srv.URL, "stator_pat_secret").Get(context.Background(), "/auth/me", nil, &out); err != nil {
		t.Fatal(err)
	}
	if leaked != "" {
		t.Errorf("the token went to another host: %q", leaked)
	}
}

func TestDescribeKnowsStatorFromEverythingElse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       error
	}{
		{"Stator", `{"product":"stator","build":{"version":"1"},"apiVersion":"1","capabilities":[]}`, 200, nil},
		{"Stator in capitals", `{"product":"Stator","build":{"version":"1"},"apiVersion":"1","capabilities":[]}`, 200, nil},
		{"Armature", `{"product":"armature","build":{"version":"1"},"apiVersion":"1","capabilities":[]}`, 200, ErrNotStator},
		{"a web page", `<html></html>`, 200, ErrNotStator},
		{"nothing there", `{"error":{"code":"not_found","message":"No."}}`, 404, ErrNotStator},
		{"a failing Stator", `{}`, 503, ErrUnreachable},
	} {
		srv := stator(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/meta" || r.Header.Get("Authorization") != "" {
				w.WriteHeader(http.StatusTeapot)
				return
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})
		_, err := loopback().Describe(context.Background(), srv.URL)
		if (tc.want == nil && err != nil) || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

// Stator names a capability with its version after an @ (Stator's #409); any version of the name counts.
func TestACapabilityIsKnownByItsName(t *testing.T) {
	for _, tc := range []struct {
		listed []string
		want   bool
	}{
		{nil, false},
		{[]string{"webhooks@1"}, false},
		{[]string{RemoteLinksCapability}, true},
		{[]string{"webhooks@1", RemoteLinksCapability + "@2"}, true},
		{[]string{RemoteLinksCapability + "s@1"}, false},
	} {
		if got := (&Meta{Capabilities: tc.listed}).Offers(RemoteLinksCapability); got != tc.want {
			t.Errorf("%v offers remote links: %v, want %v", tc.listed, got, tc.want)
		}
	}
}

func TestABaseURLIsAnOriginOnly(t *testing.T) {
	prod := &Service{opts: Options{Allow: netguard.ParseAllow("stator-stub")}}
	dev := &Service{opts: Options{Development: true}}
	for _, tc := range []struct {
		in, want string
		s        *Service
	}{
		{"https://Stator.Example.com/", "https://stator.example.com", prod},
		{" https://stator.example.com:8443 ", "https://stator.example.com:8443", prod},
		{"http://stator-stub:8080", "http://stator-stub:8080", prod},
		{"http://localhost:20009", "http://localhost:20009", dev},
		{"http://stator.example.com", "", prod},
		{"https://stator.example.com/api/v1", "", prod},
		{"https://user@stator.example.com", "", prod},
		{"https://stator.example.com?x=1", "", prod},
		{"https://stator.example.com#top", "", prod},
		{"ftp://stator.example.com", "", prod},
		{"stator.example.com", "", prod},
	} {
		got, err := tc.s.normalizeBaseURL(tc.in)
		var field *FieldError
		switch {
		case tc.want == "" && (!errors.As(err, &field) || field.Field != "baseUrl"):
			t.Errorf("%q was taken as %q, %v", tc.in, got, err)
		case tc.want != "" && got != tc.want:
			t.Errorf("%q = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

// A token is sealed for its installation and person: moved onto another
// member's row, or another installation's, it does not open.
func TestASealedTokenOpensOnlyForItsOwner(t *testing.T) {
	box, err := secret.New([]byte(strings.Repeat("k", secret.KeySize)))
	if err != nil {
		t.Fatal(err)
	}
	installation, alice, bob := uuid.New(), uuid.New(), uuid.New()
	sealed, err := box.Seal([]byte("stator_pat_x"), tokenContext(installation, alice))
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := box.Open(sealed, tokenContext(installation, alice)); err != nil || string(plain) != "stator_pat_x" {
		t.Errorf("the owner's token did not open: %q, %v", plain, err)
	}
	armatureContext := append(append([]byte("armature.token:"), installation[:]...), alice[:]...)
	for _, context := range [][]byte{tokenContext(installation, bob), tokenContext(uuid.New(), alice), webhookContext(installation), armatureContext} {
		if _, err := box.Open(sealed, context); !errors.Is(err, secret.ErrTampered) {
			t.Errorf("a token opened under another context: %v", err)
		}
	}
	var none *secret.Box
	s := &Service{box: none}
	var field *FieldError
	if _, err := s.seal("stator_pat_x", tokenContext(installation, alice), "token"); !errors.As(err, &field) || field.Field != "token" {
		t.Errorf("without a key, storing a token is %v, want a sentence on token", err)
	}
}

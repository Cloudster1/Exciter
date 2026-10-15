package armature

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

// Armature itself is not here, so these stand-ins answer as it does; the
// integration suite runs the same client against the contract-tested stub.

func loopback() *Client { return NewClient(netguard.ParseAllow("127.0.0.0/8"), nil) }

func armature(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestMeReadsWhomATokenActsAs(t *testing.T) {
	user, org := uuid.New(), uuid.New()
	srv := armature(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" || r.Header.Get("Authorization") != "Bearer armature_pat_good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"principal":{"user":{"id":"` + user.String() + `","name":"Alice","email":"alice@example.com"},
			"org":{"id":"` + org.String() + `","slug":"acme","name":"Acme"}},"limits":{"uploadBytes":1},
			"token":{"id":"` + uuid.NewString() + `","name":"Exciter","scopes":["` + ScopeRead + `"],"createdAt":"2026-09-01T09:00:00Z","projects":[]}}`))
	})
	me, err := loopback().As(srv.URL+"/", "armature_pat_good").Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.User.ID != user || me.User.Name != "Alice" || me.OrgID != org || !me.ReadOnly {
		t.Errorf("me = %+v", me)
	}
	if _, err := loopback().As(srv.URL, "armature_pat_bad").Me(context.Background()); !errors.Is(err, ErrRejected) || StatusOf(err) != StatusRejected {
		t.Errorf("a 401 is %v, want ErrRejected", err)
	}
}

func TestArmaturesAnswersBecomeStatusesAndRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   Status
		code   string
	}{
		{"a server error", 502, `{}`, StatusUnreachable, ""},
		{"too many requests", 429, `{}`, StatusUnreachable, ""},
		{"a refusal", 403, `{"error":{"code":"forbidden","message":"You may not file issues in CP."}}`, StatusUnreachable, "forbidden"},
		{"a bad query", 400, `{"error":{"code":"bad_query","message":"Nope.","position":7}}`, StatusUnreachable, "bad_query"},
		{"not JSON", 200, `<html>`, StatusUnreachable, ""},
	} {
		srv := armature(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})
		var out map[string]any
		err := loopback().As(srv.URL, "armature_pat_x").Get(context.Background(), "/issues", nil, &out)
		if StatusOf(err) != tc.want {
			t.Errorf("%s: status %s, want %s (%v)", tc.name, StatusOf(err), tc.want, err)
		}
		var refused *RefusedError
		if tc.code != "" && (!errors.As(err, &refused) || refused.Code != tc.code || refused.Status != tc.status) {
			t.Errorf("%s: %v is not Armature's own refusal", tc.name, err)
		}
		if tc.code == "bad_query" && (refused.Position == nil || *refused.Position != 7) {
			t.Errorf("the position of a bad query was lost: %+v", refused)
		}
	}
}

func TestAnAnswerIsReadOnlyUpToItsBound(t *testing.T) {
	srv := armature(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`"` + strings.Repeat("x", MaxResponseBytes) + `"`))
	})
	var out string
	if err := loopback().As(srv.URL, "armature_pat_x").Get(context.Background(), "/issues", nil, &out); !errors.Is(err, ErrUnreachable) {
		t.Errorf("an answer past MaxResponseBytes was read: %v", err)
	}
}

// Every call passes the guard: without the operator's word, an Armature on
// the server's own network is not reached, and says so as unreachable.
func TestTheGuardStandsInFrontOfEveryCall(t *testing.T) {
	reached := false
	srv := armature(t, func(http.ResponseWriter, *http.Request) { reached = true })
	err := NewClient(netguard.ParseAllow(""), nil).As(srv.URL, "armature_pat_x").Get(context.Background(), "/auth/me", nil, nil)
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, netguard.ErrBlocked) || reached {
		t.Errorf("a call inside the network went out: %v, reached %v", err, reached)
	}
}

// The backchannel reaches a public origin at another address, with the public
// Host header, as EXCITER_ARMATURE_BACKCHANNEL maps it.
func TestTheBackchannelRewritesThePublicOrigin(t *testing.T) {
	var host string
	srv := armature(t, func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		_, _ = w.Write([]byte(`{"product":"armature","capabilities":[]}`))
	})
	client := NewClient(netguard.ParseAllow("127.0.0.0/8"), map[string]string{"http://armature.example:8080": srv.URL})
	if _, err := client.Describe(context.Background(), "http://armature.example:8080"); err != nil {
		t.Fatal(err)
	}
	if host != "armature.example:8080" {
		t.Errorf("Host = %q, want the public name", host)
	}
}

// A token never follows a redirect to another host.
func TestATokenStaysWithItsHost(t *testing.T) {
	var leaked string
	elsewhere := armature(t, func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	})
	srv := armature(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(elsewhere.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	})
	client := NewClient(netguard.ParseAllow("127.0.0.0/8, ::1/128, localhost"), nil)
	var out map[string]any
	if err := client.As(srv.URL, "armature_pat_secret").Get(context.Background(), "/auth/me", nil, &out); err != nil {
		t.Fatal(err)
	}
	if leaked != "" {
		t.Errorf("the token went to another host: %q", leaked)
	}
}

func TestDescribeKnowsArmatureFromEverythingElse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       error
	}{
		{"Armature", `{"product":"armature","build":{"version":"1"},"apiVersion":"1","capabilities":["issue-embed@1"]}`, 200, nil},
		{"Armature in capitals", `{"product":"Armature","build":{"version":"1"},"apiVersion":"1","capabilities":[]}`, 200, nil},
		{"Stator", `{"product":"stator","build":{"version":"1"},"apiVersion":"1","capabilities":[]}`, 200, ErrNotArmature},
		{"another API", `{"info":{"title":"Another"}}`, 200, ErrNotArmature},
		{"a web page", `<html></html>`, 200, ErrNotArmature},
		{"nothing there", `{"error":{"code":"not_found","message":"No."}}`, 404, ErrNotArmature},
		{"a failing Armature", `{}`, 503, ErrUnreachable},
	} {
		srv := armature(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestABaseURLIsAnOriginOnly(t *testing.T) {
	prod := &Service{opts: Options{Allow: netguard.ParseAllow("armature-stub")}}
	dev := &Service{opts: Options{Development: true}}
	for _, tc := range []struct {
		in, want string
		s        *Service
	}{
		{"https://Armature.Example.com/", "https://armature.example.com", prod},
		{" https://armature.example.com:8443 ", "https://armature.example.com:8443", prod},
		{"http://armature-stub:8080", "http://armature-stub:8080", prod},
		{"http://localhost:20008", "http://localhost:20008", dev},
		{"http://armature.example.com", "", prod},
		{"https://armature.example.com/api/v1", "", prod},
		{"https://user@armature.example.com", "", prod},
		{"https://armature.example.com?x=1", "", prod},
		{"https://armature.example.com#top", "", prod},
		{"ftp://armature.example.com", "", prod},
		{"armature.example.com", "", prod},
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

// A token is sealed for its organization and person: moved onto another
// member's row, or another organization's, it does not open.
func TestASealedTokenOpensOnlyForItsOwner(t *testing.T) {
	box, err := secret.New([]byte(strings.Repeat("k", secret.KeySize)))
	if err != nil {
		t.Fatal(err)
	}
	org, alice, bob := uuid.New(), uuid.New(), uuid.New()
	sealed, err := box.Seal([]byte("armature_pat_x"), tokenContext(org, alice))
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := box.Open(sealed, tokenContext(org, alice)); err != nil || string(plain) != "armature_pat_x" {
		t.Errorf("the owner's token did not open: %q, %v", plain, err)
	}
	for _, context := range [][]byte{tokenContext(org, bob), tokenContext(uuid.New(), alice), webhookContext(org)} {
		if _, err := box.Open(sealed, context); !errors.Is(err, secret.ErrTampered) {
			t.Errorf("a token opened under another context: %v", err)
		}
	}
	var none *secret.Box
	s := &Service{box: none}
	var field *FieldError
	if _, err := s.seal("armature_pat_x", tokenContext(org, alice), "token"); !errors.As(err, &field) || field.Field != "token" {
		t.Errorf("without a key, storing a token is %v, want a sentence on token", err)
	}
}

func TestAnIdentityBelongsToTheConnectedArmature(t *testing.T) {
	learned := uuid.New()
	e := &endpoint{BaseURL: "https://armature.example"}
	if !e.owns(&Me{OrgID: uuid.New()}) || !e.owns(&Me{}) || e.owns(nil) {
		t.Error("before an id is learned, any token the address answers belongs")
	}
	e.ArmatureOrgID = &learned
	if !e.owns(&Me{OrgID: learned}) || e.owns(&Me{OrgID: uuid.New()}) {
		t.Error("once learned, the id decides")
	}
	if !e.owns(&Me{}) {
		t.Error("an Armature that reports no organization has only its address to go by")
	}
}

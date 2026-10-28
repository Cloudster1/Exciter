//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/tokenexchange"
)

// Connecting people to Stator at sign-in (#274) on its own terms (#327): stator/exchange.go is a copy of Armature's
// code, so its refusal, its forgetting and its renewing are tried for Stator alone.

// renewDelay is how long the renewing partner takes over a refresh, long enough for a second read to arrive meanwhile.
const renewDelay = 500 * time.Millisecond

// A wrong secret at Stator is Stator's sentence and leaves Armature's exchange standing; a new address and
// disconnecting each forget a grant from a sign-in, and the next sign-in connects again.
func TestStatorsExchangeAtSignInStandsOnItsOwn(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	h.keepStator(t)
	statorOffersItsOwn(t)
	kc := newKeycloak(t)
	api := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, api, kc.issuer(), false)
	h.letIn(t, aliceEmail, "admin")
	alice := h.userID(t, aliceEmail)
	forget := func() { h.cleanupExec(t, h.super, `DELETE FROM partner_exchange WHERE user_id = $1`, alice) }
	forget()
	t.Cleanup(forget)
	owner := api.as(t, h.owner(t, "stator-exchange").user)
	tenant := stubTenant(t)
	for _, reset := range []func(){
		func() { stubControl(t, http.MethodDelete, tenant, nil) },
		func() { statorStub(t, http.MethodDelete, tenant, nil) },
	} {
		reset()
		t.Cleanup(reset)
	}
	clientID := "exciter-" + tenant
	app := map[string]any{"clientId": clientID, "clientSecret": exchangeSecret, "keycloakClientId": exciterClient}
	stubControl(t, http.MethodPut, tenant+"/oauth-app", app)
	statorStub(t, http.MethodPut, tenant+"/oauth-app", app)
	connection := map[string]any{"clientId": clientID, "clientSecret": exchangeSecret}
	want(t, owner.put(t, "/api/v1/armature/connection", merged(connection, "baseUrl", armatureURL(t))), http.StatusOK, "connect Armature as the app")
	want(t, owner.put(t, "/api/v1/stator/connection", merged(connection, "baseUrl", statorURL(t))), http.StatusOK, "connect Stator as the app")
	account := func(t *testing.T, b *browser, partner string) (map[string]any, map[string]any) {
		t.Helper()
		got := b.getJSON(t, "/api/v1/"+partner+"/account")["account"].(map[string]any)
		signIn, _ := got["signIn"].(map[string]any)
		return got, signIn
	}
	tokensOfAlice := func() int { return h.countRows(t, `SELECT count(*) FROM stator_token WHERE user_id = $1`, alice) }
	connectedAtSignIn := func(t *testing.T, base string) {
		t.Helper()
		b := signedIn(t, api, "alice", alicePassword)
		if got, signIn := account(t, b, "stator"); got["source"] != "sign_in" || got["status"] != "ok" || got["baseUrl"] != base || signIn["connected"] != true {
			t.Errorf("after signing in alice's Stator account reads %v", got)
		}
	}

	t.Run("a wrong client secret at Stator is Stator's sentence, and Armature's exchange stands", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "clientSecret": "not-the-secret"}), http.StatusOK, "a wrong secret at Stator")
		b := signedIn(t, api, "alice", alicePassword)
		got, signIn := account(t, b, "stator")
		if got["connected"] != false || signIn["connected"] != false ||
			signIn["message"] != tokenexchange.Why("Stator", &tokenexchange.Refusal{Code: "invalid_client"}) {
			t.Errorf("after Stator refused the app alice's Stator account reads %v", got)
		}
		if arm, armSignIn := account(t, b, "armature"); arm["source"] != "sign_in" || arm["status"] != "ok" || armSignIn["connected"] != true {
			t.Errorf("Armature's exchange went with Stator's refusal: %v", arm)
		}
		if grants := stubGrants(t, statorStub, tenant); grants["alice"] != nil {
			t.Errorf("Stator holds a grant for alice although it refused the app: %v", grants)
		}
		if n := tokensOfAlice(); n != 0 {
			t.Errorf("alice holds %d Stator tokens after the refusal", n)
		}
		want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "clientSecret": exchangeSecret}), http.StatusOK, "the right secret again")
	})

	t.Run("a new address forgets the grant of a sign-in, and the next sign-in connects again", func(t *testing.T) {
		connectedAtSignIn(t, statorURL(t))
		elsewhere := os.Getenv(envStatorStub)
		moved := obj(t, want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": elsewhere}), http.StatusOK, "move the connection"), "connection")
		if moved["connected"] != float64(0) || moved["exchangeOffered"] != true {
			t.Errorf("the moved connection reads %v", moved)
		}
		if n := tokensOfAlice(); n != 0 {
			t.Errorf("alice keeps %d Stator tokens from her sign-in after the move", n)
		}
		connectedAtSignIn(t, elsewhere)
		want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t)}), http.StatusOK, "move the connection back")
	})

	t.Run("disconnecting forgets the grant of a sign-in, and the next sign-in connects again", func(t *testing.T) {
		b := signedIn(t, api, "alice", alicePassword)
		if got, _ := account(t, b, "stator"); got["source"] != "sign_in" {
			t.Fatalf("alice is not connected by her sign-in: %v", got)
		}
		if resp, body := b.send(t, http.MethodDelete, api.URL+httpapi.APIPrefix+"/stator/account/token"); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("disconnecting answered %d %s", resp.StatusCode, body)
		}
		if n := tokensOfAlice(); n != 0 {
			t.Errorf("alice keeps %d Stator tokens after disconnecting", n)
		}
		h.settle(t)
		if got, _ := account(t, b, "stator"); got["connected"] != false || got["status"] != "not_connected" {
			t.Errorf("after disconnecting alice's account reads %v", got)
		}
		connectedAtSignIn(t, statorURL(t))
	})
}

// renewingPartner exchanges for an access token that is due at once and renews it after renewDelay, counting renewals.
type renewingPartner struct {
	refreshes atomic.Int32
	url       string
}

func newRenewingPartner(t *testing.T) *renewingPartner {
	t.Helper()
	p := &renewingPartner{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == tokenexchange.TokenPath && r.FormValue("grant_type") == tokenexchange.GrantType:
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "renew_access_0", "token_type": "Bearer", "expires_in": 1,
				"refresh_token": "renew_refresh_0", "scope": "read write", "issued_token_type": tokenexchange.TokenTypeAccess})
		case r.URL.Path == tokenexchange.TokenPath && r.FormValue("grant_type") == tokenexchange.GrantRefresh:
			n := strconv.Itoa(int(p.refreshes.Add(1)))
			time.Sleep(renewDelay)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "renew_access_" + n, "token_type": "Bearer", "expires_in": 3600,
				"refresh_token": "renew_refresh_" + n, "scope": "read write"})
		case r.URL.Path == "/api/v1/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"id": "0195f000-0000-7000-8000-0000000000a1", "name": "Ada", "email": "ada@partner.example"}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	p.url = srv.URL
	return p
}

// Two reads at once while a grant from a sign-in is due renew it once: a rotated refresh token presented twice would
// end the grant at Stator.
func TestReadsAtOnceRenewAStatorGrantOnce(t *testing.T) {
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "stator-renew-once").user)
	var svc *faultyService
	for _, each := range faultyServices(t, h) {
		if each.partner == tokenexchange.PartnerStator {
			svc = &each
		}
	}
	p := newRenewingPartner(t)
	svc.connect(t, h, owner, &faultyPartner{url: p.url})
	user := h.addPerson(t, "member")
	svc.exchange(context.Background(), user)
	var connected bool
	if err := h.super.QueryRow(context.Background(), `SELECT connected FROM partner_exchange WHERE partner = $1 AND user_id = $2`,
		tokenexchange.PartnerStator, user).Scan(&connected); err != nil || !connected {
		t.Fatalf("the sign-in did not connect: %v", err)
	}
	h.settle(t)

	ctx := db.WithUser(context.Background(), user)
	statuses, errs := make([]string, 2), make([]error, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i], errs[i] = svc.viewer(ctx)
		}()
	}
	wg.Wait()
	for i := range statuses {
		if errs[i] != nil || statuses[i] != "ok" {
			t.Errorf("read %d answers %q %v, want ok", i+1, statuses[i], errs[i])
		}
	}
	if n := p.refreshes.Load(); n != 1 {
		t.Errorf("two reads at once renewed the grant %d times, want once", n)
	}
	if status, err := svc.viewer(ctx); err != nil || status != "ok" || p.refreshes.Load() != 1 {
		t.Errorf("a read after the renewal answers %q %v and renewed %d times in all", status, err, p.refreshes.Load())
	}
}

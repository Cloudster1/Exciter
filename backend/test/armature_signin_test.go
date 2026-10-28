//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tokenexchange"
)

// Every sentence the sign-in's exchange can keep for Armature (#326), and a grant from a sign-in renewed once by two
// reads at once, refused without being ended, and given to an app that may only read.

const (
	// signInSlack is what a sign-in may take past SignInTimeout to keep why the exchange gave up.
	signInSlack = time.Second
	// grantTTLSeconds is an access token's life at the stub, long enough that a renewed one is not due again within
	// the test, which RefreshMargin would make it at once below a minute.
	grantTTLSeconds = 600
)

// otherOrgPartner exchanges any subject and answers /auth/me as somebody of an organization of its own.
func otherOrgPartner(t *testing.T) *faultyPartner {
	t.Helper()
	org := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case tokenexchange.TokenPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "other_access", "token_type": "Bearer", "expires_in": grantTTLSeconds,
				"refresh_token": "other_refresh", "scope": "read issue.write", "issued_token_type": tokenexchange.TokenTypeAccess})
		case "/api/v1/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"principal": map[string]any{"user": map[string]any{"id": uuid.NewString(), "name": "Ada", "email": "ada@other.example"}, "org": map[string]any{"id": org}},
				"token":     map[string]any{"scopes": []string{"read", "issue.write"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &faultyPartner{url: srv.URL}
}

// signInSentence is the sentence the person's last sign-in kept for Armature, as their account page reads it.
func signInSentence(t *testing.T, c *client) (account, signIn map[string]any) {
	t.Helper()
	account = obj(t, want(t, c.get(t, "/api/v1/armature/account"), http.StatusOK, "the account after the sign-in"), "account")
	signIn, _ = account["signIn"].(map[string]any)
	if signIn == nil {
		t.Fatalf("the account keeps no outcome of the sign-in: %v", account)
	}
	return account, signIn
}

func TestEverySignInSentenceForArmatureIsKept(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "arm-sentences").user)
	armatureFaults := faultyServices(t, h)[0]

	t.Run("an Armature that does not offer the exchange is not asked", func(t *testing.T) {
		var offered []string
		for _, c := range armatureStubOffers(t) {
			if c != tokenexchange.Capability {
				offered = append(offered, c)
			}
		}
		stubControl(t, http.MethodPut, "capabilities", map[string]any{"capabilities": offered})
		saved := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "clientId": "exciter-sentences", "clientSecret": exchangeSecret}),
			http.StatusOK, "connect an Armature without the exchange"), "connection")
		if saved["exchangeOffered"] != false {
			t.Fatalf("the connection reads %v", saved)
		}
		person := h.addPerson(t, "member")
		h.armature(t).ExchangeAtSignIn(context.Background(), person, "subject")
		h.settle(t)
		account, signIn := signInSentence(t, api.as(t, person))
		says := "The connected Armature does not offer " + tokenexchange.Capability + ", so Exciter cannot connect you there at sign-in. Connect with a token below, or ask an administrator to update Armature."
		if signIn["connected"] != false || signIn["message"] != says || account["connected"] != false {
			t.Errorf("without the exchange the account reads %v", account)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE user_id = $1`, person); n != 0 {
			t.Errorf("%d tokens were stored", n)
		}
		stubControl(t, http.MethodPut, "capabilities", map[string]any{"capabilities": nil})
	})

	t.Run("an exchanged token of another organization is not stored", func(t *testing.T) {
		partner := otherOrgPartner(t)
		armatureFaults.connect(t, h, owner, partner)
		h.cleanupExec(t, h.super, `UPDATE armature_connection SET armature_org_id = $1`, uuid.New())
		h.settle(t)
		person := h.addPerson(t, "member")
		armatureFaults.exchange(context.Background(), person)
		h.settle(t)
		_, signIn := signInSentence(t, api.as(t, person))
		says := "Armature answered the sign-in for another organization than the one connected at " + partner.url + ", so you were not connected there. Ask an administrator to check the connection."
		if signIn["connected"] != false || signIn["message"] != says {
			t.Errorf("a token of another organization reads %v", signIn)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE user_id = $1`, person); n != 0 {
			t.Errorf("%d tokens of another organization were stored", n)
		}
	})

	t.Run("a slow Armature costs the sign-in at most its timeout, and the account says so", func(t *testing.T) {
		partner := newFaultyPartner(t, tokenexchange.SignInTimeout+2*time.Second, http.StatusUnauthorized)
		armatureFaults.connect(t, h, owner, partner)
		person := h.addPerson(t, "member")
		started := time.Now()
		armatureFaults.exchange(context.Background(), person)
		if took := time.Since(started); took < tokenexchange.SignInTimeout || took > tokenexchange.SignInTimeout+signInSlack {
			t.Errorf("the sign-in's exchange took %s, want about %s", took, tokenexchange.SignInTimeout)
		}
		h.settle(t)
		account, signIn := signInSentence(t, api.as(t, person))
		if signIn["connected"] != false || signIn["message"] != tokenexchange.Why("Armature", context.DeadlineExceeded) || account["connected"] != false {
			t.Errorf("after a slow Armature the account reads %v", account)
		}
	})
}

// armatureStubOffers is what the stub's /meta lists, which a test narrows.
func armatureStubOffers(t *testing.T) []string {
	t.Helper()
	resp, err := http.Get(os.Getenv(envArmatureStub) + armature.APIPath + "/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var meta struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil || !slices.Contains(meta.Capabilities, tokenexchange.Capability) {
		t.Fatalf("the stub offers %v: %v", meta.Capabilities, err)
	}
	return meta.Capabilities
}

func TestAGrantFromASignInIsRenewedOnceAndRefusedAsArmatureSays(t *testing.T) {
	key := projectKey(t, "AGR")
	h := newHarness(t)
	h.keepArmature(t)
	h.keepStator(t)
	kc := newKeycloak(t)
	api := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, api, kc.issuer(), false)
	h.letIn(t, aliceEmail, "admin")
	alice := h.userID(t, aliceEmail)
	forget := func() { h.cleanupExec(t, h.super, `DELETE FROM partner_exchange WHERE user_id = $1`, alice) }
	forget()
	t.Cleanup(forget)
	owner := api.as(t, h.owner(t, "arm-renewals").user)
	tenant := stubTenant(t)
	clientID := "exciter-" + tenant
	reset := func() { stubControl(t, http.MethodDelete, tenant, nil) }
	reset()
	t.Cleanup(reset)
	app := map[string]any{"clientId": clientID, "clientSecret": exchangeSecret, "keycloakClientId": exciterClient, "accessTtlSeconds": grantTTLSeconds}
	// appWith is the app with one field changed at the stub, which ends every grant of it there.
	appWith := func(field string, value any) map[string]any {
		out := map[string]any{field: value}
		for k, v := range app {
			if k != field {
				out[k] = v
			}
		}
		return out
	}
	stubControl(t, http.MethodPut, tenant+"/oauth-app", app)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "clientId": clientID, "clientSecret": exchangeSecret}),
		http.StatusOK, "connect Armature as the app")
	b := signedIn(t, api, "alice", alicePassword)
	if account := b.getJSON(t, "/api/v1/armature/account")["account"].(map[string]any); account["source"] != "sign_in" || account["status"] != "ok" {
		t.Fatalf("alice's account after the sign-in reads %v", account)
	}
	svc := h.armature(t)
	asAlice := db.WithUser(context.Background(), alice)
	due := func() {
		t.Helper()
		h.cleanupExec(t, h.super, `UPDATE armature_token SET access_expires_at = now() WHERE user_id = $1`, alice)
		h.settle(t)
	}
	refreshes := func() float64 {
		t.Helper()
		g := stubGrants(t, armatureStub, tenant)["alice"]
		if g == nil {
			t.Fatalf("alice holds no grant at the stub: %v", stubGrants(t, armatureStub, tenant))
		}
		return g["refreshes"].(float64)
	}

	t.Run("two reads at once renew a due grant once, and it stands", func(t *testing.T) {
		before := refreshes()
		due()
		var (
			wg       sync.WaitGroup
			start    = make(chan struct{})
			statuses = make([]armature.Status, 2)
			errs     = make([]error, 2)
		)
		for i := range statuses {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, statuses[i], errs[i] = svc.Viewer(asAlice)
			}()
		}
		close(start)
		wg.Wait()
		for i := range statuses {
			if statuses[i] != armature.StatusOK || errs[i] != nil {
				t.Errorf("read %d at once answered %s %v", i+1, statuses[i], errs[i])
			}
		}
		if after := refreshes(); after != before+1 {
			t.Errorf("two reads at once refreshed the grant %v times, want once", after-before)
		}
		h.settle(t)
		status, found, err := svc.Lookup(asAlice, []string{"CP-1"})
		if err != nil || status != armature.StatusOK || len(found) != 1 || found[0].Issue == nil {
			t.Errorf("after the renewal a read answers %s %v %v", status, found, err)
		}
	})

	t.Run("a refresh refused for the app's secret marks the grant rejected and waits", func(t *testing.T) {
		stubControl(t, http.MethodPut, tenant+"/oauth-app", appWith("clientSecret", "rotated-at-armature"))
		due()
		failedAt := func() string {
			t.Helper()
			var status, at string
			if err := h.super.QueryRow(t.Context(), `SELECT status, coalesce(refresh_failed_at::text, '') FROM armature_token WHERE user_id = $1`, alice).Scan(&status, &at); err != nil {
				t.Fatal(err)
			}
			if status != "rejected" || at == "" {
				t.Errorf("after the refused refresh the token is %s, failed at %q", status, at)
			}
			return at
		}
		if _, status, err := svc.Viewer(asAlice); status != armature.StatusRejected || err != nil {
			t.Errorf("the refused refresh answers %s %v", status, err)
		}
		first := failedAt()
		h.settle(t)
		if account := b.getJSON(t, "/api/v1/armature/account")["account"].(map[string]any); account["status"] != "rejected" || account["source"] != "sign_in" {
			t.Errorf("after the refused refresh the account reads %v", account)
		}
		if _, status, err := svc.Viewer(asAlice); status != armature.StatusRejected || err != nil {
			t.Errorf("the next read answers %s %v", status, err)
		}
		if again := failedAt(); again != first {
			t.Errorf("the next read asked for a refresh again: it failed at %s, then at %s", first, again)
		}
	})

	t.Run("an app allowed only to read connects read-only, and links nothing", func(t *testing.T) {
		stubControl(t, http.MethodPut, tenant+"/oauth-app", appWith("scope", "read"))
		h.cleanupExec(t, h.super, `DELETE FROM armature_token WHERE user_id = $1`, alice)
		again := signedIn(t, api, "alice", alicePassword)
		account := again.getJSON(t, "/api/v1/armature/account")["account"].(map[string]any)
		if account["source"] != "sign_in" || account["status"] != "ok" || account["readOnly"] != true {
			t.Fatalf("alice's account from a read-only app reads %v", account)
		}
		if g := stubGrants(t, armatureStub, tenant)["alice"]; g == nil || g["scope"] != "read" {
			t.Errorf("the stub's grant is %v", g)
		}
		again.post201(t, "/api/v1/projects", map[string]any{"key": key, "name": "Read only"})
		req := again.post201(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"})["requirement"].(map[string]any)
		resp, raw := again.postJSON(t, "/api/v1/requirements/"+key+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "implements"})
		var refused struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(raw), &refused)
		if resp.StatusCode != http.StatusConflict || refused.Error.Code != "armature_rejected" {
			t.Errorf("a link through a read-only grant answered %d %s", resp.StatusCode, raw)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_issue_link WHERE requirement_id = $1`, req["id"]); n != 0 {
			t.Errorf("%d links were made through a read-only grant", n)
		}
	})
}

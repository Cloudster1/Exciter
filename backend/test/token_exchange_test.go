//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/stator"
	"github.com/cloudster1/exciter/backend/internal/tokenexchange"
)

// Connecting people to Armature and Stator at sign-in (#274): with the realm shared, Exciter trades a person's
// Keycloak access token for their tokens at each stub, which verify it against the realm's keys as Armature does,
// and every read and link is made with those tokens, renewed as they run out.

const exchangeSecret = "exciter-exchange-secret"

// getJSON reads a JSON answer of the API with the browser's session.
func (b *browser) getJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	resp, body := b.get(t, b.api.URL+path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, resp.StatusCode, body)
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (b *browser) post201(t *testing.T, path string, body any) map[string]any {
	t.Helper()
	resp, raw := b.postJSON(t, path, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST %s = %d %s", path, resp.StatusCode, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// stubGrants are the grants a stub holds for a tenant, by person.
func stubGrants(t *testing.T, stub func(*testing.T, string, string, any) map[string]any, tenant string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, g := range stub(t, http.MethodGet, tenant+"/oauth-grants", nil)["grants"].([]any) {
		grant := g.(map[string]any)
		out[grant["person"].(string)] = grant
	}
	return out
}

// armatureStub reads what armature-stub answers under /_stub/, as statorStub does for Stator's.
func armatureStub(t *testing.T, _, path string, _ any) map[string]any {
	t.Helper()
	resp, err := http.Get(os.Getenv(envArmatureStub) + "/_stub/" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPeopleAreConnectedToArmatureAndStatorAtSignIn(t *testing.T) {
	key := projectKey(t, "TXC")
	h := newHarness(t)
	h.keepArmature(t)
	h.keepStator(t)
	statorOffersItsOwn(t)
	kc := newKeycloak(t)
	api := newAPIServer(t, h)
	h.singleSignOn(t)
	configureProvider(t, api, kc.issuer(), false)
	h.letIn(t, aliceEmail, "admin")
	h.letIn(t, bobEmail, "member")
	alice, bob := h.userID(t, aliceEmail), h.userID(t, bobEmail)
	forget := func() {
		h.cleanupExec(t, h.super, `DELETE FROM partner_exchange WHERE user_id = ANY ($1)`, []uuid.UUID{alice, bob})
	}
	forget()
	t.Cleanup(forget)
	home := h.owner(t, "token-exchange")
	owner := api.as(t, home.user)
	tenant := stubTenant(t)
	clientID := "exciter-" + tenant
	for _, reset := range []func(){
		func() { stubControl(t, http.MethodDelete, tenant, nil) },
		func() { statorStub(t, http.MethodDelete, tenant, nil) },
	} {
		reset()
		t.Cleanup(reset)
	}
	// Exciter is the partner app at both, named by its client in the realm, which the realm's audience mappers
	// let each stub accept; a short hour makes every use a refresh.
	app := map[string]any{"clientId": clientID, "clientSecret": exchangeSecret, "keycloakClientId": exciterClient, "accessTtlSeconds": 30}
	stubControl(t, http.MethodPut, tenant+"/oauth-app", app)
	statorStub(t, http.MethodPut, tenant+"/oauth-app", app)
	connection := map[string]any{"clientId": clientID, "clientSecret": exchangeSecret}
	armatureConn := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", merged(connection, "baseUrl", armatureURL(t))), http.StatusOK, "connect Armature as the app"), "connection")
	statorConn := obj(t, want(t, owner.put(t, "/api/v1/stator/connection", merged(connection, "baseUrl", statorURL(t))), http.StatusOK, "connect Stator as the app"), "connection")

	t.Run("the connections keep the app, its secret sealed and never answered", func(t *testing.T) {
		for name, c := range map[string]map[string]any{"Armature": armatureConn, "Stator": statorConn} {
			if c["clientId"] != clientID || c["clientSecretSet"] != true || c["tokenExchange"] != true || c["exchangeOffered"] != true {
				t.Errorf("the %s connection reads %v", name, c)
			}
			if raw, _ := json.Marshal(c); strings.Contains(string(raw), exchangeSecret) {
				t.Errorf("the %s connection answers the secret", name)
			}
		}
		var sealed []byte
		if err := h.super.QueryRow(t.Context(), `SELECT client_secret FROM armature_connection`).Scan(&sealed); err != nil || strings.Contains(string(sealed), exchangeSecret) {
			t.Errorf("the client secret is stored as %q: %v", sealed, err)
		}
	})

	b := signedIn(t, api, "alice", alicePassword)
	t.Run("alice is connected to both by signing in, with no token pasted", func(t *testing.T) {
		for _, partner := range []string{"armature", "stator"} {
			account := b.getJSON(t, "/api/v1/"+partner+"/account")["account"].(map[string]any)
			user, _ := account["user"].(map[string]any)
			signIn, _ := account["signIn"].(map[string]any)
			if account["status"] != "ok" || account["source"] != "sign_in" || user["name"] != "Alice" || signIn["connected"] != true {
				t.Errorf("alice's %s account reads %v", partner, account)
			}
		}
		var sealed []byte
		if err := h.super.QueryRow(t.Context(), `SELECT token FROM armature_token WHERE user_id = $1 AND source = 'exchange'`, alice).Scan(&sealed); err != nil ||
			strings.Contains(string(sealed), "armature_oat_") {
			t.Errorf("the exchanged token is stored as %q: %v", sealed, err)
		}
	})

	t.Run("the database keeps what the exchange stores to the service", func(t *testing.T) {
		conn := h.appConn(t, alice)
		if n := seesRows(t, conn, `SELECT count(*) FROM partner_exchange`); n != 2 {
			t.Errorf("alice reads %d outcomes, want her own two", n)
		}
		refusedOutright(t, conn, "claiming a connection at sign-in", `INSERT INTO partner_exchange (partner, user_id, connected) VALUES ('armature', $1, true)`, alice)
		refusedOutright(t, conn, "rewriting the outcome", `UPDATE partner_exchange SET message = 'fine' WHERE user_id = $1`, alice)
		refusedOutright(t, conn, "passing a pasted token for an exchanged one", `UPDATE armature_token SET source = 'token' WHERE user_id = $1`, alice)
		refusedOutright(t, conn, "planting a refresh token", `UPDATE stator_token SET refresh_token = '\x00' WHERE user_id = $1`, alice)
		bobConn := h.appConn(t, bob)
		if n := seesRows(t, bobConn, `SELECT count(*) FROM partner_exchange`); n != 0 {
			t.Errorf("bob reads %d of alice's outcomes", n)
		}
		touchesNothing(t, bobConn, "a member naming the app", `UPDATE armature_connection SET client_id = 'mine'`)
	})

	t.Run("her links are made as her while she is away, the tokens renewed as they run out", func(t *testing.T) {
		b.post201(t, "/api/v1/projects", map[string]any{"key": key, "name": "Connected at sign-in"})
		req := b.post201(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage within 100 ms"})["requirement"].(map[string]any)
		page := statorPages(t, tenant)["Brake timing"]
		b.post201(t, "/api/v1/requirements/"+key+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "implements"})
		b.post201(t, "/api/v1/requirements/"+key+"-R1/stator-links", map[string]any{"pageId": page, "relation": "specifies"})
		// The stack's own worker makes both links, as cmd/worker builds it: a worker of the test's own would take the
		// Stator event too and find no handler for it.
		h.settle(t)
		h.drained(t)
		id := uuid.MustParse(req["id"].(string))
		if on := linksOn(t, tenant, testAppURL(t)+armature.RequirementPath+id.String())["CP-1"]; on == nil || on["createdByName"] != "Alice" {
			t.Errorf("the link on CP-1 is %v, want one alice made", on)
		}
		if on := statorLinksOn(t, tenant, stator.RequirementURL(testAppURL(t), id))[page]; on == nil || on["createdByName"] != "Alice" {
			t.Errorf("the link on the page is %v, want one alice made; Exciter reads it as %v", on, statorLinkStates(t, api.as(t, alice), key+"-R1"))
		}
		for name, grants := range map[string]map[string]map[string]any{
			"Armature": stubGrants(t, armatureStub, tenant),
			"Stator":   stubGrants(t, statorStub, tenant),
		} {
			if g := grants["alice"]; g == nil || g["refreshes"].(float64) < 1 {
				t.Errorf("alice's grant at %s was never renewed: %v", name, grants)
			}
		}
	})

	t.Run("a token bob pasted himself stays when he signs in", func(t *testing.T) {
		bobClient := api.as(t, bob)
		want(t, bobClient.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(tenant, "bob")}), http.StatusOK, "bob pastes a token")
		signedIn(t, api, "bob", bobPassword)
		if account := obj(t, want(t, bobClient.get(t, "/api/v1/armature/account"), http.StatusOK, "bob's account"), "account"); account["source"] != "token" {
			t.Errorf("bob's own token was replaced: %v", account)
		}
		if account := obj(t, want(t, bobClient.get(t, "/api/v1/stator/account"), http.StatusOK, "bob's Stator account"), "account"); account["source"] != "sign_in" {
			t.Errorf("bob, with no token of his own at Stator, was not connected there: %v", account)
		}
	})

	t.Run("a failed exchange never fails the sign-in, and says why", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "clientSecret": "not-the-secret"}), http.StatusOK, "a wrong secret")
		h.cleanupExec(t, h.super, `DELETE FROM armature_token WHERE user_id = $1`, alice)
		again := signedIn(t, api, "alice", alicePassword)
		account := again.getJSON(t, "/api/v1/armature/account")["account"].(map[string]any)
		signIn, _ := account["signIn"].(map[string]any)
		if account["connected"] != false || signIn["connected"] != false ||
			signIn["message"] != tokenexchange.Why("Armature", &tokenexchange.Refusal{Code: "invalid_client"}) {
			t.Errorf("after a refused exchange alice's account reads %v", account)
		}
		if stat := again.getJSON(t, "/api/v1/stator/account")["account"].(map[string]any); stat["source"] != "sign_in" || stat["status"] != "ok" {
			t.Errorf("Stator's exchange went with Armature's refusal: %v", stat)
		}
	})

	t.Run("a grant the partner ended is forgotten, and the account says so", func(t *testing.T) {
		statorStub(t, http.MethodPut, tenant+"/oauth-app", app)
		resp, raw := b.postJSON(t, "/api/v1/stator/account/check", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("the check answered %d %s", resp.StatusCode, raw)
		}
		h.settle(t)
		account := b.getJSON(t, "/api/v1/stator/account")["account"].(map[string]any)
		signIn, _ := account["signIn"].(map[string]any)
		if account["connected"] != false || signIn["connected"] != false || !strings.Contains(signIn["message"].(string), "Stator ended the connection") {
			t.Errorf("after Stator ended the grant alice's account reads %v", account)
		}
	})

	t.Run("nobody is connected to a partner that does not offer the exchange", func(t *testing.T) {
		statorOffers(t, "page-links@1", "page-remote-links@1", "webhooks@1")
		want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t)}), http.StatusOK, "Stator is asked again")
		h.cleanupExec(t, h.super, `DELETE FROM stator_token WHERE user_id = $1`, alice)
		again := signedIn(t, api, "alice", alicePassword)
		account := again.getJSON(t, "/api/v1/stator/account")["account"].(map[string]any)
		signIn, _ := account["signIn"].(map[string]any)
		if account["connected"] != false || !strings.Contains(signIn["message"].(string), tokenexchange.Capability) {
			t.Errorf("alice's Stator account reads %v", account)
		}
		if grants := stubGrants(t, statorStub, tenant); grants["alice"] != nil {
			t.Errorf("Stator was asked anyway: %v", grants)
		}
	})
}

// merged is the map with one more field, so a test states a body once and varies the rest.
func merged(m map[string]any, key string, value any) map[string]any {
	out := map[string]any{key: value}
	for k, v := range m {
		out[k] = v
	}
	return out
}

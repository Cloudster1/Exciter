//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/stator"
)

// Connecting Stator (#40) against the stator-stub, which a contract test holds to Stator's own document.

const (
	// envStator is the stub as a browser opens it, which is the base URL an administrator enters; envStatorStub is
	// where this process reaches it.
	envStator     = "EXCITER_TEST_STATOR_URL"
	envStatorStub = "EXCITER_TEST_STATOR_STUB_URL"
	// statorAllow is what the stack lets the guard through to, both stubs as compose names them.
	statorAllow         = "stator-stub,armature-stub"
	statorWebhookSecret = "stator_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
)

func statorURL(t *testing.T) string {
	t.Helper()
	if os.Getenv(envStator) == "" || os.Getenv(envStatorStub) == "" {
		t.Fatalf("%s is not set; run the suite with make test-integration against the running stack", envStator)
	}
	return os.Getenv(envStator)
}

// stator is the service as cmd/api builds it, over the stack's stub.
func (h *harness) stator(t *testing.T) *stator.Service {
	t.Helper()
	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	allow := netguard.ParseAllow(statorAllow)
	client := stator.NewClient(allow, map[string]string{statorURL(t): os.Getenv(envStatorStub)})
	return stator.NewService(h.cluster, box, client, stator.NewCache(h.valkey(t), discard()),
		stator.Options{AppURL: "http://" + appHost, Allow: allow, Development: true, TokenExchange: true, Log: discard()})
}

func statorPat(tenant, person string) string { return stator.TokenPrefix + tenant + "_" + person }

// statorOffers sets what the stub's /meta lists, for every tenant at once; none when no capability is named.
func statorOffers(t *testing.T, capabilities ...string) {
	t.Helper()
	body := `{"capabilities":["` + strings.Join(capabilities, `","`) + `"]}`
	if len(capabilities) == 0 {
		body = `{"capabilities":[]}`
	}
	setStatorOffers(t, body)
}

// statorOffersItsOwn puts back what Stator itself lists, page-remote-links@1 among it.
func statorOffersItsOwn(t *testing.T) {
	t.Helper()
	setStatorOffers(t, `{"capabilities":null}`)
}

func setStatorOffers(t *testing.T, body string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, os.Getenv(envStatorStub)+"/_stub/capabilities", strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("tell the stub what it offers: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("the stub answered %d to its capabilities", resp.StatusCode)
	}
}

func TestAnAdministratorConnectsStatorAndMembersTheirTokens(t *testing.T) {
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-connect")
	tenant := stubTenant(t)
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	base := statorURL(t)
	bobToken := statorPat(tenant, "bob")

	if got := want(t, owner.get(t, "/api/v1/stator/connection"), http.StatusOK, "no connection yet"); got.Body["connection"] != nil {
		t.Errorf("the installation starts with a connection: %s", got.Raw)
	}
	errorCode(t, want(t, bob.get(t, "/api/v1/stator/connection"), http.StatusForbidden, "a member reads the connection"))
	account := obj(t, want(t, bob.get(t, "/api/v1/stator/account"), http.StatusOK, "an account before any connection"), "account")
	if account["configured"] != false || account["status"] != "not_configured" || account["baseUrl"] != "" {
		t.Errorf("before a connection the account is %v", account)
	}
	if code := errorCode(t, want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": bobToken}), http.StatusConflict, "a token before a connection")); code != "stator_not_configured" {
		t.Errorf("code %s", code)
	}

	for _, tc := range []struct {
		name, baseURL, secret, field, says string
	}{
		{"an address that is no origin", base + "/api/v1", "", "baseUrl", "without a path"},
		{"a host the guard does not let through", "http://valkey:6379", "", "baseUrl", "EXCITER_OUTBOUND_ALLOW"},
		{"an address inside the network", "http://10.11.12.13", "", "baseUrl", "inside the server's own network"},
		{"an Armature rather than a Stator", "http://armature-stub:8080", "", "baseUrl", "it is not Stator"},
		{"a secret Stator never shows", base, "hunter2", "webhookSecret", stator.WebhookSecretPrefix},
		{"an Armature secret", base, webhookSecret, "webhookSecret", stator.WebhookSecretPrefix},
	} {
		body := map[string]any{"baseUrl": tc.baseURL}
		if tc.secret != "" {
			body["webhookSecret"] = tc.secret
		}
		if msg := fieldError(t, want(t, owner.put(t, "/api/v1/stator/connection", body), http.StatusUnprocessableEntity, tc.name), tc.field); !strings.Contains(msg, tc.says) {
			t.Errorf("%s: %q does not say %q", tc.name, msg, tc.says)
		}
	}
	errorCode(t, want(t, bob.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": base}), http.StatusForbidden, "a member connects Stator"))

	saved := want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": base + "/", "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	conn := obj(t, saved, "connection")
	// The stub offers what Stator does, remote links on pages among it.
	if conn["baseUrl"] != base || conn["webhookSecretSet"] != true || conn["remoteLinks"] != true || conn["connected"] != float64(0) {
		t.Errorf("the connection is %v", conn)
	}
	if conn["webhookUrl"] != "http://"+appHost+stator.WebhookPath || len(conn["webhookTopics"].([]any)) != len(stator.WebhookTopics) {
		t.Errorf("the webhook settings are %v %v", conn["webhookUrl"], conn["webhookTopics"])
	}
	if bytes.Contains(saved.Raw, []byte(statorWebhookSecret)) {
		t.Error("the webhook secret was answered")
	}
	var sealed []byte
	if err := h.super.QueryRow(context.Background(), `SELECT webhook_secret FROM stator_connection`).Scan(&sealed); err != nil || bytes.Contains(sealed, []byte(statorWebhookSecret)) || len(sealed) == 0 {
		t.Errorf("the webhook secret is not sealed at rest: %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action = $2 AND data::text NOT LIKE '%stator_whs_%'`, home.user, stator.ActionConnectionSaved); n != 1 {
		t.Errorf("connecting is audited %d times without the secret", n)
	}

	for _, tc := range []struct{ name, token, says string }{
		{"not a Stator token", "armature_pat_something", stator.TokenPrefix},
		{"a token Stator refuses", "stator_pat_revoked", "Stator did not accept this token. Make a new one under Your account, Access tokens in Stator and paste it here."},
	} {
		if msg := fieldError(t, want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": tc.token}), http.StatusUnprocessableEntity, tc.name), "token"); !strings.Contains(msg, tc.says) {
			t.Errorf("%s: %q does not say %q", tc.name, msg, tc.says)
		}
	}
	stored := want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects")
	account = obj(t, stored, "account")
	if account["configured"] != true || account["connected"] != true || account["status"] != "ok" || account["baseUrl"] != base || account["checkedAt"] == nil {
		t.Errorf("after connecting the account is %v", account)
	}
	if user := obj(t, stored, "account", "user"); user["name"] != "Bob" || user["email"] != "bob@"+tenant+".stator.test" {
		t.Errorf("the token acts as %v, want Bob", user)
	}
	if bytes.Contains(stored.Raw, []byte(bobToken)) {
		t.Error("the token was answered")
	}
	var sealedToken []byte
	var tokenID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id, token FROM stator_token WHERE user_id = $1`, bobID).Scan(&tokenID, &sealedToken); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealedToken, []byte(bobToken)) || bytes.Contains(sealedToken, []byte("_bob")) {
		t.Error("the token is readable at rest")
	}

	got := obj(t, want(t, bob.get(t, "/api/v1/stator/account"), http.StatusOK, "bob reads his account"), "account")
	if got["status"] != "ok" || got["connected"] != true {
		t.Errorf("bob reads %v", got)
	}
	checked := obj(t, want(t, bob.post(t, "/api/v1/stator/account/check", nil), http.StatusOK, "bob checks his token"), "account")
	if checked["status"] != "ok" {
		t.Errorf("a check answered %v", checked)
	}
	if n := obj(t, want(t, owner.get(t, "/api/v1/stator/connection"), http.StatusOK, "the owner reads"), "connection")["connected"]; n != float64(1) {
		t.Errorf("connected = %v, want 1", n)
	}

	// A new token is a new row, so nothing cached for the old one is read for it.
	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects again")
	var again uuid.UUID
	_ = h.super.QueryRow(context.Background(), `SELECT id FROM stator_token WHERE user_id = $1`, bobID).Scan(&again)
	if again == tokenID {
		t.Error("storing a token again kept the row id that keys the cache")
	}

	// Revoked in Stator, a stored token is marked rejected by a check, and a new one clears it.
	box, _ := secret.New(testSecretKey(t))
	installed := h.installation(t).ID
	bound := append(append([]byte("stator.token:"), installed[:]...), bobID[:]...)
	revoked, _ := box.Seal([]byte("stator_pat_revoked"), bound)
	if _, err := h.super.Exec(context.Background(), `UPDATE stator_token SET token = $2 WHERE user_id = $1`, bobID, revoked); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	if got := obj(t, want(t, bob.post(t, "/api/v1/stator/account/check", nil), http.StatusOK, "check a revoked token"), "account"); got["status"] != "rejected" || got["connected"] != true {
		t.Errorf("a revoked token checks as %v", got)
	}
	if got := obj(t, want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": bobToken}), http.StatusOK, "store a new token"), "account"); got["status"] != "ok" {
		t.Errorf("a new token is %v", got["status"])
	}

	// A read-only Exciter token reads the account and changes nothing, and none stores a Stator token.
	_, readOnly := makeToken(t, bob, map[string]any{"name": "stator-reader", "scopes": []string{"read"}})
	script := api.withToken(bobID, readOnly)
	want(t, script.get(t, "/api/v1/stator/account"), http.StatusOK, "a read-only token reads")
	errorCode(t, want(t, script.post(t, "/api/v1/stator/account/check", nil), http.StatusForbidden, "a read-only token checks"))
	errorCode(t, want(t, script.delete(t, "/api/v1/stator/account/token"), http.StatusForbidden, "a read-only token disconnects"))
	_, full := makeToken(t, bob, map[string]any{"name": "stator-writer"})
	if code := errorCode(t, want(t, api.withToken(bobID, full).put(t, "/api/v1/stator/account/token", map[string]any{"token": bobToken}), http.StatusForbidden, "a token stores a Stator token")); code != "session_only" {
		t.Errorf("code %s", code)
	}

	// Only the secret changes: the tokens stay.
	kept := obj(t, want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": base, "webhookSecret": ""}), http.StatusOK, "remove the secret"), "connection")
	if kept["webhookSecretSet"] != false || kept["connected"] != float64(1) {
		t.Errorf("removing the secret changed more: %v", kept)
	}
	// Another address, even of the same Stator, forgets every token.
	moved := obj(t, want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": os.Getenv(envStatorStub)}), http.StatusOK, "move the connection"), "connection")
	if moved["connected"] != float64(0) {
		t.Errorf("a new address kept the tokens: %v", moved)
	}
	if got := obj(t, want(t, bob.get(t, "/api/v1/stator/account"), http.StatusOK, "bob after the move"), "account"); got["connected"] != false || got["status"] != "not_connected" {
		t.Errorf("bob is still connected after the move: %v", got)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action = $2 AND data->>'tokensForgotten' = '1'`, home.user, stator.ActionConnectionSaved); n != 1 {
		t.Errorf("the move is audited %d times with the forgotten token", n)
	}

	want(t, bob.delete(t, "/api/v1/stator/account/token"), http.StatusNoContent, "disconnect with no token")
	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects to the new address")
	want(t, bob.delete(t, "/api/v1/stator/account/token"), http.StatusNoContent, "bob disconnects")
	if n := h.countRows(t, `SELECT count(*) FROM stator_token`); n != 0 {
		t.Errorf("%d tokens are left after disconnecting", n)
	}

	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects before the removal")
	errorCode(t, want(t, bob.delete(t, "/api/v1/stator/connection"), http.StatusForbidden, "a member removes the connection"))
	want(t, owner.delete(t, "/api/v1/stator/connection"), http.StatusNoContent, "remove the connection")
	if n := h.countRows(t, `SELECT count(*) FROM stator_token`); n != 0 {
		t.Errorf("%d tokens outlived the connection", n)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action = $2`, home.user, stator.ActionConnectionRemoved); n != 1 {
		t.Errorf("the removal is audited %d times", n)
	}
	if got := obj(t, want(t, bob.get(t, "/api/v1/stator/account"), http.StatusOK, "bob after the removal"), "account"); got["status"] != "not_configured" {
		t.Errorf("bob after the removal: %v", got)
	}
	errorCode(t, want(t, api.anonymous().get(t, "/api/v1/stator/account"), http.StatusUnauthorized, "nobody reads an account"))
}

// Whether Stator takes remote links on pages (#42) is read from its /meta whenever the address is saved or a token
// checked, by its exact name: a Stator listing only a later version promises something else.
func TestTheConnectionKnowsWhatStatorOffers(t *testing.T) {
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-offers")
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	base := statorURL(t)

	statorOffers(t)
	if conn := obj(t, want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": base}), http.StatusOK, "connect a Stator that takes no remote links"), "connection"); conn["remoteLinks"] != false {
		t.Errorf("a Stator listing nothing takes remote links: %v", conn)
	}
	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(stubTenant(t), "bob")}), http.StatusOK, "bob connects")

	// Stator is upgraded: a member's check finds it, though only an administrator may write the connection.
	statorOffers(t, "webhooks@1", stator.RemoteLinksCapability)
	want(t, bob.post(t, "/api/v1/stator/account/check", nil), http.StatusOK, "bob checks his token")
	h.settle(t)
	if conn := obj(t, want(t, owner.get(t, "/api/v1/stator/connection"), http.StatusOK, "after the upgrade"), "connection"); conn["remoteLinks"] != true || conn["connected"] != float64(1) {
		t.Errorf("a check did not learn what Stator offers: %v", conn)
	}
	// Saving the same address reads it again, and keeps the tokens.
	statorOffers(t, "webhooks@1", "page-remote-links@2")
	if conn := obj(t, want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": base}), http.StatusOK, "save again"), "connection"); conn["remoteLinks"] != false || conn["connected"] != float64(1) {
		t.Errorf("saving again did not read what Stator offers, or took page-remote-links@2 for @1: %v", conn)
	}
}

// Stator not answering is 502, never a 401 that would read as a sign-in to Exciter; a check records it without
// dropping the token, and the address people use stays when it is saved again meanwhile.
func TestAStatorThatDoesNotAnswer(t *testing.T) {
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-silent")
	tenant := stubTenant(t)
	owner := api.as(t, home.user)
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t)}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "alice")}), http.StatusOK, "connect a token")

	// Behind the service's back: the same host on a port where nothing listens.
	silent := "http://stator-stub:9"
	if _, err := h.super.Exec(context.Background(), `
		ALTER TABLE stator_connection DISABLE TRIGGER stator_connection_moved;
		UPDATE stator_connection SET base_url = 'http://stator-stub:9', remote_links = true;
		ALTER TABLE stator_connection ENABLE TRIGGER stator_connection_moved;`); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	if code := errorCode(t, want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "alice")}), http.StatusBadGateway, "a token while Stator is down")); code != "stator_unreachable" {
		t.Errorf("code %s", code)
	}
	checked := obj(t, want(t, owner.post(t, "/api/v1/stator/account/check", nil), http.StatusOK, "a check while Stator is down"), "account")
	if checked["status"] != "unreachable" || checked["connected"] != true {
		t.Errorf("the check is %v", checked)
	}
	kept := obj(t, want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": silent, "webhookSecret": statorWebhookSecret}), http.StatusOK, "save while Stator is down"), "connection")
	if kept["webhookSecretSet"] != true || kept["remoteLinks"] != true || kept["connected"] != float64(1) {
		t.Errorf("saving while Stator is down lost what it knew: %v", kept)
	}
	if msg := fieldError(t, want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": "http://stator-stub:10"}), http.StatusUnprocessableEntity, "move to a silent address"), "baseUrl"); !strings.Contains(msg, "Stator did not answer") {
		t.Errorf("a silent new address is refused with %q", msg)
	}
}

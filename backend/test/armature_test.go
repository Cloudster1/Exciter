//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/secret"
)

// Connecting Armature (#26) against the armature-stub, the one stand-in the suite has, which a contract test holds
// to Armature's own document.

const (
	// envArmature is the stub as a browser opens it, which is the base URL an administrator enters;
	// envArmatureStub is where this process reaches it.
	envArmature     = "EXCITER_TEST_ARMATURE_URL"
	envArmatureStub = "EXCITER_TEST_ARMATURE_STUB_URL"
	// armatureStubHost is the one host inside the network the guard lets through.
	armatureStubHost = "armature-stub"
	webhookSecret    = "armature_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
)

// testSecretKey is the stack's EXCITER_SECRET_KEY, so the stack's own worker opens the tokens the suite stores.
func testSecretKey(t *testing.T) []byte {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(os.Getenv("EXCITER_TEST_SECRET_KEY"))
	if err != nil || len(key) != secret.KeySize {
		t.Fatal("EXCITER_TEST_SECRET_KEY is not the stack's key; run the suite with make test-integration against the running stack")
	}
	return key
}

func armatureURL(t *testing.T) string {
	t.Helper()
	if os.Getenv(envArmature) == "" || os.Getenv(envArmatureStub) == "" {
		t.Fatalf("%s is not set; run the suite with make test-integration against the running stack", envArmature)
	}
	return os.Getenv(envArmature)
}

func (h *harness) valkey(t *testing.T) *redis.Client {
	t.Helper()
	opts, err := redis.ParseURL(h.cfg.Valkey.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// armature is the service as cmd/api builds it, over the stack's stub.
func (h *harness) armature(t *testing.T) *armature.Service {
	t.Helper()
	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	allow := netguard.ParseAllow(armatureStubHost)
	client := armature.NewClient(allow, map[string]string{armatureURL(t): os.Getenv(envArmatureStub)})
	return armature.NewService(h.cluster, box, client, armature.NewCache(h.valkey(t), discard()),
		armature.Options{AppURL: "http://" + appHost, Allow: allow, Development: true, Log: discard()})
}

func patFor(tenant, person string) string { return armature.TokenPrefix + tenant + "_" + person }

func fieldError(t *testing.T, r response, field string) string {
	t.Helper()
	if code := errorCode(t, r); code != "validation_failed" {
		t.Fatalf("code %s, want validation_failed: %s", code, r.Raw)
	}
	msg, _ := obj(t, r, "error", "fields")[field].(string)
	if msg == "" {
		t.Fatalf("no sentence on %s: %s", field, r.Raw)
	}
	return msg
}

func TestAnAdministratorConnectsArmatureAndMembersTheirTokens(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-connect")
	slug := stubTenant(t)
	owner := api.as(t, home.user, home.org)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org)
	base := armatureURL(t)
	bobToken := patFor(slug, "bob")

	if got := want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "no connection yet"); got.Body["connection"] != nil {
		t.Errorf("a new organization has a connection: %s", got.Raw)
	}
	errorCode(t, want(t, bob.get(t, "/api/v1/armature/connection"), http.StatusForbidden, "a member reads the connection"))
	account := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "an account before any connection"), "account")
	if account["configured"] != false || account["status"] != "not_configured" || account["baseUrl"] != "" {
		t.Errorf("before a connection the account is %v", account)
	}
	if code := errorCode(t, want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusConflict, "a token before a connection")); code != "armature_not_configured" {
		t.Errorf("code %s", code)
	}

	for _, tc := range []struct {
		name, baseURL, secret, field, says string
	}{
		{"an address that is no origin", base + "/api/v1", "", "baseUrl", "without a path"},
		{"a host the guard does not let through", "http://valkey:6379", "", "baseUrl", "EXCITER_OUTBOUND_ALLOW"},
		{"an address inside the network", "http://10.11.12.13", "", "baseUrl", "inside the server's own network"},
		{"a secret Armature never shows", base, "hunter2", "webhookSecret", armature.WebhookSecretPrefix},
	} {
		body := map[string]any{"baseUrl": tc.baseURL}
		if tc.secret != "" {
			body["webhookSecret"] = tc.secret
		}
		if msg := fieldError(t, want(t, owner.put(t, "/api/v1/armature/connection", body), http.StatusUnprocessableEntity, tc.name), tc.field); !strings.Contains(msg, tc.says) {
			t.Errorf("%s: %q does not say %q", tc.name, msg, tc.says)
		}
	}
	errorCode(t, want(t, bob.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base}), http.StatusForbidden, "a member connects Armature"))

	saved := want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base + "/", "webhookSecret": webhookSecret}), http.StatusOK, "connect Armature")
	conn := obj(t, saved, "connection")
	if conn["baseUrl"] != base || conn["orgSlug"] != nil || conn["webhookSecretSet"] != true || conn["armatureOrgId"] != nil {
		t.Errorf("the connection is %v", conn)
	}
	if conn["webhookUrl"] != "http://"+appHost+armature.WebhookPath || len(conn["webhookTopics"].([]any)) != len(armature.WebhookTopics) {
		t.Errorf("the webhook settings are %v %v", conn["webhookUrl"], conn["webhookTopics"])
	}
	if bytes.Contains(saved.Raw, []byte(webhookSecret)) {
		t.Error("the webhook secret was answered")
	}
	var sealed []byte
	if err := h.super.QueryRow(context.Background(), `SELECT webhook_secret FROM armature_connection WHERE org_id = $1`, home.org).Scan(&sealed); err != nil || bytes.Contains(sealed, []byte(webhookSecret)) || len(sealed) == 0 {
		t.Errorf("the webhook secret is not sealed at rest: %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action = $2 AND data::text NOT LIKE '%armature_whs_%'`, home.user, armature.ActionConnectionSaved); n != 1 {
		t.Errorf("connecting is audited %d times without the secret", n)
	}

	for _, tc := range []struct{ name, token, says string }{
		{"not an Armature token", "exciter_pat_something", armature.TokenPrefix},
		{"a token Armature refuses", "armature_pat_revoked", "Armature did not accept this token. Make a new one under Tokens in Armature and paste it here."},
	} {
		if msg := fieldError(t, want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": tc.token}), http.StatusUnprocessableEntity, tc.name), "token"); !strings.Contains(msg, tc.says) {
			t.Errorf("%s: %q does not say %q", tc.name, msg, tc.says)
		}
	}
	stored := want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects")
	account = obj(t, stored, "account")
	if account["configured"] != true || account["connected"] != true || account["status"] != "ok" || account["baseUrl"] != base || account["checkedAt"] == nil || account["readOnly"] != false {
		t.Errorf("after connecting the account is %v", account)
	}
	if name := obj(t, stored, "account", "user")["name"]; name != "Bob" {
		t.Errorf("the token acts as %v, want Bob", name)
	}
	if bytes.Contains(stored.Raw, []byte(bobToken)) {
		t.Error("the token was answered")
	}
	var sealedToken []byte
	var tokenID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id, token FROM armature_token WHERE org_id = $1 AND user_id = $2`, home.org, bobID).Scan(&tokenID, &sealedToken); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealedToken, []byte(bobToken)) || bytes.Contains(sealedToken, []byte("_bob")) {
		t.Error("the token is readable at rest")
	}
	if n := h.countRows(t, `SELECT count(*) FROM armature_connection WHERE org_id = $1 AND armature_org_id IS NOT NULL`, home.org); n != 1 {
		t.Error("the Armature organization's id was not learned from the first token")
	}
	// The address alone picks the Armature; what the first token taught keeps every later one to it.
	if msg := fieldError(t, want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor("globex-corp", "alice")}), http.StatusUnprocessableEntity, "a token of another Armature"), "token"); !strings.Contains(msg, "another Armature than the one connected at "+base) {
		t.Errorf("a token of another Armature is refused with %q", msg)
	}

	got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob reads his account"), "account")
	if got["status"] != "ok" || got["connected"] != true {
		t.Errorf("bob reads %v", got)
	}
	checked := obj(t, want(t, bob.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "bob checks his token"), "account")
	if checked["status"] != "ok" {
		t.Errorf("a check answered %v", checked)
	}
	if n := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the owner reads"), "connection")["connected"]; n != float64(1) {
		t.Errorf("connected = %v, want 1", n)
	}

	// A new token is a new row, so nothing cached for the old one is read for it.
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects again")
	var again uuid.UUID
	_ = h.super.QueryRow(context.Background(), `SELECT id FROM armature_token WHERE org_id = $1 AND user_id = $2`, home.org, bobID).Scan(&again)
	if again == tokenID {
		t.Error("storing a token again kept the row id that keys the cache")
	}

	// A read-only Exciter token reads the account and changes nothing.
	_, readOnly := makeToken(t, bob, map[string]any{"name": "armature-reader", "scopes": []string{"read"}})
	script := api.withToken(bobID, readOnly)
	want(t, script.get(t, "/api/v1/armature/account"), http.StatusOK, "a read-only token reads")
	errorCode(t, want(t, script.post(t, "/api/v1/armature/account/check", nil), http.StatusForbidden, "a read-only token checks"))
	errorCode(t, want(t, script.delete(t, "/api/v1/armature/account/token"), http.StatusForbidden, "a read-only token disconnects"))

	// Only the secret changes: the tokens stay.
	kept := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base, "webhookSecret": ""}), http.StatusOK, "remove the secret"), "connection")
	if kept["webhookSecretSet"] != false || kept["connected"] != float64(1) || kept["armatureOrgId"] == nil {
		t.Errorf("removing the secret changed more: %v", kept)
	}
	// Another address, even of the same Armature, forgets every token.
	moved := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": os.Getenv(envArmatureStub)}), http.StatusOK, "move the connection"), "connection")
	if moved["connected"] != float64(0) || moved["armatureOrgId"] != nil {
		t.Errorf("a new address kept the tokens: %v", moved)
	}
	if got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob after the move"), "account"); got["connected"] != false || got["status"] != "not_connected" {
		t.Errorf("bob is still connected after the move: %v", got)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action = $2 AND data->>'tokensForgotten' = '1'`, home.user, armature.ActionConnectionSaved); n != 1 {
		t.Errorf("the move is audited %d times with the forgotten token", n)
	}

	want(t, bob.delete(t, "/api/v1/armature/account/token"), http.StatusNoContent, "disconnect with no token")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects to the new address")
	want(t, bob.delete(t, "/api/v1/armature/account/token"), http.StatusNoContent, "bob disconnects")
	if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE org_id = $1`, home.org); n != 0 {
		t.Errorf("%d tokens are left after disconnecting", n)
	}

	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": bobToken}), http.StatusOK, "bob connects before the removal")
	errorCode(t, want(t, bob.delete(t, "/api/v1/armature/connection"), http.StatusForbidden, "a member removes the connection"))
	want(t, owner.delete(t, "/api/v1/armature/connection"), http.StatusNoContent, "remove the connection")
	if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE org_id = $1`, home.org); n != 0 {
		t.Errorf("%d tokens outlived the connection", n)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action = $2`, home.user, armature.ActionConnectionRemoved); n != 1 {
		t.Errorf("the removal is audited %d times", n)
	}
	if got := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob after the removal"), "account"); got["status"] != "not_configured" {
		t.Errorf("bob after the removal: %v", got)
	}
	errorCode(t, want(t, api.anonymous().get(t, "/api/v1/armature/account"), http.StatusUnauthorized, "nobody reads an account"))
}

// A token of Armature's read scope connects, and the account says it can make no issues or links.
func TestAReadOnlyArmatureTokenIsNamedSo(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-reader")
	slug := stubTenant(t)
	owner := api.as(t, home.user, home.org)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	account := obj(t, want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "reader")}), http.StatusOK, "a read-only token"), "account")
	if account["readOnly"] != true || account["status"] != "ok" {
		t.Errorf("a read-only token is %v", account)
	}
	if checked := obj(t, want(t, owner.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "check it"), "account"); checked["readOnly"] != true {
		t.Errorf("a check forgot the token reads only: %v", checked)
	}
	account = obj(t, want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "a full token"), "account")
	if account["readOnly"] != false {
		t.Errorf("a full token reads as read-only: %v", account)
	}
}

// Armature not answering is 502, never a 401 that would read as a sign-in to Exciter, and a check records it
// without dropping the token.
func TestAnArmatureThatDoesNotAnswer(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-silent")
	slug := stubTenant(t)
	owner := api.as(t, home.user, home.org)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "connect a token")

	// Behind the service's back: the same host on a port where nothing listens.
	if _, err := h.super.Exec(context.Background(), `
		ALTER TABLE armature_connection DISABLE TRIGGER armature_connection_moved;
		UPDATE armature_connection SET base_url = 'http://armature-stub:9' WHERE org_id = '`+home.org.String()+`';
		ALTER TABLE armature_connection ENABLE TRIGGER armature_connection_moved;`); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	if code := errorCode(t, want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusBadGateway, "a token while Armature is down")); code != "armature_unreachable" {
		t.Errorf("code %s", code)
	}
	checked := obj(t, want(t, owner.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "a check while Armature is down"), "account")
	if checked["status"] != "unreachable" || checked["connected"] != true {
		t.Errorf("the check is %v", checked)
	}
}

// Revoked in Armature, a stored token is marked rejected by a check and not used again until its owner stores a
// new one or a check finds it working.
func TestACheckFindsARevokedToken(t *testing.T) {
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-revoked")
	slug := stubTenant(t)
	owner := api.as(t, home.user, home.org)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "connect a token")
	service := h.armature(t)
	h.settle(t)
	if v, status, err := service.Viewer(home.ctx); err != nil || status != armature.StatusOK || v.Caller.BaseURL() != armatureURL(t) {
		t.Fatalf("the viewer is %v %s %v", v, status, err)
	}

	box, _ := secret.New(testSecretKey(t))
	bound := append(append([]byte("armature.token:"), home.org[:]...), home.user[:]...)
	revoked, _ := box.Seal([]byte("armature_pat_revoked"), bound)
	if _, err := h.super.Exec(context.Background(), `UPDATE armature_token SET token = $2 WHERE user_id = $1`, home.user, revoked); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	if got := obj(t, want(t, owner.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "check a revoked token"), "account"); got["status"] != "rejected" {
		t.Errorf("a revoked token checks as %v", got["status"])
	}
	h.settle(t)
	if _, status, _ := service.Viewer(home.ctx); status != armature.StatusRejected {
		t.Errorf("a rejected token is handed out: %s", status)
	}
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "store a new token")
	h.settle(t)
	if _, status, _ := service.Viewer(home.ctx); status != armature.StatusOK {
		t.Errorf("a new token is not used: %s", status)
	}
}

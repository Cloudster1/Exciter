//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/httpapi"
	"github.com/cloudster1/exciter/backend/internal/oauth"
)

// pastDue is how far back a row is moved to have run out, well clear of the clock of either database.
const pastDue = time.Minute

// pendingRequest begins an authorization as the client and answers the consent page's path for it.
func (c *handClient) pendingRequest(t *testing.T) string {
	t.Helper()
	id := sentTo(t, c.authorizeURL(nil)).Query().Get("request")
	if id == "" {
		t.Fatal("the authorization did not reach the consent page")
	}
	return httpapi.APIPrefix + "/oauth/requests/" + id
}

// runOut moves a row's expiry into the past as the superuser, and waits until the replica has it too.
func (h *harness) runOut(t *testing.T, table, column string, key any) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `UPDATE `+table+` SET expires_at = now() - make_interval(secs => $2) WHERE `+column+` = $1`,
		key, pastDue.Seconds()); err != nil {
		t.Fatalf("let a row of %s run out: %v", table, err)
	}
	h.settle(t)
}

// challenged says whether MCP refuses a bearer with 401 and the challenge naming a refused token.
func challenged(t *testing.T, a *apiServer, bearer string) bool {
	t.Helper()
	resp := mcpAs(t, a, bearer, "tools/list", nil)
	return resp.StatusCode == http.StatusUnauthorized && strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="invalid_token"`)
}

// refreshes says whether a refresh token turns over for the client, and answers the next tokens.
func (c *handClient) refreshes(t *testing.T, refresh string) (bool, map[string]any) {
	t.Helper()
	status, got := c.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
	return status == http.StatusOK, got
}

// A request is answered once: the second answer is 404 and changes nothing, and one past RequestTTL is 404 to read
// and to answer.
func TestAConsentIsAnsweredOnceAndRunsOut(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	person := a.as(t, h.owner(t, "consent-once").user)
	c, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Twice-asked assistant"})
	grants := func() int {
		return h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE user_id = $1 AND client_id = $2`, person.user, c.id)
	}
	granted := func() int {
		return h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'oauth.granted' AND actor_user_id = $1 AND data->>'clientId' = $2`,
			person.user, c.id)
	}

	t.Run("answered twice, the second is 404 and leaves one grant", func(t *testing.T) {
		path := c.pendingRequest(t)
		first := want(t, person.post(t, path, map[string]any{"allow": true}), http.StatusOK, "allow")
		if back, err := url.Parse(first.Body["redirectTo"].(string)); err != nil || back.Query().Get("code") == "" {
			t.Fatalf("the first answer sends the browser to %v", first.Body["redirectTo"])
		}
		refuse(t, "answering again", person.post(t, path, map[string]any{"allow": true}), http.StatusNotFound, "not_found")
		refuse(t, "denying after allowing", person.post(t, path, map[string]any{"allow": false}), http.StatusNotFound, "not_found")
		if n, logged := grants(), granted(); n != 1 || logged != 1 {
			t.Errorf("after two answers the client holds %d grants and the log %d oauth.granted rows, want 1 and 1", n, logged)
		}
	})

	t.Run("a request past RequestTTL is 404 to read and to answer", func(t *testing.T) {
		path := c.pendingRequest(t)
		h.runOut(t, "oauth_request", "id", strings.TrimPrefix(path, httpapi.APIPrefix+"/oauth/requests/"))
		refuse(t, "reading a request that ran out", person.get(t, path), http.StatusNotFound, "not_found")
		refuse(t, "allowing a request that ran out", person.post(t, path, map[string]any{"allow": true}), http.StatusNotFound, "not_found")
		if n, logged := grants(), granted(); n != 1 || logged != 1 {
			t.Errorf("a request that ran out made a grant: %d grants, %d oauth.granted rows", n, logged)
		}
	})
}

// Denying over the API sends the client back with access_denied, its state and the issuer, writes no grant, code or
// log row, and the request is gone.
func TestADenialOverTheAPIWritesNothing(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	person := a.as(t, h.owner(t, "consent-deny").user)
	c, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Declined assistant"})
	path := c.pendingRequest(t)
	requestID := strings.TrimPrefix(path, httpapi.APIPrefix+"/oauth/requests/")

	got := want(t, person.post(t, path, map[string]any{"allow": false}), http.StatusOK, "deny")
	back, err := url.Parse(got.Body["redirectTo"].(string))
	if err != nil {
		t.Fatal(err)
	}
	q := back.Query()
	if back.Host != "127.0.0.1:9" || back.Path != "/callback" || q.Get("error") != oauth.ErrAccessDenied || q.Get("state") != "the-state" ||
		q.Get("iss") != a.URL || q.Get("code") != "" || !strings.HasSuffix(q.Get("error_description"), ".") {
		t.Errorf("a denial sends the browser to %s", back)
	}
	for what, n := range map[string]int{
		"grants":           h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE client_id = $1`, c.id),
		"codes":            h.countRows(t, `SELECT count(*) FROM oauth_code k JOIN oauth_grant g ON g.id = k.grant_id WHERE g.client_id = $1`, c.id),
		"log rows":         h.countRows(t, `SELECT count(*) FROM audit_log WHERE action LIKE 'oauth.%' AND data->>'clientId' = $1`, c.id),
		"pending requests": h.countRows(t, `SELECT count(*) FROM oauth_request WHERE id = $1`, requestID),
	} {
		if n != 0 {
			t.Errorf("a denial left %d %s", n, what)
		}
	}
	refuse(t, "reading a denied request", person.get(t, path), http.StatusNotFound, "not_found")
}

// A code past CodeTTL is invalid_grant, and an access token past AccessTTL is refused at MCP with invalid_token.
func TestOAuthCodesAndAccessTokensRunOut(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	person := a.as(t, h.owner(t, "oauth-expiry").user)
	c, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Slow assistant"})

	t.Run("a code past CodeTTL", func(t *testing.T) {
		code := c.allow(t, person, map[string]any{"allow": true})
		h.runOut(t, "oauth_code", "code_hash", auth.HashToken(code))
		status, got := c.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callback},
			"code_verifier": {c.verifier}, "resource": {a.URL + httpapi.APIPrefix + "/mcp"}})
		wantRefused(t, "a code that ran out", status, got, oauth.ErrInvalidGrant)
		if n := h.countRows(t, `SELECT count(*) FROM oauth_token t JOIN oauth_grant g ON g.id = t.grant_id WHERE g.client_id = $1`, c.id); n != 0 {
			t.Errorf("a code that ran out issued %d tokens", n)
		}
	})

	t.Run("an access token past AccessTTL", func(t *testing.T) {
		access := c.exchange(t, c.allow(t, person, map[string]any{"allow": true}))["access_token"].(string)
		rpcResult(t, mcpAs(t, a, access, "tools/list", nil))
		h.runOut(t, "oauth_token", "token_hash", auth.HashToken(access))
		if !challenged(t, a, access) {
			t.Error("an access token that ran out is not refused with 401 invalid_token")
		}
	})
}

// A client cannot use or end another's tokens: a refresh is invalid_grant without ending the owner's grant, and a
// revocation answers 200 and ends nothing.
func TestAClientCannotUseOrEndAnothersTokens(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	person := a.as(t, h.owner(t, "cross-client").user)
	owning, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Owning assistant"})
	other, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Other assistant"})
	tokens := owning.exchange(t, owning.allow(t, person, map[string]any{"allow": true}))

	status, got := other.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}})
	wantRefused(t, "another client's refresh token", status, got, oauth.ErrInvalidGrant)
	if n := h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE client_id = $1 AND user_id = $2`, owning.id, person.user); n != 1 {
		t.Fatalf("another client's refresh ended the owner's grant: %d left", n)
	}
	ok, turned := owning.refreshes(t, tokens["refresh_token"].(string))
	if !ok {
		t.Fatalf("the owner's refresh token no longer turns over: %v", turned)
	}
	access := turned["access_token"].(string)

	if status, _ := other.form(t, "/oauth/revoke", url.Values{"token": {access}}); status != http.StatusOK {
		t.Errorf("another client revoking answers %d, want 200", status)
	}
	if status, _ := other.form(t, "/oauth/revoke", url.Values{"token": {turned["refresh_token"].(string)}}); status != http.StatusOK {
		t.Errorf("another client revoking a refresh token answers %d, want 200", status)
	}
	h.settle(t)
	rpcResult(t, mcpAs(t, a, access, "tools/list", nil))
	if ok, again := owning.refreshes(t, turned["refresh_token"].(string)); !ok {
		t.Errorf("another client's revocation ended the owner's refresh token: %v", again)
	}
}

// Consenting again for the same client replaces the grant: the first one's access and refresh tokens stop, and
// the person lists one grant of it.
func TestConsentingAgainReplacesTheGrant(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	person := a.as(t, h.owner(t, "consent-again").user)
	c, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Reconnected assistant"})
	first := c.exchange(t, c.allow(t, person, map[string]any{"allow": true}))
	second := c.exchange(t, c.allow(t, person, map[string]any{"allow": true, "readOnly": true}))
	h.settle(t)

	if !challenged(t, a, first["access_token"].(string)) {
		t.Error("the first grant's access token still works")
	}
	if ok, got := c.refreshes(t, first["refresh_token"].(string)); ok || got["error"] != oauth.ErrInvalidGrant {
		t.Errorf("the first grant's refresh token answers %v", got)
	}
	rpcResult(t, mcpAs(t, a, second["access_token"].(string), "tools/list", nil))
	var mine []map[string]any
	for _, each := range list(t, want(t, person.get(t, "/api/v1/oauth/grants"), http.StatusOK, "list my apps"), "grants") {
		if g := each.(map[string]any); g["client"].(map[string]any)["id"] == c.id {
			mine = append(mine, g)
		}
	}
	if len(mine) != 1 || mine[0]["scopes"].([]any)[0] != "read" {
		t.Errorf("the person lists %v of the client, want the second grant alone", mine)
	}
}

// partnerRow is what an administrator's changes to a partner app touch, read as the superuser.
func partnerRow(t *testing.T, h *harness, id string) string {
	t.Helper()
	var row string
	if err := h.super.QueryRow(context.Background(), `SELECT name || '|' || array_to_string(scopes, ',') || '|' || encode(secret_hash, 'hex')
		FROM oauth_client WHERE id = $1`, id).Scan(&row); err != nil {
		t.Fatalf("read the partner app: %v", err)
	}
	return row
}

// A member changes, rotates and removes no partner app, an administrator's change that breaks a rule is 422 on
// the field, and a rotated secret is the one that authenticates.
func TestPartnerAppsRefuseAMemberAndABadChange(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	admin := a.as(t, h.owner(t, "partner-refusals").user)
	member := a.as(t, h.addPerson(t, "member"))
	app := registerPartner(t, admin, map[string]any{"name": "Refused wiki", "keycloakClientId": statorClient})
	base := "/api/v1/oauth/clients/" + app.id
	before := partnerRow(t, h, app.id)

	t.Run("a member", func(t *testing.T) {
		refuse(t, "a member renaming", member.patch(t, base, map[string]any{"name": "Mine", "scopes": []string{"read", "write"}}), http.StatusForbidden, "forbidden")
		refuse(t, "a member rotating", member.post(t, base+"/rotate-secret", nil), http.StatusForbidden, "forbidden")
		refuse(t, "a member removing", member.delete(t, base), http.StatusForbidden, "forbidden")
		if now := partnerRow(t, h, app.id); now != before {
			t.Errorf("the member's refusals changed the app from %s to %s", before, now)
		}
	})

	t.Run("an administrator's change that breaks a rule", func(t *testing.T) {
		refuse422(t, "write alone", admin.patch(t, base, map[string]any{"scopes": []string{"write"}}), "scopes")
		refuse422(t, "a scope nobody has", admin.patch(t, base, map[string]any{"scopes": []string{"read", "admin"}}), "scopes")
		refuse422(t, "no name", admin.patch(t, base, map[string]any{"name": "  "}), "name")
		refuse422(t, "an address that is none", admin.patch(t, base, map[string]any{"clientUri": "ftp://wiki"}), "clientUri")
		refuse(t, "rotating an app nobody has", admin.post(t, "/api/v1/oauth/clients/nothing/rotate-secret", nil), http.StatusNotFound, "not_found")
		if now := partnerRow(t, h, app.id); now != before {
			t.Errorf("the refused changes moved the app from %s to %s", before, now)
		}
	})

	t.Run("the rotated secret authenticates and the old one does not", func(t *testing.T) {
		rotated := partnerApp{id: app.id, secret: obj(t, want(t, admin.post(t, base+"/rotate-secret", nil), http.StatusOK, "rotate"), "client")["secret"].(string)}
		h.settle(t)
		// A refresh token nobody has gets past the client's authentication to the grant, which refuses it.
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"exciter_ort_nobody"}}
		if status, got := rotated.token(t, a, form); status != http.StatusBadRequest || got["error"] != oauth.ErrInvalidGrant {
			t.Errorf("the new secret answers %d %v, want it let through to invalid_grant", status, got)
		}
		if status, got := app.token(t, a, form); status != http.StatusUnauthorized || got["error"] != oauth.ErrInvalidClient {
			t.Errorf("the old secret answers %d %v, want invalid_client", status, got)
		}
	})
}

// One sweep removes what has run out, the grants nobody can use, and a registration unused past UnusedClientTTL;
// a partner app with no grant and a live grant stay.
func TestTheSweeperRemovesWhatRanOut(t *testing.T) {
	h := newHarness(t)
	a := newOAuthServer(t, h)
	person := a.as(t, h.owner(t, "sweeper").user)

	asking, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Asking assistant"})
	request := strings.TrimPrefix(asking.pendingRequest(t), httpapi.APIPrefix+"/oauth/requests/")
	h.runOut(t, "oauth_request", "id", request)

	slow, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Slow assistant"})
	code := slow.allow(t, person, map[string]any{"allow": true})
	h.runOut(t, "oauth_code", "code_hash", auth.HashToken(code))

	live, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Live assistant"})
	tokens := live.exchange(t, live.allow(t, person, map[string]any{"allow": true}))
	h.runOut(t, "oauth_token", "token_hash", auth.HashToken(tokens["access_token"].(string)))

	// A grant whose code was traded and whose refresh token is gone, as an ended rotation leaves one.
	spent, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Spent assistant"})
	spent.exchange(t, spent.allow(t, person, map[string]any{"allow": true}))
	if _, err := h.super.Exec(context.Background(), `DELETE FROM oauth_code k USING oauth_grant g WHERE g.id = k.grant_id AND g.client_id = $1`, spent.id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(context.Background(), `DELETE FROM oauth_token t USING oauth_grant g WHERE g.id = t.grant_id AND g.client_id = $1 AND t.kind = 'refresh'`,
		spent.id); err != nil {
		t.Fatal(err)
	}

	unused, _, _ := register(t, a, newAddress(), map[string]any{"client_name": "Unused assistant"})
	partner := registerPartner(t, a.as(t, person.user), map[string]any{"name": "Idle partner"})
	if _, err := h.super.Exec(context.Background(), `UPDATE oauth_client SET created_at = now() - make_interval(secs => $2) WHERE id = ANY ($1)`,
		[]string{unused.id, partner.id}, (oauth.UnusedClientTTL + pastDue).Seconds()); err != nil {
		t.Fatal(err)
	}

	if err := oauth.NewSweeper(h.cluster, testLog(t)).Once(context.Background()); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	for what, n := range map[string]int{
		"the request that ran out":      h.countRows(t, `SELECT count(*) FROM oauth_request WHERE id = $1`, request),
		"the code that ran out":         h.countRows(t, `SELECT count(*) FROM oauth_code WHERE code_hash = $1`, auth.HashToken(code)),
		"the grant its code left":       h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE client_id = $1`, slow.id),
		"the access token that ran out": h.countRows(t, `SELECT count(*) FROM oauth_token WHERE token_hash = $1`, auth.HashToken(tokens["access_token"].(string))),
		"the grant with nothing to use": h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE client_id = $1`, spent.id),
		"the registration nobody used":  h.countRows(t, `SELECT count(*) FROM oauth_client WHERE id = $1`, unused.id),
	} {
		if n != 0 {
			t.Errorf("the sweep left %s (%d rows)", what, n)
		}
	}
	for what, n := range map[string]int{
		"the partner app with no grant":  h.countRows(t, `SELECT count(*) FROM oauth_client WHERE id = $1`, partner.id),
		"the live grant":                 h.countRows(t, `SELECT count(*) FROM oauth_grant WHERE client_id = $1`, live.id),
		"the live refresh token":         h.countRows(t, `SELECT count(*) FROM oauth_token WHERE token_hash = $1`, auth.HashToken(tokens["refresh_token"].(string))),
		"the client asking a moment ago": h.countRows(t, `SELECT count(*) FROM oauth_client WHERE id = $1`, asking.id),
	} {
		if n != 1 {
			t.Errorf("the sweep removed %s (%d rows left, want 1)", what, n)
		}
	}
	h.settle(t)
	if ok, got := live.refreshes(t, tokens["refresh_token"].(string)); !ok {
		t.Errorf("the live grant no longer refreshes after the sweep: %v", got)
	}
}

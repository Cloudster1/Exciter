package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/exchangestub"
)

// The token endpoint the stub serves outside routes (#274), as Stator's document has it since its #461: the exchange,
// a refresh that rotates, and a replayed refresh token that ends the grant, each answer in Stator's shape.

const tokenPath = "/oauth/token"

// fakeKeycloak reads "kc|email|azp|aud" as a verified token, so the contract needs no identity provider.
func fakeKeycloak(_ context.Context, raw string) (*exchangestub.Claims, error) {
	parts := strings.Split(raw, "|")
	if len(parts) != 4 || parts[0] != "kc" {
		return nil, errors.New("it is not a current token signed by the identity provider")
	}
	return &exchangestub.Claims{Email: parts[1], AuthorizedBy: parts[2], Audience: []string{parts[3]}}, nil
}

// form posts to the token endpoint as a partner app does, checking the form and the answer against Stator's document.
func (c *contract) form(id, secret string, values url.Values, want int) map[string]any {
	c.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.base+apiPrefix+tokenPath, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(id), url.QueryEscape(secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("POST %s %v = %d, want %d: %s", tokenPath, values, resp.StatusCode, want, raw)
	}
	op := c.doc.Paths[tokenPath]["post"]
	if op == nil || op.RequestBody == nil || op.RequestBody.Content["application/x-www-form-urlencoded"].Schema == nil {
		c.t.Fatal("Stator's token endpoint takes no form")
	}
	flat := map[string]string{}
	for k := range values {
		flat[k] = values.Get(k)
	}
	sent, _ := json.Marshal(flat)
	if err := c.doc.ValidateJSON(op.RequestBody.Content["application/x-www-form-urlencoded"].Schema, sent); err != nil {
		c.t.Errorf("Stator would refuse the form %s: %v", sent, err)
	}
	answer, ok := op.Responses[strconv.Itoa(want)]
	if !ok {
		c.t.Fatalf("Stator's token endpoint never answers %d", want)
	}
	if err := c.doc.ValidateJSON(answer.Content["application/json"].Schema, raw); err != nil {
		c.t.Errorf("POST %s answered %d in a shape Stator does not: %v\n%s", tokenPath, want, err, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func TestTheTokenExchangeFitsStatorsDocument(t *testing.T) {
	doc := loadStator(t)
	server := httptest.NewServer(newStub(fakeKeycloak).handler())
	defer server.Close()
	c := &contract{t: t, doc: doc, base: server.URL, served: map[string]bool{}}
	c.expect("PUT", stubPrefix+"/acme/oauth-app", "", map[string]any{"clientId": "exciter-app", "clientSecret": "s3cret", "keycloakClientId": "exciter"}, http.StatusNoContent)
	subject := func(token string) url.Values {
		return url.Values{"grant_type": {exchangestub.GrantExchange}, "subject_token": {token}, "subject_token_type": {exchangestub.TokenTypeAccess}}
	}

	if refused := c.form("exciter-app", "wrong", subject("kc|alice@suite.test|exciter|"+keycloakClient), http.StatusUnauthorized); refused["error"] != "invalid_client" {
		t.Errorf("a wrong secret is refused with %v", refused)
	}
	c.form("exciter-app", "s3cret", subject("kc|alice@suite.test|armature|"+keycloakClient), http.StatusBadRequest)
	c.form("exciter-app", "s3cret", subject("kc|alice@suite.test|exciter|armature"), http.StatusBadRequest)
	got := c.form("exciter-app", "s3cret", subject("kc|alice@suite.test|exciter|"+keycloakClient), http.StatusOK)
	access, refresh := got["access_token"].(string), got["refresh_token"].(string)
	if !strings.HasPrefix(access, accessTokenPrefix) || !strings.HasPrefix(refresh, refreshTokenPrefix) || got["token_type"] != "Bearer" {
		t.Errorf("the exchange answered %v", got)
	}
	if me := c.expect("GET", "/auth/me", access, nil, http.StatusOK)["user"].(map[string]any); me["name"] != "Alice" || me["email"] != "alice@acme.stator.test" {
		t.Errorf("an exchanged token acts as %v, want alice of the app's tenant", me)
	}

	renewed := c.form("exciter-app", "s3cret", url.Values{"grant_type": {exchangestub.GrantRefresh}, "refresh_token": {refresh}}, http.StatusOK)
	if renewed["refresh_token"] == refresh || renewed["access_token"] == access {
		t.Errorf("a refresh did not rotate the tokens: %v", renewed)
	}
	if status, _ := c.call("GET", "/auth/me", access, nil); status != http.StatusUnauthorized {
		t.Errorf("the access token before the refresh still answers %d", status)
	}
	c.expect("GET", "/auth/me", renewed["access_token"].(string), nil, http.StatusOK)

	if ended := c.form("exciter-app", "s3cret", url.Values{"grant_type": {exchangestub.GrantRefresh}, "refresh_token": {refresh}}, http.StatusBadRequest); ended["error"] != "invalid_grant" {
		t.Errorf("a replayed refresh token is refused with %v", ended)
	}
	if status, _ := c.call("GET", "/auth/me", renewed["access_token"].(string), nil); status != http.StatusUnauthorized {
		t.Errorf("a replayed refresh token left the grant standing: %d", status)
	}
}

// Command suite-seed connects the suite's Exciter, Armature and Stator through their APIs (#31), run by make
// suite-seed on the suite's network; running it again leaves what is already right.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

// Where each product answers inside the suite's network, and the accounts its own seed made.
const (
	exciterAPI  = "http://suite-exciter-api:8080/api/v1"
	armatureAPI = "http://suite-armature-api:8080/api/v1"
	statorAPI   = "http://suite-stator-api:8080/api/v1"

	exciterAdmin, exciterPassword   = "admin@exciter.test", "exciter admin password"
	statorAdmin, statorPassword     = "admin@stator.test", "stator admin password"
	armatureOwner, armaturePassword = "ada@armature.test", "demo password please"

	// armatureClient and its secret are the suite realm's, deploy/keycloak/realm.json.
	armatureClient, armatureSecret = "armature", "armature-dev-secret"
	// Armature posts to each product's api by its suite alias; Exciter and Stator check the signature, not the address.
	exciterHook = exciterAPI + "/armature/webhook"
	statorHook  = statorAPI + "/armature/webhook"
	// tokenName names the token the seed makes for alice, so an earlier one is revoked rather than piling up.
	tokenName = "Suite"
	// appName is Exciter as a connected app of Armature and Stator, which connects people at sign-in (#274), by
	// exchanging their token of the realm's exciterClient; exchangeCapability is what a partner lists when it can.
	appName            = "Exciter"
	exciterClient      = "exciter"
	exchangeCapability = "oauth.token-exchange@1"

	callTimeout = 30 * time.Second
	// envFileMode keeps the file holding alice's token hers alone.
	envFileMode = 0o600
)

// person is somebody of the suite's realm, made a member of Armature with the realm's password.
type person struct{ email, name, role, password string }

var people = []person{
	{"alice@suite.test", "Alice", "admin", "alice password"},
	{"bob@suite.test", "Bob", "member", "bob password"},
}

func main() {
	if err := run(); err != nil {
		slog.Error("the suite could not be connected", "error", err)
		os.Exit(1)
	}
}

func env(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("a setting is missing; run this through make suite-seed", "setting", key)
		os.Exit(2)
	}
	return v
}

func run() error {
	ctx := context.Background()
	keycloak := "http://localhost:" + env("SUITE_KEYCLOAK_PORT")
	armatureURL := "http://localhost:" + env("SUITE_ARMATURE_WEB_PORT")

	armature, err := signIn(ctx, armatureAPI, armatureOwner, armaturePassword)
	if err != nil {
		return fmt.Errorf("sign in to Armature as its seed's owner: %w", err)
	}
	if err := armature.send(ctx, http.MethodPut, "/oidc-provider", map[string]any{
		"issuer": keycloak + "/realms/dynamo", "clientId": armatureClient, "clientSecret": armatureSecret,
		"createGroups": true, "enabled": true,
	}, nil); err != nil {
		return fmt.Errorf("point Armature at the suite's Keycloak: %w", err)
	}
	for _, p := range people {
		if err := armature.armaturePerson(ctx, p); err != nil {
			return fmt.Errorf("make %s a member of Armature: %w", p.email, err)
		}
	}
	alice, err := signIn(ctx, armatureAPI, people[0].email, people[0].password)
	if err != nil {
		return fmt.Errorf("sign in to Armature as alice: %w", err)
	}
	token, err := alice.freshToken(ctx, "alice's Armature")
	if err != nil {
		return err
	}

	exciter, err := signIn(ctx, exciterAPI, exciterAdmin, exciterPassword)
	if err != nil {
		return fmt.Errorf("sign in to Exciter as its administrator: %w", err)
	}
	stator, err := signIn(ctx, statorAPI, statorAdmin, statorPassword)
	if err != nil {
		return fmt.Errorf("sign in to Stator as its administrator: %w", err)
	}
	exciterURL := "http://localhost:" + env("SUITE_EXCITER_WEB_PORT")
	statorURL := "http://localhost:" + env("SUITE_STATOR_WEB_PORT")
	toArmature := map[string]any{"baseUrl": armatureURL}
	id, secret, err := armature.partnerApp(ctx, map[string]any{
		"name": appName, "redirectUris": []string{exciterURL + "/api/v1/armature/oauth/callback"},
		"scopes": []string{"read", "issue.write"}, "confidential": true, "keycloakClientId": exciterClient,
	}, true)
	if err != nil {
		return fmt.Errorf("register Exciter as a connected app of Armature: %w", err)
	}
	toArmature["clientId"], toArmature["clientSecret"] = id, secret
	products := []struct {
		name, hook string
		api        *session
		connection map[string]any
	}{
		{"Exciter", exciterHook, exciter, toArmature},
		{"Stator", statorHook, stator, map[string]any{"baseUrl": armatureURL}},
	}
	for _, p := range products {
		var saved struct {
			Connection struct {
				WebhookTopics []string `json:"webhookTopics"`
			} `json:"connection"`
		}
		if err := p.api.send(ctx, http.MethodPut, "/armature/connection", p.connection, &saved); err != nil {
			return fmt.Errorf("connect %s to Armature: %w", p.name, err)
		}
		secret, err := armature.webhook(ctx, p.name, p.hook, saved.Connection.WebhookTopics)
		if err != nil {
			return fmt.Errorf("point Armature's webhooks at %s: %w", p.name, err)
		}
		p.connection["webhookSecret"] = secret
		if err := p.api.send(ctx, http.MethodPut, "/armature/connection", p.connection, nil); err != nil {
			return fmt.Errorf("store the webhook secret in %s: %w", p.name, err)
		}
		slog.Info("connected to Armature", "product", p.name)
	}

	// Exciter reads Stator's pages as each person; it connects them at sign-in once Stator offers the exchange
	// (praetorianer777/Stator#461), and until then each person pastes a Stator token.
	toStator := map[string]any{"baseUrl": statorURL}
	if offers, err := offersExchange(ctx, statorAPI); err != nil {
		return fmt.Errorf("ask Stator what it offers: %w", err)
	} else if offers {
		// Exciter puts its links on the pages it traces, so it writes as well as reads.
		id, secret, err := stator.partnerApp(ctx, map[string]any{
			"name": appName, "scopes": []string{"read", "write"}, "keycloakClientId": exciterClient,
		}, false)
		if err != nil {
			return fmt.Errorf("register Exciter as a connected app of Stator: %w", err)
		}
		toStator["clientId"], toStator["clientSecret"] = id, secret
	} else {
		slog.Info("Stator does not offer the token exchange yet, so people connect Exciter to it with a token")
	}
	if err := exciter.send(ctx, http.MethodPut, "/stator/connection", toStator, nil); err != nil {
		return fmt.Errorf("connect Exciter to Stator: %w", err)
	}
	if os.Getenv("SUITE_DEMO") == "1" {
		if err := addDemo(ctx, exciter, armature, stator, token); err != nil {
			return err
		}
	}

	lines := []string{
		"SUITE_KEYCLOAK_URL=" + keycloak,
		"SUITE_EXCITER_URL=http://localhost:" + env("SUITE_EXCITER_WEB_PORT"),
		"SUITE_ARMATURE_URL=" + armatureURL,
		"SUITE_STATOR_URL=" + statorURL,
		"SUITE_ALICE_ARMATURE_TOKEN=" + token,
	}
	return os.WriteFile(env("SUITE_ENV_FILE"), []byte(strings.Join(lines, "\n")+"\n"), envFileMode)
}

// session is somebody signed in to one product, by its session cookie.
type session struct {
	base   string
	client *http.Client
}

func signIn(ctx context.Context, base, email, password string) (*session, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	s := &session{base: base, client: &http.Client{Jar: jar, Timeout: callTimeout}}
	return s, s.send(ctx, http.MethodPost, "/auth/login", map[string]any{"email": email, "password": password}, nil)
}

// armaturePerson makes p an account of Armature, or brings an existing one to
// p's role and password: Armature's own seed lets the realm's people in too.
func (s *session) armaturePerson(ctx context.Context, p person) error {
	err := s.send(ctx, http.MethodPost, "/users", map[string]any{"email": p.email, "name": p.name, "role": p.role, "password": p.password}, nil)
	if !isConflict(err) {
		return err
	}
	var page struct {
		Users []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"users"`
	}
	if err := s.send(ctx, http.MethodGet, "/users?q="+url.QueryEscape(p.email), nil, &page); err != nil {
		return err
	}
	for _, u := range page.Users {
		if !strings.EqualFold(u.Email, p.email) {
			continue
		}
		if err := s.send(ctx, http.MethodPatch, "/users/"+u.ID, map[string]any{"role": p.role, "isActive": true}, nil); err != nil {
			return err
		}
		return s.send(ctx, http.MethodPut, "/users/"+u.ID+"/password", map[string]any{"password": p.password}, nil)
	}
	return fmt.Errorf("%s exists in Armature but is missing from its list of accounts", p.email)
}

// refusal is an answer of 400 or more, with the product's own sentence.
type refusal struct {
	status int
	body   string
}

func (r *refusal) Error() string { return fmt.Sprintf("answered %d: %s", r.status, r.body) }

func isConflict(err error) bool {
	var r *refusal
	return errors.As(err, &r) && r.status == http.StatusConflict
}

func (s *session) send(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return &refusal{status: resp.StatusCode, body: strings.TrimSpace(string(raw))}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// freshToken revokes the token an earlier run made and makes another, whose secret the product shows only once.
func (s *session) freshToken(ctx context.Context, whose string) (string, error) {
	var listed struct {
		Tokens []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tokens"`
	}
	if err := s.send(ctx, http.MethodGet, "/tokens", nil, &listed); err != nil {
		return "", fmt.Errorf("list %s tokens: %w", whose, err)
	}
	for _, t := range listed.Tokens {
		if t.Name == tokenName {
			if err := s.send(ctx, http.MethodDelete, "/tokens/"+t.ID, nil, nil); err != nil {
				return "", fmt.Errorf("revoke the earlier of %s tokens: %w", whose, err)
			}
		}
	}
	var made struct {
		Token struct {
			Secret string `json:"secret"`
		} `json:"token"`
	}
	if err := s.send(ctx, http.MethodPost, "/tokens", map[string]any{"name": tokenName}, &made); err != nil {
		return "", fmt.Errorf("make one of %s tokens: %w", whose, err)
	}
	return made.Token.Secret, nil
}

// webhook makes Armature's endpoint for a product, or gives the one an earlier run made a new secret, and answers
// the secret, which Armature shows only then.
func (s *session) webhook(ctx context.Context, name, url string, topics []string) (string, error) {
	var listed struct {
		Webhooks []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"webhooks"`
	}
	if err := s.send(ctx, http.MethodGet, "/webhooks", nil, &listed); err != nil {
		return "", err
	}
	var answer struct {
		Webhook struct {
			Secret string `json:"secret"`
		} `json:"webhook"`
	}
	at := slices.IndexFunc(listed.Webhooks, func(w struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}) bool {
		return w.Name == name
	})
	if at >= 0 {
		id := listed.Webhooks[at].ID
		if err := s.send(ctx, http.MethodPatch, "/webhooks/"+id, map[string]any{"name": name, "url": url, "topics": topics, "version": 1}, nil); err != nil {
			return "", err
		}
		err := s.send(ctx, http.MethodPost, "/webhooks/"+id+"/rotate-secret", nil, &answer)
		return answer.Webhook.Secret, err
	}
	err := s.send(ctx, http.MethodPost, "/webhooks", map[string]any{"name": name, "url": url, "topics": topics, "version": 1}, &answer)
	return answer.Webhook.Secret, err
}

// partnerApp registers Exciter as the connected app body describes, named appName, and answers its client id and
// secret, which the product shows only then. One an earlier run registered gets a new secret where the product
// rotates one, and is registered anew where it does not, as Stator.
func (s *session) partnerApp(ctx context.Context, body map[string]any, rotates bool) (id, secret string, err error) {
	type client struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Secret string `json:"secret"`
	}
	var listed struct {
		Clients []client `json:"clients"`
	}
	if err := s.send(ctx, http.MethodGet, "/oauth/clients", nil, &listed); err != nil {
		return "", "", err
	}
	var answer struct {
		Client client `json:"client"`
	}
	if at := slices.IndexFunc(listed.Clients, func(c client) bool { return c.Name == appName }); at >= 0 {
		earlier := "/oauth/clients/" + listed.Clients[at].ID
		if rotates {
			err := s.send(ctx, http.MethodPost, earlier+"/rotate-secret", nil, &answer)
			return answer.Client.ID, answer.Client.Secret, err
		}
		if err := s.send(ctx, http.MethodDelete, earlier, nil, nil); err != nil {
			return "", "", err
		}
	}
	err = s.send(ctx, http.MethodPost, "/oauth/clients", body, &answer)
	return answer.Client.ID, answer.Client.Secret, err
}

// offersExchange says whether the product at api lists the token exchange on its /meta, which anybody may read.
func offersExchange(ctx context.Context, api string) (bool, error) {
	var meta struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := (&session{base: api, client: &http.Client{Timeout: callTimeout}}).send(ctx, http.MethodGet, "/meta", nil, &meta); err != nil {
		return false, err
	}
	return slices.Contains(meta.Capabilities, exchangeCapability), nil
}

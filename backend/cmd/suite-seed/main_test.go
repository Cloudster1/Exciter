package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeProduct keeps what the seed makes at a product, webhooks, connected apps and tokens, as their APIs answer it;
// every secret it hands out is new.
type fakeProduct struct {
	mu       sync.Mutex
	made     int
	webhooks map[string]string // id to name
	clients  map[string]string
	tokens   map[string]string
}

func newFakeProduct(t *testing.T) (*fakeProduct, *session) {
	t.Helper()
	p := &fakeProduct{webhooks: map[string]string{}, clients: map[string]string{}, tokens: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(srv.Close)
	return p, &session{base: srv.URL, client: srv.Client()}
}

// locked changes what the fake keeps, which the server's goroutine writes between the seed's calls.
func (p *fakeProduct) locked(f func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f()
}

// held is a copy of one of the fake's maps, read under its lock.
func (p *fakeProduct) held(of *map[string]string) map[string]string {
	out := map[string]string{}
	p.locked(func() {
		for k, v := range *of {
			out[k] = v
		}
	})
	return out
}

func (p *fakeProduct) next(kind string) (id, secret string) {
	p.made++
	n := strconv.Itoa(p.made)
	return kind + "-" + n, kind + "-secret-" + n
}

func listed(of map[string]string) []map[string]string {
	out := []map[string]string{}
	for id, name := range of {
		out = append(out, map[string]string{"id": id, "name": name})
	}
	return out
}

func (p *fakeProduct) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	name, _ := body["name"].(string)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	answer := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/webhooks":
		answer(map[string]any{"webhooks": listed(p.webhooks)})
	case r.Method == http.MethodPost && r.URL.Path == "/webhooks":
		id, secret := p.next("webhook")
		p.webhooks[id] = name
		answer(map[string]any{"webhook": map[string]string{"id": id, "secret": secret}})
	case r.Method == http.MethodPatch && len(parts) == 2 && parts[0] == "webhooks" && p.webhooks[parts[1]] != "":
		p.webhooks[parts[1]] = name
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "webhooks" && parts[2] == "rotate-secret" && p.webhooks[parts[1]] != "":
		_, secret := p.next("webhook")
		answer(map[string]any{"webhook": map[string]string{"id": parts[1], "secret": secret}})
	case r.Method == http.MethodGet && r.URL.Path == "/oauth/clients":
		answer(map[string]any{"clients": listed(p.clients)})
	case r.Method == http.MethodPost && r.URL.Path == "/oauth/clients":
		id, secret := p.next("client")
		p.clients[id] = name
		answer(map[string]any{"client": map[string]string{"id": id, "name": name, "secret": secret}})
	case r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "clients" && parts[3] == "rotate-secret" && p.clients[parts[2]] != "":
		_, secret := p.next("client")
		answer(map[string]any{"client": map[string]string{"id": parts[2], "name": p.clients[parts[2]], "secret": secret}})
	case r.Method == http.MethodDelete && len(parts) == 3 && parts[1] == "clients" && p.clients[parts[2]] != "":
		delete(p.clients, parts[2])
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/tokens":
		answer(map[string]any{"tokens": listed(p.tokens)})
	case r.Method == http.MethodPost && r.URL.Path == "/tokens":
		id, secret := p.next("token")
		p.tokens[id] = name
		answer(map[string]any{"token": map[string]string{"id": id, "secret": secret}})
	case r.Method == http.MethodDelete && len(parts) == 2 && parts[0] == "tokens" && p.tokens[parts[1]] != "":
		delete(p.tokens, parts[1])
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "no such route in the fake", http.StatusNotFound)
	}
}

// Running the seed again gives each thing it makes a new secret and makes none of them twice, as its doc says.
func TestASecondRunGivesNewSecretsAndMakesNothingTwice(t *testing.T) {
	ctx := context.Background()
	product, s := newFakeProduct(t)
	topics := []string{"issue.updated"}

	first, err := s.webhook(ctx, "Exciter", "http://exciter/hook", topics)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.webhook(ctx, "Exciter", "http://exciter/hook", topics)
	if err != nil || again == "" || again == first {
		t.Errorf("the second run's webhook secret is %q after %q: %v", again, first, err)
	}
	if webhooks := product.held(&product.webhooks); len(webhooks) != 1 {
		t.Errorf("two runs made %d webhooks, want one", len(webhooks))
	}

	for _, rotates := range []bool{true, false} {
		product.locked(func() { product.clients = map[string]string{} })
		_, firstSecret, err := s.partnerApp(ctx, map[string]any{"name": appName}, rotates)
		if err != nil {
			t.Fatal(err)
		}
		id, secret, err := s.partnerApp(ctx, map[string]any{"name": appName}, rotates)
		clients := product.held(&product.clients)
		if err != nil || secret == "" || secret == firstSecret || clients[id] != appName {
			t.Errorf("rotates %v: the second run's app is %q with %q after %q: %v", rotates, id, secret, firstSecret, err)
		}
		if len(clients) != 1 {
			t.Errorf("rotates %v: two runs registered %d apps, want one", rotates, len(clients))
		}
	}

	product.locked(func() { product.tokens["kept"] = "Another token" })
	token, err := s.freshToken(ctx, "alice's")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.freshToken(ctx, "alice's")
	if err != nil || fresh == "" || fresh == token {
		t.Errorf("the second run's token is %q after %q: %v", fresh, token, err)
	}
	tokens := product.held(&product.tokens)
	named := 0
	for _, name := range tokens {
		if name == tokenName {
			named++
		}
	}
	if named != 1 || tokens["kept"] == "" {
		t.Errorf("two runs leave %d tokens named %s and %v", named, tokenName, tokens)
	}
}

func TestOnlyA409IsAConflict(t *testing.T) {
	for status, want := range map[int]bool{http.StatusConflict: true, http.StatusBadRequest: false, http.StatusUnprocessableEntity: false, http.StatusInternalServerError: false} {
		if got := isConflict(&refusal{status: status}); got != want {
			t.Errorf("isConflict(%d) = %v, want %v", status, got, want)
		}
	}
	if isConflict(context.DeadlineExceeded) || isConflict(nil) {
		t.Error("an error that is no answer is a conflict")
	}
}

//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// makeToken makes a personal access token from a session and answers its secret.
func makeToken(t *testing.T, c *client, body map[string]any) (id, secret string) {
	t.Helper()
	made := obj(t, want(t, c.post(t, "/api/v1/tokens", body), http.StatusCreated, "make a token"), "token")
	secret, _ = made["secret"].(string)
	if !strings.HasPrefix(secret, "exciter_pat_") {
		t.Fatalf("the secret %q is not a personal access token", secret)
	}
	return made["id"].(string), secret
}

// rpc calls the assistants' endpoint and answers the result of a successful JSON-RPC answer.
func rpc(t *testing.T, c *client, method string, params map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	got := want(t, c.post(t, "/api/v1/mcp", body), http.StatusOK, "call "+method)
	result, ok := got.Body["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s answered no result: %s", method, got.Raw)
	}
	return result
}

// expire moves a token's expiry into the past, past the trigger that keeps a token as it was made.
func expire(t *testing.T, h *harness, id string) {
	t.Helper()
	ctx := context.Background()
	tx, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE api_token SET expires_at = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func toolText(result map[string]any) string {
	return result["content"].([]any)[0].(map[string]any)["text"].(string)
}

// A person makes tokens from a session and a script acts with one as them: a full token as far as they may, a
// read-only one reading alone, and a limited one inside its projects; the MCP endpoint offers each what it may.
func TestTokensOverTheAPI(t *testing.T) {
	f := newProjectFixture(t)
	for _, key := range []string{"TOK", "OTH"} {
		want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
		want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Pay the basket"}), http.StatusCreated, "write in "+key)
	}
	person := f.plain

	t.Run("made from a session alone, once with its secret, and listed without it", func(t *testing.T) {
		refuse(t, "nobody signed in", f.api.anonymous().post(t, "/api/v1/tokens", map[string]any{"name": "x"}), http.StatusUnauthorized, "unauthorized")
		refuse(t, "no name", person.post(t, "/api/v1/tokens", map[string]any{"name": " "}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a scope nobody enforces", person.post(t, "/api/v1/tokens", map[string]any{"name": "x", "scopes": []string{"write"}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "an expiry gone by", person.post(t, "/api/v1/tokens", map[string]any{"name": "x", "expiresAt": "2020-01-01T00:00:00Z"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a project nobody has", person.post(t, "/api/v1/tokens", map[string]any{"name": "x", "projects": []string{"NOPE"}}), http.StatusUnprocessableEntity, "validation_failed")
		_, secret := makeToken(t, person, map[string]any{"name": "deploy"})
		refuse(t, "a token makes a token", f.api.withToken(person.user, secret).post(t, "/api/v1/tokens", map[string]any{"name": "more"}), http.StatusForbidden, "session_only")
		listed := list(t, want(t, person.get(t, "/api/v1/tokens"), http.StatusOK, "list mine"), "tokens")
		if len(listed) != 1 || listed[0].(map[string]any)["secret"] != nil || listed[0].(map[string]any)["allProjects"] != true {
			t.Errorf("the list shows the token once, without its secret: %v", listed)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM api_token WHERE token_hash = sha256(convert_to($1, 'UTF8'))`, secret); n != 1 {
			t.Error("the token is not kept as the SHA-256 of its secret")
		}
	})

	t.Run("a full token acts as its owner in its organization, and says so", func(t *testing.T) {
		_, secret := makeToken(t, person, map[string]any{"name": "full"})
		script := f.api.withToken(person.user, secret)
		me := want(t, script.get(t, "/api/v1/auth/me"), http.StatusOK, "who am I")
		token := obj(t, me, "token")
		if token["name"] != "full" || token["allProjects"] != true || me.Body["role"] == nil {
			t.Errorf("/auth/me with a token: %s", me.Raw)
		}
		if want(t, person.get(t, "/api/v1/auth/me"), http.StatusOK, "a session").Body["token"] != nil {
			t.Error("a session reports a token")
		}
		want(t, script.post(t, "/api/v1/projects/TOK/requirements", map[string]any{"title": "Written by a script"}), http.StatusCreated, "write with it")
		refuse(t, "a malformed token", f.api.withToken(person.user, "exciter_pat_short").get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("a read-only token reads and is refused every write, through MCP too", func(t *testing.T) {
		_, secret := makeToken(t, person, map[string]any{"name": "read", "scopes": []string{"read"}})
		reader := f.api.withToken(person.user, secret)
		want(t, reader.get(t, "/api/v1/requirements/TOK-R1"), http.StatusOK, "read with it")
		refuse(t, "a write", reader.post(t, "/api/v1/projects/TOK/requirements", map[string]any{"title": "No"}), http.StatusForbidden, "read_only_token")
		refuse(t, "a change of language", reader.patch(t, "/api/v1/auth/me", map[string]any{"locale": "de"}), http.StatusForbidden, "read_only_token")
		tools := rpc(t, reader, "tools/list", nil)["tools"].([]any)
		for _, each := range tools {
			if each.(map[string]any)["name"] == "create_requirement" {
				t.Error("a read-only token is offered create_requirement")
			}
		}
		got := rpc(t, reader, "tools/call", map[string]any{"name": "get_requirement", "arguments": map[string]any{"reqKey": "TOK-R1"}})
		if got["isError"] == true || !strings.Contains(toolText(got), "Pay the basket") {
			t.Errorf("get_requirement through MCP: %v", got)
		}
		refused := rpc(t, reader, "tools/call", map[string]any{"name": "create_requirement", "arguments": map[string]any{"projectKey": "TOK", "title": "No"}})
		if refused["isError"] != true || !strings.Contains(toolText(refused), "can only read") {
			t.Errorf("create_requirement with a read-only token: %v", refused)
		}
	})

	t.Run("a token limited to projects reaches nothing else and nothing organization-wide", func(t *testing.T) {
		_, secret := makeToken(t, person, map[string]any{"name": "limited", "projects": []string{"tok"}})
		limited := f.api.withToken(person.user, secret)
		if keys := keysOf(t, want(t, limited.get(t, "/api/v1/projects"), http.StatusOK, "list projects")); strings.Join(keys, ",") != "TOK" {
			t.Errorf("a token limited to TOK lists %v", keys)
		}
		refuse(t, "a project outside it", limited.get(t, "/api/v1/projects/OTH"), http.StatusNotFound, "not_found")
		refuse(t, "a requirement outside it", limited.get(t, "/api/v1/requirements/OTH-R1"), http.StatusNotFound, "not_found")
		refuse(t, "the tokens", limited.get(t, "/api/v1/tokens"), http.StatusForbidden, "projects_token")
		refuse(t, "a new project", limited.post(t, "/api/v1/projects", map[string]any{"key": "NEW", "name": "New"}), http.StatusForbidden, "projects_token")
		want(t, limited.post(t, "/api/v1/projects/TOK/requirements", map[string]any{"title": "Inside"}), http.StatusCreated, "write inside it")
		token := obj(t, want(t, limited.get(t, "/api/v1/auth/me"), http.StatusOK, "who am I"), "token")
		if token["allProjects"] != false || strings.Join(toStrings(token["projects"]), ",") != "TOK" {
			t.Errorf("/auth/me reports the limit as %v", token)
		}
		got := rpc(t, limited, "tools/call", map[string]any{"name": "list_projects", "arguments": map[string]any{}})
		if text := toolText(got); !strings.Contains(text, `"TOK"`) || strings.Contains(text, `"OTH"`) {
			t.Errorf("list_projects through MCP: %s", text)
		}
	})

	t.Run("revoked by its owner or an administrator, it stops at once", func(t *testing.T) {
		id, secret := makeToken(t, person, map[string]any{"name": "short-lived"})
		script := f.api.withToken(person.user, secret)
		want(t, script.get(t, "/api/v1/auth/me"), http.StatusOK, "works")
		refuse(t, "somebody else's", f.owner.delete(t, "/api/v1/tokens/"+id), http.StatusNotFound, "not_found")
		want(t, person.delete(t, "/api/v1/tokens/"+id), http.StatusNoContent, "revoke it")
		refuse(t, "a revoked token", script.get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "unauthorized")

		otherID, otherSecret := makeToken(t, person, map[string]any{"name": "admin takes"})
		refuse(t, "a member lists the organization's", person.get(t, "/api/v1/tokens/everyone"), http.StatusForbidden, "forbidden")
		all := list(t, want(t, f.owner.get(t, "/api/v1/tokens/everyone"), http.StatusOK, "the organization's"), "tokens")
		found := false
		for _, each := range all {
			tok := each.(map[string]any)
			if tok["id"] == otherID && tok["owner"].(map[string]any)["id"] == person.user.String() {
				found = true
			}
		}
		if !found {
			t.Errorf("the organization's list lacks the member's token: %v", all)
		}
		refuse(t, "a member revokes through the organization's list", person.delete(t, "/api/v1/tokens/everyone/"+otherID), http.StatusForbidden, "forbidden")
		want(t, f.owner.delete(t, "/api/v1/tokens/everyone/"+otherID), http.StatusNoContent, "an administrator revokes it")
		refuse(t, "revoked by an administrator", f.api.withToken(person.user, otherSecret).get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "unauthorized")
		if n := f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action IN ('token.created', 'token.revoked')`, f.home.org); n < 2 {
			t.Errorf("the audit log holds %d token entries", n)
		}
	})

	t.Run("an expired token is nobody", func(t *testing.T) {
		id, secret := makeToken(t, person, map[string]any{"name": "expiring", "expiresAt": "2099-01-01T00:00:00Z"})
		expire(t, f.h, id)
		refuse(t, "an expired token", f.api.withToken(person.user, secret).get(t, "/api/v1/auth/me"), http.StatusUnauthorized, "unauthorized")
		refuse(t, "the endpoint without anybody", f.api.anonymous().post(t, "/api/v1/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}),
			http.StatusUnauthorized, "unauthorized")
	})
}

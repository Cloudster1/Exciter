//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

const (
	providerPath   = "/api/v1/oidc-provider"
	groupRolesPath = "/api/v1/oidc-provider/group-roles"
)

// An administrator sets the provider up through the settings page's API; the secret goes in and never comes
// out, a blank one keeps it, and every save is on the record with where it came from.
func TestTheProviderIsTheAdministratorsToConfigure(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	org := h.makeOrg(t, "provider-settings")
	admin, adminID := adminOf(t, h, a, org)
	member := a.as(t, h.addPerson(t, org.ID, "member"), org.ID)
	callback := a.URL + httpapi.APIPrefix + "/auth/oidc/callback"

	t.Run("before it is set up there is none, and the callback to register", func(t *testing.T) {
		got := want(t, admin.get(t, providerPath), http.StatusOK, "read the provider")
		if got.Body["provider"] != nil || got.Body["callbackUrl"] != callback {
			t.Fatalf("provider = %s", got.Raw)
		}
	})

	t.Run("an issuer that is no URL is refused by field", func(t *testing.T) {
		got := want(t, admin.put(t, providerPath, map[string]any{"issuer": "id.example.test", "clientId": "exciter", "enabled": true, "createGroups": false}),
			http.StatusUnprocessableEntity, "save a bad issuer")
		if errorCode(t, got) != "validation_failed" || obj(t, got, "error", "fields")["issuer"] == nil {
			t.Fatalf("a bad issuer = %s", got.Raw)
		}
	})

	body := map[string]any{"issuer": "https://id.example.test/realms/acme/", "clientId": " exciter ", "clientSecret": "the client secret", "enabled": true, "createGroups": true}
	t.Run("saving trims, defaults, and never hands the secret back", func(t *testing.T) {
		got := want(t, admin.put(t, providerPath, body), http.StatusOK, "save the provider")
		p := obj(t, got, "provider")
		if p["issuer"] != "https://id.example.test/realms/acme" || p["clientId"] != "exciter" || p["hasSecret"] != true || p["groupsClaim"] != "groups" || p["scopes"] != "openid profile email" {
			t.Fatalf("saved provider = %s", got.Raw)
		}
		if strings.Contains(string(got.Raw), "the client secret") {
			t.Fatal("the answer carries the client secret")
		}
		delete(body, "clientSecret")
		body["enabled"] = false
		again := want(t, admin.put(t, providerPath, body), http.StatusOK, "save without the secret")
		if p := obj(t, again, "provider"); p["hasSecret"] != true || p["enabled"] != false {
			t.Fatalf("a save without a secret = %s, want the stored secret kept", again.Raw)
		}
	})

	t.Run("each save is recorded with its address and without the secret", func(t *testing.T) {
		rows, err := h.super.Query(context.Background(), `
			SELECT data, host(ip) FROM audit_log WHERE org_id = $1 AND action = 'sso.provider_saved' AND actor_user_id = $2
			ORDER BY created_at, id`, org.ID, adminID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var secrets []string
		for rows.Next() {
			var raw []byte
			var ip string
			if err := rows.Scan(&raw, &ip); err != nil {
				t.Fatal(err)
			}
			var data map[string]any
			_ = json.Unmarshal(raw, &data)
			secrets = append(secrets, data["clientSecret"].(string))
			if ip != "127.0.0.1" {
				t.Errorf("the save was recorded from %q, want the caller's address", ip)
			}
		}
		if strings.Join(secrets, " ") != "set kept" {
			t.Fatalf("recorded secrets = %v, want set then kept", secrets)
		}
	})

	t.Run("a member neither reads nor changes it", func(t *testing.T) {
		want(t, member.get(t, providerPath), http.StatusForbidden, "a member reads the provider")
		want(t, member.put(t, providerPath, body), http.StatusForbidden, "a member saves the provider")
	})
}

// Group roles need a provider, a group and a grantable role, and are an administrator's alone.
func TestGroupRolesAreRefusedWhatCannotWork(t *testing.T) {
	h := newHarness(t)
	a := newAPIServer(t, h)
	org := h.makeOrg(t, "group-role-refusals")
	admin, _ := adminOf(t, h, a, org)
	member := a.as(t, h.addPerson(t, org.ID, "member"), org.ID)

	got := want(t, admin.post(t, groupRolesPath, map[string]any{"group": "engineering", "role": "member"}), http.StatusUnprocessableEntity, "map before a provider")
	if msg, _ := obj(t, got, "error", "fields")["group"].(string); !strings.Contains(msg, "single sign-on first") {
		t.Errorf("mapping without a provider says %s", got.Raw)
	}
	want(t, admin.put(t, providerPath, map[string]any{"issuer": "https://id.example.test", "clientId": "exciter", "enabled": true, "createGroups": false}), http.StatusOK, "set up a provider")
	for name, attempt := range map[string]map[string]any{
		"a blank group":   {"group": "  ", "role": "member"},
		"ownership":       {"group": "founders", "role": "owner"},
		"a role made up":  {"group": "founders", "role": "king"},
		"no role at all ": {"group": "founders"},
	} {
		if code := errorCode(t, want(t, admin.post(t, groupRolesPath, attempt), http.StatusUnprocessableEntity, name)); code != "validation_failed" {
			t.Errorf("%s answers %s", name, code)
		}
	}
	want(t, member.get(t, groupRolesPath), http.StatusForbidden, "a member lists the mapping")
	want(t, member.post(t, groupRolesPath, map[string]any{"group": "engineering", "role": "admin"}), http.StatusForbidden, "a member maps a group")
	if code := errorCode(t, want(t, admin.delete(t, groupRolesPath+"/"+h.suffix()+"-not-a-uuid"), http.StatusBadRequest, "unmap a malformed id")); code == "" {
		t.Error("a malformed id answers without a code")
	}
}

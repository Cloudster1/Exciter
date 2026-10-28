//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Straight through SQL as exciter_app (#327): a page link opens only under the connected Stator's address and is never
// moved by its editors, a Stator token needs a connection, and a person whose role is taken loses every token.
func TestRawSQLKeepsStatorLinksAtTheirPageAndTokensToAConnection(t *testing.T) {
	key := projectKey(t, "SGL")
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-guards")
	tenant := stubTenant(t)
	owner := api.as(t, home.user)
	editorID, carolID := h.addPerson(t, "member"), h.addPerson(t, "member")
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t)}), http.StatusOK, "connect Stator")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Guarded pages"}), http.StatusCreated, "make a project")
	live := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Live"}), http.StatusCreated, "write a requirement"), "requirement")
	h.settle(t)
	ctx := context.Background()
	editor := h.appConn(t, editorID)
	insert := `INSERT INTO stator_page_link (requirement_id, relation, page_id, space_key, url, created_by) VALUES ($1, 'specifies', $2, 'ENG', $3, $4) RETURNING id`
	linksOfLive := func() int {
		return h.countRows(t, `SELECT count(*) FROM stator_page_link WHERE requirement_id = $1`, live["id"])
	}

	t.Run("a link opens under the connected Stator's address alone", func(t *testing.T) {
		page := uuid.New()
		_, err := editor.Exec(ctx, insert, live["id"], page, "javascript:alert(1)", editorID)
		if !isCheckViolation(err) {
			t.Errorf("a link to javascript:alert(1) was taken: %v", err)
		}
		_, err = editor.Exec(ctx, insert, live["id"], page, "https://evil.example/s/ENG/p/"+page.String(), editorID)
		if !isCheckViolation(err) || !strings.Contains(err.Error(), "connected Stator's address") {
			t.Errorf("a link to another host was taken: %v", err)
		}
		_, err = editor.Exec(ctx, insert, live["id"], page, statorURL(t)+".evil.example/s/ENG/p/"+page.String(), editorID)
		if !isCheckViolation(err) {
			t.Errorf("a link to a host that only starts like Stator's was taken: %v", err)
		}
		if n := linksOfLive(); n != 0 {
			t.Fatalf("the refused links left %d", n)
		}
		var id uuid.UUID
		if err := editor.QueryRow(ctx, insert, live["id"], page, statorURL(t)+"/s/ENG/p/"+page.String(), editorID).Scan(&id); err != nil {
			t.Fatalf("a link under the connection's address is refused: %v", err)
		}
		for what, sql := range map[string]string{
			"moving the link's address": `UPDATE stator_page_link SET url = 'https://evil.example/s/ENG/p/x' WHERE id = $1`,
			"moving the link's space":   `UPDATE stator_page_link SET space_key = 'SEC' WHERE id = $1`,
		} {
			if _, err := editor.Exec(ctx, sql, id); !isPermissionDenied(err) {
				t.Errorf("%s was not refused by privilege: %v", what, err)
			}
		}
		// The address check stands under the trigger too: the webhook's own writes, as the admin role, keep to it.
		if _, err := h.super.Exec(ctx, `UPDATE stator_page_link SET url = 'javascript:alert(1)' WHERE id = $1`, id); !refusedBy(err, "stator_page_link_url_check") {
			t.Errorf("even the superuser gave the link a javascript: address: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM stator_page_link WHERE id = $1 AND space_key = 'ENG' AND url = $2`, id, statorURL(t)+"/s/ENG/p/"+page.String()); n != 1 {
			t.Error("the refused updates moved the link")
		}
	})

	t.Run("a person whose role is taken loses a pasted token and one from a sign-in", func(t *testing.T) {
		want(t, api.as(t, editorID).put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "the editor pastes a token")
		// What a sign-in's exchange stores, written as the service's admin role does.
		h.cleanupExec(t, h.super, `
			INSERT INTO stator_token (user_id, token, status, source, refresh_token, access_expires_at)
			VALUES ($1, '\x00', 'ok', 'exchange', '\x00', now() + interval '1 hour')`, carolID)
		both := []uuid.UUID{editorID, carolID}
		if n := h.countRows(t, `SELECT count(*) FROM stator_token WHERE user_id = ANY ($1)`, both); n != 2 {
			t.Fatalf("%d tokens before the roles go, want 2", n)
		}
		h.cleanupExec(t, h.super, `UPDATE app_user SET role = NULL WHERE id = ANY ($1)`, both)
		if n := h.countRows(t, `SELECT count(*) FROM stator_token WHERE user_id = ANY ($1)`, both); n != 0 {
			t.Errorf("%d tokens outlived their people's roles", n)
		}
	})

	t.Run("a token needs a connection to be stored", func(t *testing.T) {
		bobID := h.addPerson(t, "member")
		want(t, owner.delete(t, "/api/v1/stator/connection"), http.StatusNoContent, "disconnect Stator")
		h.settle(t)
		_, err := h.appConn(t, bobID).Exec(ctx, `INSERT INTO stator_token (user_id, token, status) VALUES ($1, '\x00', 'ok')`, bobID)
		if !refusedBy(err, "stator_token_needs_stator_connection") {
			t.Errorf("a token without a connection was taken: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM stator_token WHERE user_id = $1`, bobID); n != 0 {
			t.Errorf("bob holds %d tokens without a connection", n)
		}
	})
}

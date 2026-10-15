//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Straight through SQL as exciter_app: a member reads and changes only their own Stator token, only administrators
// the connection, and nobody what Stator offers.
func TestRawSQLWallsStatorRows(t *testing.T) {
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "stator-wall")
	tenant := stubTenant(t)
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "alice")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects")
	h.settle(t)

	t.Run("a member reads their own token and where Stator is, nothing more", func(t *testing.T) {
		conn := h.appConn(t, bobID)
		if n := seesRows(t, conn, `SELECT count(*) FROM stator_token`); n != 1 {
			t.Errorf("bob reads %d tokens, want his own", n)
		}
		if n := seesRows(t, conn, `SELECT count(*) FROM stator_connection`); n != 0 {
			t.Error("bob reads the connection with its sealed secret")
		}
		if n := seesRows(t, conn, `SELECT count(*) FROM stator_endpoint()`); n != 1 {
			t.Error("bob cannot learn where Stator is")
		}
		var connected *int
		if err := conn.QueryRow(context.Background(), `SELECT stator_connected_count()`).Scan(&connected); err != nil || connected != nil {
			t.Errorf("bob counts the connected members: %v %v", connected, err)
		}
		touchesNothing(t, conn, "pointing the connection elsewhere", `UPDATE stator_connection SET base_url = 'https://evil.example'`)
		touchesNothing(t, conn, "removing the connection", `DELETE FROM stator_connection`)
		touchesNothing(t, conn, "marking the owner's token", `UPDATE stator_token SET status = 'rejected' WHERE user_id = $1`, home.user)
		touchesNothing(t, conn, "removing the owner's token", `DELETE FROM stator_token WHERE user_id = $1`, home.user)
		refusedOutright(t, conn, "storing a token for the owner", `INSERT INTO stator_token (user_id, token, status) VALUES ($1, '\x00', 'ok')`, home.user)
		refusedOutright(t, conn, "rewriting his token in place", `UPDATE stator_token SET token = '\x00' WHERE user_id = $1`, bobID)
		refusedOutright(t, conn, "choosing his row id", `INSERT INTO stator_token (user_id, id, token, status) VALUES ($1, $2, '\x00', 'ok')`, bobID, uuid.New())
		refusedOutright(t, conn, "connecting Stator", `INSERT INTO stator_connection (base_url) VALUES ('https://evil.example')`)
	})

	t.Run("an administrator keeps the connection and still reads only their own token", func(t *testing.T) {
		conn := h.appConn(t, home.user)
		if n := seesRows(t, conn, `SELECT count(*) FROM stator_token`); n != 1 {
			t.Errorf("the owner reads %d tokens, want their own", n)
		}
		if n := seesRows(t, conn, `SELECT stator_connected_count()`); n != 2 {
			t.Errorf("the owner counts %d connected members, want 2", n)
		}
		refusedOutright(t, conn, "claiming Stator takes remote links", `UPDATE stator_connection SET remote_links = true`)
		refusedOutright(t, conn, "a base URL with a path", `UPDATE stator_connection SET base_url = 'https://elsewhere.example/api'`)
		if _, err := conn.Exec(context.Background(), `UPDATE stator_connection SET base_url = 'https://elsewhere.example'`); err != nil {
			t.Fatalf("the owner moves the connection: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM stator_token`); n != 0 {
			t.Errorf("raw SQL moved the connection and kept %d tokens", n)
		}
	})

	t.Run("a member whose role is taken away loses their token", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t)}), http.StatusOK, "connect Stator again")
		want(t, bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects again")
		if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET role = NULL WHERE id = $1`, bobID); err != nil {
			t.Fatal(err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM stator_token WHERE user_id = $1`, bobID); n != 0 {
			t.Errorf("bob keeps %d tokens without a role", n)
		}
	})
}

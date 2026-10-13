//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// refusedOutright fails unless the database refuses the statement with an error.
func refusedOutright(t *testing.T, conn *pgx.Conn, what, sql string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql, args...); err == nil {
		t.Errorf("%s went through", what)
	}
}

// touchesNothing fails unless the statement runs and finds no row it may change.
func touchesNothing(t *testing.T, conn *pgx.Conn, what, sql string, args ...any) {
	t.Helper()
	tag, err := conn.Exec(context.Background(), sql, args...)
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	if tag.RowsAffected() != 0 {
		t.Errorf("%s changed %d rows", what, tag.RowsAffected())
	}
}

// Straight through SQL as exciter_app: a member reads and changes only their own token, only administrators the
// connection, and nobody another organization's.
func TestRawSQLWallsArmatureRows(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-wall")
	other := h.makeMember(t, "arm-wall-other")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug, "webhookSecret": webhookSecret}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects")
	h.settle(t)

	t.Run("a member reads their own token and where Armature is, nothing more", func(t *testing.T) {
		conn := h.appConn(t, home.org, bobID)
		if n := seesRows(t, conn, `SELECT count(*) FROM armature_token`); n != 1 {
			t.Errorf("bob reads %d tokens, want his own", n)
		}
		if n := seesRows(t, conn, `SELECT count(*) FROM armature_connection`); n != 0 {
			t.Error("bob reads the connection with its sealed secret")
		}
		if n := seesRows(t, conn, `SELECT count(*) FROM armature_endpoint()`); n != 1 {
			t.Error("bob cannot learn where Armature is")
		}
		var connected *int
		if err := conn.QueryRow(context.Background(), `SELECT armature_connected_count()`).Scan(&connected); err != nil || connected != nil {
			t.Errorf("bob counts the connected members: %v %v", connected, err)
		}
		touchesNothing(t, conn, "pointing the connection elsewhere", `UPDATE armature_connection SET base_url = 'https://evil.example'`)
		touchesNothing(t, conn, "removing the connection", `DELETE FROM armature_connection`)
		touchesNothing(t, conn, "marking the owner's token", `UPDATE armature_token SET status = 'rejected' WHERE user_id = $1`, home.user)
		touchesNothing(t, conn, "removing the owner's token", `DELETE FROM armature_token WHERE user_id = $1`, home.user)
		refusedOutright(t, conn, "storing a token for the owner", `INSERT INTO armature_token (org_id, user_id, token, status) VALUES ($1, $2, '\x00', 'ok')`, home.org, home.user)
		refusedOutright(t, conn, "rewriting his token in place", `UPDATE armature_token SET token = '\x00' WHERE user_id = $1`, bobID)
		refusedOutright(t, conn, "choosing his row id", `INSERT INTO armature_token (org_id, user_id, id, token, status) VALUES ($1, $2, $3, '\x00', 'ok')`, home.org, bobID, uuid.New())
		refusedOutright(t, conn, "connecting Armature", `INSERT INTO armature_connection (org_id, base_url, org_slug) VALUES ($1, 'https://evil.example', 'evil')`, home.org)
	})

	t.Run("an administrator keeps the connection and still reads only their own token", func(t *testing.T) {
		conn := h.appConn(t, home.org, home.user)
		if n := seesRows(t, conn, `SELECT count(*) FROM armature_token`); n != 1 {
			t.Errorf("the owner reads %d tokens, want their own", n)
		}
		if n := seesRows(t, conn, `SELECT armature_connected_count()`); n != 2 {
			t.Errorf("the owner counts %d connected members, want 2", n)
		}
		refusedOutright(t, conn, "pinning the Armature organization", `UPDATE armature_connection SET armature_org_id = $1`, uuid.New())
		refusedOutright(t, conn, "a base URL with a path", `UPDATE armature_connection SET base_url = 'https://elsewhere.example/api'`)
		if _, err := conn.Exec(context.Background(), `UPDATE armature_connection SET base_url = 'https://elsewhere.example'`); err != nil {
			t.Fatalf("the owner moves the connection: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_token WHERE org_id = $1`, home.org); n != 0 {
			t.Errorf("raw SQL moved the connection and kept %d tokens", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM armature_connection WHERE org_id = $1 AND armature_org_id IS NULL`, home.org); n != 1 {
			t.Error("the learned organization id survived the move")
		}
	})

	t.Run("another organization reaches none of it", func(t *testing.T) {
		conn := h.appConn(t, other.org, other.user)
		if n := seesRows(t, conn, `SELECT count(*) FROM armature_connection WHERE org_id = $1`, home.org); n != 0 {
			t.Error("another organization reads the connection")
		}
		if n := seesRows(t, conn, `SELECT count(*) FROM armature_endpoint()`); n != 0 {
			t.Error("another organization learns where Armature is")
		}
		touchesNothing(t, conn, "removing another organization's connection", `DELETE FROM armature_connection WHERE org_id = $1`, home.org)
		refusedOutright(t, conn, "connecting another organization", `INSERT INTO armature_connection (org_id, base_url, org_slug) VALUES ($1, 'https://evil.example', 'evil')`, home.org)
		refusedOutright(t, conn, "storing a token in another organization", `INSERT INTO armature_token (org_id, user_id, token, status) VALUES ($1, $2, '\x00', 'ok')`, home.org, other.user)
	})
}

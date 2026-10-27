//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// A member may write the log but not read it back, so each row carries a mark to be found by.
const insertAudit = `INSERT INTO audit_log (actor_user_id, action, target_type, via_kind, via_id, via_name, data)
	VALUES ($1, 'token.created', 'api_token', $2, $3, $4, jsonb_build_object('mark', $5::text))`

// viaOf reads what an audit row says it was written through.
func viaOf(t *testing.T, h *harness, id uuid.UUID) (kind, name *string) {
	t.Helper()
	if err := h.super.QueryRow(context.Background(), `SELECT via_kind, via_name FROM audit_log WHERE id = $1`, id).Scan(&kind, &name); err != nil {
		t.Fatal(err)
	}
	return kind, name
}

// Straight through SQL as exciter_app, the database writes what a row came through from the setting alone: a value
// sent by hand is overwritten, none is changed, and somebody else's token names nothing (#190).
func TestRawSQLStampsWhatARowWasWrittenThrough(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	person, stranger := h.addPerson(t, "member"), h.addPerson(t, "member")
	var token, theirs uuid.UUID
	for owner, into := range map[uuid.UUID]*uuid.UUID{person: &token, stranger: &theirs} {
		if err := h.super.QueryRow(ctx, insertToken, owner, aDigest(t), false).Scan(into); err != nil {
			t.Fatal(err)
		}
	}
	conn := h.appConn(t, person)
	write := func(kind, name *string) uuid.UUID {
		t.Helper()
		var viaID *uuid.UUID
		if kind != nil {
			viaID = &token
		}
		mark := uuid.New()
		if _, err := conn.Exec(ctx, insertAudit, person, kind, viaID, name, mark.String()); err != nil {
			t.Fatalf("write an audit entry: %v", err)
		}
		var id uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT id FROM audit_log WHERE data->>'mark' = $1`, mark.String()).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	forged, script := "oauth", "a forged name"

	t.Run("a session's row says nothing, whatever it sends", func(t *testing.T) {
		if kind, name := viaOf(t, h, write(&forged, &script)); kind != nil || name != nil {
			t.Errorf("a forged via is kept: %v %v", *kind, *name)
		}
	})

	t.Run("a row written through a token names it, whatever is sent", func(t *testing.T) {
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.ViaVar, token.String()); err != nil {
			t.Fatal(err)
		}
		id := write(&forged, &script)
		if kind, name := viaOf(t, h, id); kind == nil || *kind != "token" || *name != "script" {
			t.Errorf("the row is stamped %v %v", kind, name)
		}
		if _, err := h.super.Exec(ctx, `UPDATE audit_log SET via_name = 'renamed' WHERE id = $1`, id); err == nil {
			t.Error("what a row was written through was changed")
		}
		if _, err := h.super.Exec(ctx, `DELETE FROM api_token WHERE id = $1`, token); err != nil {
			t.Fatal(err)
		}
		if _, name := viaOf(t, h, id); name == nil || *name != "script" {
			t.Errorf("a revoked token's name is lost: %v", name)
		}
	})

	t.Run("somebody else's token names nothing", func(t *testing.T) {
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.ViaVar, theirs.String()); err != nil {
			t.Fatal(err)
		}
		if kind, _ := viaOf(t, h, write(nil, nil)); kind != nil {
			t.Errorf("a token held by somebody else is named: %v", *kind)
		}
	})
}

// viaFixed recognises the refusal of a change to what a row was written through.
func viaFixed(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Message == "via_fixed"
}

// Straight through SQL, the stamps on comments and trace links never change, a forged name gives way to the token
// held, and a connected app is named only while its grant is the person's own (#321).
func TestRawSQLKeepsWhatEveryStampedRowWasWrittenThrough(t *testing.T) {
	key := projectKey(t, "VSQ")
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "via-sql").user)
	personID, strangerID := h.addPerson(t, "member"), h.addPerson(t, "member")
	person := api.as(t, personID)
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	r1 := obj(t, want(t, person.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"}), http.StatusCreated, "write R1"), "requirement")
	want(t, person.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Lights dim"}), http.StatusCreated, "write R2")
	tokenID, secret := makeToken(t, person, map[string]any{"name": "Nightly script"})
	h.settle(t)
	script := api.withToken(personID, secret)
	thread := obj(t, want(t, script.post(t, "/api/v1/requirements/"+key+"-R1/comments", commentBody("Checked overnight.")), http.StatusCreated,
		"comment through the token"), "thread")
	commentID := thread["comments"].([]any)[0].(map[string]any)["id"]
	linkID := obj(t, want(t, script.post(t, "/api/v1/requirements/"+key+"-R1/links", map[string]any{"typeKey": "relates", "otherKey": key + "-R2"}),
		http.StatusCreated, "link through the token"), "link")["id"]
	h.settle(t)
	ctx := context.Background()
	conn := h.appConn(t, personID)
	hold := func(t *testing.T, id any) {
		t.Helper()
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.ViaVar, id); err != nil {
			t.Fatal(err)
		}
	}
	hold(t, tokenID)

	t.Run("a comment's and a trace link's stamp never change", func(t *testing.T) {
		// exciter_app may not update those columns at all; the trigger holds even a role that may.
		for _, c := range []struct {
			what, sql string
			id        any
		}{
			{"a comment's stamp renamed", `UPDATE comment SET via_name = 'renamed' WHERE id = $1`, commentID},
			{"a trace link's stamp cleared", `UPDATE trace_link SET via_kind = NULL WHERE id = $1`, linkID},
		} {
			if _, err := conn.Exec(ctx, c.sql, c.id); !isPermissionDenied(err) {
				t.Errorf("%s as exciter_app: %v", c.what, err)
			}
			if _, err := h.super.Exec(ctx, c.sql, c.id); !viaFixed(err) {
				t.Errorf("%s as the superuser: %v", c.what, err)
			}
		}
		var name string
		if err := h.super.QueryRow(ctx, `SELECT via_name FROM comment WHERE id = $1`, commentID).Scan(&name); err != nil || name != "Nightly script" {
			t.Errorf("the comment names %q: %v", name, err)
		}
	})

	t.Run("a forged name on a revision gives way to the token held", func(t *testing.T) {
		if _, err := conn.Exec(ctx, `INSERT INTO requirement_revision (requirement_id, number, title, statement, hash, created_by, via_kind, via_id, via_name)
			VALUES ($1, 2, 'Brakes engage at once', '{"type":"doc"}', 'h', current_actor_id(), 'oauth', $2, 'a forged name')`, r1["id"], uuid.New()); err != nil {
			t.Fatalf("write revision 2: %v", err)
		}
		var kind, name string
		if err := h.super.QueryRow(ctx, `SELECT via_kind, via_name FROM requirement_revision WHERE requirement_id = $1 AND number = 2`, r1["id"]).Scan(&kind, &name); err != nil ||
			kind != "token" || name != "Nightly script" {
			t.Errorf("revision 2 is stamped %q %q: %v", kind, name, err)
		}
	})

	t.Run("a connected app is named by its client only while the grant is the person's own", func(t *testing.T) {
		client := "via-sql-" + uuid.NewString()
		if _, err := h.super.Exec(ctx, `INSERT INTO oauth_client (id, kind, name, redirect_uris, grant_types)
			VALUES ($1, 'registered', 'Desk assistant', '{https://x.test/cb}', '{authorization_code}')`, client); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM oauth_client WHERE id = $1`, client) })
		grant := func(user uuid.UUID) uuid.UUID {
			var id uuid.UUID
			if err := h.super.QueryRow(ctx, `INSERT INTO oauth_grant (user_id, client_id, redirect_uri, read_only) VALUES ($1, $2, 'https://x.test/cb', false) RETURNING id`,
				user, client).Scan(&id); err != nil {
				t.Fatal(err)
			}
			return id
		}
		mine, theirs := grant(personID), grant(strangerID)
		stamp := func(t *testing.T, via uuid.UUID) (kind, name *string) {
			t.Helper()
			hold(t, via)
			var id uuid.UUID
			if err := conn.QueryRow(ctx, `INSERT INTO requirement_change (requirement_id, field, actor_id) VALUES ($1, 'owner', current_actor_id()) RETURNING id`,
				r1["id"]).Scan(&id); err != nil {
				t.Fatalf("log a change: %v", err)
			}
			if err := h.super.QueryRow(ctx, `SELECT via_kind, via_name FROM requirement_change WHERE id = $1`, id).Scan(&kind, &name); err != nil {
				t.Fatal(err)
			}
			return kind, name
		}
		if kind, name := stamp(t, mine); kind == nil || *kind != "oauth" || *name != "Desk assistant" {
			t.Errorf("the person's own grant stamps %v %v", kind, name)
		}
		if kind, _ := stamp(t, theirs); kind != nil {
			t.Errorf("somebody else's grant stamps %s", *kind)
		}
		// Revoking a grant deletes it, so an ended grant is one that is no longer there.
		if _, err := h.super.Exec(ctx, `DELETE FROM oauth_grant WHERE id = $1`, mine); err != nil {
			t.Fatal(err)
		}
		if kind, _ := stamp(t, mine); kind != nil {
			t.Errorf("a revoked grant stamps %s", *kind)
		}
	})
}

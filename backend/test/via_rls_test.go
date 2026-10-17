//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"

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

//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

const insertToken = `INSERT INTO api_token (user_id, name, token_hash, projects_only) VALUES ($1, 'script', $2, $3) RETURNING id`

func aDigest(t *testing.T) []byte {
	t.Helper()
	digest := make([]byte, 32)
	if _, err := rand.Read(digest); err != nil {
		t.Fatal(err)
	}
	return digest
}

func seesRows(t *testing.T, conn *pgx.Conn, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// limitTo makes a connection act as a token limited to projects, as db.WithUserInProjects does.
func limitTo(t *testing.T, conn *pgx.Conn, projects ...uuid.UUID) {
	t.Helper()
	setting := "{}"
	if len(projects) > 0 {
		setting = "{" + projects[0].String()
		for _, p := range projects[1:] {
			setting += "," + p.String()
		}
		setting += "}"
	}
	if _, err := conn.Exec(context.Background(), `SELECT set_config($1, $2, false)`, db.TokenProjectsVar, setting); err != nil {
		t.Fatal(err)
	}
}

// Straight through SQL as exciter_app: a token is its owner's and an administrator's, never changed, made with only
// projects its owner views, and a limited one reaches those alone and nothing organization-wide.
func TestRawSQLGuardsTokens(t *testing.T) {
	limKey := projectKey(t, "LIM")
	w := newTraceWorld(t, "tokens")
	ctx := context.Background()
	admin := w.h.appConn(t, w.a.user)
	var mine uuid.UUID
	if err := w.editor.QueryRow(ctx, insertToken, w.editorID, aDigest(t), false).Scan(&mine); err != nil {
		t.Fatalf("a member makes a token of their own: %v", err)
	}

	t.Run("made, seen and revoked by its owner, and seen and revoked by an administrator", func(t *testing.T) {
		var id uuid.UUID
		if err := w.editor.QueryRow(ctx, insertToken, w.viewerID, aDigest(t), false).Scan(&id); err == nil {
			t.Error("a member made a token in somebody else's name")
		}
		if seesRows(t, w.viewer, `SELECT count(*) FROM api_token WHERE id = $1`, mine) != 0 {
			t.Error("a member sees somebody else's token")
		}
		if seesRows(t, admin, `SELECT count(*) FROM api_token WHERE id = $1`, mine) != 1 {
			t.Error("an administrator does not see a member's token")
		}
		if tag, err := w.viewer.Exec(ctx, `DELETE FROM api_token WHERE id = $1`, mine); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("a member revoked somebody else's token: %v", err)
		}
		if _, err := w.editor.Exec(ctx, `UPDATE api_token SET scopes = '{}', expires_at = NULL WHERE id = $1`, mine); err == nil {
			t.Error("the app role changed a token, which would widen it")
		}
		if _, err := w.h.super.Exec(ctx, `UPDATE api_token SET projects_only = true WHERE id = $1`, mine); !isCheckViolation(err) {
			t.Errorf("a token's reach was changed after it was made: %v", err)
		}
		var spare uuid.UUID
		if err := w.editor.QueryRow(ctx, insertToken, w.editorID, aDigest(t), false).Scan(&spare); err != nil {
			t.Fatal(err)
		}
		if tag, err := admin.Exec(ctx, `DELETE FROM api_token WHERE id = $1`, spare); err != nil || tag.RowsAffected() != 1 {
			t.Errorf("an administrator could not revoke a member's token: %v", err)
		}
	})

	t.Run("made with only the projects its owner views, in the transaction that makes it", func(t *testing.T) {
		tx, err := w.viewer.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var limited uuid.UUID
		if err := tx.QueryRow(ctx, insertToken, w.viewerID, aDigest(t), true).Scan(&limited); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO api_token_project (token_id, project_id) VALUES ($1, $2)`, limited, w.here); err != nil {
			t.Fatalf("a viewer names the project they view: %v", err)
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT p`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO api_token_project (token_id, project_id) VALUES ($1, $2)`, limited, w.other); err == nil {
			t.Error("a token named a project its owner may not view")
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT p`); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := w.viewer.Exec(ctx, `INSERT INTO api_token_project (token_id, project_id) VALUES ($1, $2)`, limited, w.here); err == nil {
			t.Error("a project was added to a token made earlier")
		}
		if _, err := w.editor.Exec(ctx, `INSERT INTO api_token_project (token_id, project_id) VALUES ($1, $2)`, mine, w.here); err == nil {
			t.Error("a full token was narrowed afterwards")
		}
	})

	t.Run("a limited token reaches its projects alone, and nothing of the whole organization", func(t *testing.T) {
		limited := w.h.appConn(t, w.a.user)
		limitTo(t, limited, w.here)
		if seesRows(t, limited, `SELECT count(*) FROM project WHERE id = $1`, w.other) != 0 || seesRows(t, limited, `SELECT count(*) FROM project WHERE id = $1`, w.here) != 1 {
			t.Error("the owner's limited token sees outside its project, or not inside it")
		}
		if seesRows(t, limited, `SELECT count(*) FROM requirement WHERE project_id = $1`, w.other) != 0 {
			t.Error("a limited token reads requirements outside its project")
		}
		var held bool
		if err := limited.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'administer'), current_actor_administers()`, w.here, w.a.user).Scan(&held, new(bool)); err != nil || !held {
			t.Errorf("the owner's token does not administer the project it names: %v", err)
		}
		var administers bool
		if err := limited.QueryRow(ctx, `SELECT current_actor_administers()`).Scan(&administers); err != nil || administers {
			t.Errorf("a limited token administers the organization: %v", err)
		}
		if seesRows(t, limited, `SELECT count(*) FROM api_token`) != 0 {
			t.Error("a limited token reads the organization's tokens")
		}
		var id uuid.UUID
		if err := limited.QueryRow(ctx, `INSERT INTO project (key, name, created_by) VALUES ('`+limKey+`', 'Limited', current_actor_id()) RETURNING id`).Scan(&id); err == nil {
			t.Error("a limited token made a project")
		}
		// The limit is the caller's alone: asked about somebody else, the rules answer for that person.
		if err := limited.QueryRow(ctx, `SELECT perm_project_holds($1, $2, 'view')`, w.other, w.editorID).Scan(&held); err != nil || !held {
			t.Errorf("the limit leaked onto another person's rights: %v", err)
		}
		none := w.h.appConn(t, w.a.user)
		limitTo(t, none)
		if seesRows(t, none, `SELECT count(*) FROM project`) != 0 {
			t.Error("a token whose projects are all gone reaches some")
		}
	})
}

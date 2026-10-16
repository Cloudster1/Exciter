//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Straight through SQL as exciter_app (#49): a reuse starts from a library requirement of another project with its
// words, never takes another source, and while it follows, its words and revision are the source's at its latest.
func TestRawSQLGuardsReuse(t *testing.T) {
	rlbKey, rapKey, rotKey := projectKey(t, "RLB"), projectKey(t, "RAP"), projectKey(t, "ROT")
	f := newProjectFixture(t)
	editorID := f.h.addPerson(t, "member")
	editor := f.api.as(t, editorID)
	for _, p := range []struct{ key, name string }{{rlbKey, "Walled library"}, {rapKey, "Walled product"}, {rotKey, "No library"}} {
		want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": p.key, "name": p.name}), http.StatusCreated, "make "+p.name)
	}
	want(t, f.owner.patch(t, "/api/v1/projects/"+rlbKey, map[string]any{"settings": map[string]any{"library": true}}), http.StatusOK, "mark the library")
	write := func(projectKey, title string) map[string]any {
		return obj(t, want(t, editor.post(t, "/api/v1/projects/"+projectKey+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title), "requirement")
	}
	source, other, plain := write(rlbKey, "Log every login"), write(rlbKey, "Lock after five tries"), write(rotKey, "Not shelved")
	want(t, editor.post(t, "/api/v1/requirements/"+source["key"].(string)+"/reuse", map[string]any{"projectKey": rapKey}), http.StatusCreated, "reuse it")
	reused := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+rapKey+"-R1"), http.StatusOK, "read the reuse"), "requirement")
	f.h.settle(t)
	ctx := context.Background()
	conn := f.h.appConn(t, editorID)
	var appID uuid.UUID
	if err := f.h.super.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, rapKey).Scan(&appID); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO requirement (project_id, title, statement, normative_hash, created_by, reuse_source, reuse_revision, reuse_state)
		VALUES ($1, $2, '{"type":"doc"}', 'h', current_actor_id(), $3, $4, $5)`

	t.Run("a reuse starts from a library requirement of another project, with its words", func(t *testing.T) {
		refusedOutright(t, conn, "a source outside a library", insert, appID, "Not shelved", plain["id"], 1, "following")
		refusedOutright(t, conn, "words of its own", insert, appID, "Log some logins", source["id"], 1, "following")
		refusedOutright(t, conn, "an old revision", insert, appID, "Log every login", source["id"], 0, "following")
		refusedOutright(t, conn, "a variant from the start", insert, appID, "Log every login", source["id"], 1, "variant")
		refusedOutright(t, conn, "a half of a reuse", `INSERT INTO requirement (project_id, title, statement, normative_hash, created_by, reuse_source)
			VALUES ($1, 'Log every login', '{"type":"doc"}', 'h', current_actor_id(), $2)`, appID, source["id"])
		if _, err := conn.Exec(ctx, insert, appID, "Log every login", source["id"], 1, "following"); err != nil {
			t.Errorf("a reuse with the source's words is refused: %v", err)
		}
	})

	t.Run("while it follows, its words are the source's, and it takes no other source", func(t *testing.T) {
		refusedOutright(t, conn, "new words", `UPDATE requirement SET title = 'Log some logins' WHERE id = $1`, reused["id"])
		refusedOutright(t, conn, "a revision the source never had", `UPDATE requirement SET reuse_revision = 7 WHERE id = $1`, reused["id"])
		refusedOutright(t, conn, "another source", `UPDATE requirement SET reuse_source = $2 WHERE id = $1`, reused["id"], other["id"])
	})

	t.Run("a variant changes its words but keeps the revision it branched from, and follows again only with the source's words", func(t *testing.T) {
		if _, err := conn.Exec(ctx, `UPDATE requirement SET reuse_state = 'variant', title = 'Log logins on the product' WHERE id = $1`, reused["id"]); err != nil {
			t.Fatalf("an editor makes a variant: %v", err)
		}
		refusedOutright(t, conn, "another branch revision", `UPDATE requirement SET reuse_revision = 2 WHERE id = $1`, reused["id"])
		refusedOutright(t, conn, "following with its own words", `UPDATE requirement SET reuse_state = 'following' WHERE id = $1`, reused["id"])
		if _, err := conn.Exec(ctx, `UPDATE requirement SET reuse_state = 'following', title = 'Log every login' WHERE id = $1`, reused["id"]); err != nil {
			t.Errorf("following again with the source's words is refused: %v", err)
		}
	})
}

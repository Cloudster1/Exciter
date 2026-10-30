//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The service refusing is not proof (#312): straight through SQL as exciter_app, the trash and an archived project
// close the discussion and the files, nobody is told of a deleted comment, and watching stays within what one views.

const commentDoc = `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}`

func TestRawSQLClosesTheDiscussionInTheTrashAndInAnArchive(t *testing.T) {
	w := newCommentWorld(t, "CMS")
	ctx := context.Background()
	threadID := obj(t, want(t, w.bob.post(t, "/api/v1/requirements/"+w.reqKey+"/comments", commentBody("Why?")), http.StatusCreated, "bob comments"),
		"thread")["id"].(string)
	w.h.settle(t)
	bob := w.h.appConn(t, w.bobID)
	before := w.h.discussionOf(t, w.reqID)
	writes := func(t *testing.T, where string) {
		t.Helper()
		if _, err := bob.Exec(ctx, `INSERT INTO comment_thread (requirement_id, created_by) VALUES ($1, $2)`, w.reqID, w.bobID); !isPolicyViolation(err) {
			t.Errorf("an editor started a thread %s: %v", where, err)
		}
		if _, err := bob.Exec(ctx, `INSERT INTO comment (thread_id, requirement_id, revision, author_id, body) VALUES ($1, $2, 1, $3, $4)`,
			threadID, w.reqID, w.bobID, commentDoc); !isPolicyViolation(err) {
			t.Errorf("an editor replied %s: %v", where, err)
		}
	}

	want(t, w.bob.delete(t, "/api/v1/requirements/"+w.reqKey), http.StatusNoContent, "trash R1")
	writes(t, "in the trash")
	want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/trash/"+w.reqID+"/restore", nil), http.StatusOK, "restore R1")

	want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/archive", nil), http.StatusOK, "archive the project")
	writes(t, "in an archived project")
	if _, err := bob.Exec(ctx, `UPDATE comment SET body = $2, edited_at = now() WHERE id = $1`, threadID, commentDoc); !isPolicyViolation(err) {
		t.Errorf("the author rewrote his comment in an archived project: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE comment_thread SET resolved_at = now(), resolved_by = $2 WHERE id = $1`, threadID, w.bobID); !isPolicyViolation(err) {
		t.Errorf("a thread was resolved in an archived project: %v", err)
	}
	if after := w.h.discussionOf(t, w.reqID); after != before {
		t.Errorf("the discussion changed:\nbefore %s\nafter  %s", before, after)
	}
	want(t, w.owner.delete(t, "/api/v1/projects/"+w.key+"/archive"), http.StatusOK, "unarchive the project")
	if n := affects(t, bob, `UPDATE comment SET body = $2, edited_at = now() WHERE id = $1`, threadID, commentDoc); n != 1 {
		t.Error("the author could not rewrite his comment once the project was open again")
	}
}

func TestRawSQLTellsNobodyOfADeletedComment(t *testing.T) {
	w := newCommentWorld(t, "NLC")
	comments := "/api/v1/requirements/" + w.reqKey + "/comments"
	live := obj(t, want(t, w.dave.post(t, comments, commentBody("Still here.")), http.StatusCreated, "dave comments"), "thread")["id"].(string)
	doomed := obj(t, want(t, w.dave.post(t, comments, commentBody("Gone soon.")), http.StatusCreated, "dave comments again"), "thread")["id"].(string)
	want(t, w.dave.delete(t, "/api/v1/comments/"+doomed), http.StatusNoContent, "dave deletes the second")
	w.h.settle(t)
	// The worker writes each row acting for the person it tells.
	worker := w.h.appConn(t, w.carolID)
	insert := `INSERT INTO notification (user_id, event_id, kind, requirement_id, revision, thread_id, comment_id)
		VALUES ($1, $2, 'commented', $3, 1, $4, $4)`
	if n := affects(t, worker, insert, w.carolID, uuid.New(), w.reqID, doomed); n != 0 {
		t.Errorf("%d rows were written about a deleted comment", n)
	}
	if n := affects(t, worker, insert, w.carolID, uuid.New(), w.reqID, live); n != 1 {
		t.Errorf("%d rows were written about a live comment, want 1", n)
	}
	if n := w.h.countRows(t, `SELECT count(*) FROM notification WHERE comment_id = $1`, doomed); n != 0 {
		t.Errorf("%d notifications name the deleted comment", n)
	}
}

func TestRawSQLKeepsWatchesAndFilesWithinReach(t *testing.T) {
	w := newCommentWorld(t, "WSQ")
	h := w.h
	ctx := context.Background()
	hidden := projectKey(t, "WSX")
	hiddenID := obj(t, want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": hidden, "name": "Hidden"}), http.StatusCreated,
		"make a hidden project"), "project")["id"].(string)
	if _, err := h.super.Exec(ctx, `DELETE FROM role_assignment WHERE project_id = $1 AND group_id = (SELECT id FROM groups WHERE builtin = 'everyone')`, hiddenID); err != nil {
		t.Fatal(err)
	}
	secret := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+hidden+"/requirements", map[string]any{"title": "Secret"}), http.StatusCreated,
		"write a secret"), "requirement")["id"].(string)
	w.closeToEveryone(t)
	eve := h.addPerson(t, "member")
	h.grantAs(t, w.projectID, eve, "viewer")
	want(t, w.owner.api.as(t, eve).put(t, "/api/v1/requirements/"+w.reqKey+"/watch", nil), http.StatusOK, "eve watches R1")
	h.settle(t)
	carol, owner, bob := h.appConn(t, w.carolID), h.appConn(t, w.ownerID), h.appConn(t, w.bobID)
	watchers := func(conn *pgx.Conn, requirement string) []string {
		t.Helper()
		rows, err := conn.Query(ctx, `SELECT user_id::text FROM requirement_watchers($1) ORDER BY user_id`, requirement)
		if err != nil {
			t.Fatal(err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	has := func(ids []string, id uuid.UUID) bool {
		for _, each := range ids {
			if each == id.String() {
				return true
			}
		}
		return false
	}

	t.Run("a project watch only on a project one may view", func(t *testing.T) {
		if _, err := carol.Exec(ctx, `INSERT INTO watch (user_id, kind, project_id) VALUES ($1, 'project', $2)`, w.carolID, hiddenID); !isPolicyViolation(err) {
			t.Errorf("carol watched a project she may not view: %v", err)
		}
		if n := affects(t, carol, `INSERT INTO watch (user_id, kind, project_id) VALUES ($1, 'project', $2)`, w.carolID, w.projectID); n != 1 {
			t.Error("carol could not watch the project she views")
		}
	})

	t.Run("who watches is read only by who may view, and only of who may too", func(t *testing.T) {
		if got := watchers(owner, secret); len(got) == 0 {
			t.Fatal("the secret's author does not watch it")
		}
		if got := watchers(carol, secret); len(got) != 0 {
			t.Errorf("carol read who watches a requirement she may not view: %v", got)
		}
		if got := watchers(carol, w.reqID); !has(got, eve) || !has(got, w.ownerID) {
			t.Fatalf("carol reads R1's watchers as %v, without eve or the author", got)
		}
		if _, err := h.super.Exec(ctx, `DELETE FROM role_assignment WHERE project_id = $1 AND user_id = $2`, w.projectID, eve); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		if got := watchers(carol, w.reqID); has(got, eve) || !has(got, w.ownerID) {
			t.Errorf("after eve lost her grant carol reads R1's watchers as %v", got)
		}
	})

	t.Run("a file goes only on a requirement out of the trash in an open project", func(t *testing.T) {
		var id uuid.UUID
		want(t, w.bob.delete(t, "/api/v1/requirements/"+w.reqKey), http.StatusNoContent, "trash R1")
		if err := bob.QueryRow(ctx, insertFile, w.reqID, w.bobID).Scan(&id); !isPolicyViolation(err) {
			t.Errorf("an editor attached a file in the trash: %v", err)
		}
		want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/trash/"+w.reqID+"/restore", nil), http.StatusOK, "restore R1")
		want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/archive", nil), http.StatusOK, "archive the project")
		if err := bob.QueryRow(ctx, insertFile, w.reqID, w.bobID).Scan(&id); !isPolicyViolation(err) {
			t.Errorf("an editor attached a file in an archived project: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM attachment WHERE requirement_id = $1`, w.reqID); n != 0 {
			t.Errorf("%d files were attached", n)
		}
		want(t, w.owner.delete(t, "/api/v1/projects/"+w.key+"/archive"), http.StatusOK, "unarchive the project")
		if err := bob.QueryRow(ctx, insertFile, w.reqID, w.bobID).Scan(&id); err != nil {
			t.Errorf("an editor could not attach a file once R1 was out of the trash and its project open: %v", err)
		}
	})
}

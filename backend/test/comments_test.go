//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Comments and mentions (#33): a requirement's discussion, written by whoever may comment, rewritten by its author
// alone and deleted by its author or the project's administrators, through the API and straight through SQL.

// commentBody is a comment's document: some words, and a mention of each person given.
func commentBody(words string, mentions ...uuid.UUID) map[string]any {
	content := []any{map[string]any{"type": "text", "text": words}}
	for _, id := range mentions {
		content = append(content, map[string]any{"type": "mention", "attrs": map[string]any{"id": id.String(), "label": "Someone"}})
	}
	return map[string]any{"body": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": content}}}}
}

// lastEvent is the payload of the newest outbox event of a topic about a requirement.
func (h *harness) lastEvent(t *testing.T, topic, requirementID string) map[string]any {
	t.Helper()
	var raw []byte
	if err := h.super.QueryRow(context.Background(), `
		SELECT payload FROM outbox_event WHERE topic = $1 AND payload ->> 'requirementId' = $2
		ORDER BY created_at DESC, id DESC LIMIT 1`, topic, requirementID).Scan(&raw); err != nil {
		t.Fatalf("no %s event about %s: %v", topic, requirementID, err)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// commentWorld is a project whose everybody reads, with an editor and a reviewer named, and a requirement in it.
type commentWorld struct {
	h                               *harness
	key, reqKey, reqID              string
	owner, bob, carol, dave         *client
	ownerID, bobID, carolID, daveID uuid.UUID
	projectID                       uuid.UUID
}

func newCommentWorld(t *testing.T, prefix string) *commentWorld {
	t.Helper()
	key := projectKey(t, prefix)
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "comments")
	w := &commentWorld{h: h, key: key, ownerID: home.user, bobID: h.addPerson(t, "member"), carolID: h.addPerson(t, "member"), daveID: h.addPerson(t, "member")}
	w.owner, w.bob, w.carol, w.dave = api.as(t, w.ownerID), api.as(t, w.bobID), api.as(t, w.carolID), api.as(t, w.daveID)
	project := obj(t, want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Discussed"}), http.StatusCreated, "make a project"), "project")
	w.projectID = uuid.MustParse(project["id"].(string))
	if _, err := h.super.Exec(t.Context(), `UPDATE project_grant SET role = 'viewer' WHERE project_id = $1 AND subject_type = 'everyone'`, w.projectID); err != nil {
		t.Fatal(err)
	}
	h.grantAs(t, w.projectID, w.bobID, "editor")
	h.grantAs(t, w.projectID, w.daveID, "reviewer")
	made := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage within 100 ms"}), http.StatusCreated, "write R1"), "requirement")
	w.reqKey, w.reqID = key+"-R1", made["id"].(string)
	return w
}

func TestARequirementIsDiscussed(t *testing.T) {
	w := newCommentWorld(t, "CMT")
	comments := "/api/v1/requirements/" + w.reqKey + "/comments"

	var threadID string
	t.Run("a reviewer starts a thread, which records the revision and names whom it mentions", func(t *testing.T) {
		thread := obj(t, want(t, w.dave.post(t, comments, commentBody("Why 100 ms? ", w.bobID, w.carolID)), http.StatusCreated, "dave comments"), "thread")
		threadID = thread["id"].(string)
		first := thread["comments"].([]any)[0].(map[string]any)
		if first["id"] != threadID || first["revision"] != float64(1) || first["can"].(map[string]any)["edit"] != true || thread["resolved"] != false {
			t.Errorf("the thread reads %v", thread)
		}
		event := w.h.lastEvent(t, "comment.created", w.reqID)
		if event["reply"] != false || len(event["mentioned"].([]any)) != 2 || event["actorId"] != w.daveID.String() {
			t.Errorf("the event reads %v", event)
		}
		refuse(t, "carol, who may only read", w.carol.post(t, comments, commentBody("Me too")), http.StatusForbidden, "forbidden")
		listed := list(t, want(t, w.carol.get(t, comments), http.StatusOK, "carol reads"), "threads")
		if len(listed) != 1 || listed[0].(map[string]any)["can"].(map[string]any)["reply"] != false {
			t.Errorf("carol reads %v", listed)
		}
	})

	t.Run("a reply goes to the end on the revision of its day, and reopens the thread", func(t *testing.T) {
		resolved := obj(t, want(t, w.bob.put(t, "/api/v1/comment-threads/"+threadID+"/resolution", nil), http.StatusOK, "bob resolves"), "thread")
		if resolved["resolved"] != true || resolved["resolvedByName"] == "" {
			t.Errorf("the resolved thread reads %v", resolved)
		}
		resolution := "/api/v1/comment-threads/" + threadID + "/resolution"
		refuse(t, "carol reopening it", w.carol.delete(t, resolution), http.StatusForbidden, "forbidden")
		if reopened := obj(t, want(t, w.dave.delete(t, resolution), http.StatusOK, "dave reopens"), "thread"); reopened["resolved"] != false {
			t.Errorf("the reopened thread reads %v", reopened)
		}
		if event := w.h.lastEvent(t, "thread.reopened", w.reqID); event["threadId"] != threadID {
			t.Errorf("the reopening's event reads %v", event)
		}
		want(t, w.bob.put(t, resolution, nil), http.StatusOK, "bob resolves again")
		want(t, w.owner.patch(t, "/api/v1/requirements/"+w.reqKey, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise R1")
		got := want(t, w.owner.post(t, "/api/v1/comments/"+threadID+"/replies", commentBody("Because of the new sensor.")), http.StatusCreated, "the owner replies")
		reply, thread := obj(t, got, "comment"), obj(t, got, "thread")
		if reply["revision"] != float64(2) || thread["resolved"] != false || len(thread["comments"].([]any)) != 2 {
			t.Errorf("the reply reads %v in %v", reply, thread)
		}
		if event := w.h.lastEvent(t, "comment.created", w.reqID); event["reply"] != true || len(event["mentioned"].([]any)) != 0 {
			t.Errorf("the reply's event reads %v", event)
		}
		if got := obj(t, want(t, w.carol.get(t, "/api/v1/comments/"+reply["id"].(string)), http.StatusOK, "the thread of a reply"), "thread"); got["id"] != threadID {
			t.Errorf("a reply finds thread %v", got["id"])
		}
	})

	t.Run("only the author rewrites a comment, and an edit tells only whom it adds", func(t *testing.T) {
		edited := obj(t, want(t, w.dave.patch(t, "/api/v1/comments/"+threadID, commentBody("Why 80 ms now? ", w.bobID, w.carolID, w.ownerID)), http.StatusOK, "dave edits"), "comment")
		if edited["editedAt"] == nil {
			t.Errorf("the edit reads %v", edited)
		}
		if event := w.h.lastEvent(t, "comment.edited", w.reqID); len(event["mentioned"].([]any)) != 1 || event["mentioned"].([]any)[0] != w.ownerID.String() {
			t.Errorf("the edit told %v, want the owner alone", event["mentioned"])
		}
		refuse(t, "bob rewriting dave's words", w.bob.patch(t, "/api/v1/comments/"+threadID, commentBody("Mine now")), http.StatusForbidden, "forbidden")
		refuse(t, "the owner rewriting dave's words", w.owner.patch(t, "/api/v1/comments/"+threadID, commentBody("Mine now")), http.StatusForbidden, "forbidden")
	})

	t.Run("the author or an administrator deletes a comment, and its place stays", func(t *testing.T) {
		refuse(t, "bob deleting dave's comment", w.bob.delete(t, "/api/v1/comments/"+threadID), http.StatusForbidden, "forbidden")
		want(t, w.owner.delete(t, "/api/v1/comments/"+threadID), http.StatusNoContent, "the owner deletes dave's comment")
		thread := obj(t, want(t, w.dave.get(t, "/api/v1/comments/"+threadID), http.StatusOK, "the thread"), "thread")
		first := thread["comments"].([]any)[0].(map[string]any)
		if first["deleted"] != true || first["body"] != nil || first["authorName"] == "" {
			t.Errorf("the deleted comment reads %v", first)
		}
		if n := w.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'comment.deleted' AND target_id = $1`, threadID); n != 1 {
			t.Errorf("%d audit entries for the moderation, want 1", n)
		}
		own := obj(t, want(t, w.bob.post(t, comments, commentBody("A thought")), http.StatusCreated, "bob comments"), "thread")
		want(t, w.bob.delete(t, "/api/v1/comments/"+own["id"].(string)), http.StatusNoContent, "bob deletes his own")
		if n := len(list(t, want(t, w.bob.get(t, comments), http.StatusOK, "the list"), "threads")); n != 1 {
			t.Errorf("%d threads listed, want the one with a live comment", n)
		}
		counts := obj(t, want(t, w.bob.get(t, "/api/v1/requirements/"+w.reqKey), http.StatusOK, "R1"), "requirement")["comments"].(map[string]any)
		if counts["threads"] != float64(1) || counts["open"] != float64(1) {
			t.Errorf("R1 counts %v", counts)
		}
	})

	t.Run("what may not be written is refused before it is", func(t *testing.T) {
		fieldError(t, w.bob.post(t, comments, map[string]any{"body": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph"}}}}), "body")
		fieldError(t, w.bob.post(t, comments, map[string]any{"body": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "horizontalRule"}}}}), "body")
		people := list(t, want(t, w.bob.get(t, "/api/v1/requirements/"+w.reqKey+"/mentionable?limit=50"), http.StatusOK, "the picker"), "people")
		found := false
		for _, each := range people {
			p := each.(map[string]any)
			if p["id"] == w.carolID.String() {
				found = p["canView"] == true
			}
		}
		if !found {
			t.Error("the picker does not offer carol as one who may read R1")
		}
		fieldError(t, w.bob.get(t, "/api/v1/requirements/"+w.reqKey+"/mentionable?limit=0"), "limit")
		want(t, w.bob.delete(t, "/api/v1/requirements/"+w.reqKey), http.StatusNoContent, "trash R1")
		refuse(t, "a comment on a requirement in the trash", w.bob.post(t, comments, commentBody("Late")), http.StatusConflict, "requirement_trashed")
		want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/trash/"+w.reqID+"/restore", nil), http.StatusOK, "restore R1")
		want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/archive", nil), http.StatusOK, "archive the project")
		refuse(t, "a comment in an archived project", w.bob.post(t, comments, commentBody("Late")), http.StatusConflict, "project_archived")
		want(t, w.owner.delete(t, "/api/v1/projects/"+w.key+"/archive"), http.StatusOK, "unarchive the project")
	})
}

func TestRawSQLHoldsCommentsToTheirRules(t *testing.T) {
	w := newCommentWorld(t, "CMQ")
	ctx := context.Background()
	thread := obj(t, want(t, w.dave.post(t, "/api/v1/requirements/"+w.reqKey+"/comments", commentBody("Why?")), http.StatusCreated, "dave comments"), "thread")
	threadID := thread["id"].(string)
	w.h.settle(t)
	dave, bob, carol, owner := w.h.appConn(t, w.daveID), w.h.appConn(t, w.bobID), w.h.appConn(t, w.carolID), w.h.appConn(t, w.ownerID)
	body := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}`

	if _, err := carol.Exec(ctx, `INSERT INTO comment_thread (requirement_id, created_by) VALUES ($1, $2)`, w.reqID, w.carolID); !isPolicyViolation(err) {
		t.Errorf("carol, who may only read, started a thread: %v", err)
	}
	if _, err := dave.Exec(ctx, `INSERT INTO comment (thread_id, requirement_id, revision, author_id, body) VALUES ($1, $2, 1, $3, $4)`,
		threadID, w.reqID, w.bobID, body); !isPolicyViolation(err) {
		t.Errorf("dave wrote a comment in bob's name: %v", err)
	}
	var revision int
	if err := dave.QueryRow(ctx, `INSERT INTO comment (thread_id, requirement_id, revision, author_id, body) VALUES ($1, $2, 99, $3, $4) RETURNING revision`,
		threadID, w.reqID, w.daveID, body).Scan(&revision); err != nil || revision != 1 {
		t.Errorf("a comment claiming revision 99 is on revision %d: %v", revision, err)
	}
	if _, err := bob.Exec(ctx, `UPDATE comment SET body = $2 WHERE id = $1`, threadID, body); !isPolicyViolation(err) {
		t.Errorf("bob rewrote dave's comment: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, threadID, w.bobID); !isPolicyViolation(err) {
		t.Errorf("bob, who does not administer the project, deleted dave's comment: %v", err)
	}
	if _, err := dave.Exec(ctx, `UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, threadID, w.bobID); !isPolicyViolation(err) {
		t.Errorf("dave deleted his comment in bob's name: %v", err)
	}
	if _, err := dave.Exec(ctx, `UPDATE comment SET created_at = now() - interval '1 day' WHERE id = $1`, threadID); err == nil {
		t.Error("dave moved his comment back in time")
	}
	if n := affects(t, owner, `UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, threadID, w.ownerID); n != 1 {
		t.Error("the project's administrator could not delete dave's comment")
	}
	if _, err := dave.Exec(ctx, `UPDATE comment SET body = $2, deleted_at = NULL, deleted_by = NULL WHERE id = $1`, threadID, body); err == nil {
		t.Error("dave brought back a deleted comment")
	}
	if _, err := owner.Exec(ctx, `DELETE FROM comment WHERE id = $1`, threadID); err == nil {
		t.Error("a comment was deleted outright")
	}
	if _, err := carol.Exec(ctx, `UPDATE comment_thread SET resolved_at = now(), resolved_by = $2 WHERE id = $1`, threadID, w.carolID); !isPolicyViolation(err) {
		t.Errorf("carol, who may only read, resolved a thread: %v", err)
	}
	if _, err := bob.Exec(ctx, `UPDATE comment_thread SET resolved_at = now(), resolved_by = $2 WHERE id = $1`, threadID, w.daveID); !isPolicyViolation(err) {
		t.Errorf("bob resolved a thread in dave's name: %v", err)
	}
	if n := affects(t, bob, `UPDATE comment_thread SET resolved_at = now(), resolved_by = $2 WHERE id = $1`, threadID, w.bobID); n != 1 {
		t.Error("bob could not resolve the thread in his own name")
	}
	if _, err := dave.Exec(ctx, `INSERT INTO outbox_event (topic, payload) VALUES ('comment.created', $1)`,
		`{"actorId":"`+w.daveID.String()+`","requirementId":"`+w.reqID+`","mentioned":["`+uuid.NewString()+`"]}`); !isPolicyViolation(err) {
		t.Errorf("an event mentioned somebody who is no member: %v", err)
	}
}

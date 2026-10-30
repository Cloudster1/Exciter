//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The edges of the discussion (#312): what is refused leaves every comment and thread as it was, a mention and the
// picker reach only who may read, an edit and a reopening tell whom they concern, and an assistant reads it too.

// discussionOf is every comment's words and edit time and every thread's resolution on a requirement, read past
// every policy, so a refusal can be shown to have changed none of it.
func (h *harness) discussionOf(t *testing.T, requirementID string) string {
	t.Helper()
	var comments, threads string
	if err := h.super.QueryRow(context.Background(), `
		SELECT COALESCE(string_agg(id::text || ' ' || COALESCE(body::text, '-') || ' ' || COALESCE(edited_at::text, '-'), '|' ORDER BY id), ''),
		       COALESCE((SELECT string_agg(t.id::text || ' ' || COALESCE(t.resolved_at::text, '-'), '|' ORDER BY t.id)
		                 FROM comment_thread t WHERE t.requirement_id = $1), '')
		FROM comment WHERE requirement_id = $1`, requirementID).Scan(&comments, &threads); err != nil {
		t.Fatal(err)
	}
	return comments + "\n" + threads
}

// rename gives a person a name of the test's choosing, behind the policies' back.
func (h *harness) rename(t *testing.T, id uuid.UUID, name string) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET name = $2 WHERE id = $1`, id, name); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
}

// closeToEveryone takes away the grant that lets every member read the world's project, leaving carol a viewer.
func (w *commentWorld) closeToEveryone(t *testing.T) {
	t.Helper()
	if _, err := w.h.super.Exec(t.Context(), `DELETE FROM role_assignment WHERE project_id = $1 AND group_id = (SELECT id FROM groups WHERE builtin = 'everyone')`, w.projectID); err != nil {
		t.Fatal(err)
	}
	w.h.grantAs(t, w.projectID, w.carolID, "viewer")
}

// emptyBody is a comment that says nothing.
func emptyBody() map[string]any {
	return map[string]any{"body": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph"}}}}
}

func TestCommentRefusalsLeaveTheDiscussionAsItWas(t *testing.T) {
	w := newCommentWorld(t, "CMR")
	comments := "/api/v1/requirements/" + w.reqKey + "/comments"
	threadID := obj(t, want(t, w.bob.post(t, comments, commentBody("Why 100 ms?")), http.StatusCreated, "bob comments"), "thread")["id"].(string)
	replyID := obj(t, want(t, w.dave.post(t, "/api/v1/comments/"+threadID+"/replies", commentBody("Because of the sensor.")), http.StatusCreated,
		"dave replies"), "comment")["id"].(string)
	goneID := obj(t, want(t, w.bob.post(t, comments, commentBody("Never mind.")), http.StatusCreated, "bob comments again"), "thread")["id"].(string)
	want(t, w.bob.delete(t, "/api/v1/comments/"+goneID), http.StatusNoContent, "bob deletes it")
	resolution := "/api/v1/comment-threads/" + threadID + "/resolution"
	unchanged := func(t *testing.T, before string) {
		t.Helper()
		if after := w.h.discussionOf(t, w.reqID); after != before {
			t.Errorf("a refusal changed the discussion:\nbefore %s\nafter  %s", before, after)
		}
	}

	t.Run("a deleted comment and a reply's id are not found", func(t *testing.T) {
		before := w.h.discussionOf(t, w.reqID)
		refuse(t, "rewriting a deleted comment", w.bob.patch(t, "/api/v1/comments/"+goneID, commentBody("Back again")), http.StatusNotFound, "not_found")
		refuse(t, "resolving by a reply's id", w.bob.put(t, "/api/v1/comment-threads/"+replyID+"/resolution", nil), http.StatusNotFound, "not_found")
		refuse(t, "reopening by a reply's id", w.bob.delete(t, "/api/v1/comment-threads/"+replyID+"/resolution"), http.StatusNotFound, "not_found")
		unchanged(t, before)
	})

	t.Run("a reader neither resolves nor replies", func(t *testing.T) {
		before := w.h.discussionOf(t, w.reqID)
		refuse(t, "carol resolving", w.carol.put(t, resolution, nil), http.StatusForbidden, "forbidden")
		refuse(t, "carol replying", w.carol.post(t, "/api/v1/comments/"+threadID+"/replies", commentBody("Me too")), http.StatusForbidden, "forbidden")
		unchanged(t, before)
	})

	t.Run("a comment that says nothing is refused on its body", func(t *testing.T) {
		before := w.h.discussionOf(t, w.reqID)
		fieldError(t, w.bob.patch(t, "/api/v1/comments/"+threadID, emptyBody()), "body")
		fieldError(t, w.dave.post(t, "/api/v1/comments/"+threadID+"/replies", emptyBody()), "body")
		unchanged(t, before)
	})

	t.Run("an id that is no id is a bad request, for there is no field to refuse", func(t *testing.T) {
		before := w.h.discussionOf(t, w.reqID)
		refuse(t, "deleting no id", w.bob.delete(t, "/api/v1/comments/nothing"), http.StatusBadRequest, "bad_request")
		refuse(t, "resolving no id", w.bob.put(t, "/api/v1/comment-threads/nothing/resolution", nil), http.StatusBadRequest, "bad_request")
		refuse(t, "reopening no id", w.bob.delete(t, "/api/v1/comment-threads/nothing/resolution"), http.StatusBadRequest, "bad_request")
		unchanged(t, before)
	})

	t.Run("a requirement in the trash takes no edit, resolution or reply", func(t *testing.T) {
		want(t, w.bob.delete(t, "/api/v1/requirements/"+w.reqKey), http.StatusNoContent, "trash R1")
		before := w.h.discussionOf(t, w.reqID)
		refuse(t, "an edit in the trash", w.bob.patch(t, "/api/v1/comments/"+threadID, commentBody("Late")), http.StatusConflict, "requirement_trashed")
		refuse(t, "a resolution in the trash", w.bob.put(t, resolution, nil), http.StatusConflict, "requirement_trashed")
		refuse(t, "a reply in the trash", w.bob.post(t, "/api/v1/comments/"+threadID+"/replies", commentBody("Late")), http.StatusConflict, "requirement_trashed")
		unchanged(t, before)
		want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/trash/"+w.reqID+"/restore", nil), http.StatusOK, "restore R1")
	})

	t.Run("an archived project takes no delete, edit, resolution or reopening", func(t *testing.T) {
		want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/archive", nil), http.StatusOK, "archive the project")
		before := w.h.discussionOf(t, w.reqID)
		refuse(t, "a delete in an archived project", w.bob.delete(t, "/api/v1/comments/"+threadID), http.StatusConflict, "project_archived")
		refuse(t, "an edit in an archived project", w.bob.patch(t, "/api/v1/comments/"+threadID, commentBody("Late")), http.StatusConflict, "project_archived")
		refuse(t, "a resolution in an archived project", w.bob.put(t, resolution, nil), http.StatusConflict, "project_archived")
		refuse(t, "a reopening in an archived project", w.bob.delete(t, resolution), http.StatusConflict, "project_archived")
		unchanged(t, before)
		want(t, w.owner.delete(t, "/api/v1/projects/"+w.key+"/archive"), http.StatusOK, "unarchive the project")
	})
}

// pickerFor lists the mention picker's answer for q as bob, by id.
func (w *commentWorld) pickerFor(t *testing.T, q string) map[string]map[string]any {
	t.Helper()
	path := "/api/v1/requirements/" + w.reqKey + "/mentionable?limit=50&q=" + url.QueryEscape(q)
	out := map[string]map[string]any{}
	for _, each := range list(t, want(t, w.bob.get(t, path), http.StatusOK, "the picker for "+q), "people") {
		p := each.(map[string]any)
		out[p["id"].(string)] = p
	}
	return out
}

// startsAWord says whether name or email has a word that starts with prefix, as the picker matches.
func startsAWord(p map[string]any, prefix string) bool {
	prefix = strings.ToLower(prefix)
	if strings.HasPrefix(strings.ToLower(p["email"].(string)), prefix) {
		return true
	}
	return slices.ContainsFunc(strings.Split(strings.ToLower(p["name"].(string)), " "), func(word string) bool { return strings.HasPrefix(word, prefix) })
}

func TestMentionsAndThePickerReachOnlyWhoMayRead(t *testing.T) {
	w := newCommentWorld(t, "CMM")
	h := w.h
	w.closeToEveryone(t)
	tag := h.suffix()
	h.rename(t, w.carolID, "Carol "+tag)
	outsider := h.namedPerson(t, "Oscar "+tag)
	gone := h.namedPerson(t, "Gina "+tag)
	h.grantAs(t, w.projectID, gone, "viewer")
	if _, err := h.super.Exec(t.Context(), `UPDATE app_user SET is_active = false WHERE id = $1`, gone); err != nil {
		t.Fatal(err)
	}
	h.settle(t)
	world := []uuid.UUID{w.ownerID, w.bobID, w.carolID, w.daveID, outsider, gone}

	t.Run("a comment tells neither a member without a grant nor one deactivated", func(t *testing.T) {
		want(t, w.bob.post(t, "/api/v1/requirements/"+w.reqKey+"/comments", commentBody("Look at this ", outsider, gone, w.carolID)),
			http.StatusCreated, "bob mentions three")
		event := h.lastEvent(t, "comment.created", w.reqID)
		if got := event["mentioned"].([]any); len(got) != 1 || got[0] != w.carolID.String() {
			t.Errorf("the event mentions %v, want carol alone", got)
		}
		h.drained(t)
		for name, id := range map[string]uuid.UUID{"the member without a grant": outsider, "the deactivated member": gone} {
			if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1`, id); n != 0 {
				t.Errorf("%s was told %d things", name, n)
			}
		}
		if n := h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND kind = 'mentioned'`, w.carolID); n != 1 {
			t.Errorf("carol was told of her mention %d times, want once", n)
		}
	})

	t.Run("the picker narrows by what a name or address starts with", func(t *testing.T) {
		found := w.pickerFor(t, "car")
		for _, id := range world {
			if _, ok := found[id.String()]; ok != (id == w.carolID) {
				t.Errorf("the picker for car offers %s: %t", id, ok)
			}
		}
		for _, p := range found {
			if !startsAWord(p, "car") {
				t.Errorf("the picker for car offers %v", p)
			}
		}
		if found[w.carolID.String()]["canView"] != true {
			t.Errorf("carol, a viewer, reads %v", found[w.carolID.String()])
		}
	})

	t.Run("a percent sign and an underscore match themselves", func(t *testing.T) {
		percent, underscore := h.namedPerson(t, "%Percent "+tag), h.namedPerson(t, "_Under "+tag)
		for q, only := range map[string]uuid.UUID{"%": percent, "_": underscore} {
			found := w.pickerFor(t, q)
			for _, id := range append([]uuid.UUID{percent, underscore}, world...) {
				if _, ok := found[id.String()]; ok != (id == only) {
					t.Errorf("the picker for %q offers %s: %t", q, id, ok)
				}
			}
			for _, p := range found {
				if !startsAWord(p, q) {
					t.Errorf("the picker for %q offers %v, which it matched as a pattern", q, p)
				}
			}
		}
	})

	t.Run("a member without a grant is offered as one who may not read, a deactivated one not at all", func(t *testing.T) {
		if p, ok := w.pickerFor(t, "Oscar "+tag)[outsider.String()]; !ok || p["canView"] != false {
			t.Errorf("the member without a grant reads %v (offered %t)", p, ok)
		}
		if _, ok := w.pickerFor(t, "Gina "+tag)[gone.String()]; ok {
			t.Error("the picker offers a deactivated member")
		}
	})

	t.Run("a reader is not offered the picker", func(t *testing.T) {
		refuse(t, "carol asking whom to mention", w.carol.get(t, "/api/v1/requirements/"+w.reqKey+"/mentionable?q=car"), http.StatusForbidden, "forbidden")
	})
}

func TestAnEditAndAReopeningTellWhoTheyConcern(t *testing.T) {
	w := newCommentWorld(t, "CME")
	h := w.h
	req := "/api/v1/requirements/" + w.reqKey
	// The author watches what they write; without that only the edit's mention tells them.
	want(t, w.owner.delete(t, req+"/watch"), http.StatusNoContent, "the author stops watching R1")
	want(t, w.carol.put(t, req+"/watch", nil), http.StatusOK, "carol watches R1")
	threadID := obj(t, want(t, w.dave.post(t, req+"/comments", commentBody("Why 100 ms?")), http.StatusCreated, "dave comments"), "thread")["id"].(string)
	h.drained(t)
	aboutIt := `SELECT count(*) FROM notification WHERE user_id = $1 AND comment_id = $2`
	if n := h.countRows(t, aboutIt, w.carolID, threadID); n != 1 {
		t.Fatalf("carol, watching, was told of the comment %d times", n)
	}

	t.Run("an edit that adds a mention tells the one it adds, and nobody twice", func(t *testing.T) {
		want(t, w.dave.patch(t, "/api/v1/comments/"+threadID, commentBody("Why 100 ms, really? ", w.ownerID, w.carolID)), http.StatusOK, "dave edits")
		h.drained(t)
		var mention map[string]any
		for _, n := range told(t, w.owner, w.reqID) {
			if n["kind"] == "mentioned" && n["commentId"] == threadID {
				mention = n
			}
		}
		if mention == nil || mention["excerpt"] != "Why 100 ms, really? @Someone@Someone" {
			t.Errorf("the author was told of the edit's mention as %v", mention)
		}
		if n := h.countRows(t, aboutIt, w.carolID, threadID); n != 1 {
			t.Errorf("carol, already told of the comment, has %d rows about it", n)
		}
	})

	resolution := "/api/v1/comment-threads/" + threadID + "/resolution"
	resolvedFor := func(id uuid.UUID) int {
		return h.countRows(t, `SELECT count(*) FROM notification WHERE user_id = $1 AND thread_id = $2 AND kind = 'resolved'`, id, threadID)
	}
	events := func(topic string) int {
		return h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = $1 AND payload ->> 'threadId' = $2`, topic, threadID)
	}

	t.Run("resolving twice tells once", func(t *testing.T) {
		want(t, w.bob.post(t, "/api/v1/comments/"+threadID+"/replies", commentBody("The sensor.")), http.StatusCreated, "bob replies")
		want(t, w.owner.put(t, resolution, nil), http.StatusOK, "the owner resolves")
		h.drained(t)
		if resolvedFor(w.daveID) != 1 || resolvedFor(w.bobID) != 1 {
			t.Fatalf("the writers were told of the resolution %d and %d times", resolvedFor(w.daveID), resolvedFor(w.bobID))
		}
		want(t, w.owner.put(t, resolution, nil), http.StatusOK, "the owner resolves it again")
		h.drained(t)
		if n := events("thread.resolved"); n != 1 {
			t.Errorf("%d resolution events, want one", n)
		}
		if resolvedFor(w.daveID) != 1 || resolvedFor(w.bobID) != 1 {
			t.Errorf("a second resolution told the writers again: %d and %d", resolvedFor(w.daveID), resolvedFor(w.bobID))
		}
	})

	t.Run("reopening tells the thread's writers", func(t *testing.T) {
		want(t, w.owner.delete(t, resolution), http.StatusOK, "the owner reopens")
		h.drained(t)
		if n := events("thread.reopened"); n != 1 {
			t.Errorf("%d reopening events, want one", n)
		}
		if resolvedFor(w.daveID) != 2 || resolvedFor(w.bobID) != 2 {
			t.Errorf("after the reopening the writers have %d and %d rows, want 2 each", resolvedFor(w.daveID), resolvedFor(w.bobID))
		}
		if n := resolvedFor(w.ownerID); n != 0 {
			t.Errorf("the owner was told %d times of their own resolving", n)
		}
	})
}

func TestAnAssistantReadsTheDiscussion(t *testing.T) {
	w := newCommentWorld(t, "CMP")
	w.closeToEveryone(t)
	api := w.owner.api
	comments := "/api/v1/requirements/" + w.reqKey + "/comments"
	threadID := obj(t, want(t, w.dave.post(t, comments, commentBody("Why 100 ms?")), http.StatusCreated, "dave comments"), "thread")["id"].(string)
	want(t, w.bob.post(t, "/api/v1/comments/"+threadID+"/replies", commentBody("Because of the sensor.")), http.StatusCreated, "bob replies")
	outsider := w.h.addPerson(t, "member")
	_, bobSecret := makeToken(t, w.bob, map[string]any{"name": "bob's assistant"})
	_, outsiderSecret := makeToken(t, api.as(t, outsider), map[string]any{"name": "an outsider's assistant"})
	w.h.settle(t)

	idsOf := func(threads []any) []string {
		var out []string
		for _, each := range threads {
			th := each.(map[string]any)
			for _, c := range th["comments"].([]any) {
				out = append(out, th["id"].(string)+"/"+c.(map[string]any)["id"].(string))
			}
		}
		return out
	}
	listed := list(t, want(t, w.bob.get(t, comments), http.StatusOK, "bob lists the discussion"), "threads")
	told := list(t, response{Body: connectAssistant(t, api, bobSecret).call(t, "list_comments", map[string]any{"reqKey": w.reqKey})}, "threads")
	sameList(t, "the threads list_comments answers", idsOf(told), idsOf(listed)...)
	words := []string{"Why 100 ms?", "Because of the sensor."}
	for i, c := range told[0].(map[string]any)["comments"].([]any) {
		if md, _ := c.(map[string]any)["bodyMarkdown"].(string); !strings.Contains(md, words[i]) {
			t.Errorf("comment %d reads %q as Markdown, want %q", i, md, words[i])
		}
	}
	if said := connectAssistant(t, api, outsiderSecret).refused(t, "list_comments", map[string]any{"reqKey": w.reqKey}); said == "" {
		t.Error("the refusal says nothing")
	}
}

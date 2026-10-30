//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// The glossary (#210): terms marked where requirements use them, kept in revisions, and a changed definition suspect
// for the owners of their uses until confirmed; library glossaries reach as far as the reader may read.

func termsUsed(t *testing.T, c *client, reqKey string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+reqKey+"/terms"), http.StatusOK, "read the terms"), "terms") {
		rt := each.(map[string]any)
		state := "current"
		if rt["suspect"] == true {
			state = "suspect"
		}
		if rt["explicit"] == true {
			state += " explicit"
		}
		out[rt["term"].(map[string]any)["name"].(string)] = state
	}
	return out
}

// markedDoc is a paragraph whose words are linked to a term on purpose.
func markedDoc(before, marked, after, termID string) map[string]any {
	return map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{
		map[string]any{"type": "text", "text": before},
		map[string]any{"type": "text", "text": marked, "marks": []any{map[string]any{"type": "term", "attrs": map[string]any{"termId": termID}}}},
		map[string]any{"type": "text", "text": after},
	}}}}
}

func TestAGlossaryMarksWhereRequirementsUseItsTerms(t *testing.T) {
	w := newCommentWorld(t, "GLS")
	terms := "/api/v1/projects/" + w.key + "/terms"
	reqs := "/api/v1/projects/" + w.key + "/requirements"
	pedal := obj(t, want(t, w.owner.post(t, terms, map[string]any{"name": "Brake pedal", "synonyms": []string{"foot brake"},
		"avoid": []string{"stopper"}, "definition": docOf("The lever the driver presses to slow the car.")}), http.StatusCreated, "define Brake pedal"), "term")
	pedalID := pedal["id"].(string)

	t.Run("editors define terms, each word once in the glossary", func(t *testing.T) {
		if pedal["revision"] != float64(1) || pedal["status"] != "proposed" || pedal["projectKey"] != w.key {
			t.Errorf("the term reads %v", pedal)
		}
		want(t, w.bob.post(t, terms, map[string]any{"name": "Wheel"}), http.StatusCreated, "bob defines Wheel")
		refuse(t, "carol defining a term", w.carol.post(t, terms, map[string]any{"name": "Axle"}), http.StatusForbidden, "forbidden")
		refuse(t, "a name in another case", w.bob.post(t, terms, map[string]any{"name": "brake PEDAL"}), http.StatusConflict, "term_exists")
		refuse(t, "a synonym another term has", w.bob.post(t, terms, map[string]any{"name": "Pedal unit", "synonyms": []string{"Foot brake"}}),
			http.StatusConflict, "term_exists")
		fieldError(t, w.bob.post(t, terms, map[string]any{"name": " "}), "name")
		fieldError(t, w.bob.post(t, terms, map[string]any{"name": "Rim", "status": "nonsense"}), "status")
		listed := list(t, want(t, w.carol.get(t, terms+"?q=pedal"), http.StatusOK, "carol finds it"), "terms")
		if len(listed) != 1 || listed[0].(map[string]any)["name"] != "Brake pedal" {
			t.Errorf("q=pedal finds %v", listed)
		}
	})

	var r2, r3 string
	t.Run("a requirement uses a term by its words in any inflection, or by an explicit link", func(t *testing.T) {
		r2 = obj(t, want(t, w.bob.post(t, reqs, map[string]any{"title": "Return", "statement": docOf("The brake pedals shall return within 1 s."),
			"ownerId": w.bobID.String()}), http.StatusCreated, "bob writes R2"), "requirement")["key"].(string)
		if got := termsUsed(t, w.carol, r2); got["Brake pedal"] != "current" {
			t.Errorf("R2 uses %v", got)
		}
		r3 = obj(t, want(t, w.bob.post(t, reqs, map[string]any{"title": "Hold", "statement": markedDoc("The ", "lever", " shall hold.", pedalID),
			"ownerId": w.bobID.String()}), http.StatusCreated, "bob writes R3 with a link"), "requirement")["key"].(string)
		if got := termsUsed(t, w.carol, r3); got["Brake pedal"] != "current explicit" {
			t.Errorf("R3 uses %v", got)
		}
		uses := list(t, want(t, w.carol.get(t, "/api/v1/terms/"+pedalID+"/uses"), http.StatusOK, "the term's uses"), "uses")
		var keys []string
		for _, u := range uses {
			keys = append(keys, u.(map[string]any)["requirement"].(map[string]any)["key"].(string))
		}
		if !slices.Contains(keys, r2) || !slices.Contains(keys, r3) {
			t.Errorf("the term is used by %v", keys)
		}
		if got := rowKeys(t, want(t, w.carol.get(t, reqs+"?query="+url.QueryEscape(`term = "foot brake"`)), http.StatusOK, "query")); !slices.Contains(got, r2) {
			t.Errorf(`term = "foot brake" keeps %v`, got)
		}
		if got := rowKeys(t, want(t, w.carol.get(t, reqs+"?query="+url.QueryEscape("term IS EMPTY")), http.StatusOK, "query")); slices.Contains(got, r2) || !slices.Contains(got, w.reqKey) {
			t.Errorf("term IS EMPTY keeps %v", got)
		}
	})

	t.Run("a word not to use is a finding that names the term instead", func(t *testing.T) {
		r4 := obj(t, want(t, w.bob.post(t, reqs, map[string]any{"title": "Stop", "statement": docOf("The stopper shall hold the car.")}),
			http.StatusCreated, "bob writes R4"), "requirement")["key"].(string)
		q := obj(t, want(t, w.carol.get(t, "/api/v1/requirements/"+r4+"/quality"), http.StatusOK, "R4's findings"), "quality")
		var fix string
		for _, each := range q["findings"].([]any) {
			if f := each.(map[string]any); f["rule"] == "avoid_term" {
				fix = f["fix"].(string)
			}
		}
		if fix != `Use "Brake pedal" instead.` {
			t.Errorf("the avoid finding fixes as %q in %v", fix, q["findings"])
		}
	})

	t.Run("a changed definition makes its uses suspect and tells their owner, until confirmed", func(t *testing.T) {
		refuse(t, "a change on an old revision", w.owner.patch(t, "/api/v1/terms/"+pedalID, map[string]any{"baseRevision": 0, "status": "approved"}),
			http.StatusConflict, "stale_revision")
		approved := obj(t, want(t, w.owner.patch(t, "/api/v1/terms/"+pedalID, map[string]any{"baseRevision": 1, "status": "approved"}),
			http.StatusOK, "approve the term"), "term")
		if approved["revision"] != float64(2) || approved["definitionRevision"] != float64(1) {
			t.Errorf("approved, the term reads %v", approved)
		}
		if got := termsUsed(t, w.carol, r2); got["Brake pedal"] != "current" {
			t.Errorf("a new status alone leaves R2 at %v", got)
		}
		changed := obj(t, want(t, w.owner.patch(t, "/api/v1/terms/"+pedalID, map[string]any{"baseRevision": 2,
			"definition": docOf("The lever the driver presses to slow or hold the car."), "comment": "Holding counts too."}), http.StatusOK, "redefine it"), "term")
		if changed["revision"] != float64(3) || changed["definitionRevision"] != float64(3) {
			t.Errorf("redefined, the term reads %v", changed)
		}
		if got := termsUsed(t, w.carol, r2); got["Brake pedal"] != "suspect" {
			t.Errorf("after the change R2 reads %v", got)
		}
		if got := rowKeys(t, want(t, w.carol.get(t, reqs+"?query="+url.QueryEscape("suspect > 0")), http.StatusOK, "query")); !slices.Contains(got, r2) {
			t.Errorf("suspect > 0 keeps %v", got)
		}
		w.h.drained(t)
		var told bool
		for _, each := range list(t, want(t, w.bob.get(t, "/api/v1/notifications"), http.StatusOK, "bob's notifications"), "notifications") {
			n := each.(map[string]any)
			if n["kind"] == "term_changed" && n["excerpt"] == "Brake pedal" {
				told = true
			}
		}
		if !told {
			t.Error("bob was not told that a term his requirements use changed")
		}
		clear := "/api/v1/requirements/" + r2 + "/terms/" + pedalID + "/clear"
		refuse(t, "a confirmation of an old definition", w.bob.post(t, clear, map[string]any{"termRevision": 1, "comment": "Still holds."}),
			http.StatusConflict, "stale_revision")
		refuse(t, "carol confirming", w.carol.post(t, clear, map[string]any{"termRevision": 3, "comment": "Still holds."}), http.StatusForbidden, "forbidden")
		fieldError(t, w.bob.post(t, clear, map[string]any{"termRevision": 3, "comment": ""}), "comment")
		confirmed := obj(t, want(t, w.bob.post(t, clear, map[string]any{"termRevision": 3, "comment": "Still holds."}), http.StatusOK, "bob confirms"), "term")
		if confirmed["suspect"] != false || confirmed["seenRevision"] != float64(3) {
			t.Errorf("confirmed, the use reads %v", confirmed)
		}
		revisions := list(t, want(t, w.carol.get(t, "/api/v1/terms/"+pedalID+"/revisions"), http.StatusOK, "the revisions"), "revisions")
		if len(revisions) != 3 || revisions[0].(map[string]any)["comment"] != "Holding counts too." {
			t.Errorf("the revisions read %v", revisions)
		}
		want(t, w.carol.get(t, "/api/v1/terms/"+pedalID+"/revisions/1"), http.StatusOK, "revision 1")
		refuse(t, "a revision that is not", w.carol.get(t, "/api/v1/terms/"+pedalID+"/revisions/9"), http.StatusNotFound, "not_found")
	})

	t.Run("a search finds terms beside requirements", func(t *testing.T) {
		got := want(t, w.carol.get(t, "/api/v1/search?q=pedal"), http.StatusOK, "search")
		found := false
		for _, each := range list(t, got, "terms") {
			if each.(map[string]any)["id"] == pedalID {
				found = true
			}
		}
		if !found {
			t.Errorf("the search answers terms %v", list(t, got, "terms"))
		}
	})

	t.Run("a project uses a library's glossary as far as the reader may read it", func(t *testing.T) {
		lib := projectKey(t, "GLL")
		made := obj(t, want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": lib, "name": "Parts"}), http.StatusCreated, "make a library"), "project")
		if _, err := w.h.super.Exec(context.Background(), `DELETE FROM role_assignment WHERE project_id = $1 AND group_id = (SELECT id FROM groups WHERE builtin = 'everyone')`, made["id"]); err != nil {
			t.Fatal(err)
		}
		caliper := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+lib+"/terms", map[string]any{"name": "Caliper"}), http.StatusCreated, "define Caliper"), "term")
		glossaries := "/api/v1/projects/" + w.key + "/glossaries"
		fieldError(t, w.owner.put(t, glossaries, map[string]any{"libraries": []string{lib}}), "libraries[0]")
		want(t, w.owner.patch(t, "/api/v1/projects/"+lib, map[string]any{"settings": map[string]any{"library": true}}), http.StatusOK, "mark it a library")
		refuse(t, "bob choosing glossaries", w.bob.put(t, glossaries, map[string]any{"libraries": []string{lib}}), http.StatusForbidden, "forbidden")
		chosen := obj(t, want(t, w.owner.put(t, glossaries, map[string]any{"libraries": []string{lib}}), http.StatusOK, "use the library"), "glossaries")
		if libs := chosen["libraries"].([]any); len(libs) != 1 || libs[0].(map[string]any)["terms"] != float64(1) {
			t.Errorf("the glossaries read %v", chosen)
		}
		if read := obj(t, want(t, w.carol.get(t, glossaries), http.StatusOK, "carol reads which glossaries the project uses"), "glossaries"); len(read["libraries"].([]any)) != 0 {
			t.Errorf("carol, who may not read the library, reads the glossaries %v", read)
		}
		w.h.drained(t)
		r5 := obj(t, want(t, w.owner.post(t, reqs, map[string]any{"title": "Grip", "statement": docOf("The calipers shall grip the disc.")}),
			http.StatusCreated, "the owner writes R5"), "requirement")["key"].(string)
		if got := termsUsed(t, w.owner, r5); got["Caliper"] != "current" {
			t.Errorf("for the owner R5 uses %v", got)
		}
		if got := termsUsed(t, w.carol, r5); got["Caliper"] != "" {
			t.Errorf("carol, who may not read the library, sees R5 use %v", got)
		}
		refuse(t, "carol reading the library's term", w.carol.get(t, "/api/v1/terms/"+caliper["id"].(string)), http.StatusNotFound, "not_found")
		listed := list(t, want(t, w.owner.get(t, "/api/v1/projects/"+w.key+"/terms"), http.StatusOK, "the owner lists the terms"), "terms")
		if len(listed) < 3 {
			t.Errorf("the owner lists %v", listed)
		}
	})

	t.Run("an administrator deletes a term, which leaves the words", func(t *testing.T) {
		if got := obj(t, want(t, w.carol.get(t, "/api/v1/terms/"+pedalID), http.StatusOK, "carol reads the term"), "term"); got["uses"].(float64) < 2 {
			t.Errorf("the term reads %v", got)
		}
		refuse(t, "bob deleting a term", w.bob.delete(t, "/api/v1/terms/"+pedalID), http.StatusForbidden, "forbidden")
		want(t, w.owner.delete(t, "/api/v1/terms/"+pedalID), http.StatusNoContent, "the owner deletes it")
		refuse(t, "the term gone", w.carol.get(t, "/api/v1/terms/"+pedalID), http.StatusNotFound, "not_found")
		if got := termsUsed(t, w.carol, r3); len(got) != 0 {
			t.Errorf("after the delete R3 uses %v", got)
		}
		want(t, w.carol.get(t, "/api/v1/requirements/"+r3), http.StatusOK, "R3 still reads")
	})
}

// Straight through SQL as exciter_app: a term's revisions are the database's, a use sees what the term says and only
// a confirmation moves it, a reader sees no use of a term they may not read, and a project uses only libraries.
func TestRawSQLKeepsGlossariesToTheirReaders(t *testing.T) {
	w := newCommentWorld(t, "GLR")
	ctx := context.Background()
	term := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/terms", map[string]any{"name": "Brake"}), http.StatusCreated, "define Brake"), "term")
	termID := uuid.MustParse(term["id"].(string))
	reqID := uuid.MustParse(w.reqID)

	viewer := w.h.appConn(t, w.carolID)
	refusedOutright(t, viewer, "a viewer defines a term", `INSERT INTO glossary_term (project_id, name, definition, created_by) VALUES ($1, 'Disc', '{}', $2)`,
		w.projectID, w.carolID)
	touchesNothing(t, viewer, "a viewer renames a term", `UPDATE glossary_term SET name = 'Stopper', updated_by = $2 WHERE id = $1`, termID, w.carolID)
	refusedOutright(t, viewer, "a viewer marks a use", `INSERT INTO term_use (requirement_id, term_id, explicit, seen_revision) VALUES ($1, $2, true, 1)`,
		reqID, termID)

	editor := w.h.appConn(t, w.bobID)
	refusedOutright(t, editor, "an editor writes a revision", `INSERT INTO glossary_term_revision (term_id, number, name, definition, synonyms, avoid, status, comment)
		VALUES ($1, 9, 'x', '{}', '{}', '{}', 'proposed', '')`, termID)
	refusedOutright(t, editor, "an editor rewrites a revision", `UPDATE glossary_term_revision SET name = 'x' WHERE term_id = $1`, termID)
	touchesNothing(t, editor, "an editor deletes a term", `DELETE FROM glossary_term WHERE id = $1`, termID)
	refusedOutright(t, editor, "an editor moves the revision", `UPDATE glossary_term SET revision = 99, updated_by = $2 WHERE id = $1`, termID, w.bobID)
	if _, err := editor.Exec(ctx, `INSERT INTO term_use (requirement_id, term_id, explicit, seen_revision) VALUES ($1, $2, true, 99)
		ON CONFLICT (requirement_id, term_id) DO UPDATE SET explicit = true`, reqID, termID); err != nil {
		t.Fatalf("an editor marks a use: %v", err)
	}
	var seen, current int
	if err := w.h.super.QueryRow(ctx, `SELECT u.seen_revision, t.definition_revision FROM term_use u JOIN glossary_term t ON t.id = u.term_id
		WHERE u.requirement_id = $1 AND u.term_id = $2`, reqID, termID).Scan(&seen, &current); err != nil || seen != current {
		t.Errorf("a use was written seeing revision %d of a term at %d (%v)", seen, current, err)
	}
	refusedOutright(t, editor, "an editor confirms by hand", `UPDATE term_use SET seen_revision = 99 WHERE requirement_id = $1`, reqID)
	refusedOutright(t, editor, "an editor confirms without a comment", `SELECT term_use_clear($1, $2, $3, ' ')`, reqID, termID, current)

	other := projectKey(t, "GLO")
	made := obj(t, want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Other"}), http.StatusCreated, "another project"), "project")
	admin := w.h.appConn(t, w.ownerID)
	refusedOutright(t, admin, "using a project that is no library", `INSERT INTO project_glossary (project_id, library_id, position, added_by) VALUES ($1, $2, 0, $3)`,
		w.projectID, made["id"], w.ownerID)
	refusedOutright(t, admin, "using the project itself", `INSERT INTO project_glossary (project_id, library_id, position, added_by) VALUES ($1, $1, 0, $2)`,
		w.projectID, w.ownerID)
	refusedOutright(t, editor, "an editor choosing glossaries", `INSERT INTO project_glossary (project_id, library_id, position, added_by) VALUES ($1, $2, 0, $3)`,
		w.projectID, made["id"], w.bobID)

	if _, err := w.h.super.Exec(ctx, `UPDATE project SET settings = settings || '{"library": true}' WHERE id = $1`, made["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := w.h.super.Exec(ctx, `DELETE FROM role_assignment WHERE project_id = $1 AND group_id = (SELECT id FROM groups WHERE builtin = 'everyone')`, made["id"]); err != nil {
		t.Fatal(err)
	}
	hidden := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+other+"/terms", map[string]any{"name": "Hidden"}), http.StatusCreated, "a library term"), "term")
	if _, err := w.h.super.Exec(ctx, `INSERT INTO term_use (requirement_id, term_id, explicit, seen_revision) VALUES ($1, $2, false, 1)`, reqID, hidden["id"]); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := viewer.QueryRow(ctx, `SELECT count(*) FROM term_use WHERE term_id = $1`, hidden["id"]).Scan(&n); err != nil || n != 0 {
		t.Errorf("a reader without the library reads %d of its uses (%v)", n, err)
	}
}

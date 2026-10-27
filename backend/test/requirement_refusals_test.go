//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// What a requirement's restore, copy, lock, labels, owner and trash refuse, each with its status and code, and
// with the rows as they were (#309).

// tooLong is one character more than a bound of length allows.
func tooLong(length int) string { return strings.Repeat("x", length+1) }

// revisionsOf counts the revisions a requirement has written, past every policy.
func (s *lockScene) revisionsOf(t *testing.T, id string) int {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1`, id)
}

// inProject counts the requirements of a project, trashed or not.
func (s *lockScene) inProject(t *testing.T, key string) int {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1`, key)
}

// lockSentence is what the database says of a locked requirement, nothing when it is not.
func (s *lockScene) lockSentence(t *testing.T, id string) string {
	t.Helper()
	var sentence *string
	if err := s.h.super.QueryRow(context.Background(), `SELECT requirement_lock_sentence($1)`, id).Scan(&sentence); err != nil {
		t.Fatal(err)
	}
	if sentence == nil {
		return ""
	}
	return *sentence
}

// write puts a requirement in the scene's project as its owner, answering it.
func (s *lockScene) write(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	return obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", body), http.StatusCreated, "write "+body["title"].(string)), "requirement")
}

// otherProject makes a project of the scene's owner whose everyone role is role, answering its key.
func (s *lockScene) otherProject(t *testing.T, prefix, role string) string {
	t.Helper()
	key := projectKey(t, prefix)
	made := obj(t, want(t, s.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key), "project")
	if _, err := s.h.super.Exec(t.Context(), `UPDATE project_grant SET role = $2 WHERE project_id = $1 AND subject_type = 'everyone'`, made["id"], role); err != nil {
		t.Fatal(err)
	}
	return key
}

func (s *lockScene) archive(t *testing.T, key string) {
	t.Helper()
	want(t, s.owner.put(t, "/api/v1/projects/"+key+"/archive", nil), http.StatusOK, "archive "+key)
}

func TestARevisionIsRestoredOnlyWhereTheRequirementMayChange(t *testing.T) {
	s := newLockScene(t, "RRS")
	r1 := "/api/v1/requirements/" + s.req("1")
	want(t, s.bob.patch(t, r1, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "bob writes revision 2")
	restore := func(c *client, body map[string]any) response { return c.post(t, r1+"/revisions/1/restore", body) }
	unchanged := func(what string, revisions int) {
		t.Helper()
		if n := s.revisionsOf(t, s.reqID); n != revisions {
			t.Errorf("%s: R1 holds %d revisions, want %d", what, n, revisions)
		}
	}

	t.Run("a reader restores nothing, and a comment past its bound is refused", func(t *testing.T) {
		refuse(t, "carol, who reads, restores", restore(s.carol, map[string]any{"baseRevision": 2}), http.StatusForbidden, "forbidden")
		if msg := fieldError(t, restore(s.bob, map[string]any{"baseRevision": 2, "comment": tooLong(requirement.MaxCommentLength)}), "comment"); msg == "" {
			t.Error("a long comment is refused without a sentence")
		}
		unchanged("after the refusals", 2)
	})

	t.Run("a requirement locked by hand is refused in the database's words", func(t *testing.T) {
		want(t, s.owner.post(t, r1+"/lock", map[string]any{"reason": "Released in 2.0"}), http.StatusOK, "lock R1")
		r := restore(s.bob, map[string]any{"baseRevision": 2})
		refuse(t, "a restore of the hand-locked R1", r, http.StatusConflict, "requirement_locked")
		if msg, sentence := refusalMessage(t, r), s.lockSentence(t, s.reqID); msg != sentence || !strings.Contains(msg, "Released in 2.0") {
			t.Errorf("the refusal says %q, the database %q", msg, sentence)
		}
		unchanged("locked by hand", 2)
		want(t, s.owner.delete(t, r1+"/lock"), http.StatusOK, "unlock R1")
	})

	t.Run("a requirement locked by its status is refused in the database's words", func(t *testing.T) {
		s.lockDraft(t, true)
		r := restore(s.bob, map[string]any{"baseRevision": 2})
		refuse(t, "a restore of R1 in the locking Draft", r, http.StatusConflict, "requirement_locked")
		if msg, sentence := refusalMessage(t, r), s.lockSentence(t, s.reqID); msg != sentence || !strings.Contains(msg, "locked in the status Draft") {
			t.Errorf("the refusal says %q, the database %q", msg, sentence)
		}
		unchanged("locked by its status", 2)
		s.lockDraft(t, false)
	})

	t.Run("a requirement in the trash restores no revision", func(t *testing.T) {
		want(t, s.bob.delete(t, r1), http.StatusNoContent, "trash R1")
		refuse(t, "a restore in the trash", restore(s.bob, map[string]any{"baseRevision": 2}), http.StatusConflict, "requirement_trashed")
		unchanged("in the trash", 2)
		want(t, s.bob.post(t, "/api/v1/projects/"+s.key+"/trash/"+s.reqID+"/restore", nil), http.StatusOK, "take R1 out of the trash")
	})

	t.Run("where every revision asks for a comment, a restore without one says what it restored", func(t *testing.T) {
		want(t, s.owner.patch(t, "/api/v1/projects/"+s.key, map[string]any{"settings": map[string]any{"requireRevisionComment": true}}),
			http.StatusOK, "ask for a comment on every revision")
		fieldError(t, s.bob.patch(t, r1, map[string]any{"title": "Brakes engage within 60 ms", "baseRevision": 2}), "comment")
		got := want(t, restore(s.bob, map[string]any{"baseRevision": 2}), http.StatusOK, "restore revision 1 without a comment")
		if revision := obj(t, got, "revision"); revision["comment"] != "Restored revision 1" || revision["restoredFrom"] != float64(1) {
			t.Errorf("the restore wrote %v", revision)
		}
		unchanged("after the restore", 3)
	})

	t.Run("an archived project restores nothing", func(t *testing.T) {
		s.archive(t, s.key)
		refuse(t, "a restore in an archived project", s.owner.post(t, r1+"/revisions/2/restore", map[string]any{"baseRevision": 3}),
			http.StatusConflict, "project_archived")
		unchanged("archived", 3)
	})
}

func TestACopyLandsOnlyWhereItsPersonMayWrite(t *testing.T) {
	s := newLockScene(t, "RCP")
	r1 := "/api/v1/requirements/" + s.req("1") + "/copy"
	viewed, archived := s.otherProject(t, "RCV", "viewer"), s.otherProject(t, "RCA", "editor")
	s.archive(t, archived)
	before := s.inProject(t, s.key)
	nothingLanded := func(what string) {
		t.Helper()
		if n, v, a := s.inProject(t, s.key), s.inProject(t, viewed), s.inProject(t, archived); n != before || v != 0 || a != 0 {
			t.Errorf("%s left %d requirements here (want %d), %d in %s and %d in %s", what, n, before, v, viewed, a, archived)
		}
	}

	t.Run("a reader, a project only read and an archived project take no copy", func(t *testing.T) {
		refuse(t, "carol, who reads, copies within the project", s.carol.post(t, r1, map[string]any{"place": place(nil, nil)}), http.StatusForbidden, "forbidden")
		refuse(t, "bob copies into a project he only reads", s.bob.post(t, r1, map[string]any{"projectKey": viewed, "place": place(nil, nil)}),
			http.StatusForbidden, "forbidden")
		refuse(t, "a copy into an archived project", s.bob.post(t, r1, map[string]any{"projectKey": archived, "place": place(nil, nil)}),
			http.StatusConflict, "project_archived")
		nothingLanded("the refusals")
	})

	t.Run("a requirement in the trash is not copied", func(t *testing.T) {
		gone := s.write(t, map[string]any{"title": "Brakes squeal when worn"})
		want(t, s.bob.delete(t, "/api/v1/requirements/"+gone["key"].(string)), http.StatusNoContent, "trash it")
		before = s.inProject(t, s.key)
		refuse(t, "a copy of a trashed requirement", s.bob.post(t, "/api/v1/requirements/"+gone["key"].(string)+"/copy", map[string]any{"place": place(nil, nil)}),
			http.StatusConflict, "requirement_trashed")
		nothingLanded("a copy from the trash")
	})

	t.Run("a section with more below it than one copy makes is refused on children", func(t *testing.T) {
		big := s.write(t, map[string]any{"kind": "section", "title": "Everything"})
		// Written straight into the table: a thousand saves through the API would only make the test slow.
		if _, err := s.h.super.Exec(t.Context(), `
			INSERT INTO requirement (project_id, title, statement, normative_hash, created_by, parent_id)
			SELECT p.id, 'Part ' || i, '{"type":"doc"}', 'h', $2, $3 FROM project p, generate_series(1, $4::int) i WHERE p.key = $1`,
			s.key, s.ownerID, big["id"], requirement.MaxCopy); err != nil {
			t.Fatal(err)
		}
		before = s.inProject(t, s.key)
		r := s.bob.post(t, "/api/v1/requirements/"+big["key"].(string)+"/copy", map[string]any{"place": place(nil, nil), "children": true})
		refuse(t, "a copy of more than MaxCopy", r, http.StatusUnprocessableEntity, "validation_failed")
		if r.Status == http.StatusUnprocessableEntity {
			fieldError(t, r, "children")
		}
		nothingLanded("a copy past the bound")
	})

	t.Run("a child trashed on its own stays behind, and copied counts only what came", func(t *testing.T) {
		left := s.write(t, map[string]any{"title": "Brake fluid is checked", "place": place(s.sectionID, s.r3ID)})
		want(t, s.bob.delete(t, "/api/v1/requirements/"+left["key"].(string)), http.StatusNoContent, "trash one child alone")
		made := want(t, s.bob.post(t, "/api/v1/requirements/"+s.req("2")+"/copy", map[string]any{"place": place(nil, nil), "children": true}),
			http.StatusCreated, "copy the section with what is below it")
		if made.Body["copied"] != float64(2) {
			t.Errorf("the copy says it copied %v, want the section and R3", made.Body["copied"])
		}
		section := obj(t, made, "requirement")
		titles := []string{}
		for _, each := range list(t, want(t, s.bob.get(t, "/api/v1/projects/"+s.key+"/tree"), http.StatusOK, "read the tree"), "nodes") {
			if node := each.(map[string]any); node["parentId"] == section["id"] {
				titles = append(titles, node["title"].(string))
			}
		}
		sameList(t, "below the copy", titles, "Brake lights follow the pedal")
	})
}

func TestAHandLockIsTheAdministratorsOfAnOpenProject(t *testing.T) {
	s := newLockScene(t, "RLK")
	r1 := "/api/v1/requirements/" + s.req("1") + "/lock"
	guest := s.owner.api.as(t, s.h.addPerson(t, "guest"))
	notLocked := func(what, id string) {
		t.Helper()
		if n := s.h.countRows(t, `SELECT count(*) FROM requirement WHERE id = $1 AND locked_at IS NULL`, id); n != 1 {
			t.Errorf("%s locked it", what)
		}
	}

	t.Run("a reader, a guest, a trashed requirement and a long reason are refused", func(t *testing.T) {
		refuse(t, "carol, who reads, locks", s.carol.post(t, r1, map[string]any{"reason": "Released"}), http.StatusForbidden, "forbidden")
		refuse(t, "a guest locks", guest.post(t, r1, map[string]any{"reason": "Released"}), http.StatusNotFound, "not_found")
		fieldError(t, s.owner.post(t, r1, map[string]any{"reason": tooLong(requirement.MaxLockReasonLength)}), "reason")
		want(t, s.owner.post(t, r1, map[string]any{"reason": strings.Repeat("x", requirement.MaxLockReasonLength)}), http.StatusOK, "a reason at the bound")
		want(t, s.owner.delete(t, r1), http.StatusOK, "unlock it again")
		notLocked("the refusals", s.reqID)

		gone := s.write(t, map[string]any{"title": "Brakes squeal when worn"})
		want(t, s.bob.delete(t, "/api/v1/requirements/"+gone["key"].(string)), http.StatusNoContent, "trash it")
		refuse(t, "a lock in the trash", s.owner.post(t, "/api/v1/requirements/"+gone["key"].(string)+"/lock", map[string]any{"reason": "Released"}),
			http.StatusConflict, "section_not_locked")
		notLocked("a lock in the trash", gone["id"].(string))
	})

	t.Run("locking again changes the reason and logs it", func(t *testing.T) {
		want(t, s.owner.post(t, r1, map[string]any{"reason": "Released in 2.0"}), http.StatusOK, "lock R1")
		got := obj(t, want(t, s.owner.post(t, r1, map[string]any{"reason": "Released in 2.1"}), http.StatusOK, "lock R1 again"), "requirement")
		if lock := got["lock"].(map[string]any); lock["reason"] != "Released in 2.1" {
			t.Errorf("locked again R1 reads %v", lock)
		}
		rows, err := s.h.super.Query(t.Context(), `SELECT from_value || '>' || to_value || ' ' || reason FROM requirement_change
			WHERE requirement_id = $1 AND field = 'lock' ORDER BY id`, s.reqID)
		if err != nil {
			t.Fatal(err)
		}
		var logged []string
		for rows.Next() {
			var each string
			if err := rows.Scan(&each); err != nil {
				t.Fatal(err)
			}
			logged = append(logged, each)
		}
		sameList(t, "R1's lock changes", logged[len(logged)-2:], ">locked Released in 2.0", "locked>locked Released in 2.1")
	})

	t.Run("an archived project takes no lock", func(t *testing.T) {
		s.archive(t, s.key)
		r3 := "/api/v1/requirements/" + s.req("3") + "/lock"
		// An archived project changes nothing, so the lock is refused as every other change there is.
		refuse(t, "a lock in an archived project", s.owner.post(t, r3, map[string]any{"reason": "Released"}), http.StatusConflict, "project_archived")
		notLocked("a lock in an archived project", s.r3ID)
	})
}

// Labels are not what a requirement says, so a lock by hand keeps them open as it keeps its owner and priority.
func TestARequirementLockedByHandStillTakesLabels(t *testing.T) {
	s := newLockScene(t, "RLL")
	labels := "/api/v1/requirements/" + s.req("1") + "/labels"
	want(t, s.bob.post(t, labels, map[string]any{"name": "brakes"}), http.StatusOK, "label R1")
	want(t, s.owner.post(t, "/api/v1/requirements/"+s.req("1")+"/lock", map[string]any{"reason": "Released in 2.0"}), http.StatusOK, "lock R1")

	got := want(t, s.bob.post(t, labels, map[string]any{"name": "Safety critical"}), http.StatusOK, "label the locked R1")
	sameList(t, "the labels of the locked R1", toStrings(got.Body["labels"]), "brakes", "safety-critical")
	want(t, s.bob.delete(t, labels+"/brakes"), http.StatusNoContent, "take a label off the locked R1")
	refuse(t, "a label name that is no label", s.bob.delete(t, labels+"/bad%21"), http.StatusUnprocessableEntity, "validation_failed")
	sameList(t, "the labels after", toStrings(want(t, s.bob.get(t, labels), http.StatusOK, "read the labels").Body["labels"]), "safety-critical")
	if got := obj(t, want(t, s.bob.get(t, "/api/v1/requirements/"+s.req("1")), http.StatusOK, "read R1"), "requirement"); got["revision"] != float64(1) {
		t.Errorf("labels on the locked R1 wrote revision %v", got["revision"])
	}
}

func TestAnOwnerIsSomebodyWhoReadsTheProject(t *testing.T) {
	s := newLockScene(t, "ROW")
	r1, section := "/api/v1/requirements/"+s.req("1"), "/api/v1/requirements/"+s.req("2")
	want(t, s.owner.patch(t, r1, map[string]any{"ownerId": s.bobID}), http.StatusOK, "bob owns R1")
	stranger := s.h.addPerson(t, "guest")

	fieldError(t, s.owner.patch(t, r1, map[string]any{"ownerId": stranger}), "ownerId")
	if got := obj(t, want(t, s.bob.get(t, r1), http.StatusOK, "read R1"), "requirement"); got["owner"].(map[string]any)["id"] != s.bobID.String() {
		t.Errorf("a refused owner changed R1's to %v", got["owner"])
	}
	fieldError(t, s.owner.patch(t, section, map[string]any{"ownerId": s.bobID}), "ownerId")
	fieldError(t, s.owner.patch(t, section, map[string]any{"priority": "must"}), "priority")
	if got := obj(t, want(t, s.bob.get(t, section), http.StatusOK, "read the section"), "requirement"); got["owner"] != nil || got["priority"] != nil {
		t.Errorf("the section reads owner %v, priority %v", got["owner"], got["priority"])
	}
}

// A save that sends the statement it read, in any order of its keys, changes nothing a field rule holds (#309).
func TestASaveOfTheSameStatementMeetsNoFieldRule(t *testing.T) {
	s := newLockScene(t, "RFS")
	r1 := "/api/v1/requirements/" + s.req("1")
	written := statementSaying("The brakes engage.")
	want(t, s.owner.patch(t, r1, map[string]any{"statement": written, "baseRevision": 1}), http.StatusOK, "give R1 words")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/field-rules/statement", map[string]any{"editableBy": []map[string]any{{"roles": []string{"admin"}}}}),
		http.StatusOK, "only admins change the statement")
	got := obj(t, want(t, s.bob.patch(t, r1, map[string]any{"title": "Brakes engage at once", "statement": written, "baseRevision": 2}),
		http.StatusOK, "bob saves a new title with the statement as he read it"), "requirement")
	if got["title"] != "Brakes engage at once" || got["revision"] != float64(3) {
		t.Errorf("the save reads %v at revision %v", got["title"], got["revision"])
	}
	fieldError(t, s.bob.patch(t, r1, map[string]any{"statement": statementSaying("The brakes hold."), "baseRevision": 3}), "statement")
}

func TestTheTrashKeepsToItsProjectAndItsItems(t *testing.T) {
	s := newLockScene(t, "RTR")
	other := s.otherProject(t, "RTO", "editor")
	trashOf := func(key string) string { return "/api/v1/projects/" + key + "/trash" }
	theirs := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+other+"/requirements", map[string]any{"title": "Theirs"}), http.StatusCreated, "write in "+other),
		"requirement")
	want(t, s.owner.delete(t, "/api/v1/requirements/"+theirs["key"].(string)), http.StatusNoContent, "trash theirs")
	stillTrashed := func(what, id string) {
		t.Helper()
		if n := s.h.countRows(t, `SELECT count(*) FROM requirement WHERE id = $1 AND trashed_at IS NOT NULL`, id); n != 1 {
			t.Errorf("%s took it out of the trash", what)
		}
	}

	t.Run("another project's item is not found by id", func(t *testing.T) {
		id := theirs["id"].(string)
		refuse(t, "a restore across projects", s.owner.post(t, trashOf(s.key)+"/"+id+"/restore", nil), http.StatusNotFound, "not_found")
		refuse(t, "a purge across projects", s.owner.delete(t, trashOf(s.key)+"/"+id), http.StatusNotFound, "not_found")
		stillTrashed("a restore across projects", id)
	})

	t.Run("a reader restores nothing, and an archived project nothing either", func(t *testing.T) {
		want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("1")), http.StatusNoContent, "trash R1")
		refuse(t, "carol, who reads, restores", s.carol.post(t, trashOf(s.key)+"/"+s.reqID+"/restore", nil), http.StatusForbidden, "forbidden")
		stillTrashed("carol's restore", s.reqID)
		refuse(t, "a malformed id", s.owner.post(t, trashOf(s.key)+"/not-an-id/restore", nil), http.StatusBadRequest, "bad_request")
		want(t, s.bob.post(t, trashOf(s.key)+"/"+s.reqID+"/restore", nil), http.StatusOK, "bob restores R1")
	})

	t.Run("a child that went with its parent is restored with it, not by its own id", func(t *testing.T) {
		want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("2")), http.StatusNoContent, "trash the section with R3")
		refuse(t, "restoring R3 alone", s.bob.post(t, trashOf(s.key)+"/"+s.r3ID+"/restore", nil), http.StatusNotFound, "not_found")
		stillTrashed("restoring R3 alone", s.r3ID)
		want(t, s.bob.post(t, trashOf(s.key)+"/"+s.sectionID+"/restore", nil), http.StatusOK, "restore the section and R3")
	})

	t.Run("a child trashed alone before its parent comes back at the top, and not with the parent", func(t *testing.T) {
		alone := s.write(t, map[string]any{"title": "Brake pads are checked", "place": place(s.sectionID, s.r3ID)})
		later := s.write(t, map[string]any{"title": "Brake discs are checked", "place": place(s.sectionID, alone["id"])})
		for _, each := range []map[string]any{alone, later} {
			want(t, s.bob.delete(t, "/api/v1/requirements/"+each["key"].(string)), http.StatusNoContent, "trash "+each["key"].(string)+" alone")
		}
		want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("2")), http.StatusNoContent, "trash the section")
		items := map[string]map[string]any{}
		for _, each := range list(t, want(t, s.bob.get(t, trashOf(s.key)), http.StatusOK, "list the trash"), "items") {
			item := each.(map[string]any)
			items[item["key"].(string)] = item
		}
		for _, each := range []map[string]any{alone, later} {
			if item := items[each["key"].(string)]; item["parentInTree"] != false || item["requirements"] != float64(1) {
				t.Errorf("%s in the trash reads %v", each["key"], item)
			}
		}

		got := obj(t, want(t, s.bob.post(t, trashOf(s.key)+"/"+alone["id"].(string)+"/restore", nil), http.StatusOK, "restore one child alone"), "requirement")
		if got["parentId"] != nil || strings.Contains(got["outline"].(string), ".") {
			t.Errorf("the child whose parent is in the trash came back at %v under %v", got["outline"], got["parentId"])
		}
		want(t, s.bob.post(t, trashOf(s.key)+"/"+s.sectionID+"/restore", nil), http.StatusOK, "restore the section")
		stillTrashed("restoring the section", later["id"].(string))
		for _, each := range list(t, want(t, s.bob.get(t, trashOf(s.key)), http.StatusOK, "list the trash again"), "items") {
			if item := each.(map[string]any); item["key"] == later["key"] && item["parentInTree"] != true {
				t.Errorf("with its section back, %s reads %v", later["key"], item)
			}
		}
	})

	t.Run("an archived project's trash is neither restored nor emptied", func(t *testing.T) {
		want(t, s.bob.delete(t, "/api/v1/requirements/"+s.req("1")), http.StatusNoContent, "trash R1")
		s.archive(t, s.key)
		refuse(t, "a restore in an archived project", s.owner.post(t, trashOf(s.key)+"/"+s.reqID+"/restore", nil), http.StatusConflict, "project_archived")
		refuse(t, "a purge in an archived project", s.owner.delete(t, trashOf(s.key)+"/"+s.reqID), http.StatusConflict, "project_archived")
		refuse(t, "emptying an archived project's trash", s.owner.delete(t, trashOf(s.key)), http.StatusConflict, "project_archived")
		stillTrashed("the archived project's trash", s.reqID)
	})
}

// A diagram changes nothing, yet drawing one is a POST, which a token that only reads or only reaches some
// projects is refused before any handler (#309).
func TestATokenThatMayNotDrawADiagramIsRefused(t *testing.T) {
	f := newProjectFixture(t)
	key := projectKey(t, "RDG")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	user := f.plain.user
	draw := map[string]any{"language": "plantuml", "source": pumpDiagram}
	for name, body := range map[string]map[string]any{
		"read_only_token": {"name": "reads", "scopes": []string{"read"}},
		"projects_token":  {"name": "limited", "projects": []string{key}},
		"evidence_token":  {"name": "evidence", "scopes": []string{"evidence"}},
	} {
		_, secret := makeToken(t, f.plain, body)
		refuse(t, "a "+name+" draws a diagram", f.api.withToken(user, secret).post(t, "/api/v1/diagrams", draw), http.StatusForbidden, name)
	}
}

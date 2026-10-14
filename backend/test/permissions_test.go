//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func subjectOf(kind string, id uuid.UUID) map[string]any {
	return map[string]any{"type": kind, "id": id}
}

func grantRow(subject map[string]any, role string) map[string]any {
	return map[string]any{"subject": subject, "role": role}
}

// rolesOf reads a grants answer as "type:name=role" rows, in the order given.
func rolesOf(t *testing.T, r response) []string {
	t.Helper()
	var out []string
	for _, each := range list(t, r, "grants") {
		g := each.(map[string]any)
		sub := g["subject"].(map[string]any)
		out = append(out, sub["type"].(string)+":"+sub["name"].(string)+"="+g["role"].(string))
	}
	return out
}

// A project's administrators decide who holds which role in it, roles add up, a guest is let in only by name
// or group, and the inspector says why somebody may or may not do something, as the database answers it.
func TestProjectGrantsOverTheAPI(t *testing.T) {
	graKey := projectKey(t, "GRA")
	f := newProjectFixture(t)
	ownerName := "Olive Owner"
	if _, err := f.h.super.Exec(t.Context(), `UPDATE app_user SET name = $2 WHERE id = $1`, f.home.user, ownerName); err != nil {
		t.Fatal(err)
	}
	plainID, guestID := f.h.namedPerson(t, f.home.org, "Paul Plain"), f.h.addPerson(t, f.home.org, "guest")
	plain, guest := f.api.as(t, plainID, f.home.org), f.api.as(t, guestID, f.home.org)
	reviewers := f.h.makeGroup(t, f.home.org, "Reviewers", plainID)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": graKey, "name": "Granted"}), http.StatusCreated, "make a project")
	grants := "/api/v1/projects/" + graKey + "/grants"

	t.Run("a new project is its maker's to run and everybody's to work in", func(t *testing.T) {
		sameList(t, "the first grants", rolesOf(t, want(t, f.owner.get(t, grants), http.StatusOK, "list grants")),
			"everyone:Everyone=editor", "user:"+ownerName+"=admin")
		refuse(t, "an editor reads the grants", plain.get(t, grants), http.StatusForbidden, "forbidden")
		refuse(t, "a guest reads the grants", guest.get(t, grants), http.StatusNotFound, "not_found")
		refuse(t, "somebody no grant names reads the grants", f.outside.get(t, grants), http.StatusNotFound, "not_found")
	})

	t.Run("the grants are replaced as a whole, and roles add up", func(t *testing.T) {
		got := want(t, f.owner.put(t, grants, map[string]any{"grants": []any{
			grantRow(subjectOf("user", f.home.user), "admin"),
			grantRow(map[string]any{"type": "everyone"}, "viewer"),
			grantRow(subjectOf("group", reviewers), "reviewer"),
			grantRow(subjectOf("user", plainID), "editor"),
			grantRow(subjectOf("user", guestID), "editor"),
		}}), http.StatusOK, "replace the grants")
		if rows := rolesOf(t, got); len(rows) != 5 || rows[0] != "everyone:Everyone=viewer" || rows[1][:6] != "group:" {
			t.Errorf("the answer lists everyone, then groups, then people: %v", rows)
		}
		can := obj(t, want(t, plain.get(t, "/api/v1/projects/"+graKey), http.StatusOK, "read as the editor"), "project", "can")
		if can["edit"] != true || can["approve"] != true || can["administer"] != false {
			t.Errorf("an editor on their own and a reviewer through a group edits and approves: %v", can)
		}
		can = obj(t, want(t, guest.get(t, "/api/v1/projects/"+graKey), http.StatusOK, "read as the guest"), "project", "can")
		if can["edit"] != true || can["approve"] != false {
			t.Errorf("the guest is an editor by name: %v", can)
		}
	})

	t.Run("a table nobody may hold is refused", func(t *testing.T) {
		owner := grantRow(subjectOf("user", f.home.user), "admin")
		for what, rows := range map[string][]any{
			"admin for everyone":  {owner, grantRow(map[string]any{"type": "everyone"}, "admin")},
			"admin for a guest":   {owner, grantRow(subjectOf("user", guestID), "admin")},
			"a role that is none": {grantRow(subjectOf("user", f.home.user), "boss")},
			"nobody at all":       {owner, map[string]any{"subject": map[string]any{"type": "user"}, "role": "viewer"}},
			"a kind of subject":   {owner, grantRow(map[string]any{"type": "robot"}, "viewer")},
			"somebody twice":      {owner, grantRow(subjectOf("user", f.home.user), "viewer")},
		} {
			refuse(t, what, f.owner.put(t, grants, map[string]any{"grants": rows}), http.StatusUnprocessableEntity, "validation_failed")
		}
		refuse(t, "no administrator", f.owner.put(t, grants, map[string]any{"grants": []any{
			grantRow(map[string]any{"type": "everyone"}, "editor")}}), http.StatusConflict, "no_administrator")
		refuse(t, "an editor replaces the grants", plain.put(t, grants, map[string]any{"grants": []any{
			grantRow(subjectOf("user", plainID), "admin")}}), http.StatusForbidden, "forbidden")
	})

	t.Run("the inspector says what the database answers, and why", func(t *testing.T) {
		report := obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+graKey+"/access/"+plainID.String()), http.StatusOK, "inspect the editor"), "access")
		can := obj(t, want(t, plain.get(t, "/api/v1/projects/"+graKey), http.StatusOK, "read as the editor"), "project", "can")
		for _, each := range report["rights"].([]any) {
			right := each.(map[string]any)
			if right["allowed"] != can[right["right"].(string)] {
				t.Errorf("the inspector says %s is %v, the project says %v", right["right"], right["allowed"], can[right["right"].(string)])
			}
		}
		guestReport := obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+graKey+"/access/"+guestID.String()), http.StatusOK, "inspect the guest"), "access")
		approve := guestReport["rights"].([]any)[3].(map[string]any)
		if approve["right"] != "approve" || approve["allowed"] != false {
			t.Errorf("the guest does not approve: %v", approve)
		}
		refuse(t, "somebody who is nobody", f.owner.get(t, "/api/v1/projects/"+graKey+"/access/"+uuid.NewString()), http.StatusNotFound, "not_found")
		refuse(t, "an editor inspects", plain.get(t, "/api/v1/projects/"+graKey+"/access/"+guestID.String()), http.StatusForbidden, "forbidden")
		refuse(t, "an id that is none", f.owner.get(t, "/api/v1/projects/"+graKey+"/access/nobody"), http.StatusBadRequest, "bad_request")
	})

	t.Run("the pickers and the caller's own rights", func(t *testing.T) {
		// Other tests' people and groups share the lists, so what counts is that this test's are found as made.
		if found := withID(list(t, want(t, f.owner.get(t, "/api/v1/people?q=plain&limit=50"), http.StatusOK, "pick a person"), "people"), plainID); found == nil || found["name"] != "Paul Plain" {
			t.Errorf("a word of the name does not find Paul Plain")
		}
		if found := withID(list(t, want(t, plain.get(t, "/api/v1/groups?q=rev&limit=50"), http.StatusOK, "pick a group"), "groups"), reviewers); found == nil || found["memberCount"] != float64(1) {
			t.Errorf("Reviewers is not found with its one member: %v", found)
		}
		refuse(t, "too many people", f.owner.get(t, "/api/v1/people?limit=51"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "no groups at all", f.owner.get(t, "/api/v1/groups?limit=0"), http.StatusUnprocessableEntity, "validation_failed")
		if mine := obj(t, want(t, f.owner.get(t, "/api/v1/access/me"), http.StatusOK, "the owner's rights"), "can"); mine["createProject"] != true {
			t.Errorf("the owner makes projects: %v", mine)
		}
		if mine := obj(t, want(t, plain.get(t, "/api/v1/access/me"), http.StatusOK, "a member's rights"), "can"); mine["createProject"] != false || mine["administer"] != false {
			t.Errorf("a member neither makes projects nor administers: %v", mine)
		}
		refuse(t, "nobody asks", f.api.anonymous().get(t, "/api/v1/access/me"), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("an archived project's grants stay, and the change is audited", func(t *testing.T) {
		want(t, f.owner.put(t, "/api/v1/projects/"+graKey+"/archive", nil), http.StatusOK, "archive")
		refuse(t, "grants of an archived project", f.owner.put(t, grants, map[string]any{"grants": []any{
			grantRow(subjectOf("user", f.home.user), "admin")}}), http.StatusConflict, "project_archived")
		if n := f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action = 'project.grants_changed'`, f.home.user); n != 1 {
			t.Errorf("the log holds %d grant changes, want 1", n)
		}
	})
}

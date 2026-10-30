//go:build integration

package test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func subjectOf(kind string, id uuid.UUID) map[string]any {
	return map[string]any{"type": kind, "id": id}
}

func grantRow(subject map[string]any, role string) map[string]any {
	return map[string]any{"subject": subject, "role": role}
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
	plainID, guestID := f.h.namedPerson(t, "Paul Plain"), f.h.addPerson(t, "guest")
	plain, guest := f.api.as(t, plainID), f.api.as(t, guestID)
	reviewers := f.h.makeGroup(t, "Reviewers", plainID)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": graKey, "name": "Granted"}), http.StatusCreated, "make a project")
	grants := "/api/v1/role-assignments?project=" + graKey

	t.Run("a new project is its maker's to run and every member's to work in", func(t *testing.T) {
		sameList(t, "the first grants", f.owner.grantsOf(t, graKey), "admin:"+ownerName, "editor:group Everyone")
		refuse(t, "an editor reads the grants", plain.get(t, grants), http.StatusForbidden, "forbidden")
		refuse(t, "a guest reads the grants", guest.get(t, grants), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "somebody no grant names reads the grants", f.outside.get(t, grants), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("grants are given one at a time, and roles add up", func(t *testing.T) {
		want(t, f.owner.setGrants(t, graKey, []any{
			grantRow(subjectOf("user", f.home.user), "admin"),
			grantRow(map[string]any{"type": "everyone"}, "viewer"),
			grantRow(subjectOf("group", reviewers), "reviewer"),
			grantRow(subjectOf("user", plainID), "editor"),
			grantRow(subjectOf("user", guestID), "editor"),
		}), http.StatusOK, "grant the roles")
		rows := list(t, want(t, f.owner.get(t, grants), http.StatusOK, "list the grants"), "assignments")
		if len(rows) != 5 || rows[0].(map[string]any)["role"] != "admin" {
			t.Errorf("the grants list the most powerful role first: %v", rows)
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

	t.Run("a grant nobody may hold is refused", func(t *testing.T) {
		everyone := f.h.everyoneGroup(t)
		for what, body := range map[string]map[string]any{
			"project administrator for Everyone": {"role": "admin", "projectKey": graKey, "groupId": everyone},
			"project administrator for a guest":  {"role": "admin", "projectKey": graKey, "userId": guestID},
			"a role that is none":                {"role": "boss", "projectKey": graKey, "userId": plainID},
			"nobody at all":                      {"role": "viewer", "projectKey": graKey},
			"a person and a group at once":       {"role": "viewer", "projectKey": graKey, "userId": plainID, "groupId": reviewers},
			"somebody who is nobody":             {"role": "viewer", "projectKey": graKey, "userId": uuid.New()},
			"the installation's role in it":      {"role": "administrator", "projectKey": graKey, "userId": plainID},
			"a project nobody sees":              {"role": "viewer", "projectKey": "NOPE", "userId": plainID},
		} {
			refuse(t, what, f.owner.post(t, "/api/v1/role-assignments", body), http.StatusUnprocessableEntity, "validation_failed")
		}
		mine := list(t, want(t, f.owner.get(t, grants+"&role=admin"), http.StatusOK, "the administrators' grants"), "assignments")
		if len(mine) != 1 {
			t.Fatalf("the project has %d administrator grants, want 1", len(mine))
		}
		last := mine[0].(map[string]any)["id"].(string)
		refuse(t, "taking the last administrator grant", f.owner.delete(t, "/api/v1/role-assignments/"+last), http.StatusConflict, "no_administrator")
		refuse(t, "an editor grants themselves the project", plain.post(t, "/api/v1/role-assignments", map[string]any{"role": "admin", "projectKey": graKey, "userId": plainID}),
			http.StatusForbidden, "forbidden")
		refuse(t, "an editor takes a grant", plain.delete(t, "/api/v1/role-assignments/"+last), http.StatusForbidden, "forbidden")
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
		ownerReport := obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+graKey+"/access/"+f.home.user.String()), http.StatusOK, "inspect the administrator"), "access")
		if ownerReport["administers"] != true {
			t.Errorf("the inspector does not say the installation's administrator administers it: %v", ownerReport)
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
		everyone := list(t, want(t, plain.get(t, "/api/v1/groups?limit=50"), http.StatusOK, "the groups"), "groups")[0].(map[string]any)
		if everyone["builtin"] != "everyone" || everyone["memberCount"].(float64) < 2 {
			t.Errorf("the groups do not start with Everyone, holding the members: %v", everyone)
		}
		refuse(t, "too many people", f.owner.get(t, "/api/v1/people?limit=51"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "no groups at all", f.owner.get(t, "/api/v1/groups?limit=0"), http.StatusUnprocessableEntity, "validation_failed")
		if mine := obj(t, want(t, f.owner.get(t, "/api/v1/access/me"), http.StatusOK, "the administrator's rights"), "can"); mine["createProject"] != true || mine["administer"] != true {
			t.Errorf("the administrator makes projects and administers: %v", mine)
		}
		if mine := obj(t, want(t, plain.get(t, "/api/v1/access/me"), http.StatusOK, "a member's rights"), "can"); mine["createProject"] != false || mine["administer"] != false {
			t.Errorf("a member neither makes projects nor administers: %v", mine)
		}
		refuse(t, "nobody asks", f.api.anonymous().get(t, "/api/v1/access/me"), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("an archived project's grants stay, and each change is audited", func(t *testing.T) {
		want(t, f.owner.put(t, "/api/v1/projects/"+graKey+"/archive", nil), http.StatusOK, "archive")
		refuse(t, "granting in an archived project", f.owner.post(t, "/api/v1/role-assignments", map[string]any{"role": "viewer", "projectKey": graKey, "userId": plainID}),
			http.StatusConflict, "project_archived")
		some := list(t, want(t, f.owner.get(t, grants), http.StatusOK, "the archived project's grants"), "assignments")
		refuse(t, "taking a grant of an archived project", f.owner.delete(t, "/api/v1/role-assignments/"+some[len(some)-1].(map[string]any)["id"].(string)),
			http.StatusConflict, "project_archived")
		var added, removed int
		if err := f.h.super.QueryRow(t.Context(), `
			SELECT count(*) FILTER (WHERE action = 'grant.added'), count(*) FILTER (WHERE action = 'grant.removed')
			FROM audit_log WHERE actor_user_id = $1 AND data->>'project' = $2`, f.home.user, graKey).Scan(&added, &removed); err != nil {
			t.Fatal(err)
		}
		if added != 4 || removed != 1 {
			t.Errorf("the log holds %d grants added and %d taken, want the 4 new ones and 1", added, removed)
		}
	})
}

// A renamed role and a role the installation adds reach what the API answers and the buttons it offers at once:
// what somebody may do follows the permissions, never a role's name.
func TestARenamedAndANewRoleReachTheButtons(t *testing.T) {
	key := projectKey(t, "RNR")
	f := newProjectFixture(t)
	f.h.keepRoles(t)
	signerID := f.h.namedPerson(t, "Sid Signer")
	signer := f.api.as(t, signerID)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Renamed"}), http.StatusCreated, "make a project")
	want(t, f.owner.setGrants(t, key, []any{grantRow(subjectOf("user", f.home.user), "admin")}), http.StatusOK, "keep the project to its maker")
	canOf := func() map[string]any {
		t.Helper()
		return obj(t, want(t, signer.get(t, "/api/v1/projects/"+key), http.StatusOK, "read as the signer"), "project", "can")
	}

	t.Run("a renamed built-in role keeps what it holds", func(t *testing.T) {
		renamed := obj(t, want(t, f.owner.patch(t, "/api/v1/roles/reviewer", map[string]any{"name": "Checker"}), http.StatusOK, "rename the reviewer"), "role")
		if renamed["name"] != "Checker" || renamed["role"] != "reviewer" || renamed["builtin"] != true {
			t.Fatalf("the renamed role reads %v", renamed)
		}
		want(t, f.owner.post(t, "/api/v1/role-assignments", map[string]any{"role": "reviewer", "projectKey": key, "userId": signerID}), http.StatusCreated, "grant the checker")
		if can := canOf(); can["approve"] != true || can["edit"] != false {
			t.Errorf("a checker, the reviewer renamed, reads %v", can)
		}
		if rows := f.owner.grantsOf(t, key); !slices.Contains(rows, "reviewer:Sid Signer") {
			t.Errorf("the grant is not listed by its key: %v", rows)
		}
	})

	t.Run("a new role gives what the matrix ticks for it, and follows a change at once", func(t *testing.T) {
		made := obj(t, want(t, f.owner.post(t, "/api/v1/roles", map[string]any{"name": "Release signer", "permissions": []string{"view", "comment", "edit"}}),
			http.StatusCreated, "add a role"), "role")
		if made["role"] != "release_signer" || made["builtin"] != false {
			t.Fatalf("the new role reads %v", made)
		}
		want(t, f.owner.post(t, "/api/v1/role-assignments", map[string]any{"role": "release_signer", "projectKey": key, "userId": signerID}), http.StatusCreated, "grant the new role")
		if can := canOf(); can["edit"] != true || can["administer"] != false {
			t.Errorf("a release signer reads %v, want edit", can)
		}
		want(t, f.owner.patch(t, "/api/v1/roles/release_signer", map[string]any{"permissions": []string{"view", "administer"}}), http.StatusOK, "tick administer")
		if can := canOf(); can["administer"] != true || can["purge"] != true {
			t.Errorf("a release signer who administers reads %v", can)
		}
		want(t, signer.patch(t, "/api/v1/projects/"+key, map[string]any{"name": "Renamed by the signer"}), http.StatusOK, "the signer runs the project")
		want(t, f.owner.delete(t, "/api/v1/roles/release_signer"), http.StatusNoContent, "delete the new role")
		if can := canOf(); can["administer"] != false || can["approve"] != true {
			t.Errorf("deleting the role did not take what it gave: %v", can)
		}
	})
}

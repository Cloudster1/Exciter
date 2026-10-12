//go:build integration

package test

import (
	"net/http"
	"testing"
)

// projectFixture is an organization with its owner, a plain member, a guest and somebody from elsewhere.
type projectFixture struct {
	h                            *harness
	api                          *apiServer
	home, away                   member
	owner, plain, guest, outside *client
}

func newProjectFixture(t *testing.T) *projectFixture {
	t.Helper()
	h := newHarness(t)
	api := newAPIServer(t, h)
	f := &projectFixture{h: h, api: api, home: h.makeMember(t, "projects"), away: h.makeMember(t, "projects-away")}
	f.owner = api.as(t, f.home.user, f.home.org)
	f.plain = api.as(t, h.addPerson(t, f.home.org, "member"), f.home.org)
	f.guest = api.as(t, h.addPerson(t, f.home.org, "guest"), f.home.org)
	f.outside = api.as(t, f.away.user, f.away.org)
	return f
}

func keysOf(t *testing.T, r response) []string {
	t.Helper()
	var keys []string
	for _, each := range list(t, r, "projects") {
		keys = append(keys, each.(map[string]any)["key"].(string))
	}
	return keys
}

// A project is made by the organization's administrators, read by its members, kept from guests and from
// every other organization, and refuses what only its administrators may do to anybody else.
func TestProjectsOverTheAPI(t *testing.T) {
	f := newProjectFixture(t)

	made := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{
		"key": " chk ", "name": " Checkout ", "description": "Paying for the basket.",
	}), http.StatusCreated, "make a project"), "project")
	if made["key"] != "CHK" || made["name"] != "Checkout" || made["archivedAt"] != nil {
		t.Fatalf("the key is normalized and the name trimmed: %v", made)
	}
	if can := made["can"].(map[string]any); can["administer"] != true || can["purge"] != true {
		t.Errorf("the organization's owner administers a new project: %v", can)
	}

	t.Run("only the organization's administrators make one", func(t *testing.T) {
		refuse(t, "a member makes a project", f.plain.post(t, "/api/v1/projects", map[string]any{"key": "MINE", "name": "Mine"}), http.StatusForbidden, "forbidden")
		refuse(t, "a duplicate key", f.owner.post(t, "/api/v1/projects", map[string]any{"key": "chk", "name": "Again"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a key of the wrong shape", f.owner.post(t, "/api/v1/projects", map[string]any{"key": "2X", "name": "Bad"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a template that is not one", f.owner.post(t, "/api/v1/projects", map[string]any{"key": "TPL", "name": "T", "template": "nope"}), http.StatusUnprocessableEntity, "validation_failed")
		want(t, f.outside.post(t, "/api/v1/projects", map[string]any{"key": "CHK", "name": "Theirs"}), http.StatusCreated, "another organization may use the same key")
	})

	t.Run("members read projects, guests and other organizations do not", func(t *testing.T) {
		sameList(t, "a member's projects", keysOf(t, want(t, f.plain.get(t, "/api/v1/projects"), http.StatusOK, "list as a member")), "CHK")
		if got := keysOf(t, want(t, f.guest.get(t, "/api/v1/projects"), http.StatusOK, "list as a guest")); len(got) != 0 {
			t.Errorf("a guest sees projects without a grant: %v", got)
		}
		refuse(t, "a guest reads a project", f.guest.get(t, "/api/v1/projects/CHK"), http.StatusNotFound, "not_found")
		refuse(t, "a member changes a project", f.plain.patch(t, "/api/v1/projects/CHK", map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
		refuse(t, "a member archives a project", f.plain.put(t, "/api/v1/projects/CHK/archive", nil), http.StatusForbidden, "forbidden")
		refuse(t, "an odd archived flag", f.plain.get(t, "/api/v1/projects?archived=maybe"), http.StatusUnprocessableEntity, "validation_failed")
		got := obj(t, want(t, f.plain.get(t, "/api/v1/projects/chk"), http.StatusOK, "read as a member"), "project")
		if can := got["can"].(map[string]any); can["edit"] != true || can["administer"] != false {
			t.Errorf("a member edits and does not administer: %v", can)
		}
	})

	t.Run("a key is suggested, checked and free only once", func(t *testing.T) {
		check := obj(t, want(t, f.plain.get(t, "/api/v1/projects/key-check?name=Checkout%20service"), http.StatusOK, "suggest a key"), "check")
		if check["suggestion"] != "CS" {
			t.Errorf("the suggestion for Checkout service = %v", check["suggestion"])
		}
		check = obj(t, want(t, f.plain.get(t, "/api/v1/projects/key-check?name=chk"), http.StatusOK, "suggest past a taken key"), "check")
		if check["suggestion"] != "CHK2" {
			t.Errorf("the suggestion past CHK = %v", check["suggestion"])
		}
		check = obj(t, want(t, f.plain.get(t, "/api/v1/projects/key-check?key=chk"), http.StatusOK, "check a taken key"), "check")
		if check["valid"] != true || check["available"] != false || check["key"] != "CHK" {
			t.Errorf("CHK is valid and taken: %v", check)
		}
		refuse(t, "nobody checks a key", f.api.anonymous().get(t, "/api/v1/projects/key-check?key=X"), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("a new key keeps the old one resolving and taken", func(t *testing.T) {
		moved := obj(t, want(t, f.owner.put(t, "/api/v1/projects/CHK/key", map[string]any{"key": "pay"}), http.StatusOK, "change the key"), "project")
		if moved["key"] != "PAY" || len(moved["formerKeys"].([]any)) != 1 || moved["formerKeys"].([]any)[0] != "CHK" {
			t.Fatalf("PAY was CHK: %v", moved)
		}
		if got := obj(t, want(t, f.plain.get(t, "/api/v1/projects/CHK"), http.StatusOK, "read by the old key"), "project"); got["key"] != "PAY" {
			t.Errorf("the old key answers with the current one: %v", got["key"])
		}
		refuse(t, "the old key for a new project", f.owner.post(t, "/api/v1/projects", map[string]any{"key": "CHK", "name": "Squatter"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a key of the wrong shape", f.owner.put(t, "/api/v1/projects/PAY/key", map[string]any{"key": "p"}), http.StatusUnprocessableEntity, "validation_failed")
		back := obj(t, want(t, f.owner.put(t, "/api/v1/projects/PAY/key", map[string]any{"key": "CHK"}), http.StatusOK, "take the old key back"), "project")
		if back["key"] != "CHK" || len(back["formerKeys"].([]any)) != 1 || back["formerKeys"].([]any)[0] != "PAY" {
			t.Errorf("taking CHK back leaves PAY as the alias: %v", back)
		}
	})

	t.Run("settings and details change for administrators", func(t *testing.T) {
		got := obj(t, want(t, f.owner.patch(t, "/api/v1/projects/CHK", map[string]any{
			"name": "Checkout and payment", "settings": map[string]any{"requireRevisionComment": true},
		}), http.StatusOK, "change a project"), "project")
		if got["name"] != "Checkout and payment" || got["settings"].(map[string]any)["requireRevisionComment"] != true {
			t.Errorf("the change did not stick: %v", got)
		}
		refuse(t, "a blank name", f.owner.patch(t, "/api/v1/projects/CHK", map[string]any{"name": " "}), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("an archived project is read only and goes only once archived", func(t *testing.T) {
		refuse(t, "deleting before archiving", f.owner.delete(t, "/api/v1/projects/CHK"), http.StatusConflict, "project_not_archived")
		archived := obj(t, want(t, f.owner.put(t, "/api/v1/projects/CHK/archive", nil), http.StatusOK, "archive"), "project")
		if archived["archivedAt"] == nil {
			t.Fatal("the project was not archived")
		}
		if can := archived["can"].(map[string]any); can["edit"] != false || can["administer"] != true {
			t.Errorf("an archived project is read only but still administered: %v", can)
		}
		if got := keysOf(t, want(t, f.plain.get(t, "/api/v1/projects"), http.StatusOK, "list without archived")); len(got) != 0 {
			t.Errorf("an archived project is listed: %v", got)
		}
		sameList(t, "with archived", keysOf(t, want(t, f.plain.get(t, "/api/v1/projects?archived=true"), http.StatusOK, "list with archived")), "CHK")
		refuse(t, "renaming an archived project", f.owner.patch(t, "/api/v1/projects/CHK", map[string]any{"name": "Again"}), http.StatusConflict, "project_archived")
		refuse(t, "a member unarchives", f.plain.delete(t, "/api/v1/projects/CHK/archive"), http.StatusForbidden, "forbidden")
		want(t, f.owner.delete(t, "/api/v1/projects/CHK/archive"), http.StatusOK, "unarchive")
		want(t, f.owner.put(t, "/api/v1/projects/CHK/archive", nil), http.StatusOK, "archive again")
		want(t, f.owner.delete(t, "/api/v1/projects/CHK"), http.StatusNoContent, "delete")
		refuse(t, "a deleted project", f.owner.get(t, "/api/v1/projects/CHK"), http.StatusNotFound, "not_found")
		refuse(t, "its old key", f.owner.get(t, "/api/v1/projects/PAY"), http.StatusNotFound, "not_found")
		want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": "PAY", "name": "Payments"}), http.StatusCreated, "a deleted project's keys are free")
	})

	t.Run("the audit log names each act", func(t *testing.T) {
		got := f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action LIKE 'project.%'`, f.home.org)
		// created twice (CHK, PAY), the key changed twice, archived twice, unarchived once, deleted once
		if got != 8 {
			t.Errorf("the log holds %d project entries, want 8", got)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic IN ('project.key_changed', 'project.deleted')`, f.home.org); n != 3 {
			t.Errorf("the outbox holds %d project events, want 3", n)
		}
	})
}

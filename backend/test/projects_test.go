//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
)

// projectFixture is an owner of the installation, a plain member, a guest, and an outsider: a guest no grant
// names, who reaches nothing, as somebody from another installation would.
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
	f := &projectFixture{h: h, api: api, home: h.owner(t, "projects")}
	f.away = member{user: h.addPerson(t, "guest")}
	f.owner = api.as(t, f.home.user)
	f.plain = api.as(t, h.addPerson(t, "member"))
	f.guest = api.as(t, h.addPerson(t, "guest"))
	f.outside = api.as(t, f.away.user)
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

// A project is made by the installation's administrators, read by its members, kept from guests, and refuses
// what only its administrators may do to anybody else.
func TestProjectsOverTheAPI(t *testing.T) {
	chkKey := projectKey(t, "CHK")
	mineKey := projectKey(t, "MINE")
	tplKey := projectKey(t, "TPL")
	payKey := projectKey(t, "PAY")
	f := newProjectFixture(t)

	made := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{
		"key": " " + strings.ToLower(chkKey) + " ", "name": " Checkout ", "description": "Paying for the basket.",
	}), http.StatusCreated, "make a project"), "project")
	if made["key"] != chkKey || made["name"] != "Checkout" || made["archivedAt"] != nil {
		t.Fatalf("the key is normalized and the name trimmed: %v", made)
	}
	if can := made["can"].(map[string]any); can["administer"] != true || can["purge"] != true {
		t.Errorf("an owner of the installation administers a new project: %v", can)
	}

	t.Run("only the administrators make one", func(t *testing.T) {
		refuse(t, "a member makes a project", f.plain.post(t, "/api/v1/projects", map[string]any{"key": mineKey, "name": "Mine"}), http.StatusForbidden, "forbidden")
		refuse(t, "a duplicate key", f.owner.post(t, "/api/v1/projects", map[string]any{"key": strings.ToLower(chkKey), "name": "Again"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a key of the wrong shape", f.owner.post(t, "/api/v1/projects", map[string]any{"key": "2X", "name": "Bad"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a template that is not one", f.owner.post(t, "/api/v1/projects", map[string]any{"key": tplKey, "name": "T", "template": "nope"}), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("members read projects, guests do not", func(t *testing.T) {
		sameList(t, "a member's projects", ours(t, keysOf(t, want(t, f.plain.get(t, "/api/v1/projects"), http.StatusOK, "list as a member"))), chkKey)
		if got := keysOf(t, want(t, f.guest.get(t, "/api/v1/projects"), http.StatusOK, "list as a guest")); len(got) != 0 {
			t.Errorf("a guest sees projects without a grant: %v", got)
		}
		refuse(t, "a guest reads a project", f.guest.get(t, "/api/v1/projects/"+chkKey), http.StatusNotFound, "not_found")
		refuse(t, "a member changes a project", f.plain.patch(t, "/api/v1/projects/"+chkKey, map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
		refuse(t, "a member archives a project", f.plain.put(t, "/api/v1/projects/"+chkKey+"/archive", nil), http.StatusForbidden, "forbidden")
		refuse(t, "an odd archived flag", f.plain.get(t, "/api/v1/projects?archived=maybe"), http.StatusUnprocessableEntity, "validation_failed")
		got := obj(t, want(t, f.plain.get(t, "/api/v1/projects/"+strings.ToLower(chkKey)), http.StatusOK, "read as a member"), "project")
		if can := got["can"].(map[string]any); can["edit"] != true || can["administer"] != false {
			t.Errorf("a member edits and does not administer: %v", can)
		}
	})

	t.Run("a key is suggested, checked and free only once", func(t *testing.T) {
		check := obj(t, want(t, f.plain.get(t, "/api/v1/projects/key-check?name=Checkout%20service"), http.StatusOK, "suggest a key"), "check")
		if check["suggestion"] != "CS" {
			t.Errorf("the suggestion for Checkout service = %v", check["suggestion"])
		}
		check = obj(t, want(t, f.plain.get(t, "/api/v1/projects/key-check?name="+chkKey), http.StatusOK, "suggest past a taken key"), "check")
		if check["suggestion"] != chkKey+"2" {
			t.Errorf("the suggestion past "+chkKey+" = %v", check["suggestion"])
		}
		check = obj(t, want(t, f.plain.get(t, "/api/v1/projects/key-check?key="+strings.ToLower(chkKey)), http.StatusOK, "check a taken key"), "check")
		if check["valid"] != true || check["available"] != false || check["key"] != chkKey {
			t.Errorf(chkKey+" is valid and taken: %v", check)
		}
		refuse(t, "nobody checks a key", f.api.anonymous().get(t, "/api/v1/projects/key-check?key=X"), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("a new key keeps the old one resolving and taken", func(t *testing.T) {
		moved := obj(t, want(t, f.owner.put(t, "/api/v1/projects/"+chkKey+"/key", map[string]any{"key": strings.ToLower(payKey)}), http.StatusOK, "change the key"), "project")
		if moved["key"] != payKey || len(moved["formerKeys"].([]any)) != 1 || moved["formerKeys"].([]any)[0] != chkKey {
			t.Fatalf(payKey+" was "+chkKey+": %v", moved)
		}
		if got := obj(t, want(t, f.plain.get(t, "/api/v1/projects/"+chkKey), http.StatusOK, "read by the old key"), "project"); got["key"] != payKey {
			t.Errorf("the old key answers with the current one: %v", got["key"])
		}
		refuse(t, "the old key for a new project", f.owner.post(t, "/api/v1/projects", map[string]any{"key": chkKey, "name": "Squatter"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a key of the wrong shape", f.owner.put(t, "/api/v1/projects/"+payKey+"/key", map[string]any{"key": "p"}), http.StatusUnprocessableEntity, "validation_failed")
		back := obj(t, want(t, f.owner.put(t, "/api/v1/projects/"+payKey+"/key", map[string]any{"key": chkKey}), http.StatusOK, "take the old key back"), "project")
		if back["key"] != chkKey || len(back["formerKeys"].([]any)) != 1 || back["formerKeys"].([]any)[0] != payKey {
			t.Errorf("taking "+chkKey+" back leaves "+payKey+" as the alias: %v", back)
		}
	})

	t.Run("settings and details change for administrators", func(t *testing.T) {
		got := obj(t, want(t, f.owner.patch(t, "/api/v1/projects/"+chkKey, map[string]any{
			"name": "Checkout and payment", "settings": map[string]any{"requireRevisionComment": true},
		}), http.StatusOK, "change a project"), "project")
		if got["name"] != "Checkout and payment" || got["settings"].(map[string]any)["requireRevisionComment"] != true {
			t.Errorf("the change did not stick: %v", got)
		}
		refuse(t, "a blank name", f.owner.patch(t, "/api/v1/projects/"+chkKey, map[string]any{"name": " "}), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("an archived project is read only and goes only once archived", func(t *testing.T) {
		refuse(t, "deleting before archiving", f.owner.delete(t, "/api/v1/projects/"+chkKey), http.StatusConflict, "project_not_archived")
		archived := obj(t, want(t, f.owner.put(t, "/api/v1/projects/"+chkKey+"/archive", nil), http.StatusOK, "archive"), "project")
		if archived["archivedAt"] == nil {
			t.Fatal("the project was not archived")
		}
		if can := archived["can"].(map[string]any); can["edit"] != false || can["administer"] != true {
			t.Errorf("an archived project is read only but still administered: %v", can)
		}
		if got := ours(t, keysOf(t, want(t, f.plain.get(t, "/api/v1/projects"), http.StatusOK, "list without archived"))); len(got) != 0 {
			t.Errorf("an archived project is listed: %v", got)
		}
		sameList(t, "with archived", ours(t, keysOf(t, want(t, f.plain.get(t, "/api/v1/projects?archived=true"), http.StatusOK, "list with archived"))), chkKey)
		refuse(t, "renaming an archived project", f.owner.patch(t, "/api/v1/projects/"+chkKey, map[string]any{"name": "Again"}), http.StatusConflict, "project_archived")
		refuse(t, "a member unarchives", f.plain.delete(t, "/api/v1/projects/"+chkKey+"/archive"), http.StatusForbidden, "forbidden")
		want(t, f.owner.delete(t, "/api/v1/projects/"+chkKey+"/archive"), http.StatusOK, "unarchive")
		want(t, f.owner.put(t, "/api/v1/projects/"+chkKey+"/archive", nil), http.StatusOK, "archive again")
		want(t, f.owner.delete(t, "/api/v1/projects/"+chkKey), http.StatusNoContent, "delete")
		refuse(t, "a deleted project", f.owner.get(t, "/api/v1/projects/"+chkKey), http.StatusNotFound, "not_found")
		refuse(t, "its old key", f.owner.get(t, "/api/v1/projects/"+payKey), http.StatusNotFound, "not_found")
		want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": payKey, "name": "Payments"}), http.StatusCreated, "a deleted project's keys are free")
	})

	t.Run("the audit log names each act", func(t *testing.T) {
		got := f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action LIKE 'project.%'`, f.home.user)
		// created twice (CHK, PAY), the key changed twice, archived twice, unarchived once, deleted once
		if got != 8 {
			t.Errorf("the log holds %d project entries, want 8", got)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM outbox_event WHERE payload->>'actorId' = $1 AND topic IN ('project.key_changed', 'project.deleted')`, f.home.user.String()); n != 3 {
			t.Errorf("the outbox holds %d project events, want 3", n)
		}
	})
}

//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// linkTypeID finds a link type by its key in a list of them.
func linkTypeID(t *testing.T, r response, key string) string {
	t.Helper()
	for _, each := range list(t, r, "linkTypes") {
		if lt := each.(map[string]any); lt["key"] == key {
			return lt["id"].(string)
		}
	}
	t.Fatalf("no link type %s in %s", key, r.Raw)
	return ""
}

// The organization's administrators shape its link types and a project's administrators the project's own;
// a project disables a type or limits it to pairs of requirement types, and a type with links is retired.
func TestLinkTypesOverTheAPI(t *testing.T) {
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": "LTY", "name": "Typed links"}), http.StatusCreated, "make a project")
	editor := f.plain

	orgTypes := want(t, editor.get(t, "/api/v1/link-types"), http.StatusOK, "list the organization's types")
	if n := len(list(t, orgTypes, "linkTypes")); n != 6 {
		t.Errorf("the organization starts with %d link types, want 6", n)
	}
	refuse(t, "nobody signed in lists them", f.api.anonymous().get(t, "/api/v1/link-types"), http.StatusUnauthorized, "unauthorized")

	made := obj(t, want(t, f.owner.post(t, "/api/v1/link-types", map[string]any{
		"name": "Mitigates", "forward": "mitigates", "backward": "is mitigated by", "acyclic": true, "suspectEnd": "target",
	}), http.StatusCreated, "make an organization's type"), "linkType")
	if made["key"] != "mitigates" || made["scope"] != "org" || made["acyclic"] != true {
		t.Errorf("the type is keyed from its name and the organization's: %v", made)
	}
	mitigates := made["id"].(string)
	refuse(t, "a member makes one", editor.post(t, "/api/v1/link-types", map[string]any{"name": "Mine", "forward": "a", "backward": "b"}),
		http.StatusForbidden, "forbidden")
	refuse(t, "a key taken", f.owner.post(t, "/api/v1/link-types", map[string]any{"key": "derives", "name": "Again", "forward": "a", "backward": "b"}),
		http.StatusUnprocessableEntity, "validation_failed")

	renamed := obj(t, want(t, f.owner.patch(t, "/api/v1/link-types/"+mitigates, map[string]any{"name": "Mitigates risk"}), http.StatusOK, "rename it"), "linkType")
	if renamed["name"] != "Mitigates risk" || renamed["key"] != "mitigates" {
		t.Errorf("a rename keeps the key: %v", renamed)
	}
	refuse(t, "a member changes one", editor.patch(t, "/api/v1/link-types/"+mitigates, map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
	refuse(t, "a new key", f.owner.patch(t, "/api/v1/link-types/"+mitigates, map[string]any{"key": "lessens"}), http.StatusUnprocessableEntity, "validation_failed")
	refuse(t, "a member deletes one", editor.delete(t, "/api/v1/link-types/"+mitigates), http.StatusForbidden, "forbidden")
	want(t, f.owner.delete(t, "/api/v1/link-types/"+mitigates), http.StatusNoContent, "delete a type no link had")
	refuse(t, "it is gone", f.owner.delete(t, "/api/v1/link-types/"+mitigates), http.StatusNotFound, "not_found")

	own := obj(t, want(t, f.owner.post(t, "/api/v1/projects/LTY/link-types", map[string]any{
		"key": "tests", "name": "Tests", "forward": "tests", "backward": "is tested by", "counts": "verification",
	}), http.StatusCreated, "make a project's type"), "linkType")
	if own["scope"] != "project" || own["projectKey"] != "LTY" || own["enabled"] != true {
		t.Errorf("the project's own type: %v", own)
	}
	refuse(t, "an editor makes one", editor.post(t, "/api/v1/projects/LTY/link-types", map[string]any{"name": "Ours", "forward": "a", "backward": "b"}),
		http.StatusForbidden, "forbidden")
	refuse(t, "the key of the organization's", f.owner.post(t, "/api/v1/projects/LTY/link-types", map[string]any{"key": "verifies", "name": "Checks", "forward": "a", "backward": "b"}),
		http.StatusUnprocessableEntity, "validation_failed")
	want(t, f.owner.patch(t, "/api/v1/link-types/"+own["id"].(string), map[string]any{"backward": "is tested by test"}), http.StatusOK,
		"a project's administrator changes the project's own")

	types := want(t, editor.get(t, "/api/v1/projects/LTY/link-types"), http.StatusOK, "list the project's types")
	if n := len(list(t, types, "linkTypes")); n != 7 {
		t.Errorf("the project may use %d link types, want the six and its own", n)
	}
	refuse(t, "a guest lists them", f.guest.get(t, "/api/v1/projects/LTY/link-types"), http.StatusNotFound, "not_found")
	verifies := linkTypeID(t, types, "verifies")
	typeID := obj(t, want(t, f.owner.post(t, "/api/v1/projects/LTY/types", map[string]any{"name": "Test", "icon": "test", "colour": "#22aa55"}),
		http.StatusCreated, "make a requirement type"), "type")["id"].(string)
	set := obj(t, want(t, f.owner.put(t, "/api/v1/projects/LTY/link-types/"+verifies, map[string]any{
		"enabled": true, "rules": []map[string]any{{"sourceTypeId": typeID, "targetTypeId": nil}, {"sourceTypeId": typeID, "targetTypeId": nil}},
	}), http.StatusOK, "limit verifies to tests"), "linkType")
	if rules := set["rules"].([]any); len(rules) != 1 {
		t.Errorf("a repeated rule is kept once: %v", rules)
	}
	refuse(t, "an editor limits a type", editor.put(t, "/api/v1/projects/LTY/link-types/"+verifies, map[string]any{"enabled": false}),
		http.StatusForbidden, "forbidden")
	refuse(t, "a rule naming no type of the project", f.owner.put(t, "/api/v1/projects/LTY/link-types/"+verifies, map[string]any{
		"enabled": true, "rules": []map[string]any{{"sourceTypeId": uuid.NewString()}},
	}), http.StatusUnprocessableEntity, "validation_failed")
	want(t, f.owner.put(t, "/api/v1/projects/LTY/link-types/"+linkTypeID(t, types, "conflicts"), map[string]any{"enabled": false}), http.StatusOK,
		"disable conflicts")
	for _, title := range []string{"Need", "Spec"} {
		want(t, editor.post(t, "/api/v1/projects/LTY/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	refused := refuse422(t, "verifies between two requirements of the default type",
		editor.post(t, "/api/v1/requirements/LTY-R1/links", map[string]any{"typeKey": "verifies", "otherKey": "LTY-R2"}), "typeId")
	if !strings.HasPrefix(refused, "In LTY, Verifies links do not join a Requirement to a Requirement.") {
		t.Errorf("the rule's refusal: %q", refused)
	}
	refuse422(t, "a disabled type", editor.post(t, "/api/v1/requirements/LTY-R1/links", map[string]any{"typeKey": "conflicts", "otherKey": "LTY-R2"}), "typeId")

	derives := linkTypeID(t, types, "derives")
	want(t, editor.post(t, "/api/v1/requirements/LTY-R1/links", map[string]any{"typeId": derives, "otherKey": "LTY-R2"}), http.StatusCreated, "link them")
	refuse(t, "a used type is deleted", f.owner.delete(t, "/api/v1/link-types/"+derives), http.StatusConflict, "in_use")
	want(t, f.owner.patch(t, "/api/v1/link-types/"+derives, map[string]any{"retired": true}), http.StatusOK, "retire it")
	refuse422(t, "a retired type", editor.post(t, "/api/v1/requirements/LTY-R2/links", map[string]any{"typeId": derives, "otherKey": "LTY-R1"}), "typeId")
}

// refuse422 checks a refusal on one field and answers its sentence.
func refuse422(t *testing.T, what string, got response, field string) string {
	t.Helper()
	refuse(t, what, got, http.StatusUnprocessableEntity, "validation_failed")
	if got.Status != http.StatusUnprocessableEntity {
		return ""
	}
	msg, _ := obj(t, got, "error", "fields")[field].(string)
	if msg == "" || !strings.HasSuffix(msg, ".") {
		t.Errorf("%s: the refusal is no sentence on %s: %s", what, field, got.Raw)
	}
	return msg
}

// A link is made by who edits one end and views the other, read from either end, refused when it joins a
// requirement to itself, a section, or round a circle, logged on both ends, removed from either, and copied.
func TestTraceLinksOverTheAPI(t *testing.T) {
	f := newProjectFixture(t)
	project := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": "LNK", "name": "Linked"}), http.StatusCreated, "make a project"), "project")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": "SYS", "name": "System"}), http.StatusCreated, "make a second project")
	viewerID := f.h.addPerson(t, f.home.org, "guest")
	f.h.grantAs(t, f.home.org, uuid.MustParse(project["id"].(string)), viewerID, "viewer")
	viewer, editor := f.api.as(t, viewerID, f.home.org), f.plain
	for _, title := range []string{"Pay", "Refund", "Receipt"} {
		want(t, editor.post(t, "/api/v1/projects/LNK/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	want(t, editor.post(t, "/api/v1/projects/LNK/requirements", map[string]any{"title": "Money", "kind": "section"}), http.StatusCreated, "write a section")
	want(t, editor.post(t, "/api/v1/projects/SYS/requirements", map[string]any{"title": "Ledger"}), http.StatusCreated, "write a system requirement")
	links := func(c *client, key string) []any {
		return list(t, want(t, c.get(t, "/api/v1/requirements/"+key+"/links"), http.StatusOK, "read the links of "+key), "links")
	}

	made := obj(t, want(t, editor.post(t, "/api/v1/requirements/LNK-R1/links", map[string]any{"typeKey": "derives", "otherKey": "sys-r1"}),
		http.StatusCreated, "derive LNK-R1 from SYS-R1"), "link")
	other := made["other"].(map[string]any)
	if made["direction"] != "outgoing" || made["phrase"] != "derives from" || other["key"] != "SYS-R1" || made["sourceRevision"] != float64(1) || made["targetRevision"] != float64(1) {
		t.Errorf("the link as its source reads it: %v", made)
	}
	want(t, editor.post(t, "/api/v1/requirements/LNK-R2/links", map[string]any{"typeKey": "derives", "otherKey": "LNK-R1", "direction": "incoming"}),
		http.StatusCreated, "derive LNK-R1 from LNK-R2, from the target")
	want(t, editor.post(t, "/api/v1/requirements/LNK-R3/links", map[string]any{"typeKey": "derives", "otherKey": "LNK-R1"}), http.StatusCreated,
		"derive LNK-R3 from LNK-R1")

	t.Run("links read from either end", func(t *testing.T) {
		refuse(t, "a viewer of one end reads the other's", viewer.get(t, "/api/v1/requirements/SYS-R1/links"), http.StatusNotFound, "not_found")
		got := links(viewer, "LNK-R1")
		if len(got) != 2 {
			t.Fatalf("LNK-R1 has %d links, want 2: %v", len(got), got)
		}
		phrases := []string{}
		for _, each := range got {
			l := each.(map[string]any)
			phrases = append(phrases, l["phrase"].(string)+" "+l["other"].(map[string]any)["key"].(string))
		}
		sameList(t, "LNK-R1's links", phrases, "derives from LNK-R2", "is derived into LNK-R3")
		if len(links(editor, "SYS-R1")) != 1 {
			t.Error("the target reads the link too")
		}
		refuse(t, "a guest reads them", f.guest.get(t, "/api/v1/requirements/LNK-R1/links"), http.StatusNotFound, "not_found")
	})

	t.Run("refusals name what is wrong", func(t *testing.T) {
		refuse422(t, "a self-link", editor.post(t, "/api/v1/requirements/LNK-R1/links", map[string]any{"typeKey": "relates", "otherKey": "LNK-R1"}), "otherKey")
		refuse422(t, "a section", editor.post(t, "/api/v1/requirements/LNK-R1/links", map[string]any{"typeKey": "relates", "otherKey": "LNK-R4"}), "otherKey")
		refuse(t, "from a section", editor.post(t, "/api/v1/requirements/LNK-R4/links", map[string]any{"typeKey": "relates", "otherKey": "LNK-R1"}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse422(t, "an unknown requirement", editor.post(t, "/api/v1/requirements/LNK-R1/links", map[string]any{"typeKey": "relates", "otherKey": "LNK-R99"}), "otherKey")
		refuse422(t, "an unknown type", editor.post(t, "/api/v1/requirements/LNK-R1/links", map[string]any{"typeKey": "blocks", "otherKey": "LNK-R3"}), "typeId")
		refuse422(t, "a repeat", editor.post(t, "/api/v1/requirements/LNK-R1/links", map[string]any{"typeKey": "derives", "otherKey": "SYS-R1"}), "otherKey")
		cycle := refuse422(t, "a circle", editor.post(t, "/api/v1/requirements/LNK-R2/links", map[string]any{"typeKey": "derives", "otherKey": "LNK-R3"}), "otherKey")
		if !strings.HasPrefix(cycle, "LNK-R3 already derives from LNK-R2 through LNK-R1, ") {
			t.Errorf("the circle is named: %q", cycle)
		}
		refuse(t, "a viewer links", viewer.post(t, "/api/v1/requirements/LNK-R1/links", map[string]any{"typeKey": "relates", "otherKey": "LNK-R3"}),
			http.StatusForbidden, "forbidden")
	})

	t.Run("a link is logged on both ends, and removed from either", func(t *testing.T) {
		entries := list(t, want(t, editor.get(t, "/api/v1/requirements/SYS-R1/activity"), http.StatusOK, "read the far end's activity"), "entries")
		change := entries[0].(map[string]any)["change"].(map[string]any)
		if change["field"] != "link" || change["to"] != "is derived into LNK-R1" {
			t.Errorf("the far end logs the link: %v", change)
		}
		id := made["id"].(string)
		refuse(t, "a viewer of one end removes it", viewer.delete(t, "/api/v1/links/"+id), http.StatusNotFound, "not_found")
		seen := links(viewer, "LNK-R1")[0].(map[string]any)["id"].(string)
		refuse(t, "a viewer of both ends removes one", viewer.delete(t, "/api/v1/links/"+seen), http.StatusForbidden, "forbidden")
		want(t, editor.delete(t, "/api/v1/links/"+id), http.StatusNoContent, "remove it from the far end's side")
		refuse(t, "it is gone", editor.delete(t, "/api/v1/links/"+id), http.StatusNotFound, "not_found")
		if n := len(links(editor, "SYS-R1")); n != 0 {
			t.Errorf("a removed link is still listed: %d", n)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM trace_link WHERE id = $1 AND deleted_at IS NOT NULL`, id); n != 1 {
			t.Error("the removed link left no tombstone")
		}
		want(t, editor.post(t, "/api/v1/requirements/LNK-R1/links", map[string]any{"typeKey": "derives", "otherKey": "SYS-R1"}), http.StatusCreated,
			"link the two again")
	})

	t.Run("a copy carries the links when asked", func(t *testing.T) {
		copied := obj(t, want(t, editor.post(t, "/api/v1/requirements/LNK-R1/copy", map[string]any{"place": map[string]any{}, "links": true}),
			http.StatusCreated, "copy with links"), "requirement")
		if n := len(links(editor, copied["key"].(string))); n != 3 {
			t.Errorf("the copy has %d links, want the original's three", n)
		}
		plain := obj(t, want(t, editor.post(t, "/api/v1/requirements/LNK-R1/copy", map[string]any{"place": map[string]any{}}),
			http.StatusCreated, "copy without links"), "requirement")
		if n := len(links(editor, plain["key"].(string))); n != 0 {
			t.Errorf("a copy without links has %d", n)
		}
	})
}

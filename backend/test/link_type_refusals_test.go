//go:build integration

package test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// linkTypeOf reads one link type out of a list of them by its id.
func linkTypeOf(t *testing.T, r response, id string) map[string]any {
	t.Helper()
	for _, each := range list(t, r, "linkTypes") {
		if lt := each.(map[string]any); lt["id"] == id {
			return lt
		}
	}
	t.Fatalf("no link type %s in %s", id, r.Raw)
	return nil
}

// linkTypeShape is what a refused change must leave of a link type as read.
func linkTypeShape(lt map[string]any) string {
	return fmt.Sprintf("%v|%v|%v|%v|acyclic %v|retired %v", lt["key"], lt["name"], lt["forward"], lt["backward"], lt["acyclic"], lt["retired"])
}

// projectAdmin is a member who administers the project key names and nothing else.
func projectAdmin(t *testing.T, f *projectFixture, key string) *client {
	t.Helper()
	id := f.h.addPerson(t, "member")
	p := obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+key), http.StatusOK, "read "+key), "project")
	f.h.grantAs(t, uuid.MustParse(p["id"].(string)), id, "admin")
	return f.api.as(t, id)
}

// Only the installation's administrators change a type for all projects, a taken name is refused in either scope, and
// whoever may not change a type hears nothing of the formulas reading it (#313).
func TestATypeForAllProjectsIsTheAdministratorsAlone(t *testing.T) {
	key, hidden := projectKey(t, "LTA"), projectKey(t, "LTH")
	f := newProjectFixture(t)
	f.h.keepLinkTypes(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Shaped"}), http.StatusCreated, "make a project")
	admin := projectAdmin(t, f, key)
	word := strings.ToLower(projectKey(t, "W"))
	made := obj(t, want(t, f.owner.post(t, "/api/v1/link-types", map[string]any{
		"key": "guards_" + word, "name": "Guards " + word, "forward": "guards", "backward": "is guarded by",
	}), http.StatusCreated, "make a type for all projects"), "linkType")
	id := made["id"].(string)
	all := func() response {
		return want(t, f.owner.get(t, "/api/v1/link-types"), http.StatusOK, "list the types for all projects")
	}
	before := linkTypeShape(linkTypeOf(t, all(), id))

	t.Run("a project's administrator changes, retires or deletes none", func(t *testing.T) {
		refuse(t, "a project's administrator renames it", admin.patch(t, "/api/v1/link-types/"+id, map[string]any{"name": "Theirs"}),
			http.StatusForbidden, "forbidden")
		refuse(t, "a project's administrator retires it", admin.patch(t, "/api/v1/link-types/"+id, map[string]any{"retired": true}),
			http.StatusForbidden, "forbidden")
		refuse(t, "a project's administrator deletes it", admin.delete(t, "/api/v1/link-types/"+id), http.StatusForbidden, "forbidden")
		if after := linkTypeShape(linkTypeOf(t, all(), id)); after != before {
			t.Errorf("the refusals left the type as %s, want %s", after, before)
		}
	})

	t.Run("a name another type has is refused on name, across both scopes", func(t *testing.T) {
		own := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/link-types", map[string]any{
			"key": "checks_" + word, "name": "Checks " + word, "forward": "checks", "backward": "is checked by",
		}), http.StatusCreated, "make the project's own type"), "linkType")
		for _, c := range []struct {
			what, path, name string
		}{
			{"a type for all projects named as another", "/api/v1/link-types", "DERIVES"},
			{"a type for all projects named as a project's own", "/api/v1/link-types", own["name"].(string)},
			{"a project's own named as a type for all projects", "/api/v1/projects/" + key + "/link-types", "Derives"},
			{"a project's own named as another of its own", "/api/v1/projects/" + key + "/link-types", strings.ToUpper(own["name"].(string))},
		} {
			msg := refuse422(t, c.what, f.owner.post(t, c.path, map[string]any{"key": "taken_" + word, "name": c.name, "forward": "a", "backward": "b"}), "name")
			if !strings.Contains(msg, "Pick another name.") {
				t.Errorf("%s: the refusal says %q", c.what, msg)
			}
		}
		refuse422(t, "a rename to a taken name", f.owner.patch(t, "/api/v1/link-types/"+id, map[string]any{"name": "Relates"}), "name")
		if n := f.h.countRows(t, `SELECT count(*) FROM link_type WHERE key = $1`, "taken_"+word); n != 0 {
			t.Errorf("the refusals made %d types", n)
		}
		if after := linkTypeShape(linkTypeOf(t, all(), id)); after != before {
			t.Errorf("the refused rename left the type as %s", after)
		}
	})

	t.Run("a member letting a type a hidden formula reads cycle hears only that they may not", func(t *testing.T) {
		p := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": hidden, "name": "Hidden"}), http.StatusCreated, "make a hidden project"), "project")
		if _, err := f.h.super.Exec(t.Context(), `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, p["id"]); err != nil {
			t.Fatal(err)
		}
		f.h.settle(t)
		const depth = "Depth along derives"
		want(t, f.owner.post(t, "/api/v1/projects/"+hidden+"/attributes", map[string]any{
			"name": depth, "formula": `1 + coalesce(max(links('derives', out), "` + depth + `"), 0)`,
		}), http.StatusCreated, "a formula reads itself along derives")
		derives := linkTypeID(t, all(), "derives")
		for who, c := range map[string]*client{"a member": f.plain, "a project's administrator": admin} {
			answer := c.patch(t, "/api/v1/link-types/"+derives, map[string]any{"acyclic": false})
			refuse(t, who+" lets derives cycle", answer, http.StatusForbidden, "forbidden")
			if strings.Contains(string(answer.Raw), depth) || strings.Contains(string(answer.Raw), "formula") {
				t.Errorf("%s hears of the formula: %s", who, answer.Raw)
			}
		}
		if lt := linkTypeOf(t, all(), derives); lt["acyclic"] != true {
			t.Errorf("derives is no longer acyclic: %v", lt)
		}
	})
}

// A project's own type is its administrators' alone, comes back from retirement with its links, is turned acyclic
// over a circle it already has, and is refused while the project is archived (#313).
func TestAProjectsOwnLinkTypeAndItsLife(t *testing.T) {
	a, b, archived := projectKey(t, "LTP"), projectKey(t, "LTQ"), projectKey(t, "LTR")
	f := newProjectFixture(t)
	for _, k := range []string{a, b, archived} {
		want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": k, "name": k}), http.StatusCreated, "make "+k)
	}
	adminB := projectAdmin(t, f, b)
	makeType := func(project, k string, acyclic bool) string {
		return obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+project+"/link-types", map[string]any{
			"key": k, "name": strings.ToUpper(k[:1]) + k[1:], "forward": k, "backward": "is " + k + " by", "acyclic": acyclic,
		}), http.StatusCreated, "make "+k+" in "+project), "linkType")["id"].(string)
	}
	tests, follows := makeType(a, "tests", false), makeType(a, "follows", false)
	types := func(project string) response {
		return want(t, f.owner.get(t, "/api/v1/projects/"+project+"/link-types"), http.StatusOK, "list the types of "+project)
	}
	for _, title := range []string{"Brakes engage", "Brakes hold", "Brakes release"} {
		want(t, f.plain.post(t, "/api/v1/projects/"+a+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write "+title)
	}
	linkWith := func(typeID, from, to string) response {
		return f.plain.post(t, "/api/v1/requirements/"+a+"-"+from+"/links", map[string]any{"typeId": typeID, "otherKey": a + "-" + to})
	}
	linksOf := func(key string) []string {
		var out []string
		for _, each := range list(t, want(t, f.plain.get(t, "/api/v1/requirements/"+key+"/links"), http.StatusOK, "read the links of "+key), "links") {
			l := each.(map[string]any)
			out = append(out, l["phrase"].(string)+" "+l["other"].(map[string]any)["key"].(string))
		}
		return out
	}

	t.Run("another project's administrator neither changes it nor sets it in their project", func(t *testing.T) {
		before := linkTypeShape(linkTypeOf(t, types(a), tests))
		refuse(t, "B's administrator renames A's type", adminB.patch(t, "/api/v1/link-types/"+tests, map[string]any{"name": "Ours"}), http.StatusForbidden, "forbidden")
		refuse(t, "B's administrator deletes A's type", adminB.delete(t, "/api/v1/link-types/"+tests), http.StatusForbidden, "forbidden")
		refuse(t, "B's administrator sets A's type in B", adminB.put(t, "/api/v1/projects/"+b+"/link-types/"+tests, map[string]any{"enabled": false}),
			http.StatusNotFound, "not_found")
		if after := linkTypeOf(t, types(a), tests); linkTypeShape(after) != before || after["enabled"] != true {
			t.Errorf("the refusals left A's type as %v", after)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM link_type_project WHERE link_type_id = $1`, tests); n != 0 {
			t.Errorf("a project set A's type: %d rows", n)
		}
	})

	t.Run("a retired type keeps its links listed and takes new ones once it is used again", func(t *testing.T) {
		want(t, linkWith(tests, "R1", "R2"), http.StatusCreated, "R1 tests R2")
		want(t, f.owner.patch(t, "/api/v1/link-types/"+tests, map[string]any{"retired": true}), http.StatusOK, "retire tests")
		refuse422(t, "a link of the retired type", linkWith(tests, "R1", "R3"), "typeId")
		sameList(t, a+"-R1's links while retired", linksOf(a+"-R1"), "tests "+a+"-R2")
		back := obj(t, want(t, f.owner.patch(t, "/api/v1/link-types/"+tests, map[string]any{"retired": false}), http.StatusOK, "use tests again"), "linkType")
		if back["retired"] != false || back["links"] != float64(1) {
			t.Errorf("the type back from retirement reads %v", back)
		}
		want(t, linkWith(tests, "R1", "R3"), http.StatusCreated, "R1 tests R3 once tests is used again")
		sameList(t, a+"-R1's links", linksOf(a+"-R1"), "tests "+a+"-R2", "tests "+a+"-R3")
	})

	// Decided: turning a type acyclic is not refused over a circle it already has, which stays; the next circle is
	// refused. Formulas already allow for such a circle (formula.MaxSteps).
	t.Run("a type turned acyclic keeps the circle it has and refuses the next", func(t *testing.T) {
		want(t, linkWith(follows, "R1", "R2"), http.StatusCreated, "R1 follows R2")
		want(t, linkWith(follows, "R2", "R1"), http.StatusCreated, "R2 follows R1, a circle while follows may cycle")
		turned := obj(t, want(t, f.owner.patch(t, "/api/v1/link-types/"+follows, map[string]any{"acyclic": true}), http.StatusOK, "turn follows acyclic"), "linkType")
		if turned["acyclic"] != true {
			t.Errorf("follows reads %v", turned)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM trace_link WHERE type_id = $1 AND deleted_at IS NULL`, follows); n != 2 {
			t.Errorf("follows has %d links, want the circle's two", n)
		}
		want(t, linkWith(follows, "R2", "R3"), http.StatusCreated, "R2 follows R3, which closes no circle")
		cycle := refuse422(t, "a new circle", linkWith(follows, "R3", "R2"), "otherKey")
		if !strings.HasPrefix(cycle, a+"-R2 already follows "+a+"-R3") {
			t.Errorf("the circle is named: %q", cycle)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM trace_link WHERE type_id = $1 AND deleted_at IS NULL`, follows); n != 3 {
			t.Errorf("follows has %d links after the refusal, want 3", n)
		}
	})

	t.Run("an archived project's type is not changed or deleted, and a malformed id is no link type", func(t *testing.T) {
		kept := makeType(archived, "kept", false)
		want(t, f.owner.put(t, "/api/v1/projects/"+archived+"/archive", nil), http.StatusOK, "archive the project")
		refuse(t, "a rename while archived", f.owner.patch(t, "/api/v1/link-types/"+kept, map[string]any{"name": "Changed"}), http.StatusConflict, "project_archived")
		refuse(t, "a deletion while archived", f.owner.delete(t, "/api/v1/link-types/"+kept), http.StatusConflict, "project_archived")
		refuse(t, "a deletion by a malformed id", f.owner.delete(t, "/api/v1/link-types/not-an-id"), http.StatusBadRequest, "bad_request")
		if n := f.h.countRows(t, `SELECT count(*) FROM link_type WHERE id = $1 AND name = 'Kept'`, kept); n != 1 {
			t.Error("the archived project's type changed or went")
		}
	})
}

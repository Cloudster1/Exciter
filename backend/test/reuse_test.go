//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Reuse and variants (#49): a library's requirements reused in another project follow their source's words, fall
// behind when it moves on, take its revisions, and become variants with words of their own.

// reuseOf is the reuse a requirement answers, failing when it has none.
func reuseOf(t *testing.T, c *client, key string) map[string]any {
	t.Helper()
	got := obj(t, want(t, c.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement")
	reuse, ok := got["reuse"].(map[string]any)
	if !ok {
		t.Fatalf("%s follows nothing: %v", key, got["reuse"])
	}
	return reuse
}

// gridKeys are the keys a query finds in a project, in its order.
func gridKeys(t *testing.T, c *client, projectKey, query string) string {
	t.Helper()
	var keys []string
	for _, each := range list(t, want(t, c.get(t, "/api/v1/projects/"+projectKey+"/requirements?query="+url.QueryEscape(query)), http.StatusOK, query), "rows") {
		keys = append(keys, each.(map[string]any)["key"].(string))
	}
	return strings.Join(keys, " ")
}

func TestLibraryRequirementsAreReusedAndKeptAsVariants(t *testing.T) {
	libKey, appKey := projectKey(t, "LIB"), projectKey(t, "APP")
	f := newProjectFixture(t)
	f.h.started(t)
	editor := f.plain
	watcherID := f.h.addPerson(t, "member")
	watcher := f.api.as(t, watcherID)
	lib := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": libKey, "name": "Library"}), http.StatusCreated, "make the library"), "project")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": appKey, "name": "Product"}), http.StatusCreated, "make the product")
	section := obj(t, want(t, editor.post(t, "/api/v1/projects/"+libKey+"/requirements", map[string]any{"kind": "section", "title": "Braking"}),
		http.StatusCreated, "write a section"), "requirement")
	source := obj(t, want(t, editor.post(t, "/api/v1/projects/"+libKey+"/requirements", map[string]any{
		"title": "Brakes engage within 100 ms", "statement": statementSaying("The brakes engage within 100 ms of the pedal."),
		"place": place(section["id"], nil)}), http.StatusCreated, "write a requirement in it"), "requirement")
	sourceKey := source["key"].(string)
	reuse := func(c *client, key string, body map[string]any) response {
		return c.post(t, "/api/v1/requirements/"+key+"/reuse", body)
	}

	t.Run("only a library's requirements are reused, and only in another project", func(t *testing.T) {
		if code := errorCode(t, want(t, reuse(editor, sourceKey, map[string]any{"projectKey": appKey}), http.StatusConflict, "reuse from no library")); code != "not_a_library" {
			t.Errorf("code %s", code)
		}
		refuse(t, "a member marks a library", editor.patch(t, "/api/v1/projects/"+libKey, map[string]any{"settings": map[string]any{"library": true}}),
			http.StatusForbidden, "forbidden")
		marked := obj(t, want(t, f.owner.patch(t, "/api/v1/projects/"+libKey, map[string]any{"settings": map[string]any{"library": true}}),
			http.StatusOK, "the owner marks the library"), "project")
		if marked["settings"].(map[string]any)["library"] != true {
			t.Errorf("the project reads %v", marked["settings"])
		}
		if rows := f.h.auditRows(t, "project.library_set", uuid.MustParse(lib["id"].(string))); len(rows) != 1 || rows[0]["library"] != true {
			t.Errorf("the audit log holds %v", rows)
		}
		fieldError(t, reuse(editor, sourceKey, map[string]any{"projectKey": libKey}), "projectKey")
		refuse(t, "a guest reuses", reuse(f.guest, sourceKey, map[string]any{"projectKey": appKey}), http.StatusNotFound, "not_found")
	})

	made := obj(t, want(t, reuse(editor, section["key"].(string), map[string]any{"projectKey": appKey, "children": true}),
		http.StatusCreated, "reuse the section with what is below it"), "requirement")
	if made["kind"] != "section" {
		t.Fatalf("the reuse answers %v", made)
	}
	reusedKey := appKey + "-R2"

	t.Run("a reuse follows its source, and its words stay the source's", func(t *testing.T) {
		got := reuseOf(t, editor, reusedKey)
		src := got["source"].(map[string]any)
		if got["state"] != "following" || got["revision"] != float64(1) || got["behind"] != false || src["key"] != sourceKey {
			t.Errorf(reusedKey+" reads %v", got)
		}
		if code := errorCode(t, want(t, editor.patch(t, "/api/v1/requirements/"+reusedKey, map[string]any{"title": "Brakes engage at once", "baseRevision": 1}),
			http.StatusConflict, "change the words of a reuse")); code != "follows_source" {
			t.Errorf("code %s", code)
		}
		want(t, editor.patch(t, "/api/v1/requirements/"+reusedKey, map[string]any{"priority": "must"}), http.StatusOK, "its priority is its own")
		if keys := gridKeys(t, editor, appKey, "reuse = following"); keys != appKey+"-R1 "+reusedKey {
			t.Errorf("reuse = following finds %q", keys)
		}
	})

	t.Run("the source moves on, the reuse falls behind, and taking the revision writes its next one", func(t *testing.T) {
		want(t, watcher.put(t, "/api/v1/requirements/"+reusedKey+"/watch", nil), http.StatusOK, "a member watches the reuse")
		want(t, editor.patch(t, "/api/v1/requirements/"+sourceKey, map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}), http.StatusOK, "revise the source")
		f.h.drained(t)
		got := reuseOf(t, editor, reusedKey)
		if got["behind"] != true || got["source"].(map[string]any)["revision"] != float64(2) {
			t.Fatalf("after the source moved on the reuse reads %v", got)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM notification n JOIN requirement r ON r.id = n.requirement_id
			WHERE n.user_id = $1 AND n.kind = 'source_revised' AND r.reuse_source = $2`, watcherID, source["id"]); n != 1 {
			t.Errorf("the watcher was told %d times, want once", n)
		}
		if keys := gridKeys(t, editor, appKey, "reuse = behind"); keys != reusedKey {
			t.Errorf("reuse = behind finds %q", keys)
		}
		take := "/api/v1/requirements/" + reusedKey + "/reuse/take"
		if code := errorCode(t, want(t, editor.post(t, take, map[string]any{"revision": 1}), http.StatusConflict, "take a revision the source moved past")); code != "source_moved_on" {
			t.Errorf("code %s", code)
		}
		taken := obj(t, want(t, editor.post(t, take, map[string]any{"revision": 2}), http.StatusOK, "take revision 2"), "requirement")
		if taken["title"] != "Brakes engage within 80 ms" || taken["revision"] != float64(2) || taken["priority"] != "must" ||
			taken["reuse"].(map[string]any)["revision"] != float64(2) || taken["reuse"].(map[string]any)["behind"] != false {
			t.Errorf("after the take the reuse reads %v", taken)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1 AND number = 2 AND comment = $2`,
			taken["id"], "Took revision 2 of "+sourceKey); n != 1 {
			t.Error("the take wrote no revision that says what it took")
		}
	})

	t.Run("a variant has words of its own, and still sees its source move on", func(t *testing.T) {
		variant := obj(t, want(t, editor.post(t, "/api/v1/requirements/"+reusedKey+"/reuse/variant", nil), http.StatusOK, "make a variant"), "requirement")
		if variant["reuse"].(map[string]any)["state"] != "variant" {
			t.Fatalf("the variant reads %v", variant["reuse"])
		}
		want(t, editor.patch(t, "/api/v1/requirements/"+reusedKey, map[string]any{"title": "Brakes engage within 60 ms on the product", "baseRevision": 2}),
			http.StatusOK, "the variant changes its words")
		want(t, editor.patch(t, "/api/v1/requirements/"+sourceKey, map[string]any{"title": "Brakes engage within 70 ms", "baseRevision": 2}), http.StatusOK, "revise the source again")
		got := reuseOf(t, editor, reusedKey)
		if got["state"] != "variant" || got["behind"] != true || got["revision"] != float64(2) {
			t.Errorf("the variant reads %v", got)
		}
		if keys := gridKeys(t, editor, appKey, "reuse = variant"); keys != reusedKey {
			t.Errorf("reuse = variant finds %q", keys)
		}
		back := obj(t, want(t, editor.post(t, "/api/v1/requirements/"+reusedKey+"/reuse/take", map[string]any{"revision": 3}), http.StatusOK, "take the source again"), "requirement")
		if back["title"] != "Brakes engage within 70 ms" || back["reuse"].(map[string]any)["state"] != "following" {
			t.Errorf("taking the source has the variant read %v", back)
		}
	})

	t.Run("a reader who may not read the library sees the reuse but not its source", func(t *testing.T) {
		app := obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+appKey), http.StatusOK, "read the product"), "project")
		f.h.grantAs(t, uuid.MustParse(app["id"].(string)), f.away.user, "editor")
		got := reuseOf(t, f.outside, reusedKey)
		if got["source"] != nil || got["state"] != "following" {
			t.Errorf("a reader of the product alone reads %v", got)
		}
		refuse(t, "a take without the source", f.outside.post(t, "/api/v1/requirements/"+reusedKey+"/reuse/take", map[string]any{"revision": 3}),
			http.StatusForbidden, "forbidden")
	})

	t.Run("purging the source leaves a requirement of its own", func(t *testing.T) {
		want(t, editor.delete(t, "/api/v1/requirements/"+sourceKey), http.StatusNoContent, "trash the source")
		want(t, f.owner.delete(t, "/api/v1/projects/"+libKey+"/trash/"+source["id"].(string)), http.StatusNoContent, "purge it")
		got := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+reusedKey), http.StatusOK, "read the reuse"), "requirement")
		if got["reuse"] != nil {
			t.Fatalf("the reuse still follows %v", got["reuse"])
		}
		want(t, editor.patch(t, "/api/v1/requirements/"+reusedKey, map[string]any{"title": "Brakes engage within 70 ms, ours now", "baseRevision": 4}),
			http.StatusOK, "its words are its own")
		refuse(t, "a take without a source", editor.post(t, "/api/v1/requirements/"+reusedKey+"/reuse/take", map[string]any{"revision": 3}),
			http.StatusConflict, "not_reused")
		refuse(t, "a variant without a source", editor.post(t, "/api/v1/requirements/"+reusedKey+"/reuse/variant", nil),
			http.StatusConflict, "not_reused")
	})
}

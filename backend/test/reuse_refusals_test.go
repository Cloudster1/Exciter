//go:build integration

package test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// reuseScene is a library with a section of two requirements, and a product everybody reads where only the editor
// writes; the reader writes in the library alone.
type reuseScene struct {
	f              *projectFixture
	lib, app       string
	editor, reader *client
	section        map[string]any
	first, second  map[string]any
}

func newReuseScene(t *testing.T, prefix string) *reuseScene {
	t.Helper()
	f := newProjectFixture(t)
	s := &reuseScene{f: f, lib: projectKey(t, prefix+"L"), app: projectKey(t, prefix+"P")}
	editorID := f.h.addPerson(t, "member")
	s.editor, s.reader = f.api.as(t, editorID), f.plain
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": s.lib, "name": "Library"}), http.StatusCreated, "make the library")
	want(t, f.owner.patch(t, "/api/v1/projects/"+s.lib, map[string]any{"settings": map[string]any{"library": true}}), http.StatusOK, "mark the library")
	app := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": s.app, "name": "Product"}), http.StatusCreated, "make the product"), "project")
	appID := uuid.MustParse(app["id"].(string))
	if _, err := f.h.super.Exec(t.Context(), `UPDATE project_grant SET role = 'viewer' WHERE project_id = $1 AND subject_type = 'everyone'`, appID); err != nil {
		t.Fatal(err)
	}
	f.h.grantAs(t, appID, editorID, "editor")
	s.section = s.write(t, map[string]any{"kind": "section", "title": "Braking"})
	s.first = s.write(t, map[string]any{"title": "Brakes engage within 100 ms", "statement": statementSaying("The brakes engage."), "place": place(s.section["id"], nil)})
	s.second = s.write(t, map[string]any{"title": "Brakes hold on a slope", "statement": statementSaying("The car stays put."), "place": place(s.section["id"], s.first["id"])})
	return s
}

// write puts a requirement in the library.
func (s *reuseScene) write(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	return obj(t, want(t, s.editor.post(t, "/api/v1/projects/"+s.lib+"/requirements", body), http.StatusCreated, "write "+body["title"].(string)), "requirement")
}

// reuse reuses a library requirement in the product, answering the reuse's key.
func (s *reuseScene) reuse(t *testing.T, source map[string]any) string {
	t.Helper()
	made := obj(t, want(t, s.editor.post(t, "/api/v1/requirements/"+source["key"].(string)+"/reuse", map[string]any{"projectKey": s.app}),
		http.StatusCreated, "reuse "+source["key"].(string)), "requirement")
	return made["key"].(string)
}

// revise gives a library requirement a new title as its next revision.
func (s *reuseScene) revise(t *testing.T, source map[string]any, title string) {
	t.Helper()
	key := source["key"].(string)
	cur := obj(t, want(t, s.editor.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement")
	want(t, s.editor.patch(t, "/api/v1/requirements/"+key, map[string]any{"title": title, "baseRevision": cur["revision"]}), http.StatusOK, "revise "+key)
}

// inProject counts the requirements of a project, trashed or not.
func (s *reuseScene) inProject(t *testing.T, key string) int {
	t.Helper()
	return s.f.h.countRows(t, `SELECT count(*) FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1`, key)
}

// revisions counts the revisions a requirement has written.
func (s *reuseScene) revisions(t *testing.T, key string) int {
	t.Helper()
	id := obj(t, want(t, s.editor.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement")["id"]
	return s.f.h.countRows(t, `SELECT count(*) FROM requirement_revision WHERE requirement_id = $1`, id)
}

func refusalMessage(t *testing.T, r response) string {
	t.Helper()
	return obj(t, r, "error")["message"].(string)
}

// A reuse lands only in a project its person may write, which is not archived, and with what is below it when
// asked, each in its place and following its own source (#321).
func TestAReuseLandsOnlyWhereItsPersonMayWrite(t *testing.T) {
	s := newReuseScene(t, "RW")
	from := "/api/v1/requirements/" + s.first["key"].(string) + "/reuse"
	archived, nobody := projectKey(t, "RWA"), projectKey(t, "RWN")
	want(t, s.f.owner.post(t, "/api/v1/projects", map[string]any{"key": archived, "name": "Archived"}), http.StatusCreated, "make a project")
	want(t, s.f.owner.put(t, "/api/v1/projects/"+archived+"/archive", nil), http.StatusOK, "archive it")

	t.Run("a reader of the product, an archived project and a key nobody has are refused, and nothing lands", func(t *testing.T) {
		refuse(t, "a reader of the product reuses", s.reader.post(t, from, map[string]any{"projectKey": s.app}), http.StatusForbidden, "forbidden")
		refuse(t, "a reuse into an archived project", s.editor.post(t, from, map[string]any{"projectKey": archived}), http.StatusConflict, "project_archived")
		// A key nobody has is not found, as one the person may not see is: a refusal never tells which keys exist.
		refuse(t, "a reuse into a key nobody has", s.editor.post(t, from, map[string]any{"projectKey": nobody}), http.StatusNotFound, "not_found")
		if n, m := s.inProject(t, s.app), s.inProject(t, archived); n != 0 || m != 0 {
			t.Errorf("the refusals left %d requirements in the product and %d in the archived project", n, m)
		}
	})

	t.Run("a section reused with what is below it brings each child, in order, following its own source", func(t *testing.T) {
		want(t, s.editor.post(t, "/api/v1/requirements/"+s.section["key"].(string)+"/reuse", map[string]any{"projectKey": s.app, "children": true}),
			http.StatusCreated, "reuse the section with its children")
		sameList(t, "the product's tree", nodeOutlines(t, want(t, s.editor.get(t, "/api/v1/projects/"+s.app+"/tree"), http.StatusOK, "read the tree")),
			s.app+"-R1 1", s.app+"-R2 1.1", s.app+"-R3 1.2")
		for i, source := range []map[string]any{s.section, s.first, s.second} {
			key := s.app + "-R" + string(rune('1'+i))
			if got := reuseOf(t, s.editor, key); got["state"] != "following" || got["source"].(map[string]any)["key"] != source["key"] {
				t.Errorf("%s follows %v", key, got)
			}
		}
	})

	t.Run("a child written in the library section later does not arrive by itself", func(t *testing.T) {
		s.write(t, map[string]any{"title": "Brakes warn when worn", "place": place(s.section["id"], nil)})
		s.f.h.settle(t)
		if n := s.inProject(t, s.app); n != 3 {
			t.Errorf("the product holds %d requirements, want the three reused", n)
		}
	})
}

// Taking the source and making a variant change a reuse, so they are refused where it may not change, and a take
// brings only the source's latest revision (#321).
func TestATakeAndAVariantAreRefusedWhereTheReuseMayNotChange(t *testing.T) {
	s := newReuseScene(t, "RT")
	trashed, moved := s.reuse(t, s.first), s.reuse(t, s.second)
	take := func(c *client, key string, revision int) response {
		return c.post(t, "/api/v1/requirements/"+key+"/reuse/take", map[string]any{"revision": revision})
	}
	variant := func(c *client, key string) response {
		return c.post(t, "/api/v1/requirements/"+key+"/reuse/variant", nil)
	}

	t.Run("a reuse in the trash takes nothing and becomes no variant", func(t *testing.T) {
		want(t, s.editor.delete(t, "/api/v1/requirements/"+trashed), http.StatusNoContent, "trash the reuse")
		s.revise(t, s.first, "Brakes engage within 80 ms")
		refuse(t, "a take in the trash", take(s.editor, trashed, 2), http.StatusConflict, "requirement_trashed")
		refuse(t, "a variant in the trash", variant(s.editor, trashed), http.StatusConflict, "requirement_trashed")
		if got := s.f.h.countRows(t, `SELECT count(*) FROM requirement WHERE id = $1 AND reuse_revision = 1 AND reuse_state = 'following'`,
			s.idOf(t, trashed)); got != 1 {
			t.Error("the trashed reuse moved on")
		}
	})

	t.Run("a revision the source does not have is refused, naming the one it has", func(t *testing.T) {
		r := take(s.editor, moved, 5)
		refuse(t, "a revision above the source's", r, http.StatusConflict, "source_moved_on")
		if msg := refusalMessage(t, r); !strings.Contains(msg, s.second["key"].(string)+" has revision 1 now") {
			t.Errorf("the refusal says %q", msg)
		}
	})

	t.Run("a reuse locked by hand takes no new words", func(t *testing.T) {
		want(t, s.f.owner.post(t, "/api/v1/requirements/"+moved+"/lock", map[string]any{"reason": "Released in 2.0"}), http.StatusOK, "lock the reuse")
		s.revise(t, s.second, "Brakes hold on a steep slope")
		r := take(s.editor, moved, 2)
		refuse(t, "a take while locked", r, http.StatusConflict, "requirement_locked")
		if msg := refusalMessage(t, r); !strings.Contains(msg, "Released in 2.0") {
			t.Errorf("the refusal says %q", msg)
		}
		got := obj(t, want(t, s.editor.get(t, "/api/v1/requirements/"+moved), http.StatusOK, "read the reuse"), "requirement")
		if got["title"] != "Brakes hold on a slope" || got["revision"] != float64(1) || s.revisions(t, moved) != 1 {
			t.Errorf("the locked reuse reads %v at revision %v", got["title"], got["revision"])
		}
	})

	t.Run("a locked reuse whose words are the source's already moves its revision and writes none", func(t *testing.T) {
		notes := obj(t, want(t, s.f.owner.post(t, "/api/v1/projects/"+s.lib+"/attributes", map[string]any{"name": "Notes", "kind": "text"}),
			http.StatusCreated, "define an attribute in the library"), "attribute")["id"].(string)
		source := s.write(t, map[string]any{"title": "Log every login"})
		key := s.reuse(t, source)
		want(t, s.f.owner.post(t, "/api/v1/requirements/"+key+"/lock", map[string]any{"reason": "Released in 2.0"}), http.StatusOK, "lock the reuse")
		// The product has no such attribute, so the source's next revision says nothing new to the reuse.
		revised := obj(t, want(t, s.editor.patch(t, "/api/v1/requirements/"+source["key"].(string), map[string]any{
			"attributes": map[string]any{notes: "Checked by the auditors"}, "baseRevision": 1}), http.StatusOK, "revise the source's value"), "requirement")
		if revised["revision"] != float64(2) {
			t.Fatalf("a new value is revision %v", revised["revision"])
		}
		taken := obj(t, want(t, take(s.editor, key, 2), http.StatusOK, "take revision 2 while locked"), "requirement")
		if taken["revision"] != float64(1) || taken["reuse"].(map[string]any)["revision"] != float64(2) || s.revisions(t, key) != 1 {
			t.Errorf("the take reads revision %v following %v", taken["revision"], taken["reuse"])
		}
	})

	t.Run("a source in the trash, not purged, is still taken", func(t *testing.T) {
		source := s.write(t, map[string]any{"title": "Warn before the brakes fade"})
		key := s.reuse(t, source)
		s.revise(t, source, "Warn before the brakes fade on a descent")
		want(t, s.editor.delete(t, "/api/v1/requirements/"+source["key"].(string)), http.StatusNoContent, "trash the source")
		long := s.editor.post(t, "/api/v1/requirements/"+key+"/reuse/take",
			map[string]any{"revision": 2, "comment": strings.Repeat("x", requirement.MaxCommentLength+1)})
		if msg := fieldError(t, long, "comment"); !strings.Contains(msg, fmt.Sprint(requirement.MaxCommentLength)) {
			t.Errorf("a long comment is refused with %q", msg)
		}
		if s.revisions(t, key) != 1 {
			t.Error("a take refused for its comment wrote a revision")
		}
		// The source may come back from the trash, so its words are still the ones to follow.
		taken := obj(t, want(t, take(s.editor, key, 2), http.StatusOK, "take a trashed source"), "requirement")
		if taken["title"] != "Warn before the brakes fade on a descent" || taken["revision"] != float64(2) {
			t.Errorf("the take reads %v at revision %v", taken["title"], taken["revision"])
		}
	})

	t.Run("a reader makes no variant, and a variant made twice is logged once", func(t *testing.T) {
		key := s.reuse(t, s.write(t, map[string]any{"title": "Dim the lights at night"}))
		refuse(t, "a reader of the product makes a variant", variant(s.reader, key), http.StatusForbidden, "forbidden")
		for _, what := range []string{"make a variant", "make it a variant again"} {
			if got := obj(t, want(t, variant(s.editor, key), http.StatusOK, what), "requirement"); got["reuse"].(map[string]any)["state"] != "variant" {
				t.Errorf("%s: the reuse reads %v", what, got["reuse"])
			}
		}
		if n := s.f.h.countRows(t, `SELECT count(*) FROM requirement_change WHERE requirement_id = $1 AND field = 'reuse'`, s.idOf(t, key)); n != 1 {
			t.Errorf("%d reuse changes for one variant", n)
		}
	})
}

func (s *reuseScene) idOf(t *testing.T, key string) any {
	t.Helper()
	var id uuid.UUID
	project, number, _ := strings.Cut(key, "-R")
	if err := s.f.h.super.QueryRow(t.Context(), `SELECT r.id FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND r.number = $2::int`,
		project, number).Scan(&id); err != nil {
		t.Fatalf("find %s: %v", key, err)
	}
	return id
}

// A reuse has its own copies of the files its source's words show, and a take brings those of the revision it
// takes and no other (#321).
func TestAReuseCopiesTheFilesItsSourceShows(t *testing.T) {
	s := newReuseScene(t, "RF")
	source := s.write(t, map[string]any{"title": "Show the basket"})
	sourceKey := source["key"].(string)
	attach := func(name string) string {
		return obj(t, want(t, s.editor.upload(t, "/api/v1/requirements/"+sourceKey+"/attachments", name, aPicture(t)), http.StatusCreated, "attach "+name),
			"attachment")["id"].(string)
	}
	show := func(id string, base int) {
		want(t, s.editor.patch(t, "/api/v1/requirements/"+sourceKey, map[string]any{"statement": showing(id), "baseRevision": base}), http.StatusOK, "show "+id)
	}
	files := func(key string) map[string]string {
		out := map[string]string{}
		for _, each := range list(t, want(t, s.editor.get(t, "/api/v1/requirements/"+key+"/attachments"), http.StatusOK, "list the files of "+key), "attachments") {
			a := each.(map[string]any)
			out[a["fileName"].(string)] = a["id"].(string)
		}
		return out
	}
	statementOf := func(key string) string {
		got := obj(t, want(t, s.editor.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement")
		raw, _ := got["statement"].(map[string]any)
		return strings.Join(attachmentIDs(raw), " ")
	}
	basket := attach("basket.png")
	show(basket, 1)
	reused := s.reuse(t, source)

	t.Run("the reuse shows its own copy of the picture", func(t *testing.T) {
		got := files(reused)
		if len(got) != 1 || got["basket.png"] == "" || got["basket.png"] == basket {
			t.Fatalf("the reuse holds %v", got)
		}
		if shown := statementOf(reused); shown != got["basket.png"] {
			t.Errorf("the reuse's statement shows %q, want its copy %s", shown, got["basket.png"])
		}
	})

	t.Run("a take brings the newly shown file, and the dropped one does not come again", func(t *testing.T) {
		shelf := attach("shelf.png")
		show(shelf, 2)
		want(t, s.editor.post(t, "/api/v1/requirements/"+reused+"/reuse/take", map[string]any{"revision": 3}), http.StatusOK, "take revision 3")
		got := files(reused)
		names := make([]string, 0, len(got))
		for name := range got {
			names = append(names, name)
		}
		slices.Sort(names)
		sameList(t, "the reuse's files", names, "basket.png", "shelf.png")
		if got["shelf.png"] == shelf {
			t.Error("the reuse shows the source's own file, not a copy")
		}
		if shown := statementOf(reused); shown != got["shelf.png"] {
			t.Errorf("the reuse's statement shows %q, want its copy %s", shown, got["shelf.png"])
		}
	})
}

// attachmentIDs are the files a document shows, in its order.
func attachmentIDs(doc map[string]any) []string {
	var out []string
	content, _ := doc["content"].([]any)
	for _, each := range content {
		node, _ := each.(map[string]any)
		if attrs, ok := node["attrs"].(map[string]any); ok {
			if id, ok := attrs["attachmentId"].(string); ok {
				out = append(out, id)
			}
		}
		out = append(out, attachmentIDs(node)...)
	}
	return out
}

// A take brings a test case's steps, and a rating only as far as the product's scales hold its values (#321).
func TestATakeBringsStepsAndTheRatingThatFits(t *testing.T) {
	s := newReuseScene(t, "RK")
	owner := s.f.owner
	for _, key := range []string{s.lib, s.app} {
		want(t, owner.post(t, "/api/v1/projects/"+key+"/types", map[string]any{"name": "Test case", "icon": "requirement", "colour": "#4f6bed", "role": "testCase"}),
			http.StatusCreated, "a type of test cases in "+key)
		want(t, owner.post(t, "/api/v1/projects/"+key+"/types", map[string]any{"name": "Hazard", "icon": "risk", "colour": "#dc2626", "role": "risk"}),
			http.StatusCreated, "a type of risks in "+key)
	}
	want(t, owner.put(t, "/api/v1/projects/"+s.lib+"/risk-scheme", riskScheme(5, "matrix", false)), http.StatusOK, "five values a scale in the library")
	want(t, owner.put(t, "/api/v1/projects/"+s.app+"/risk-scheme", riskScheme(3, "matrix", false)), http.StatusOK, "three values a scale in the product")
	typeIn := func(key, name string) string {
		for _, each := range list(t, want(t, owner.get(t, "/api/v1/projects/"+key+"/types"), http.StatusOK, "list the types"), "types") {
			if ty := each.(map[string]any); ty["name"] == name {
				return ty["id"].(string)
			}
		}
		t.Fatalf("%s has no type %s", key, name)
		return ""
	}
	steps := func(actions ...string) map[string]any {
		var out []any
		for _, a := range actions {
			out = append(out, map[string]any{"action": statementSaying(a)})
		}
		return map[string]any{"steps": out}
	}

	t.Run("a test case's steps come with the take", func(t *testing.T) {
		source := s.write(t, map[string]any{"title": "Brake on a wet road", "typeId": typeIn(s.lib, "Test case"), "testCase": steps("Drive at the speed.")})
		key := s.reuse(t, source)
		want(t, s.editor.patch(t, "/api/v1/requirements/"+source["key"].(string), map[string]any{"testCase": steps("Drive at the speed.", "Press the pedal."),
			"baseRevision": 1}), http.StatusOK, "add a step to the source")
		taken := obj(t, want(t, s.editor.post(t, "/api/v1/requirements/"+key+"/reuse/take", map[string]any{"revision": 2}), http.StatusOK, "take revision 2"), "requirement")
		sameList(t, "the reuse's steps", stepTexts(t, testCaseOf(t, taken)), "Drive at the speed.", "Press the pedal.")
	})

	t.Run("a rating comes along only as far as the product's scales hold its values", func(t *testing.T) {
		source := s.write(t, map[string]any{"title": "Brakes fail on ice", "typeId": typeIn(s.lib, "Hazard"),
			"risk": map[string]any{"before": side(2, 3), "after": side(1, 1)}})
		key := s.reuse(t, source)
		want(t, s.editor.patch(t, "/api/v1/requirements/"+source["key"].(string), map[string]any{
			"risk": map[string]any{"before": side(5, 2), "after": side(1, 4)}, "baseRevision": 1}), http.StatusOK, "rate the source again")
		taken := obj(t, want(t, s.editor.post(t, "/api/v1/requirements/"+key+"/reuse/take", map[string]any{"revision": 2}), http.StatusOK, "take revision 2"), "requirement")
		rating := ratingOf(t, taken)
		before, after := rating["before"].(map[string]any), rating["after"].(map[string]any)
		if before["severity"] != nil || before["probability"] != float64(2) || after["severity"] != float64(1) || after["probability"] != nil {
			t.Errorf("the product's three values a scale keep %v before and %v after", before, after)
		}
	})
}

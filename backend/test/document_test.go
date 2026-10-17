//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// specEntry finds the entry a key names in a specification answer.
func specEntry(t *testing.T, r response, key string) map[string]any {
	t.Helper()
	for _, each := range list(t, r, "entries") {
		e := each.(map[string]any)
		if e["node"].(map[string]any)["key"] == key {
			return e
		}
	}
	t.Fatalf("%s is not in the specification: %v", key, r.Body)
	return nil
}

// The document edits a requirement from what its entry holds: the save names the entry's revision, a stale one is
// refused, and a requirement added after an entry stands right below it (#168).
func TestTheDocumentEditsInPlace(t *testing.T) {
	key := projectKey(t, "DOC")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Document"}), http.StatusCreated, "make a project")
	ownerID := f.h.addPerson(t, "member")
	editor := f.plain
	note := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/types", map[string]any{"name": "Note", "icon": "test", "colour": "#22aa55"}),
		http.StatusCreated, "make a type"), "type")
	write := func(body map[string]any) map[string]any {
		t.Helper()
		return obj(t, want(t, editor.post(t, "/api/v1/projects/"+key+"/requirements", body), http.StatusCreated, "write"), "requirement")
	}
	scope := write(map[string]any{"kind": "section", "title": "Scope"})
	pay := write(map[string]any{"title": "Pay", "typeId": note["id"], "place": place(scope["id"], nil),
		"ownerId": ownerID, "priority": "must"})
	payKey := pay["key"].(string)
	spec := func() response {
		t.Helper()
		return want(t, editor.get(t, "/api/v1/projects/"+key+"/spec"), http.StatusOK, "read the specification")
	}

	t.Run("an entry holds what its editor needs: type, owner, priority and approval", func(t *testing.T) {
		e := specEntry(t, spec(), payKey)
		typ, _ := e["type"].(map[string]any)
		owner, _ := e["owner"].(map[string]any)
		approval, _ := e["approval"].(map[string]any)
		if typ == nil || typ["id"] != note["id"] || typ["name"] != "Note" || owner == nil || owner["id"] != ownerID.String() || e["priority"] != "must" {
			t.Errorf("the entry's type, owner and priority: %v %v %v", e["type"], e["owner"], e["priority"])
		}
		if approval == nil || approval["pendingReviewId"] != nil || approval["approvedRevision"] != nil || approval["current"] != false {
			t.Errorf("an entry never reviewed has no approval: %v", e["approval"])
		}
		section := specEntry(t, spec(), scope["key"].(string))
		if section["type"] != nil || section["owner"] != nil || section["priority"] != nil {
			t.Errorf("a section has no type, owner or priority: %v", section)
		}
	})

	t.Run("a save names the entry's revision, and a stale one is refused", func(t *testing.T) {
		base := specEntry(t, spec(), payKey)["node"].(map[string]any)["revision"]
		saved := obj(t, want(t, editor.patch(t, "/api/v1/requirements/"+payKey, map[string]any{"title": "Pay by card", "baseRevision": base}),
			http.StatusOK, "save from the document"), "requirement")
		if saved["revision"] != float64(2) {
			t.Errorf("the save wrote revision %v, want 2", saved["revision"])
		}
		if got := specEntry(t, spec(), payKey)["node"].(map[string]any); got["title"] != "Pay by card" || got["revision"] != float64(2) {
			t.Errorf("the document reads the saved entry: %v", got)
		}
		refuse(t, "a save over the revision the document read before", editor.patch(t, "/api/v1/requirements/"+payKey,
			map[string]any{"title": "Pay in cash", "baseRevision": base}), http.StatusConflict, "revision_conflict")
	})

	t.Run("a requirement added after an entry, or as a section's first, stands where it was asked", func(t *testing.T) {
		after := write(map[string]any{"title": "Refund", "place": place(pay["parentId"], pay["id"])})
		first := write(map[string]any{"title": "Currency", "place": place(scope["id"], nil)})
		sameList(t, "the document", nodeOutlines(t, want(t, editor.get(t, "/api/v1/projects/"+key+"/tree"), http.StatusOK, "read the tree")),
			scope["key"].(string)+" 1", first["key"].(string)+" 1.1", payKey+" 1.2", after["key"].(string)+" 1.3")
	})
}

// The document moves many requirements at once, each as the single transition would, and narrows to statuses (#169).
func TestTheDocumentMovesStates(t *testing.T) {
	ctx := context.Background()
	key := projectKey(t, "DST")
	other := projectKey(t, "DSO")
	f := newProjectFixture(t)
	for _, k := range []string{key, other} {
		want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": k, "name": "States " + k}), http.StatusCreated, "make a project")
	}
	editor := f.plain
	write := func(projectKey string, body map[string]any) map[string]any {
		t.Helper()
		return obj(t, want(t, editor.post(t, "/api/v1/projects/"+projectKey+"/requirements", body), http.StatusCreated, "write"), "requirement")
	}
	scope := write(key, map[string]any{"kind": "section", "title": "Scope"})
	pay := write(key, map[string]any{"title": "Pay", "place": place(scope["id"], nil)})
	ship := write(key, map[string]any{"title": "Ship", "place": place(scope["id"], pay["id"])})
	keep := write(key, map[string]any{"title": "Keep"})
	stranger := write(other, map[string]any{"title": "Elsewhere"})
	payKey, shipKey, keepKey := pay["key"].(string), ship["key"].(string), keep["key"].(string)
	submit, ok := transitionNamed(t, editor, payKey, "Submit for review")
	if !ok {
		t.Fatal("an editor is not offered Submit for review")
	}
	bulk := "/api/v1/projects/" + key + "/transitions"

	t.Run("the read answers each requirement's transitions and leaves out what it cannot", func(t *testing.T) {
		got := list(t, want(t, editor.get(t, bulk+"?key="+payKey+"&key="+scope["key"].(string)+"&key="+stranger["key"].(string)+"&key="+key+"-R999"),
			http.StatusOK, "read the transitions"), "requirements")
		if len(got) != 1 || got[0].(map[string]any)["key"] != payKey {
			t.Fatalf("only %s is answered, not the section, another project's or one there is none of: %v", payKey, got)
		}
		names := []string{}
		for _, each := range got[0].(map[string]any)["transitions"].([]any) {
			names = append(names, each.(map[string]any)["name"].(string))
		}
		if !slices.Contains(names, "Submit for review") {
			t.Errorf("%s offers %v, without Submit for review", payKey, names)
		}
		refuse(t, "no key", editor.get(t, bulk), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a guest reads them", f.guest.get(t, bulk+"?key="+payKey), http.StatusNotFound, "not_found")
	})

	t.Run("a transition is taken on each requirement on its own, and one refused undoes nothing", func(t *testing.T) {
		got := want(t, editor.post(t, bulk, map[string]any{"transitionId": submit, "keys": []string{payKey}}), http.StatusOK, "submit one")
		if r := list(t, got, "results")[0].(map[string]any); r["outcome"] != "taken" {
			t.Fatalf("%s was not submitted: %v", payKey, r)
		}
		results := list(t, want(t, editor.post(t, bulk, map[string]any{"transitionId": submit, "keys": []string{payKey, shipKey, stranger["key"].(string)}}),
			http.StatusOK, "submit three"), "results")
		outcome := func(i int) (string, string) {
			r := results[i].(map[string]any)
			if refusal, _ := r["error"].(map[string]any); refusal != nil {
				return r["outcome"].(string), refusal["code"].(string)
			}
			return r["outcome"].(string), r["requirement"].(map[string]any)["status"].(map[string]any)["name"].(string)
		}
		for i, wanted := range [][2]string{{"refused", "transition_unavailable"}, {"taken", "In review"}, {"refused", "not_found"}} {
			if o, detail := outcome(i); o != wanted[0] || detail != wanted[1] {
				t.Errorf("result %d is %s %s, want %s %s", i, o, detail, wanted[0], wanted[1])
			}
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement r JOIN req_status st ON st.id = r.status_id WHERE r.id = $1 AND st.name = 'Draft'`, stranger["id"]); n != 1 {
			t.Error("another project's requirement moved through this project's bulk transition")
		}
	})

	t.Run("a request no requirement could pass is refused whole", func(t *testing.T) {
		refuse(t, "no keys", editor.post(t, bulk, map[string]any{"transitionId": submit, "keys": []string{}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a key twice", editor.post(t, bulk, map[string]any{"transitionId": submit, "keys": []string{keepKey, strings.ToLower(keepKey)}}),
			http.StatusUnprocessableEntity, "validation_failed")
		many := make([]string, 101)
		for i := range many {
			many[i] = fmt.Sprintf("%s-R%d", key, i+1)
		}
		refuse(t, "too many keys", editor.post(t, bulk, map[string]any{"transitionId": submit, "keys": many}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "no transition", editor.post(t, bulk, map[string]any{"keys": []string{keepKey}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a guest", f.guest.post(t, bulk, map[string]any{"transitionId": submit, "keys": []string{keepKey}}), http.StatusNotFound, "not_found")
	})

	t.Run("the database refuses a status set by hand, or a transition from where it does not start", func(t *testing.T) {
		conn := f.h.appConn(t, plainID(t, f))
		if _, err := conn.Exec(ctx, `UPDATE requirement SET status_id = (SELECT status_id FROM requirement WHERE id = $2) WHERE id = $1`,
			keep["id"], pay["id"]); !isPolicyViolation(err) {
			t.Errorf("a status was set by hand as exciter_app: %v", err)
		}
		if _, err := conn.Exec(ctx, `SELECT requirement_transition($1, $2)`, pay["id"], submit); !refusedBy(err, "requirement_transition_leaves") {
			t.Errorf("a requirement in review was submitted again through SQL: %v", err)
		}
	})

	t.Run("the document narrows to statuses and categories, with the sections above", func(t *testing.T) {
		keys := func(query string) []string {
			t.Helper()
			var out []string
			for _, each := range list(t, want(t, editor.get(t, "/api/v1/projects/"+key+"/spec?"+query), http.StatusOK, "read "+query), "entries") {
				out = append(out, each.(map[string]any)["node"].(map[string]any)["key"].(string))
			}
			return out
		}
		sameList(t, "in review", keys("category=review"), scope["key"].(string), payKey, shipKey)
		status, _ := specEntry(t, want(t, editor.get(t, "/api/v1/projects/"+key+"/spec"), http.StatusOK, "read the specification"), payKey)["node"].(map[string]any)["status"].(map[string]any)
		if status == nil || status["name"] != "In review" || status["category"] != "review" {
			t.Errorf("an entry carries its status: %v", status)
		}
		draft := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+keepKey), http.StatusOK, "read Keep"), "requirement")["status"].(map[string]any)["id"].(string)
		sameList(t, "in Draft", keys("status="+draft), keepKey)
		sameList(t, "in Draft or in review", keys("status="+draft+"&category=review"), scope["key"].(string), payKey, shipKey, keepKey)
		if total := want(t, editor.get(t, "/api/v1/projects/"+key+"/spec?category=review&limit=1"), http.StatusOK, "a window").Body["total"]; total != float64(3) {
			t.Errorf("the total counts what is kept: %v", total)
		}
		strangerStatus := obj(t, want(t, editor.get(t, "/api/v1/requirements/"+stranger["key"].(string)), http.StatusOK, "read Elsewhere"), "requirement")["status"].(map[string]any)["id"].(string)
		refuse(t, "another project's status", editor.get(t, "/api/v1/projects/"+key+"/spec?status="+strangerStatus), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a category there is none of", editor.get(t, "/api/v1/projects/"+key+"/spec?category=lost"), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a status that is no id", editor.get(t, "/api/v1/projects/"+key+"/spec?status=draft"), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("an archived project takes no bulk transition", func(t *testing.T) {
		want(t, f.owner.put(t, "/api/v1/projects/"+key+"/archive", nil), http.StatusOK, "archive the project")
		refuse(t, "a bulk transition", editor.post(t, bulk, map[string]any{"transitionId": submit, "keys": []string{keepKey}}), http.StatusConflict, "project_archived")
		want(t, f.owner.delete(t, "/api/v1/projects/"+key+"/archive"), http.StatusOK, "unarchive the project")
	})
}

// The document compares each requirement with its previous revision, its last approval or a baseline, and keeps
// only what changed when asked (#170).
func TestTheDocumentShowsWhatChanged(t *testing.T) {
	w := newCommentWorld(t, "DCH")
	key := w.key
	write := func(body map[string]any) map[string]any {
		t.Helper()
		return obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+key+"/requirements", body), http.StatusCreated, "write"), "requirement")
	}
	revise := func(reqKey string, base int, title string) {
		t.Helper()
		want(t, w.owner.patch(t, "/api/v1/requirements/"+reqKey, map[string]any{"title": title, "baseRevision": base}), http.StatusOK, "revise "+reqKey)
	}
	brakes := w.reqKey
	scope := write(map[string]any{"kind": "section", "title": "Scope"})
	pay := write(map[string]any{"title": "Pay", "place": place(scope["id"], nil)})
	ship := write(map[string]any{"title": "Ship", "place": place(scope["id"], pay["id"])})
	gone := write(map[string]any{"title": "Gone"})
	payKey, shipKey := pay["key"].(string), ship["key"].(string)
	revise(payKey, 1, "Pay by card")
	baseline := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+key+"/baselines", map[string]any{"name": "Before"}), http.StatusCreated, "take a baseline"), "baseline")
	revise(shipKey, 1, "Ship in a day")
	want(t, w.owner.post(t, "/api/v1/requirements/"+brakes+"/move", map[string]any{"place": place(scope["id"], nil)}), http.StatusOK, "move the brakes")
	want(t, w.owner.delete(t, "/api/v1/requirements/"+gone["key"].(string)), http.StatusNoContent, "trash Gone")
	refund := write(map[string]any{"title": "Refund"})

	want(t, w.owner.put(t, "/api/v1/projects/"+key+"/approval-rules", map[string]any{"required": 1,
		"approvers": []map[string]any{{"subjectType": "user", "userId": w.daveID}}}), http.StatusOK, "dave approves")
	review := obj(t, want(t, w.bob.post(t, "/api/v1/requirements/"+payKey+"/reviews", map[string]any{"revision": 2}), http.StatusCreated, "ask"), "review")
	want(t, w.dave.post(t, "/api/v1/reviews/"+review["id"].(string)+"/decisions", map[string]any{"decision": "approve", "revision": 2}), http.StatusOK, "approve")

	spec := func(query string) response {
		t.Helper()
		return want(t, w.bob.get(t, "/api/v1/projects/"+key+"/spec?"+query), http.StatusOK, "read "+query)
	}
	change := func(t *testing.T, r response, reqKey string) map[string]any {
		t.Helper()
		c, _ := specEntry(t, r, reqKey)["change"].(map[string]any)
		if c == nil {
			t.Fatalf("%s has no change: %v", reqKey, specEntry(t, r, reqKey))
		}
		return c
	}
	kindOf := func(c map[string]any) string { return fmt.Sprintf("%v %v>%v", c["kind"], c["from"], c["to"]) }
	summary := func(r response) map[string]any { return r.Body["changes"].(map[string]any) }

	t.Run("against the previous revision every requirement differs, and the title's words are marked", func(t *testing.T) {
		r := spec("changes=previous")
		if got := kindOf(change(t, r, payKey)); got != "changed 1>2" {
			t.Errorf("Pay against its previous revision: %s", got)
		}
		if got := kindOf(change(t, r, refund["key"].(string))); got != "added 0>1" {
			t.Errorf("Refund, at its first revision: %s", got)
		}
		title := change(t, r, payKey)["comparison"].(map[string]any)["title"].([]any)
		if len(title) != 1 || title[0].(map[string]any)["change"] != "modified" || !strings.Contains(fmt.Sprint(title[0]), "diffInsert") {
			t.Errorf("the title's changed words are marked: %v", title)
		}
		if specEntry(t, r, scope["key"].(string))["change"] != nil {
			t.Error("a section has a change")
		}
		if s := summary(r); s["base"] != "previous" || s["changed"] != float64(4) || len(s["removed"].([]any)) != 0 {
			t.Errorf("the summary: %v", s)
		}
	})

	t.Run("against the last approval an approved revision is unchanged and one never approved is added", func(t *testing.T) {
		r := spec("changes=approved")
		pay := change(t, r, payKey)
		if kindOf(pay) != "unchanged 2>2" || pay["comparison"] != nil {
			t.Errorf("Pay, approved at its latest revision: %v", pay)
		}
		if got := kindOf(change(t, r, shipKey)); got != "added 0>2" {
			t.Errorf("Ship, never approved: %s", got)
		}
		if s := summary(r); s["changed"] != float64(3) {
			t.Errorf("the brakes, Ship and Refund differ from their approval: %v", s)
		}
		keys := []string{}
		for _, each := range list(t, spec("changes=approved&onlyChanged=true"), "entries") {
			keys = append(keys, each.(map[string]any)["node"].(map[string]any)["key"].(string))
		}
		sameList(t, "only what changed since its approval", keys, scope["key"].(string), brakes, shipKey, refund["key"].(string))
	})

	t.Run("against a baseline it says what was revised, moved, added and removed", func(t *testing.T) {
		r := spec("changes=baseline&baseline=" + baseline["id"].(string))
		if got := kindOf(change(t, r, payKey)); got != "unchanged 2>2" {
			t.Errorf("Pay, as the baseline held it: %s", got)
		}
		if got := kindOf(change(t, r, shipKey)); got != "changed 1>2" {
			t.Errorf("Ship, revised since: %s", got)
		}
		moved := change(t, r, brakes)
		if kindOf(moved) != "moved 1>1" || moved["movedFrom"] != "1" || moved["comparison"] != nil {
			t.Errorf("the brakes, moved below Scope since: %v", moved)
		}
		if got := kindOf(change(t, r, refund["key"].(string))); got != "added 0>1" {
			t.Errorf("Refund, written since: %s", got)
		}
		s := summary(r)
		removed := s["removed"].([]any)
		if s["baselineId"] != baseline["id"] || s["changed"] != float64(3) || len(removed) != 1 || removed[0].(map[string]any)["key"] != gone["key"] ||
			removed[0].(map[string]any)["title"] != "Gone" {
			t.Errorf("the summary against the baseline: %v", s)
		}
		later := want(t, w.bob.get(t, "/api/v1/projects/"+key+"/spec?changes=baseline&baseline="+baseline["id"].(string)+"&offset=1"), http.StatusOK, "a later window")
		if len(summary(later)["removed"].([]any)) != 0 {
			t.Error("a later window lists what was removed again")
		}
	})

	t.Run("the comparison of one requirement marks its title too", func(t *testing.T) {
		c := obj(t, want(t, w.bob.get(t, "/api/v1/requirements/"+shipKey+"/diff"), http.StatusOK, "compare Ship"), "comparison")
		if title := c["title"].([]any); len(title) != 1 || !strings.Contains(fmt.Sprint(title), "diffInsert") {
			t.Errorf("Ship's comparison marks its title: %v", title)
		}
	})

	t.Run("a base there is none of, a baseline left out or not the project's, and too long a window are refused", func(t *testing.T) {
		for name, query := range map[string]string{
			"an unknown base":             "changes=lost",
			"no baseline":                 "changes=baseline",
			"a baseline there is none of": "changes=baseline&baseline=" + uuid.NewString(),
			"a baseline that is no id":    "changes=baseline&baseline=first",
			"only changed without a base": "onlyChanged=true",
			"too long a window":           "changes=previous&limit=51",
		} {
			refuse(t, name, w.bob.get(t, "/api/v1/projects/"+key+"/spec?"+query), http.StatusUnprocessableEntity, "validation_failed")
		}
	})
}

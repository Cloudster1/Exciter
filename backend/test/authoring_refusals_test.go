//go:build integration

package test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/duplicates"
	"github.com/cloudster1/exciter/backend/internal/glossary"
)

// Linking, merging, taking back an acceptance and changing a term are refused with their status and code, and leave
// every row as it was; the pairs of duplicates are paged and narrowed (#320).

// duplicateRows is what a decision about a pair may write: the decisions, the live links and the trash of a project.
type duplicateRows struct{ decisions, links, trashed int }

func duplicateRowsOf(t *testing.T, w *commentWorld) duplicateRows {
	t.Helper()
	return duplicateRows{
		decisions: w.h.countRows(t, `SELECT count(*) FROM duplicate_decision WHERE project_id = $1`, w.projectID),
		links: w.h.countRows(t, `SELECT count(*) FROM trace_link l JOIN requirement r ON r.id = l.source_id
			WHERE r.project_id = $1 AND l.deleted_at IS NULL`, w.projectID),
		trashed: w.h.countRows(t, `SELECT count(*) FROM requirement WHERE project_id = $1 AND trashed_at IS NOT NULL`, w.projectID),
	}
}

// unchangedBy fails when a refusal left the rows otherwise than it found them.
func (before duplicateRows) unchangedBy(t *testing.T, w *commentWorld, what string) {
	t.Helper()
	if after := duplicateRowsOf(t, w); after != before {
		t.Errorf("%s changed the rows from %+v to %+v", what, before, after)
	}
}

func (w *commentWorld) authored(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	return obj(t, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/requirements", body), http.StatusCreated, "write "+body["title"].(string)), "requirement")
}

func TestLinkingADuplicatePairIsRefusedWithNothingWritten(t *testing.T) {
	w := newCommentWorld(t, "DLR")
	r1, r2 := w.reqKey, w.authored(t, map[string]any{"title": "Brakes engage within 100 milliseconds"})["key"].(string)
	link := "/api/v1/projects/" + w.key + "/duplicates/link"
	before := duplicateRowsOf(t, w)

	refuse(t, "carol, who only reads, links", w.carol.post(t, link, map[string]any{"source": sideOf(r1, 1), "target": sideOf(r2, 1), "linkTypeKey": "relates"}),
		http.StatusForbidden, "forbidden")
	refuse(t, "a side at an old revision", w.bob.post(t, link, map[string]any{"source": sideOf(r1, 1), "target": sideOf(r2, 9), "linkTypeKey": "relates"}),
		http.StatusConflict, "stale_revision")
	fieldError(t, w.bob.post(t, link, map[string]any{"source": sideOf(r1, 1), "target": sideOf(r1, 1), "linkTypeKey": "relates"}), "target")
	fieldError(t, w.bob.post(t, link, map[string]any{"source": sideOf(r1, 1), "target": sideOf(r2, 1), "linkTypeKey": " "}), "linkTypeKey")
	// The type is named by its key here, so the refusal names that field, where the dialog shows it.
	fieldError(t, w.bob.post(t, link, map[string]any{"source": sideOf(r1, 1), "target": sideOf(r2, 1), "linkTypeKey": "nonsense"}), "linkTypeKey")
	before.unchangedBy(t, w, "the refused links")
	if got := pairsOf(t, w.carol, w.key, ""); !slices.Contains(got, r1+" "+r2) {
		t.Errorf("after the refusals the pairs are %v", got)
	}
}

func TestMergingADuplicatePairIsRefusedWithBothSidesKept(t *testing.T) {
	w := newCommentWorld(t, "DMR")
	r2 := w.authored(t, map[string]any{"title": "Lights shall dim within 2 seconds"})["key"].(string)
	r3 := w.authored(t, map[string]any{"title": "Lights shall dim within 2 seconds at night"})["key"].(string)
	r4 := w.authored(t, map[string]any{"title": "Dashboard shows the light level"})["key"].(string)
	want(t, w.bob.post(t, "/api/v1/requirements/"+r4+"/links", map[string]any{"typeKey": "derives", "otherKey": r3}), http.StatusCreated, "R4 derives from R3")
	other := projectKey(t, "DMO")
	want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Elsewhere"}), http.StatusCreated, "another project")
	want(t, w.owner.post(t, "/api/v1/projects/"+other+"/requirements", map[string]any{"title": "Lights shall dim within 2 seconds"}), http.StatusCreated,
		"a requirement elsewhere")
	foreign := other + "-R1"
	merge := "/api/v1/projects/" + w.key + "/duplicates/merge"
	body := func(keep, trash map[string]any, comment string) map[string]any {
		return map[string]any{"keep": keep, "trash": trash, "comment": comment}
	}
	before := duplicateRowsOf(t, w)

	refuse(t, "a side at an old revision", w.bob.post(t, merge, body(sideOf(r2, 1), sideOf(r3, 2), "")), http.StatusConflict, "stale_revision")
	fieldError(t, w.bob.post(t, merge, body(sideOf(r2, 1), sideOf(r2, 1), "")), "trash")
	fieldError(t, w.bob.post(t, merge, body(sideOf(r2, 1), sideOf(foreign, 1), "")), "trash")
	fieldError(t, w.bob.post(t, merge, body(sideOf(foreign, 1), sideOf(r3, 1), "")), "keep")
	fieldError(t, w.bob.post(t, merge, body(sideOf(r2, 1), sideOf(r3, 1), strings.Repeat("x", duplicates.MaxReasonChars+1))), "comment")
	before.unchangedBy(t, w, "the refused merges")

	want(t, w.owner.post(t, "/api/v1/requirements/"+r3+"/lock", map[string]any{"reason": "Released in 2.0"}), http.StatusOK, "the owner locks R3")
	before = duplicateRowsOf(t, w)
	r := w.bob.post(t, merge, body(sideOf(r2, 1), sideOf(r3, 1), "Night is a case of it."))
	refuse(t, "a merge that trashes a locked side", r, http.StatusConflict, "requirement_locked")
	if msg := refusalMessage(t, r); !strings.Contains(msg, "Released in 2.0") {
		t.Errorf("the refusal says %q", msg)
	}
	// The link R4 had to R3 would have been made again on R2 before the trash refused; it is undone with the rest.
	before.unchangedBy(t, w, "the merge of a locked side")
	if n := w.h.countRows(t, `SELECT count(*) FROM trace_link l JOIN requirement r ON r.id = l.target_id JOIN project p ON p.id = r.project_id
		WHERE p.key || '-R' || r.number = $1`, r2); n != 0 {
		t.Errorf("R2 holds %d links after the refused merge", n)
	}
}

// pairPage reads one page of a project's pairs as "A B", with the cursor to the next.
func pairPage(t *testing.T, c *client, projectKey, query string) ([]string, string) {
	t.Helper()
	r := want(t, c.get(t, "/api/v1/projects/"+projectKey+"/duplicates"+query), http.StatusOK, "list the pairs "+query)
	var out []string
	for _, each := range list(t, r, "pairs") {
		p := each.(map[string]any)
		out = append(out, strings.Join(slices.Sorted(slices.Values([]string{p["a"].(map[string]any)["key"].(string), p["b"].(map[string]any)["key"].(string)})), " "))
	}
	next, _ := r.Body["next"].(string)
	return out, next
}

func TestThePairsArePagedAndNarrowed(t *testing.T) {
	w := newCommentWorld(t, "DPG")
	want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/duplicate-settings", map[string]any{"threshold": duplicates.MinThreshold}), http.StatusOK,
		"list everything kept")
	r2 := w.authored(t, map[string]any{"title": "Brakes engage within 100 milliseconds"})["key"].(string)
	w.authored(t, map[string]any{"title": "Brakes engage within 100 ms on ice"})
	lighting := w.authored(t, map[string]any{"kind": "section", "title": "Lighting"})
	lamp := w.authored(t, map[string]any{"title": "Headlamps dim at dusk", "place": place(lighting["id"], nil)})
	lamps := w.authored(t, map[string]any{"title": "Headlamps dim at dusk automatically", "place": place(lighting["id"], lamp["id"])})
	brakes, headlamps := w.reqKey+" "+r2, lamp["key"].(string)+" "+lamps["key"].(string)
	all := pairsOf(t, w.carol, w.key, "")
	if len(all) < 3 || !slices.Contains(all, brakes) || !slices.Contains(all, headlamps) {
		t.Fatalf("the pairs are %v", all)
	}

	t.Run("a page at a time, each pair once, in the order of the whole list", func(t *testing.T) {
		var paged []string
		cursor := ""
		for range len(all) + 1 {
			query := "?limit=1"
			if cursor != "" {
				query += "&cursor=" + url.QueryEscape(cursor)
			}
			page, next := pairPage(t, w.carol, w.key, query)
			if len(page) != 1 {
				t.Fatalf("a page of one holds %v", page)
			}
			paged = append(paged, page...)
			if cursor = next; cursor == "" {
				break
			}
		}
		sameList(t, "the pairs a page at a time", paged, all...)
		if _, next := pairPage(t, w.carol, w.key, "?limit="+strconv.Itoa(len(all))); next != "" {
			t.Errorf("the last page still offers %q", next)
		}
	})

	t.Run("a limit outside its range, a type that is no id and a cursor nobody gave are refused", func(t *testing.T) {
		pairs := "/api/v1/projects/" + w.key + "/duplicates"
		for _, limit := range []string{"0", strconv.Itoa(duplicates.MaxLimit + 1), "many"} {
			fieldError(t, w.carol.get(t, pairs+"?limit="+limit), "limit")
		}
		fieldError(t, w.carol.get(t, pairs+"?type=brakes"), "type")
		fieldError(t, w.carol.get(t, pairs+"?cursor=-1"), "cursor")
		fieldError(t, w.carol.get(t, pairs+"?section="+url.QueryEscape("NOPE-R1")), "section")
	})

	t.Run("a section and a query keep the pairs either side of which they keep", func(t *testing.T) {
		for _, query := range []string{"?section=" + lighting["key"].(string), "?query=" + url.QueryEscape(`title ~ "Headlamps"`)} {
			got := pairsOf(t, w.carol, w.key, query)
			if !slices.Contains(got, headlamps) || slices.Contains(got, brakes) {
				t.Errorf("%s keeps %v", query, got)
			}
			for _, pair := range got {
				if !strings.Contains(pair, lamp["key"].(string)) && !strings.Contains(pair, lamps["key"].(string)) {
					t.Errorf("%s keeps %s, which has no headlamp", query, pair)
				}
			}
		}
	})
}

func TestTakingBackAnAcceptanceIsRefusedWithTheAcceptanceKept(t *testing.T) {
	w := newCommentWorld(t, "QWD")
	made := w.authored(t, map[string]any{"title": "Response", "statement": docOf("The system shall respond quickly to all requests.")})
	key := made["key"].(string)
	_, q := findingsOf(t, w.bob, key)
	vague, unbounded := findingKey(t, q, "vague"), findingKey(t, q, "unbounded")
	want(t, w.bob.post(t, "/api/v1/requirements/"+key+"/quality/accepted", map[string]any{"revision": 1, "key": vague, "reason": "Defined in the contract."}),
		http.StatusOK, "bob accepts the vague word")
	withdraw := func(c *client, findingKey, query string) response {
		return c.delete(t, "/api/v1/requirements/"+key+"/quality/accepted/"+url.PathEscape(findingKey)+query)
	}
	accepted := func() int {
		return w.h.countRows(t, `SELECT count(*) FROM quality_acceptance WHERE requirement_id = $1`, made["id"])
	}

	refuse(t, "carol, who only reads, takes it back", withdraw(w.carol, vague, "?revision=1"), http.StatusForbidden, "forbidden")
	// A key that was never accepted is refused on the key, as accepting one the revision lacks is.
	fieldError(t, withdraw(w.bob, unbounded, "?revision=1"), "key")
	fieldError(t, withdraw(w.bob, "nothing", "?revision=1"), "key")
	fieldError(t, withdraw(w.bob, vague, ""), "revision")
	fieldError(t, withdraw(w.bob, vague, "?revision=two"), "revision")
	refuse(t, "a revision that is not the current one", withdraw(w.bob, vague, "?revision=9"), http.StatusConflict, "stale_revision")
	if n := accepted(); n != 1 {
		t.Errorf("after the refusals %d acceptances are kept", n)
	}
	if found, _ := findingsOf(t, w.carol, key); found["vague: quickly"] != "accepted" || found["unbounded: all"] != "active" {
		t.Errorf("after the refusals the findings read %v", found)
	}
}

func TestChangingATermIsRefusedWithTheTermAsItWas(t *testing.T) {
	w := newCommentWorld(t, "GTR")
	terms := "/api/v1/projects/" + w.key + "/terms"
	want(t, w.owner.post(t, terms, map[string]any{"name": "Brake pedal", "synonyms": []string{"foot brake"}}), http.StatusCreated, "define Brake pedal")
	wheel := obj(t, want(t, w.bob.post(t, terms, map[string]any{"name": "Wheel", "definition": docOf("What the tyre sits on.")}), http.StatusCreated,
		"bob defines Wheel"), "term")
	path := "/api/v1/terms/" + wheel["id"].(string)
	asItWas := func(what string) {
		t.Helper()
		got := obj(t, want(t, w.carol.get(t, path), http.StatusOK, "read Wheel"), "term")
		if got["name"] != "Wheel" || got["revision"] != float64(1) || got["status"] != "proposed" {
			t.Errorf("after %s the term reads %v", what, got)
		}
	}

	t.Run("a reader, a word another term has and fields that do not hold", func(t *testing.T) {
		refuse(t, "carol, who only reads, renames it", w.carol.patch(t, path, map[string]any{"baseRevision": 1, "name": "Rim"}), http.StatusForbidden, "forbidden")
		refuse(t, "a synonym of another term as its name", w.bob.patch(t, path, map[string]any{"baseRevision": 1, "name": "Foot brake"}),
			http.StatusConflict, "term_exists")
		refuse(t, "another term's name as a synonym", w.bob.patch(t, path, map[string]any{"baseRevision": 1, "synonyms": []string{"brake pedal"}}),
			http.StatusConflict, "term_exists")
		fieldError(t, w.bob.patch(t, path, map[string]any{"baseRevision": 1, "name": " "}), "name")
		fieldError(t, w.bob.patch(t, path, map[string]any{"baseRevision": 1, "status": "retired"}), "status")
		fieldError(t, w.bob.patch(t, path, map[string]any{"baseRevision": 1, "definition": map[string]any{"type": "nonsense"}}), "definition")
		fieldError(t, w.bob.patch(t, path, map[string]any{"baseRevision": 1, "comment": strings.Repeat("c", glossary.MaxCommentChars+1)}), "comment")
		asItWas("the refusals")
	})

	t.Run("a term of an archived project is refused as archived, not as missing", func(t *testing.T) {
		archived := projectKey(t, "GTA")
		want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": archived, "name": "Old words"}), http.StatusCreated, "make a project")
		old := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+archived+"/terms", map[string]any{"name": "Drum brake"}), http.StatusCreated,
			"define Drum brake"), "term")
		want(t, w.owner.put(t, "/api/v1/projects/"+archived+"/archive", nil), http.StatusOK, "archive it")
		refuse(t, "a change in an archived project", w.owner.patch(t, "/api/v1/terms/"+old["id"].(string), map[string]any{"baseRevision": 1, "name": "Drum"}),
			http.StatusConflict, "project_archived")
		if got := obj(t, want(t, w.owner.get(t, "/api/v1/terms/"+old["id"].(string)), http.StatusOK, "read it"), "term"); got["name"] != "Drum brake" {
			t.Errorf("the archived term reads %v", got)
		}
	})

	t.Run("a delete has no input to refuse but its address", func(t *testing.T) {
		refuse(t, "carol deletes it", w.carol.delete(t, path), http.StatusForbidden, "forbidden")
		refuse(t, "an id that is no id", w.owner.delete(t, "/api/v1/terms/wheel"), http.StatusBadRequest, "bad_request")
		refuse(t, "an id nobody has", w.owner.delete(t, "/api/v1/terms/"+uuid.NewString()), http.StatusNotFound, "not_found")
		asItWas("the refused deletes")
	})

	lib := projectKey(t, "GTL")
	made := obj(t, want(t, w.owner.post(t, "/api/v1/projects", map[string]any{"key": lib, "name": "Parts"}), http.StatusCreated, "make a library"), "project")
	libID := uuid.MustParse(made["id"].(string))
	if _, err := w.h.super.Exec(t.Context(), `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, libID); err != nil {
		t.Fatal(err)
	}
	w.h.grantAs(t, libID, w.bobID, "viewer")
	want(t, w.owner.patch(t, "/api/v1/projects/"+lib, map[string]any{"settings": map[string]any{"library": true}}), http.StatusOK, "mark it a library")
	caliper := obj(t, want(t, w.owner.post(t, "/api/v1/projects/"+lib+"/terms", map[string]any{"name": "Caliper"}), http.StatusCreated, "define Caliper"), "term")
	caliperPath := "/api/v1/terms/" + caliper["id"].(string)
	want(t, w.owner.post(t, "/api/v1/projects/"+lib+"/requirements", map[string]any{"title": "Grip", "statement": docOf("The caliper shall grip.")}),
		http.StatusCreated, "a library requirement that uses it")
	want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/glossaries", map[string]any{"libraries": []string{lib}}), http.StatusOK, "use the library")
	w.h.drained(t)

	t.Run("an editor of a project that uses a library's glossary does not change the library's terms", func(t *testing.T) {
		refuse(t, "bob renames the library's term", w.bob.patch(t, caliperPath, map[string]any{"baseRevision": 1, "name": "Brake caliper"}),
			http.StatusForbidden, "forbidden")
		refuse(t, "bob deletes the library's term", w.bob.delete(t, caliperPath), http.StatusForbidden, "forbidden")
		if got := obj(t, want(t, w.bob.get(t, caliperPath), http.StatusOK, "bob reads the library's term"), "term"); got["name"] != "Caliper" || got["revision"] != float64(1) {
			t.Errorf("the library's term reads %v", got)
		}
	})

	t.Run("somebody outside a project reads neither its term's uses nor its requirement's terms", func(t *testing.T) {
		refuse(t, "carol reads the uses of the library's term", w.carol.get(t, caliperPath+"/uses"), http.StatusNotFound, "not_found")
		refuse(t, "carol reads the terms of the library's requirement", w.carol.get(t, "/api/v1/requirements/"+lib+"-R1/terms"), http.StatusNotFound, "not_found")
		if got := termsUsed(t, w.bob, lib+"-R1"); got["Caliper"] != "current" {
			t.Errorf("bob, who reads the library, sees its requirement use %v", got)
		}
	})

	t.Run("a requirement lists a library's term only to those who may read the library", func(t *testing.T) {
		r := w.authored(t, map[string]any{"title": "Grip the disc", "statement": docOf("The calipers shall grip the disc.")})["key"].(string)
		if got := termsUsed(t, w.bob, r); got["Caliper"] != "current" {
			t.Errorf("bob sees %s use %v", r, got)
		}
		if got := termsUsed(t, w.carol, r); len(got) != 0 {
			t.Errorf("carol, who may not read the library, sees %s use %v", r, got)
		}
	})
}

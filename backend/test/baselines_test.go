//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Baselines (#37): a named snapshot of a project or a section, read as a specification, compared with another or
// with now, and frozen, through the API and straight through SQL.

// baselineScene is a comment world whose R1 stands at the top, R2 is a section and R3 is below it, deriving from R1.
type baselineScene struct {
	*commentWorld
	baselines string
	sectionID string
	r3ID      string
	takenID   string
	sectionB  string
	laterID   string
}

func newBaselineScene(t *testing.T, prefix string) *baselineScene {
	t.Helper()
	w := newCommentWorld(t, prefix)
	s := &baselineScene{commentWorld: w, baselines: "/api/v1/projects/" + w.key + "/baselines"}
	reqs := "/api/v1/projects/" + w.key + "/requirements"
	section := obj(t, want(t, w.owner.post(t, reqs, map[string]any{"kind": "section", "title": "Braking"}), http.StatusCreated, "write R2"), "requirement")
	s.sectionID = section["id"].(string)
	r3 := obj(t, want(t, w.owner.post(t, reqs, map[string]any{"title": "Brake lights follow the pedal", "place": place(s.sectionID, nil)}),
		http.StatusCreated, "write R3"), "requirement")
	s.r3ID = r3["id"].(string)
	want(t, w.bob.post(t, "/api/v1/requirements/"+s.req("3")+"/links", map[string]any{"typeKey": "derives", "otherKey": s.req("1")}),
		http.StatusCreated, "R3 derives from R1")
	return s
}

func (s *baselineScene) req(n string) string { return s.key + "-R" + n }

func (s *baselineScene) of(id string) string { return "/api/v1/baselines/" + id }

// changesOf reads a comparison as key: change and how, and its links as change type source>target.
func changesOf(t *testing.T, c *client, path string) (map[string]string, []string) {
	t.Helper()
	comparison := obj(t, want(t, c.get(t, path), http.StatusOK, "compare"), "comparison")
	items := map[string]string{}
	for _, each := range comparison["items"].([]any) {
		item := each.(map[string]any)
		how := item["change"].(string)
		for _, flag := range []string{"revised", "moved", "statusChanged"} {
			if item[flag] == true {
				how += " " + flag
			}
		}
		items[item["key"].(string)] = how
	}
	var links []string
	for _, each := range comparison["links"].([]any) {
		l := each.(map[string]any)
		links = append(links, l["change"].(string)+" "+l["typeName"].(string)+" "+l["sourceKey"].(string)+">"+l["targetKey"].(string))
	}
	return items, links
}

func TestABaselineIsTakenReadAndCompared(t *testing.T) {
	s := newBaselineScene(t, "BAS")
	t.Run("the project's administrators take one of the project or of a section", s.take)
	t.Run("it reads as a specification at the revisions it holds", s.spec)
	t.Run("compared with now it says what was added, removed and changed", s.compare)
	t.Run("locked it keeps its name for good, unlocked it is deleted", s.lock)
	t.Run("a requirement purged since is named by its key", s.purged)
}

func (s *baselineScene) take(t *testing.T) {
	release := map[string]any{"name": "Release 1.0", "description": "As agreed with the customer."}
	refuse(t, "bob, who edits", s.bob.post(t, s.baselines, release), http.StatusForbidden, "forbidden")
	fieldError(t, s.owner.post(t, s.baselines, map[string]any{"name": " "}), "name")
	fieldError(t, s.owner.post(t, s.baselines, map[string]any{"name": strings.Repeat("x", 101)}), "name")
	fieldError(t, s.owner.post(t, s.baselines, map[string]any{"name": "Of a requirement", "rootKey": s.req("1")}), "rootKey")
	taken := obj(t, want(t, s.owner.post(t, s.baselines, release), http.StatusCreated, "take the baseline"), "baseline")
	s.takenID = taken["id"].(string)
	if taken["items"] != float64(3) || taken["root"] != nil || taken["locked"] != false || taken["projectKey"] != s.key ||
		taken["description"] != "As agreed with the customer." {
		t.Errorf("the baseline reads %v", taken)
	}
	refuse(t, "the same name again", s.owner.post(t, s.baselines, map[string]any{"name": "release 1.0"}), http.StatusConflict, "baseline_name_taken")
	section := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Braking 1.0", "rootKey": s.req("2")}), http.StatusCreated,
		"take the section"), "baseline")
	s.sectionB = section["id"].(string)
	if section["items"] != float64(2) || section["root"].(map[string]any)["key"] != s.req("2") {
		t.Errorf("the section's baseline reads %v", section)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM baseline_link WHERE baseline_id = $1`, s.takenID); n != 1 {
		t.Errorf("the baseline holds %d links, want R3 to R1", n)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM baseline_link WHERE baseline_id = $1`, s.sectionB); n != 0 {
		t.Errorf("the section's baseline holds %d links, though R1 is outside it", n)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'baseline.taken' AND target_id = $1`, s.takenID); n != 1 {
		t.Errorf("%d audit entries for taking the baseline", n)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM outbox_event WHERE topic = 'baseline.taken' AND payload ->> 'baselineId' = $1
		AND payload ->> 'projectId' = $2`, s.takenID, s.projectID.String()); n != 1 {
		t.Errorf("%d events for taking the baseline", n)
	}
	listed := list(t, want(t, s.carol.get(t, s.baselines), http.StatusOK, "carol lists them"), "baselines")
	if len(listed) != 2 || listed[0].(map[string]any)["id"] != s.sectionB {
		t.Errorf("the baselines, newest first, are %v", listed)
	}
	if got := obj(t, want(t, s.carol.get(t, s.of(s.takenID)), http.StatusOK, "carol reads one"), "baseline"); got["name"] != "Release 1.0" || got["items"] != float64(3) {
		t.Errorf("one baseline reads %v", got)
	}
	refuse(t, "a baseline that is none", s.carol.get(t, s.of(uuid.NewString())), http.StatusNotFound, "not_found")
}

func (s *baselineScene) spec(t *testing.T) {
	want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("3"), map[string]any{"title": "Brake lights follow the pedal at once", "baseRevision": 1}),
		http.StatusOK, "bob revises R3")
	body := want(t, s.carol.get(t, s.of(s.takenID)+"/spec"), http.StatusOK, "carol reads the baseline")
	entries := list(t, body, "entries")
	var read []string
	for _, each := range entries {
		e := each.(map[string]any)
		r := e["requirement"].(map[string]any)
		read = append(read, e["outline"].(string)+" "+r["key"].(string)+" "+r["title"].(string))
	}
	sameList(t, "the baseline's outline", read, "1 "+s.req("1")+" Brakes engage within 100 ms", "2 "+s.req("2")+" Braking",
		"2.1 "+s.req("3")+" Brake lights follow the pedal")
	if r3 := entries[2].(map[string]any); r3["requirement"].(map[string]any)["revision"] != float64(1) || r3["statement"] == nil || r3["depth"] != float64(1) {
		t.Errorf("R3 in the baseline reads %v", r3)
	}
	section := list(t, want(t, s.carol.get(t, s.of(s.sectionB)+"/spec"), http.StatusOK, "the section's baseline"), "entries")
	if len(section) != 2 || section[0].(map[string]any)["depth"] != float64(0) || section[1].(map[string]any)["outline"] != "2.1" {
		t.Errorf("the section's baseline reads %v", section)
	}
	refuse(t, "too long a window", s.carol.get(t, s.of(s.takenID)+"/spec?limit=501"), http.StatusUnprocessableEntity, "validation_failed")
}

func (s *baselineScene) compare(t *testing.T) {
	reqs := "/api/v1/projects/" + s.key + "/requirements"
	want(t, s.owner.post(t, reqs, map[string]any{"title": "Brake fluid is checked", "place": place(s.sectionID, s.r3ID)}), http.StatusCreated, "write R4")
	want(t, s.owner.delete(t, "/api/v1/requirements/"+s.req("1")), http.StatusNoContent, "trash R1")
	items, links := changesOf(t, s.carol, s.of(s.takenID)+"/compare")
	if len(items) != 3 || items[s.req("1")] != "removed" || items[s.req("3")] != "changed revised" || items[s.req("4")] != "added" {
		t.Errorf("against now the baseline differs by %v", items)
	}
	sameList(t, "the links against now", links, "removed Derives "+s.req("3")+">"+s.req("1"))
	later := obj(t, want(t, s.owner.post(t, s.baselines, map[string]any{"name": "Release 1.1"}), http.StatusCreated, "take another"), "baseline")
	s.laterID = later["id"].(string)
	between, _ := changesOf(t, s.carol, s.of(s.takenID)+"/compare?with="+s.laterID)
	if len(between) != 3 || between[s.req("4")] != "added" {
		t.Errorf("between the two baselines %v", between)
	}
	if same, links := changesOf(t, s.carol, s.of(s.laterID)+"/compare?with=now"); len(same) != 0 || len(links) != 0 {
		t.Errorf("a baseline just taken differs from now by %v %v", same, links)
	}
	fieldError(t, s.carol.get(t, s.of(s.takenID)+"/compare?with=yesterday"), "with")
	fieldError(t, s.carol.get(t, s.of(s.takenID)+"/compare?with="+uuid.NewString()), "with")
}

func (s *baselineScene) lock(t *testing.T) {
	refuse(t, "bob locks it", s.bob.patch(t, s.of(s.takenID), map[string]any{"locked": true}), http.StatusForbidden, "forbidden")
	renamed := obj(t, want(t, s.owner.patch(t, s.of(s.takenID), map[string]any{"name": "Release 1.0 final"}), http.StatusOK, "rename it"), "baseline")
	if renamed["name"] != "Release 1.0 final" {
		t.Errorf("the renamed baseline reads %v", renamed)
	}
	refuse(t, "a name another has", s.owner.patch(t, s.of(s.takenID), map[string]any{"name": "Release 1.1"}), http.StatusConflict, "baseline_name_taken")
	locked := obj(t, want(t, s.owner.patch(t, s.of(s.takenID), map[string]any{"locked": true}), http.StatusOK, "lock it"), "baseline")
	if locked["locked"] != true || locked["lockedAt"] == nil {
		t.Errorf("the locked baseline reads %v", locked)
	}
	refuse(t, "renaming it locked", s.owner.patch(t, s.of(s.takenID), map[string]any{"name": "Changed"}), http.StatusConflict, "baseline_locked")
	refuse(t, "unlocking it", s.owner.patch(t, s.of(s.takenID), map[string]any{"locked": false}), http.StatusConflict, "baseline_locked")
	refuse(t, "deleting it locked", s.owner.delete(t, s.of(s.takenID)), http.StatusConflict, "baseline_locked")
	if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'baseline.locked' AND target_id = $1`, s.takenID); n != 1 {
		t.Errorf("%d audit entries for locking the baseline", n)
	}
	refuse(t, "bob deletes one", s.bob.delete(t, s.of(s.laterID)), http.StatusForbidden, "forbidden")
	want(t, s.owner.delete(t, s.of(s.laterID)), http.StatusNoContent, "delete the unlocked one")
	refuse(t, "it is gone", s.carol.get(t, s.of(s.laterID)), http.StatusNotFound, "not_found")
	if n := s.h.countRows(t, `SELECT count(*) FROM baseline_item WHERE baseline_id = $1`, s.laterID); n != 0 {
		t.Errorf("%d items outlived their baseline", n)
	}
}

func (s *baselineScene) purged(t *testing.T) {
	items := list(t, want(t, s.owner.get(t, "/api/v1/projects/"+s.key+"/trash"), http.StatusOK, "open the trash"), "items")
	want(t, s.owner.delete(t, "/api/v1/projects/"+s.key+"/trash/"+items[0].(map[string]any)["id"].(string)), http.StatusNoContent, "purge R1")
	entries := list(t, want(t, s.carol.get(t, s.of(s.takenID)+"/spec"), http.StatusOK, "read the baseline"), "entries")
	if first := entries[0].(map[string]any); first["requirement"].(map[string]any)["key"] != s.req("1") || first["statement"] != nil {
		t.Errorf("R1, purged, reads %v", first)
	}
	if items, _ := changesOf(t, s.carol, s.of(s.takenID)+"/compare"); items[s.req("1")] != "removed" {
		t.Errorf("against now R1 is %q", items[s.req("1")])
	}
}

// A change committed while a baseline is taken is wholly out of it: the service is held between its items and its
// links, while bob revises R1 and links another requirement to it.
func TestATakenBaselineIsOneSnapshot(t *testing.T) {
	s := newBaselineScene(t, "BAT")
	h := s.h
	ctx := context.Background()
	hold, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := hold.Exec(ctx, `LOCK TABLE baseline_link IN EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan response, 1)
	go func() { done <- s.owner.post(t, s.baselines, map[string]any{"name": "Mid-flight"}) }()
	deadline := time.Now().Add(drainWait)
	for h.countRows(t, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%INSERT INTO baseline_link%'`) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("taking the baseline never reached its links")
		}
		time.Sleep(workerIdle)
	}
	want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("1"), map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}),
		http.StatusOK, "bob revises R1 meanwhile")
	want(t, s.bob.post(t, "/api/v1/requirements/"+s.req("3")+"/links", map[string]any{"typeKey": "verifies", "otherKey": s.req("1")}),
		http.StatusCreated, "and links R3 to it again")
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	taken := obj(t, want(t, <-done, http.StatusCreated, "the baseline is taken"), "baseline")
	id := taken["id"].(string)
	if n := h.countRows(t, `SELECT count(*) FROM baseline_link WHERE baseline_id = $1`, id); n != 1 {
		t.Errorf("the baseline holds %d links, want only the one made before it began", n)
	}
	if n := h.countRows(t, `SELECT count(*) FROM baseline_item WHERE baseline_id = $1 AND revision = 1`, id); n != 3 {
		t.Errorf("%d items hold their first revision, want all 3", n)
	}
	items, links := changesOf(t, s.carol, s.of(id)+"/compare")
	if len(items) != 1 || items[s.req("1")] != "changed revised" {
		t.Errorf("against now the baseline differs by %v", items)
	}
	sameList(t, "the links against now", links, "added Verifies "+s.req("3")+">"+s.req("1"))
}

// Straight through SQL as exciter_app: only a project's administrators take a baseline, its rows are written only
// as it is taken and never change, and a locked baseline keeps its name and stays.
func TestRawSQLKeepsBaselinesFrozen(t *testing.T) {
	s := newBaselineScene(t, "BAQ")
	h := s.h
	ctx := context.Background()
	h.settle(t)
	owner, bob, carol := h.appConn(t, s.ownerID), h.appConn(t, s.bobID), h.appConn(t, s.carolID)
	insert := `INSERT INTO baseline (project_id, name, created_by) VALUES ($1, $2, $3) RETURNING id`
	var id uuid.UUID
	if err := bob.QueryRow(ctx, insert, s.projectID, "Bob's", s.bobID).Scan(&id); !isPolicyViolation(err) {
		t.Errorf("an editor took a baseline: %v", err)
	}
	if err := owner.QueryRow(ctx, insert, s.projectID, "In bob's name", s.bobID).Scan(&id); !isPolicyViolation(err) {
		t.Errorf("a baseline was taken in another's name: %v", err)
	}
	item := `INSERT INTO baseline_item (baseline_id, requirement_id, number, kind, revision, rank, path) VALUES ($1, $2, 1, 'requirement', $3, 'a', '{1}')`
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, insert, s.projectID, "By hand", s.ownerID).Scan(&id); err != nil {
		t.Fatalf("the owner could not take a baseline: %v", err)
	}
	if _, err := tx.Exec(ctx, item, id, s.reqID, 1); err != nil {
		t.Fatalf("the owner could not fill it as it was taken: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, item, id, s.r3ID, 1); !refusedBy(err, "baseline_frozen") {
		t.Errorf("a baseline was filled after it was taken: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE baseline_item SET revision = 2 WHERE baseline_id = $1`, id); !isPermissionDenied(err) {
		t.Errorf("the owner changed an item: %v", err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE baseline_item SET revision = 2 WHERE baseline_id = $1`, id); !refusedBy(err, "baseline_frozen") {
		t.Errorf("an item was changed at all: %v", err)
	}
	if _, err := h.super.Exec(ctx, `DELETE FROM baseline_item WHERE baseline_id = $1`, id); !refusedBy(err, "baseline_frozen") {
		t.Errorf("an item was deleted without its baseline: %v", err)
	}
	tx, err = owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var other uuid.UUID
	if err := tx.QueryRow(ctx, insert, s.projectID, "Made up", s.ownerID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, item, other, s.reqID, 9); !refusedBy(err, "baseline_item_revision") {
		t.Errorf("a baseline held a revision R1 never had: %v", err)
	}
	_ = tx.Rollback(ctx)
	if n := affects(t, carol, `UPDATE baseline SET name = 'Mine' WHERE id = $1`, id); n != 0 {
		t.Error("a reader renamed a baseline")
	}
	var n int
	if err := carol.QueryRow(ctx, `SELECT count(*) FROM baseline_item WHERE baseline_id = $1`, id).Scan(&n); err != nil || n != 1 {
		t.Errorf("a reader sees %d items: %v", n, err)
	}
	if n := affects(t, owner, `UPDATE baseline SET locked_at = now(), locked_by = $2 WHERE id = $1`, id, s.ownerID); n != 1 {
		t.Fatal("the owner could not lock the baseline")
	}
	if _, err := owner.Exec(ctx, `UPDATE baseline SET name = 'Renamed' WHERE id = $1`, id); !refusedBy(err, "baseline_locked") {
		t.Errorf("a locked baseline was renamed: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE baseline SET locked_at = NULL WHERE id = $1`, id); !refusedBy(err, "baseline_locked") {
		t.Errorf("a locked baseline was unlocked: %v", err)
	}
	if _, err := owner.Exec(ctx, `DELETE FROM baseline WHERE id = $1`, id); !refusedBy(err, "baseline_locked") {
		t.Errorf("a locked baseline was deleted: %v", err)
	}
	if _, err := h.super.Exec(ctx, `DELETE FROM project WHERE id = $1`, s.projectID); err != nil {
		t.Errorf("a project with a locked baseline could not be deleted: %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM baseline WHERE id = $1`, id); n != 0 {
		t.Error("the locked baseline outlived its project")
	}
}

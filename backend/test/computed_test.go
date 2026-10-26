//go:build integration

package test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/baseline"
)

// Computed attributes (#213): worked out from a formula by the worker, from a requirement's own fields, its children
// and its links, again whenever what they read changes, read like any attribute and recorded by baselines.

type computedScene struct {
	*commentWorld
	weight, cost, score, total, verified string
	sectionA, sectionB                   map[string]any
}

func (s *computedScene) attribute(t *testing.T, body map[string]any) string {
	t.Helper()
	return obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/attributes", body), http.StatusCreated, "make "+body["name"].(string)), "attribute")["id"].(string)
}

func (s *computedScene) write(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	return obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", body), http.StatusCreated, "write "+body["title"].(string)), "requirement")
}

// newComputedScene is a project with Weight and Cost typed, Score worked out from Weight, Total summing the Cost of a
// section's children, and Verified counting what verifies a requirement; two sections, A holding R4 and R5.
func newComputedScene(t *testing.T, prefix string) *computedScene {
	t.Helper()
	s := &computedScene{commentWorld: newCommentWorld(t, prefix)}
	s.weight = s.attribute(t, map[string]any{"name": "Weight", "kind": "number"})
	s.cost = s.attribute(t, map[string]any{"name": "Cost", "kind": "number"})
	s.score = s.attribute(t, map[string]any{"name": "Score", "formula": `"Weight" * 2 + if(priority = 'must', 10, 0)`})
	s.total = s.attribute(t, map[string]any{"name": "Total", "formula": `sum(children, "Cost")`, "onSections": true})
	s.verified = s.attribute(t, map[string]any{"name": "Verified by", "formula": `count(links('verifies', in))`})
	s.sectionA = s.write(t, map[string]any{"title": "Brakes", "kind": "section"})
	s.sectionB = s.write(t, map[string]any{"title": "Lights", "kind": "section"})
	for _, r := range []struct {
		title  string
		weight float64
		cost   float64
	}{{"Pedal travel", 4, 10}, {"Pedal force", 1, 5}} {
		made := s.write(t, map[string]any{"title": r.title, "attributes": map[string]any{s.weight: r.weight, s.cost: r.cost}})
		want(t, s.owner.post(t, "/api/v1/requirements/"+made["key"].(string)+"/move", map[string]any{"place": place(s.sectionA["id"], nil)}),
			http.StatusOK, "move "+r.title+" into Brakes")
	}
	return s
}

// computed reads a requirement's computed value of an attribute as text, "-" when it has none.
func computed(t *testing.T, c *client, key, attr string) string {
	t.Helper()
	r := obj(t, want(t, c.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement")
	v, ok := r["computed"].(map[string]any)[attr]
	if !ok {
		return "-"
	}
	return fmt.Sprint(v)
}

// eventually waits for the worker to work a value out, as the stack's worker does a moment after a change.
func eventually(t *testing.T, c *client, key, attr, want string) {
	t.Helper()
	deadline := time.Now().Add(importWait)
	for {
		got := computed(t, c, key, attr)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s's %s is %s after %s, want %s", key, attr, got, importWait, want)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestComputedAttributesFollowWhatTheyRead(t *testing.T) {
	s := newComputedScene(t, "CMP")
	a, b := s.sectionA["key"].(string), s.sectionB["key"].(string)
	travel, force := s.key+"-R4", s.key+"-R5"

	t.Run("a value is worked out from the requirement's own fields and attributes", func(t *testing.T) {
		eventually(t, s.carol, travel, s.score, "8")
		eventually(t, s.carol, force, s.score, "2")
		want(t, s.owner.patch(t, "/api/v1/requirements/"+travel, map[string]any{"priority": "must"}), http.StatusOK, "make R4 a must")
		eventually(t, s.carol, travel, s.score, "18")
		want(t, s.owner.patch(t, "/api/v1/requirements/"+travel, map[string]any{"attributes": map[string]any{s.weight: 5}, "baseRevision": 1}),
			http.StatusOK, "weigh R4 more")
		eventually(t, s.carol, travel, s.score, "20")
		if got := computed(t, s.carol, a, s.score); got != "-" {
			t.Errorf("a section has a Score of %s, which shows on requirements alone", got)
		}
	})
	t.Run("a section sums its children, and a child moving away leaves the sum of its old section", func(t *testing.T) {
		eventually(t, s.carol, a, s.total, "15")
		eventually(t, s.carol, b, s.total, "0")
		want(t, s.owner.post(t, "/api/v1/requirements/"+force+"/move", map[string]any{"place": place(s.sectionB["id"], nil)}), http.StatusOK, "move R5 to Lights")
		eventually(t, s.carol, a, s.total, "10")
		eventually(t, s.carol, b, s.total, "5")
	})
	t.Run("a link made and removed counts on its other end", func(t *testing.T) {
		eventually(t, s.carol, travel, s.verified, "0")
		link := obj(t, want(t, s.owner.post(t, "/api/v1/requirements/"+force+"/links", map[string]any{"typeKey": "verifies", "otherKey": travel}),
			http.StatusCreated, "R5 verifies R4"), "link")
		eventually(t, s.carol, travel, s.verified, "1")
		want(t, s.owner.delete(t, "/api/v1/links/"+link["id"].(string)), http.StatusNoContent, "remove the link")
		eventually(t, s.carol, travel, s.verified, "0")
	})
	t.Run("changing a formula works the project out again, saying how far it has got", func(t *testing.T) {
		changed := obj(t, want(t, s.owner.patch(t, "/api/v1/projects/"+s.key+"/attributes/"+s.score, map[string]any{"formula": `"weight" * 3`}),
			http.StatusOK, "change Score's formula"), "attribute")
		if changed["formula"] != `"Weight" * 3` || changed["kind"] != "number" {
			t.Errorf("the changed attribute reads %v", changed)
		}
		eventually(t, s.carol, travel, s.score, "15")
		deadline := time.Now().Add(importWait)
		for {
			answer := want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/attributes"), http.StatusOK, "the attributes")
			if answer.Body["recompute"] == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the recompute still reads %v", answer.Body["recompute"])
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
	t.Run("the query language finds and orders by a computed value, and documents show it", func(t *testing.T) {
		query := url.QueryEscape(`"Score" >= 10 ORDER BY "Score" DESC`)
		if keys := rowKeys(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+query), http.StatusOK, "query")); strings.Join(keys, ",") != travel {
			t.Errorf(`"Score" >= 10 keeps %v`, keys)
		}
		grid := list(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(`kind = requirement`)), http.StatusOK, "the grid"), "rows")
		for _, each := range grid {
			row := each.(map[string]any)
			if row["key"] == travel && fmt.Sprint(row["attributes"].(map[string]any)[s.score]) != "15" {
				t.Errorf("the grid's row reads %v", row)
			}
		}
		doc := obj(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/document"), http.StatusOK, "the document"), "document")
		found := false
		for _, each := range doc["entries"].([]any) {
			e := each.(map[string]any)
			for _, p := range e["properties"].([]any) {
				prop := p.(map[string]any)
				if e["key"] == travel && prop["name"] == "Score" && prop["text"] == "15" {
					found = true
				}
			}
		}
		if !found {
			t.Error("the document does not show R4's Score")
		}
	})
	t.Run("a baseline records the values it saw, and its hash is the one Go computes", func(t *testing.T) {
		taken := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "With scores"}), http.StatusCreated, "take a baseline"), "baseline")
		id := uuid.MustParse(taken["id"].(string))
		if n := s.h.countRows(t, `SELECT count(*) FROM baseline_computed WHERE baseline_id = $1 AND attribute_id = $2`, id, uuid.MustParse(s.score)); n != 2 {
			t.Errorf("the baseline holds %d Scores, want 2", n)
		}
		want(t, s.owner.patch(t, "/api/v1/projects/"+s.key+"/attributes/"+s.score, map[string]any{"formula": `"Weight" * 4`}), http.StatusOK, "change Score again")
		eventually(t, s.carol, travel, s.score, "20")
		doc := obj(t, want(t, s.carol.get(t, "/api/v1/baselines/"+id.String()+"/document"), http.StatusOK, "the baseline's document"), "document")
		for _, each := range doc["entries"].([]any) {
			e := each.(map[string]any)
			for _, p := range e["properties"].([]any) {
				if prop := p.(map[string]any); e["key"] == travel && prop["name"] == "Score" && prop["text"] != "15" {
					t.Errorf("the baseline shows R4's Score as %v, want the 15 it saw", prop["text"])
				}
			}
		}
		conn := s.h.appConn(t, s.ownerID)
		c := baseline.Content{BaselineID: id, ProjectID: s.projectID}
		rows, err := conn.Query(context.Background(), `SELECT kind, a, b, c, revision, hash, path FROM baseline_content_rows($1)`, id)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var (
				kind     string
				a        uuid.UUID
				b, tt    *uuid.UUID
				revision *int
				hash     *string
				path     []int32
			)
			if err := rows.Scan(&kind, &a, &b, &tt, &revision, &hash, &path); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "item":
				item := baseline.ContentItem{RequirementID: a, Revision: *revision, Path: path}
				if hash != nil {
					item.Hash = *hash
				}
				c.Items = append(c.Items, item)
			case "link":
				c.Links = append(c.Links, baseline.ContentLink{TypeID: a, SourceID: *b, TargetID: *tt})
			case "computed":
				c.Computed = append(c.Computed, baseline.ContentEvidence{RequirementID: a, Line: *hash})
			}
		}
		rows.Close()
		var hash string
		if err := s.h.super.QueryRow(context.Background(), `SELECT baseline_content_hash($1)`, id).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		if len(c.Computed) == 0 || baseline.ContentHash(c) != hash {
			t.Errorf("Go hashes the baseline as %s, the database as %s, over %d computed lines", baseline.ContentHash(c), hash, len(c.Computed))
		}
	})
}

func TestAFormulaIsCheckedAndRefusedWhereItCannotHold(t *testing.T) {
	s := newComputedScene(t, "CMF")
	attrs := "/api/v1/projects/" + s.key + "/attributes"
	check := "/api/v1/projects/" + s.key + "/formula-check"

	t.Run("the check says what is wrong and where, what a formula gives, and its value for a requirement", func(t *testing.T) {
		bad := obj(t, want(t, s.owner.post(t, check, map[string]any{"formula": `"Weight" * 'two'`}), http.StatusOK, "check a bad formula"), "check")
		errs := bad["errors"].([]any)
		if len(errs) != 1 || errs[0].(map[string]any)["from"] != float64(12) || !strings.Contains(errs[0].(map[string]any)["message"].(string), "number") {
			t.Errorf("the bad formula reads %v", bad)
		}
		good := obj(t, want(t, s.owner.post(t, check, map[string]any{"formula": `"weight" + sum(children, "Cost")`, "requirementKey": s.key + "-R4"}),
			http.StatusOK, "check a good formula"), "check")
		if good["kind"] != "number" || good["formula"] != `"Weight" + sum(children, "Cost")` || fmt.Sprint(good["value"]) != "4" ||
			fmt.Sprint(good["reads"]) != "[Weight Cost]" {
			t.Errorf("the good formula reads %v", good)
		}
		fieldError(t, s.owner.post(t, check, map[string]any{"formula": "1", "requirementKey": s.key + "-R999"}), "requirementKey")
		refuse(t, "bob, an editor", s.bob.post(t, check, map[string]any{"formula": "1"}), http.StatusForbidden, "forbidden")
	})
	t.Run("a cycle is refused with the attributes named, and a formula never reads itself on one requirement", func(t *testing.T) {
		half := s.attribute(t, map[string]any{"name": "Half", "formula": `"Score" / 2`})
		answer := s.owner.patch(t, attrs+"/"+s.score, map[string]any{"formula": `"Half" + 1`})
		fieldError(t, answer, "formula")
		if msg := answer.Body["error"].(map[string]any)["fields"].(map[string]any)["formula"].(string); !strings.Contains(msg, "Half reads Score, which reads Half.") {
			t.Errorf("the cycle is said as %q", msg)
		}
		fieldError(t, s.owner.post(t, attrs, map[string]any{"name": "Self", "formula": `"Self" + 1`}), "formula")
		// A roll-up of itself over the children descends, and is safe.
		s.attribute(t, map[string]any{"name": "Tree cost", "formula": `coalesce("Cost", 0) + sum(children, "Tree cost")`, "onSections": true})
		want(t, s.owner.delete(t, attrs+"/"+half), http.StatusNoContent, "delete Half")
	})
	t.Run("an attribute a formula reads stays, and a computed one takes no typed value nor turns typed", func(t *testing.T) {
		refuse(t, "deleting Weight", s.owner.delete(t, attrs+"/"+s.weight), http.StatusConflict, "attribute_in_formula")
		refuse(t, "retiring Weight", s.owner.patch(t, attrs+"/"+s.weight, map[string]any{"retired": true}), http.StatusConflict, "attribute_in_formula")
		fieldError(t, s.owner.patch(t, "/api/v1/requirements/"+s.key+"-R4", map[string]any{"attributes": map[string]any{s.score: 3}, "baseRevision": 1}),
			"attributes."+s.score)
		fieldError(t, s.owner.patch(t, attrs+"/"+s.weight, map[string]any{"formula": "1"}), "formula")
		fieldError(t, s.owner.post(t, attrs, map[string]any{"name": "Must", "formula": "1", "required": true}), "required")
	})
	t.Run("a link type a formula reads itself along stays acyclic", func(t *testing.T) {
		s.attribute(t, map[string]any{"name": "Depth", "formula": `1 + coalesce(max(links('derives', out), "Depth"), 0)`})
		var derives string
		if err := s.h.super.QueryRow(context.Background(), `SELECT id FROM link_type WHERE key = 'derives' AND project_id IS NULL`).Scan(&derives); err != nil {
			t.Fatal(err)
		}
		refuse(t, "letting derives cycle", s.owner.patch(t, "/api/v1/link-types/"+derives, map[string]any{"acyclic": false}), http.StatusConflict, "type_in_formula")
		// Whoever may not change the type hears only that, nothing of the formulas that read it.
		answer := s.bob.patch(t, "/api/v1/link-types/"+derives, map[string]any{"acyclic": false})
		refuse(t, "bob, who does not manage derives, letting it cycle", answer, http.StatusForbidden, "forbidden")
		if strings.Contains(string(answer.Raw), "Depth") {
			t.Errorf("the refusal names the formula: %s", answer.Raw)
		}
	})
}

// Straight through SQL as exciter_app: computed values are the worker's alone, never typed, and a formula stays.
func TestRawSQLKeepsComputedValuesToTheWorker(t *testing.T) {
	s := newComputedScene(t, "CMR")
	eventually(t, s.carol, s.key+"-R4", s.score, "8")
	ctx := context.Background()
	owner := s.h.appConn(t, s.ownerID)
	var r4 uuid.UUID
	if err := s.h.super.QueryRow(ctx, `SELECT id FROM requirement WHERE project_id = $1 AND number = 4`, s.projectID).Scan(&r4); err != nil {
		t.Fatal(err)
	}
	refusedOutright(t, owner, "writing a computed value", `UPDATE requirement_computed SET "values" = '{}' WHERE requirement_id = $1`, r4)
	refusedOutright(t, owner, "adding one", `INSERT INTO computed_dirty (requirement_id, project_id) VALUES ($1, $2)`, r4, s.projectID)
	if _, err := owner.Exec(ctx, `UPDATE requirement SET attributes = attributes || jsonb_build_object($2::text, 3) WHERE id = $1`, r4, s.score); !refusedBy(err, "requirement_computed_untyped") {
		t.Errorf("typing a computed value: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE attribute_def SET formula = NULL WHERE id = $1`, s.score); !refusedBy(err, "attribute_def_formula_fixed") && !isCheckViolation(err) {
		t.Errorf("turning a computed attribute typed: %v", err)
	}
	carol := s.h.appConn(t, s.carolID)
	if _, err := carol.Exec(ctx, `SELECT computed_recompute($1)`, s.projectID); err == nil {
		t.Error("a reader recomputed the project")
	}
	var seen int
	if err := s.h.appConn(t, s.daveID).QueryRow(ctx, `SELECT count(*)::int FROM requirement_computed WHERE project_id = $1`, s.projectID).Scan(&seen); err != nil || seen == 0 {
		t.Errorf("a reviewer of the project sees %d computed rows, %v", seen, err)
	}
	outsider := s.h.addPerson(t, "member")
	if _, err := s.h.super.Exec(ctx, `DELETE FROM project_grant WHERE project_id = $1 AND subject_type = 'everyone'`, s.projectID); err != nil {
		t.Fatal(err)
	}
	if err := s.h.appConn(t, outsider).QueryRow(ctx, `SELECT count(*)::int FROM requirement_computed WHERE project_id = $1`, s.projectID).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("somebody outside the project sees %d computed rows, %v", seen, err)
	}
}

// Changes go through while workers work values out (#248): a mark is only appended, so a move never waits on the
// marks a worker holds, and the sums settle where the requirements end up.
func TestChangesGoThroughWhileValuesAreWorkedOut(t *testing.T) {
	s := newComputedScene(t, "CMQ")
	a := s.sectionA["key"].(string)
	var keys []string
	for i := range 6 {
		made := s.write(t, map[string]any{"title": fmt.Sprintf("Shoe %d", i), "attributes": map[string]any{s.cost: 1}})
		keys = append(keys, made["key"].(string))
	}

	ctx, stop := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	for range 3 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			recomputer := attribute.NewRecomputer(s.h.cluster, slog.New(slog.DiscardHandler), time.Millisecond)
			for ctx.Err() == nil {
				_, _ = recomputer.Once(ctx)
			}
		}()
	}

	var movers sync.WaitGroup
	for _, key := range keys {
		movers.Add(1)
		go func() {
			defer movers.Done()
			for round := range 8 {
				to := s.sectionA["id"]
				if round%2 == 0 {
					to = s.sectionB["id"]
				}
				if got := s.owner.post(t, "/api/v1/requirements/"+key+"/move", map[string]any{"place": place(to, nil)}); got.Status != http.StatusOK {
					t.Errorf("move %s in round %d: got %d: %s", key, round, got.Status, got.Raw)
					return
				}
			}
		}()
	}
	movers.Wait()
	stop()
	workers.Wait()
	// Every shoe ends in Brakes beside Pedal travel (10) and Pedal force (5).
	eventually(t, s.carol, a, s.total, "21")
}

// A formula calling today() is worked out again once the day moves on, though nothing it reads changed (#360).
func TestAValueReadingTodayMovesOnWithTheDay(t *testing.T) {
	s := &computedScene{commentWorld: newCommentWorld(t, "CMD")}
	ctx := context.Background()
	t.Cleanup(func() {
		// Only the clock below runs ahead; whatever it turned goes back to the day the stack's worker keeps.
		s.h.cleanupExec(t, s.h.super, `DELETE FROM computed_day WHERE day > (now() AT TIME ZONE 'UTC')::date`)
	})
	due := s.attribute(t, map[string]any{"name": "Due", "kind": "date"})
	left := s.attribute(t, map[string]any{"name": "Days left", "formula": `days(today(), "Due")`})
	now := time.Now().UTC()
	r := s.write(t, map[string]any{"title": "Brakes are serviced", "attributes": map[string]any{due: now.AddDate(0, 0, 10).Format(time.DateOnly)}})
	key := r["key"].(string)
	eventually(t, s.carol, key, left, "10")

	timeless := newCommentWorld(t, "CMN")
	(&computedScene{commentWorld: timeless}).attribute(t, map[string]any{"name": "Score", "formula": `len(title)`})

	log := slog.New(slog.DiscardHandler)
	tomorrow := attribute.NewDayTurner(s.h.cluster, log, time.Minute, func() time.Time { return now.AddDate(0, 0, 1) })
	if n, err := tomorrow.Once(ctx); err != nil || n < 1 {
		t.Fatalf("turning to tomorrow marked %d projects: %v", n, err)
	}
	eventually(t, s.carol, key, left, "9")

	if n, err := tomorrow.Once(ctx); err != nil || n != 0 {
		t.Errorf("turning to tomorrow again marked %d projects: %v", n, err)
	}
	if n, err := attribute.NewDayTurner(s.h.cluster, log, time.Minute, nil).Once(ctx); err != nil || n != 0 {
		t.Errorf("a worker whose clock is still at today marked %d projects: %v", n, err)
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM computed_day WHERE project_id = $1`, timeless.projectID); n != 0 {
		t.Errorf("a project whose formulas do not call today() was turned %d times", n)
	}
	want(t, s.owner.patch(t, "/api/v1/requirements/"+key, map[string]any{"attributes": map[string]any{due: now.AddDate(0, 0, 20).Format(time.DateOnly)},
		"baseRevision": 1}), http.StatusOK, "move R2's due date")
	eventually(t, s.carol, key, left, "19")
}

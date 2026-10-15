//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Coverage and the traceability matrix (#38): rules say what each type is asked for, the levels follow the links
// and the Armature statuses, suspect links count as partial, and the matrix lays the links out.

// coverageScene is a comment world where R2 derives from R1 and R3 verifies it, R1 is implemented by WEB-1 and
// verified by WEB-2, both done, R2 is implemented by WEB-3, still open, and R4 is a note.
type coverageScene struct {
	*commentWorld
	noteType string
	reqIDs   map[string]uuid.UUID
}

func (s *coverageScene) req(n string) string { return s.key + "-R" + n }

func newCoverageScene(t *testing.T, prefix string) *coverageScene {
	t.Helper()
	s := &coverageScene{commentWorld: newCommentWorld(t, prefix), reqIDs: map[string]uuid.UUID{}}
	s.h.keepArmature(t)
	want(t, s.owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	s.reqIDs["1"] = uuid.MustParse(s.reqID)
	s.noteType = obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/types", map[string]any{"name": "Note", "icon": "test", "colour": "#22aa55"}), http.StatusCreated,
		"make the type Note"), "type")["id"].(string)
	for _, body := range []map[string]any{
		{"title": "Brake pedal reads within 10 ms"},
		{"title": "Brake test bench run"},
		{"title": "Brakes were discussed at length", "typeId": s.noteType},
	} {
		made := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", body), http.StatusCreated, "write a requirement"), "requirement")
		s.reqIDs[strings.TrimPrefix(made["key"].(string), s.key+"-R")] = uuid.MustParse(made["id"].(string))
	}
	for from, kind := range map[string]string{"2": "derives", "3": "verifies"} {
		want(t, s.bob.post(t, "/api/v1/requirements/"+s.req(from)+"/links", map[string]any{"typeKey": kind, "otherKey": s.req("1")}),
			http.StatusCreated, "link R"+from+" to R1")
	}
	for _, l := range []struct{ req, relation, issue, category string }{
		{"1", "implements", "WEB-1", "done"}, {"1", "verifies", "WEB-2", "done"}, {"2", "implements", "WEB-3", "in_progress"},
	} {
		if _, err := s.h.super.Exec(t.Context(), `INSERT INTO armature_issue_link (requirement_id, relation, issue_key, status_category)
			VALUES ($1, $2, $3, $4)`, s.reqIDs[l.req], l.relation, l.issue, l.category); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// coverage reads the report as c with the query given: the totals, each row's three levels, and how many rows.
func (s *coverageScene) coverage(t *testing.T, c *client, query string) (map[string]any, map[string]string, float64) {
	t.Helper()
	answer := want(t, c.get(t, "/api/v1/projects/"+s.key+"/coverage"+query), http.StatusOK, "the coverage")
	summary := obj(t, answer, "summary")
	rows := map[string]string{}
	for _, each := range list(t, answer, "rows") {
		row := each.(map[string]any)
		key := row["requirement"].(map[string]any)["key"].(string)
		rows[key] = row["decomposed"].(string) + " " + row["implemented"].(string) + " " + row["verified"].(string)
	}
	return summary, rows, answer.Body["total"].(float64)
}

// totals are one dimension's full, partial, none and total.
func totals(summary map[string]any, dimension string) [4]float64 {
	d := summary[dimension].(map[string]any)
	return [4]float64{d["full"].(float64), d["partial"].(float64), d["none"].(float64), d["total"].(float64)}
}

func TestCoverageFollowsTheRulesAndTheLinks(t *testing.T) {
	s := newCoverageScene(t, "COV")
	rules := "/api/v1/projects/" + s.key + "/coverage-rules"
	t.Run("without a rule nothing is asked", func(t *testing.T) {
		summary, rows, total := s.coverage(t, s.carol, "")
		if total != 4 || rows[s.req("1")] != "na na na" || totals(summary, "verified") != [4]float64{} {
			t.Errorf("without rules the report reads %v, %v, %v", summary, rows, total)
		}
	})
	t.Run("only the project's administrators set a rule, of one of its own types", func(t *testing.T) {
		refuse(t, "bob, an editor", s.bob.put(t, rules, map[string]any{"decomposed": true}), http.StatusForbidden, "forbidden")
		fieldError(t, s.owner.put(t, rules, map[string]any{"typeId": uuid.NewString(), "verified": true}), "typeId")
		want(t, s.owner.put(t, rules, map[string]any{"decomposed": true, "implemented": true, "verified": true}), http.StatusOK, "the rule for every type")
		note := obj(t, want(t, s.owner.put(t, rules, map[string]any{"typeId": s.noteType}), http.StatusOK, "the rule for notes"), "rule")
		if note["typeName"] != "Note" || note["verified"] != false {
			t.Errorf("the note's rule reads %v", note)
		}
		listed := list(t, want(t, s.carol.get(t, rules), http.StatusOK, "carol reads the rules"), "rules")
		if len(listed) != 2 || listed[0].(map[string]any)["typeId"] != nil {
			t.Errorf("the rules read %v, the one for every type first", listed)
		}
	})
	t.Run("the levels and totals follow the links, Armature's statuses among them", func(t *testing.T) {
		summary, rows, total := s.coverage(t, s.carol, "")
		wantRows := map[string]string{
			s.req("1"): "full full full", s.req("2"): "none partial none", s.req("3"): "none none none", s.req("4"): "na na na",
		}
		for key, levels := range wantRows {
			if rows[key] != levels {
				t.Errorf("%s reads %q, want %q", key, rows[key], levels)
			}
		}
		if total != 4 || totals(summary, "decomposed") != [4]float64{1, 0, 2, 3} || totals(summary, "implemented") != [4]float64{1, 1, 1, 3} ||
			totals(summary, "verified") != [4]float64{1, 0, 2, 3} {
			t.Errorf("the totals read %v of %v", summary, total)
		}
	})
	t.Run("the report narrows like the grid, keeps rows at a level and pages them", func(t *testing.T) {
		summary, rows, total := s.coverage(t, s.carol, "?verified=none&limit=1&offset=1")
		if total != 2 || len(rows) != 1 || rows[s.req("3")] == "" || totals(summary, "verified")[3] != 3 {
			t.Errorf("unverified, second page: %v, %v of %v", summary, rows, total)
		}
		_, rows, total = s.coverage(t, s.carol, "?type="+s.noteType)
		if total != 1 || rows[s.req("4")] != "na na na" {
			t.Errorf("notes only: %v of %v", rows, total)
		}
		if _, _, total = s.coverage(t, s.carol, "?kind=section"); total != 0 {
			t.Errorf("sections were counted: %v", total)
		}
		fieldError(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/coverage?verified=most"), "verified")
	})
	t.Run("a revision turns the links it moved past partial, once the worker has cleared the cache", func(t *testing.T) {
		want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("1"), map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}),
			http.StatusOK, "bob revises R1")
		s.h.drained(t)
		if _, rows, _ := s.coverage(t, s.carol, ""); rows[s.req("1")] != "partial partial partial" {
			t.Errorf("R1 after its revision reads %q", rows[s.req("1")])
		}
	})
	t.Run("a rule removed asks what the rule for every type asks, at once", func(t *testing.T) {
		var id string
		for _, each := range list(t, want(t, s.owner.get(t, rules), http.StatusOK, "the rules"), "rules") {
			if r := each.(map[string]any); r["typeId"] == s.noteType {
				id = r["id"].(string)
			}
		}
		refuse(t, "carol, a reader", s.carol.delete(t, "/api/v1/coverage-rules/"+id), http.StatusForbidden, "forbidden")
		want(t, s.owner.delete(t, "/api/v1/coverage-rules/"+id), http.StatusNoContent, "remove the note's rule")
		if _, rows, _ := s.coverage(t, s.carol, ""); rows[s.req("4")] != "none none none" {
			t.Errorf("the note without its own rule reads %q", rows[s.req("4")])
		}
		refuse(t, "a rule removed already", s.owner.delete(t, "/api/v1/coverage-rules/"+id), http.StatusNotFound, "not_found")
	})
	t.Run("the rules of a project nobody may see are not found", func(t *testing.T) {
		refuse(t, "an unknown project", s.carol.get(t, "/api/v1/projects/NOPE"+s.key+"/coverage-rules"), http.StatusNotFound, "not_found")
	})
}

func TestTheMatrixLaysOutTheLinks(t *testing.T) {
	s := newCoverageScene(t, "MTX")
	matrix := "/api/v1/projects/" + s.key + "/matrix"
	t.Run("rows against the requirements they link to, either way", func(t *testing.T) {
		m := obj(t, want(t, s.carol.get(t, matrix), http.StatusOK, "the matrix"), "matrix")
		if m["total"] != float64(4) || len(m["rows"].([]any)) != 4 {
			t.Fatalf("the matrix reads %v", m)
		}
		var columns []string
		for _, c := range m["columns"].([]any) {
			columns = append(columns, c.(map[string]any)["requirement"].(map[string]any)["key"].(string))
		}
		if strings.Join(columns, " ") != strings.Join([]string{s.req("1"), s.req("2"), s.req("3")}, " ") {
			t.Errorf("the columns read %v", columns)
		}
		cells := map[[2]float64]string{}
		for _, c := range m["cells"].([]any) {
			cell := c.(map[string]any)
			var names []string
			for _, l := range cell["links"].([]any) {
				names = append(names, l.(map[string]any)["name"].(string))
			}
			cells[[2]float64{cell["row"].(float64), cell["column"].(float64)}] = strings.Join(names, ",")
		}
		wantCells := map[[2]float64]string{{0, 1}: "Derives", {0, 2}: "Verifies", {1, 0}: "Derives", {2, 0}: "Verifies"}
		if len(cells) != len(wantCells) {
			t.Errorf("the cells read %v", cells)
		}
		for at, names := range wantCells {
			if cells[at] != names {
				t.Errorf("the cell at %v reads %q, want %q", at, cells[at], names)
			}
		}
		fieldError(t, s.carol.get(t, matrix+"?columnProject=NOPE"+s.key), "columnProject")
	})
	t.Run("rows against the Armature issues they are traced to, a page at a time", func(t *testing.T) {
		m := obj(t, want(t, s.carol.get(t, matrix+"?columns=issues&limit=1"), http.StatusOK, "the issue matrix"), "matrix")
		columns := m["columns"].([]any)
		if len(m["rows"].([]any)) != 1 || len(columns) != 2 || columns[0].(map[string]any)["issueKey"] != "WEB-1" || m["total"] != float64(4) {
			t.Errorf("the first page of the issue matrix reads %v", m)
		}
	})
	t.Run("the file holds every row, a column per issue and the relations in the cells", func(t *testing.T) {
		file := want(t, s.carol.get(t, matrix+".csv?columns=issues"), http.StatusOK, "the matrix file")
		records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(file.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 5 || strings.Join(records[0], ",") != "Key,Title,WEB-1,WEB-2,WEB-3" ||
			records[1][2] != "implements" || records[1][3] != "verifies" || records[2][4] != "implements" {
			t.Errorf("the file reads %v", records)
		}
		fieldError(t, s.carol.get(t, matrix+".csv?columns=everything"), "columns")
	})
}

func TestRawSQLKeepsCoverageRulesToAdministrators(t *testing.T) {
	s := newCoverageScene(t, "COQ")
	other := newCoverageScene(t, "COR")
	h := s.h
	ctx := context.Background()
	h.settle(t)
	owner, bob, carol := h.appConn(t, s.ownerID), h.appConn(t, s.bobID), h.appConn(t, s.carolID)
	insert := `INSERT INTO coverage_rule (project_id, type_id, verified) VALUES ($1, $2, true)`
	if _, err := bob.Exec(ctx, insert, s.projectID, nil); !isPolicyViolation(err) {
		t.Errorf("an editor set a coverage rule: %v", err)
	}
	if _, err := owner.Exec(ctx, insert, s.projectID, other.noteType); !isPolicyViolation(err) {
		t.Errorf("a rule took another project's type: %v", err)
	}
	if _, err := owner.Exec(ctx, insert, s.projectID, s.noteType); err != nil {
		t.Fatalf("the owner could not set a rule: %v", err)
	}
	var seen int
	if err := carol.QueryRow(ctx, `SELECT count(*) FROM coverage_rule WHERE project_id = $1`, s.projectID).Scan(&seen); err != nil || seen != 1 {
		t.Errorf("carol, a reader, sees %d rules: %v", seen, err)
	}
	if tag, err := carol.Exec(ctx, `UPDATE coverage_rule SET verified = false WHERE project_id = $1`, s.projectID); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("carol changed a rule: %v, %v", tag, err)
	}
	if _, err := owner.Exec(ctx, `UPDATE coverage_rule SET project_id = $2 WHERE project_id = $1`, s.projectID, other.projectID); !isPermissionDenied(err) {
		t.Errorf("a rule moved to another project: %v", err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE project SET archived_at = now() WHERE id = $1`, s.projectID); err != nil {
		t.Fatal(err)
	}
	if tag, err := owner.Exec(ctx, `DELETE FROM coverage_rule WHERE project_id = $1`, s.projectID); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("a rule of an archived project was removed: %v, %v", tag, err)
	}
}

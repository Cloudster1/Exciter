//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
)

// baselineCells reads a baseline matrix's cells by row and column key, each link as its name, then its change and
// whether it was suspect, "?" where the baseline did not record it.
func baselineCells(t *testing.T, m map[string]any) map[string]string {
	t.Helper()
	rows, columns := m["rows"].([]any), m["columns"].([]any)
	cells := map[string]string{}
	for _, c := range m["cells"].([]any) {
		cell := c.(map[string]any)
		row := rows[int(cell["row"].(float64))].(map[string]any)["key"].(string)
		column := columns[int(cell["column"].(float64))].(map[string]any)
		name, _ := column["issueKey"].(string)
		if req, ok := column["requirement"].(map[string]any); ok {
			name = req["key"].(string)
		}
		var links []string
		for _, l := range cell["links"].([]any) {
			link := l.(map[string]any)
			read := link["name"].(string)
			if change, _ := link["change"].(string); change != "" {
				read += " " + change
			}
			switch link["suspect"] {
			case true:
				read += " suspect"
			case nil:
				read += " ?"
			}
			links = append(links, read)
		}
		cells[row+":"+name] = strings.Join(links, ",")
	}
	return cells
}

func TestABaselineKeepsItsMatrix(t *testing.T) {
	s := newCoverageScene(t, "BLM")
	if _, err := s.h.super.Exec(t.Context(), `INSERT INTO armature_issue_link (requirement_id, relation, issue_key, status_category, broken_at)
		VALUES ($1, 'implements', 'WEB-8', 'todo', now())`, s.reqIDs["4"]); err != nil {
		t.Fatal(err)
	}
	taken := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "Signed off"}), http.StatusCreated, "take a baseline"), "baseline")
	matrix := "/api/v1/baselines/" + taken["id"].(string) + "/matrix"
	read := func(query string) map[string]any {
		t.Helper()
		return obj(t, want(t, s.carol.get(t, matrix+query), http.StatusOK, "the baseline matrix "+query), "matrix")
	}
	t.Run("a baseline taken now records its links with their suspect state and status", func(t *testing.T) {
		m := read("")
		got := baselineCells(t, m)
		if taken["detail"] != float64(2) || m["detail"] != float64(2) || got[s.req("1")+":"+s.req("2")] != "Derives" || got[s.req("3")+":"+s.req("1")] != "Verifies" {
			t.Errorf("the baseline's matrix reads %v at detail %v", got, m["detail"])
		}
		issues := read("?columns=issues")
		var keys []string
		for _, c := range issues["columns"].([]any) {
			keys = append(keys, c.(map[string]any)["issueKey"].(string))
		}
		link := issues["cells"].([]any)[0].(map[string]any)["links"].([]any)[0].(map[string]any)
		if strings.Join(keys, " ") != "WEB-1 WEB-2 WEB-3" || link["status"] != "done" {
			t.Errorf("the issues read %v, the first link %v; a broken link is not recorded", keys, link)
		}
		fieldError(t, s.carol.get(t, matrix+"?columns=issues&linkType=derives"), "linkType")
	})
	t.Run("compared with now, each link says whether it was added, removed or changed", func(t *testing.T) {
		want(t, s.bob.patch(t, "/api/v1/requirements/"+s.req("1"), map[string]any{"title": "Brakes engage within 80 ms", "baseRevision": 1}),
			http.StatusOK, "bob revises R1")
		for _, l := range list(t, want(t, s.bob.get(t, "/api/v1/requirements/"+s.req("3")+"/links"), http.StatusOK, "R3's links"), "links") {
			want(t, s.bob.delete(t, "/api/v1/links/"+l.(map[string]any)["id"].(string)), http.StatusNoContent, "bob removes R3's link")
		}
		want(t, s.bob.post(t, "/api/v1/requirements/"+s.req("4")+"/links", map[string]any{"typeKey": "relates", "otherKey": s.req("2")}), http.StatusCreated, "relate R4 to R2")
		got := baselineCells(t, read("?with=now"))
		for at, link := range map[string]string{
			s.req("1") + ":" + s.req("2"): "Derives changed suspect",
			s.req("3") + ":" + s.req("1"): "Verifies removed",
			s.req("4") + ":" + s.req("2"): "Relates added",
		} {
			if got[at] != link {
				t.Errorf("the cell %s reads %q, want %q (all: %v)", at, got[at], link, got)
			}
		}
		file := want(t, s.carol.get(t, matrix+".csv?with=now"), http.StatusOK, "the compared file")
		records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(file.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(records[3], ","), "Verifies (removed)") {
			t.Errorf("the compared file reads %v", records)
		}
		fieldError(t, s.carol.get(t, matrix+"?with=yesterday"), "with")
	})
	t.Run("a baseline taken before says what it did not record", func(t *testing.T) {
		ctx := context.Background()
		tx, err := s.h.super.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO baseline (project_id, name, created_by) VALUES ($1, 'Old', $2) RETURNING id`, s.projectID, s.ownerID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{
			`INSERT INTO baseline_item (baseline_id, requirement_id, number, kind, revision, parent_id, rank, path)
			 SELECT $1, r.id, r.number, r.kind, r.revision, r.parent_id, r.rank, ARRAY[r.number] FROM requirement r WHERE r.project_id = $2`,
			`INSERT INTO baseline_link (baseline_id, link_id, type_id, type_name, source_id, target_id)
			 SELECT $1, l.id, l.type_id, t.name, l.source_id, l.target_id FROM trace_link l JOIN link_type t ON t.id = l.type_id
			 JOIN requirement r ON r.id = l.source_id WHERE r.project_id = $2 AND l.deleted_at IS NULL`,
		} {
			if _, err := tx.Exec(ctx, q, id, s.projectID); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		s.h.settle(t)
		old := obj(t, want(t, s.carol.get(t, "/api/v1/baselines/"+id+"/matrix"), http.StatusOK, "the old baseline's matrix"), "matrix")
		if old["detail"] != float64(1) || baselineCells(t, old)[s.req("1")+":"+s.req("2")] != "Derives ?" {
			t.Errorf("the old baseline's matrix reads %v at detail %v", baselineCells(t, old), old["detail"])
		}
		if pages := obj(t, want(t, s.carol.get(t, "/api/v1/baselines/"+id+"/matrix?columns=pages"), http.StatusOK, "its pages"), "matrix"); len(pages["columns"].([]any)) != 0 {
			t.Errorf("an old baseline has page columns %v", pages["columns"])
		}
	})
	t.Run("what a baseline recorded never changes, straight through SQL", func(t *testing.T) {
		s.h.settle(t)
		owner := s.h.appConn(t, s.ownerID)
		ctx := context.Background()
		if _, err := owner.Exec(ctx, `UPDATE baseline SET detail = 1 WHERE id = $1`, taken["id"]); !isPermissionDenied(err) {
			t.Errorf("a baseline's detail was changed: %v", err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO baseline_page_link (baseline_id, link_id, requirement_id, relation, page_id, space_key, url, suspect)
			VALUES ($1, gen_random_uuid(), $2, 'specifies', gen_random_uuid(), 'ENG', 'https://stator.example.com', false)`, taken["id"], s.reqIDs["1"]); err == nil {
			t.Error("a page link was put into a baseline after it was taken")
		}
	})
}

//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// traceScene is a coverage scene with a model's worth of types and requirements: N1 and N2 are needs, S1 and S2
// systems derived from N1, T1 a test verifying S1, traced to the Armature issue WEB-9; N2 and S2 lead nowhere.
type traceScene struct {
	*coverageScene
	types map[string]string
	keys  map[string]string
}

func newTraceScene(t *testing.T, prefix string) *traceScene {
	t.Helper()
	s := &traceScene{coverageScene: newCoverageScene(t, prefix), types: map[string]string{}, keys: map[string]string{}}
	for _, name := range []string{"Need", "System", "Test"} {
		s.types[name] = obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/types", map[string]any{"name": name, "icon": "test", "colour": "#2255aa"}),
			http.StatusCreated, "make the type "+name), "type")["id"].(string)
	}
	for _, r := range []struct{ name, typ, title string }{
		{"N1", "Need", "Stop within 40 m"}, {"N2", "Need", "Sound a horn"}, {"S1", "System", "Brakes engage within 100 ms"},
		{"S2", "System", "Brake lights follow the pedal"}, {"T1", "Test", "Brake bench run"},
	} {
		made := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": r.title, "typeId": s.types[r.typ]}),
			http.StatusCreated, "write "+r.name), "requirement")
		s.keys[r.name] = made["key"].(string)
	}
	for _, l := range []struct{ from, kind, to string }{{"S1", "derives", "N1"}, {"S2", "derives", "N1"}, {"T1", "verifies", "S1"}} {
		want(t, s.bob.post(t, "/api/v1/requirements/"+s.keys[l.from]+"/links", map[string]any{"typeKey": l.kind, "otherKey": s.keys[l.to]}),
			http.StatusCreated, "link "+l.from+" to "+l.to)
	}
	if _, err := s.h.super.Exec(t.Context(), `INSERT INTO armature_issue_link (requirement_id, relation, issue_key, status_category)
		SELECT id, 'implements', 'WEB-9', 'done' FROM requirement WHERE project_id = $1 AND title = 'Brake bench run'`, s.projectID); err != nil {
		t.Fatal(err)
	}
	return s
}

// linkTypeID is the id of a link type of the installation, by its key.
func (s *traceScene) linkTypeID(t *testing.T, key string) string {
	t.Helper()
	var id string
	if err := s.h.super.QueryRow(t.Context(), `SELECT id FROM link_type WHERE key = $1 AND project_id IS NULL`, key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// model is need, system, test and issue, each reached from the one before as the links read.
func (s *traceScene) model(t *testing.T) map[string]any {
	return map[string]any{"levels": []map[string]any{
		{"name": "Need", "kind": "requirements", "typeIds": []string{s.types["Need"]}},
		{"name": "System", "kind": "requirements", "typeIds": []string{s.types["System"]}, "steps": []map[string]any{{"typeId": s.linkTypeID(t, "derives"), "direction": "incoming"}}},
		{"name": "Test", "kind": "requirements", "typeIds": []string{s.types["Test"]}, "steps": []map[string]any{{"typeId": s.linkTypeID(t, "verifies"), "direction": "incoming"}}},
		{"name": "Issue", "kind": "issues", "steps": []map[string]any{{"relation": "implements"}}},
	}}
}

// lines reads a chain report's lines as the keys they pass, and broken where one ends early.
func lines(t *testing.T, c map[string]any) []string {
	t.Helper()
	var out []string
	for _, each := range c["lines"].([]any) {
		line := each.(map[string]any)
		var keys []string
		for _, n := range line["nodes"].([]any) {
			node := n.(map[string]any)
			if req, ok := node["requirement"].(map[string]any); ok {
				keys = append(keys, req["key"].(string))
			} else {
				keys = append(keys, node["issueKey"].(string))
			}
		}
		if line["broken"] == true {
			keys = append(keys, "broken")
		}
		out = append(out, strings.Join(keys, " "))
	}
	return out
}

func TestATraceModelIsSetAndItsChainsFollowed(t *testing.T) {
	s := newTraceScene(t, "TMC")
	model := "/api/v1/projects/" + s.key + "/trace-model"
	chains := "/api/v1/projects/" + s.key + "/trace-chains"
	t.Run("a project starts without a model, and its chains are none", func(t *testing.T) {
		m := obj(t, want(t, s.carol.get(t, model), http.StatusOK, "carol reads the model"), "model")
		if len(m["levels"].([]any)) != 0 || m["can"].(map[string]any)["edit"] != false {
			t.Errorf("an unset model reads %v for carol", m)
		}
		if c := obj(t, want(t, s.carol.get(t, chains), http.StatusOK, "the chains"), "chains"); len(c["lines"].([]any)) != 0 {
			t.Errorf("chains without a model read %v", c)
		}
	})
	t.Run("only an administrator sets it, and a shape that does not hold is refused on its field", func(t *testing.T) {
		refuse(t, "bob, an editor", s.bob.put(t, model, s.model(t)), http.StatusForbidden, "forbidden")
		bad := s.model(t)
		bad["levels"].([]map[string]any)[1]["steps"] = nil
		fieldError(t, s.owner.put(t, model, bad), "levels[1].steps")
		bad = s.model(t)
		bad["levels"].([]map[string]any)[0]["typeIds"] = []string{s.noteType, "0195f000-0000-7000-8000-000000000000"}
		fieldError(t, s.owner.put(t, model, bad), "levels[0].typeIds")
	})
	t.Run("the model is saved whole and read as set", func(t *testing.T) {
		m := obj(t, want(t, s.owner.put(t, model, s.model(t)), http.StatusOK, "the owner sets the model"), "model")
		levels := m["levels"].([]any)
		system := levels[1].(map[string]any)
		step := system["steps"].([]any)[0].(map[string]any)
		if len(levels) != 4 || system["types"].([]any)[0].(map[string]any)["name"] != "System" || step["typeName"] != "Derives" || step["direction"] != "incoming" {
			t.Errorf("the saved model reads %v", m)
		}
		if read := obj(t, want(t, s.carol.get(t, model), http.StatusOK, "carol reads it"), "model"); len(read["levels"].([]any)) != 4 || read["updatedBy"] == nil {
			t.Errorf("carol reads %v", read)
		}
	})
	t.Run("the chains follow the levels, and say where they break", func(t *testing.T) {
		c := obj(t, want(t, s.carol.get(t, chains), http.StatusOK, "the chains"), "chains")
		k := s.keys
		got := fmt.Sprint(lines(t, c))
		wantLines := fmt.Sprint([]string{k["N1"] + " " + k["S1"] + " " + k["T1"] + " WEB-9", k["N1"] + " " + k["S2"] + " broken", k["N2"] + " broken"})
		if got != wantLines || c["broken"] != float64(2) || c["total"] != float64(2) {
			t.Errorf("the chains read %s with %v broken of %v roots, want %s", got, c["broken"], c["total"], wantLines)
		}
		if only := obj(t, want(t, s.carol.get(t, chains+"?onlyBroken=true"), http.StatusOK, "only the broken"), "chains"); len(only["lines"].([]any)) != 2 {
			t.Errorf("only the broken chains read %v", lines(t, only))
		}
		mid := obj(t, want(t, s.carol.get(t, chains+"?level=1&level=2"), http.StatusOK, "system to test"), "chains")
		if got := fmt.Sprint(lines(t, mid)); got != fmt.Sprint([]string{k["S1"] + " " + k["T1"], k["S2"] + " broken"}) {
			t.Errorf("system to test reads %s", got)
		}
		fieldError(t, s.carol.get(t, chains+"?level=0&level=2"), "level")
		fieldError(t, s.carol.get(t, chains+"?level=3"), "level")
	})
	t.Run("the file has a column per level and where a line breaks", func(t *testing.T) {
		file := want(t, s.carol.get(t, chains+".csv"), http.StatusOK, "the chain file")
		records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(file.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(records[0], ",") != "Need,System,Test,Issue,Breaks after" || len(records) != 4 || records[2][4] != "System" || records[1][3] != "WEB-9" {
			t.Errorf("the file reads %v", records)
		}
	})
	t.Run("clearing the model leaves no chains", func(t *testing.T) {
		want(t, s.owner.put(t, model, map[string]any{"levels": []any{}}), http.StatusOK, "the owner clears the model")
		if c := obj(t, want(t, s.carol.get(t, chains), http.StatusOK, "the chains"), "chains"); len(c["lines"].([]any)) != 0 {
			t.Errorf("chains of a cleared model read %v", c)
		}
	})
}

func TestRawSQLKeepsTraceModelsToAdministrators(t *testing.T) {
	s := newTraceScene(t, "TMQ")
	other := newCoverageScene(t, "TMR")
	h := s.h
	ctx := context.Background()
	h.settle(t)
	owner, bob, carol := h.appConn(t, s.ownerID), h.appConn(t, s.bobID), h.appConn(t, s.carolID)
	insert := `INSERT INTO trace_level (project_id, position, name, kind) VALUES ($1, 0, 'Need', 'requirements') RETURNING id`
	var id string
	if err := bob.QueryRow(ctx, insert, s.projectID).Scan(&id); !isPolicyViolation(err) {
		t.Errorf("an editor set a trace level: %v", err)
	}
	if err := owner.QueryRow(ctx, insert, s.projectID).Scan(&id); err != nil {
		t.Fatalf("the owner could not set a trace level: %v", err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO trace_level_type (level_id, project_id, type_id) VALUES ($1, $2, $3)`, id, s.projectID, other.noteType); !isPolicyViolation(err) {
		t.Errorf("a level took another project's type: %v", err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO trace_level_type (level_id, project_id, type_id) VALUES ($1, $2, $3)`, id, s.projectID, s.types["Need"]); err != nil {
		t.Errorf("the owner could not give the level its type: %v", err)
	}
	var seen int
	if err := carol.QueryRow(ctx, `SELECT count(*) FROM trace_level WHERE project_id = $1`, s.projectID).Scan(&seen); err != nil || seen != 1 {
		t.Errorf("carol, a reader, sees %d levels: %v", seen, err)
	}
	if _, err := owner.Exec(ctx, `UPDATE trace_level SET name = 'Wish' WHERE id = $1`, id); !isPermissionDenied(err) {
		t.Errorf("a level was changed in place rather than the model replaced: %v", err)
	}
	if tag, err := carol.Exec(ctx, `DELETE FROM trace_level WHERE project_id = $1`, s.projectID); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("carol removed a level: %v, %v", tag, err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE project SET archived_at = now() WHERE id = $1`, s.projectID); err != nil {
		t.Fatal(err)
	}
	if tag, err := owner.Exec(ctx, `DELETE FROM trace_level WHERE project_id = $1`, s.projectID); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("a level of an archived project was removed: %v, %v", tag, err)
	}
}

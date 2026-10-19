//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/risk"
)

// riskScheme is a scheme of n values a scale, judged by the product of severity and probability: high from 12,
// medium from 5, low below; with detection, a priority number with the same levels from 1, 20 and 60.
func riskScheme(n int, method string, detection bool) map[string]any {
	scale := func(d string) map[string]any {
		values := []any{}
		for v := 1; v <= n; v++ {
			values = append(values, map[string]any{"value": v, "name": fmt.Sprintf("%s %d", d, v)})
		}
		return map[string]any{"dimension": d, "values": values}
	}
	scales := []any{scale("severity"), scale("probability")}
	levels := []any{
		map[string]any{"name": "Low", "colour": "#16a34a", "acceptable": true},
		map[string]any{"name": "Medium", "colour": "#d97706", "minRpn": 20},
		map[string]any{"name": "High", "colour": "#dc2626", "minRpn": 60},
	}
	if detection {
		scales = append(scales, scale("detection"))
	}
	matrix := []any{}
	if method == "matrix" {
		for s := 1; s <= n; s++ {
			for p := 1; p <= n; p++ {
				level := "Low"
				switch {
				case s*p >= 12:
					level = "High"
				case s*p >= 5:
					level = "Medium"
				}
				matrix = append(matrix, map[string]any{"severity": s, "probability": p, "level": level})
			}
		}
	}
	return map[string]any{"method": method, "detection": detection, "scales": scales, "levels": levels, "matrix": matrix}
}

func side(severity, probability any) map[string]any {
	return map[string]any{"severity": severity, "probability": probability, "detection": nil}
}

type riskScene struct {
	f        *projectFixture
	key      string
	hazard   string
	plain    string
	riskKey  string
	plainKey string
}

// newRiskScene makes a project with a type of risks, a five by five scheme, a plain requirement and a risk rated
// 4 and 3 before mitigation, 2 and 3 after.
func newRiskScene(t *testing.T, prefix string) *riskScene {
	t.Helper()
	f := newProjectFixture(t)
	s := &riskScene{f: f, key: projectKey(t, prefix)}
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": s.key, "name": "Brakes"}), http.StatusCreated, "make the project")
	s.hazard = obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+s.key+"/types", map[string]any{
		"name": "Hazard", "icon": "risk", "colour": "#dc2626", "role": "risk"}), http.StatusCreated, "make a type of risks"), "type")["id"].(string)
	s.plain = obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+s.key), http.StatusOK, "read the project"), "project")["settings"].(map[string]any)["defaultTypeId"].(string)
	want(t, f.owner.put(t, "/api/v1/projects/"+s.key+"/risk-scheme", riskScheme(5, "matrix", false)), http.StatusOK, "set the scheme")
	want(t, f.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Stop on ice", "statement": statementSaying("The vehicle stops.")}),
		http.StatusCreated, "write the requirement")
	want(t, f.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{
		"title": "Brakes fail on ice", "typeId": s.hazard, "statement": statementSaying("The wheels lock."),
		"risk": map[string]any{"before": side(4, 3), "after": side(2, 3)},
	}), http.StatusCreated, "write the risk")
	s.plainKey, s.riskKey = s.key+"-R1", s.key+"-R2"
	return s
}

func ratingOf(t *testing.T, r map[string]any) map[string]any {
	t.Helper()
	v, ok := r["risk"].(map[string]any)
	if !ok {
		t.Fatalf("%v carries no rating: %v", r["key"], r["risk"])
	}
	return v
}

func levelName(v any) string {
	if l, ok := v.(map[string]any); ok {
		return l["name"].(string)
	}
	return ""
}

// A risk is rated on its project's scheme: the level follows from the matrix, the rating is part of each revision,
// history compares it value by value, queries ask for it, and a scheme keeps the values ratings use.
func TestARiskIsRatedOnItsProjectsScheme(t *testing.T) {
	s := newRiskScene(t, "RSK")
	owner := s.f.owner
	refuse(t, "a reader outside the project", s.f.outside.get(t, "/api/v1/projects/"+s.key+"/risk-scheme"), http.StatusNotFound, "not_found")
	scheme := obj(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/risk-scheme"), http.StatusOK, "read the scheme"), "scheme")
	if scheme["method"] != "matrix" || len(scheme["matrix"].([]any)) != 25 || len(scheme["levels"].([]any)) != 3 {
		t.Fatalf("the scheme reads %v", scheme)
	}
	severity := scheme["scales"].([]any)[0].(map[string]any)
	if severity["label"] != "Severity" || fmt.Sprint(severity["held"]) != "[2 4]" {
		t.Errorf("the severity scale reads %v", severity)
	}

	r := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.riskKey), http.StatusOK, "read the risk"), "requirement")
	v := ratingOf(t, r)
	if levelName(v["beforeLevel"]) != "High" || levelName(v["afterLevel"]) != "Medium" || v["beforeRpn"] != float64(12) || v["state"] != "unmitigated" ||
		levelName(v["level"]) != "High" {
		t.Errorf("the rating reads %v", v)
	}
	if plain := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.plainKey), http.StatusOK, "read the requirement"), "requirement"); plain["risk"] != nil {
		t.Errorf("a requirement of another type carries %v", plain["risk"])
	}
	refuse(t, "a rating on a requirement", owner.patch(t, "/api/v1/requirements/"+s.plainKey, map[string]any{"baseRevision": 1,
		"risk": map[string]any{"before": side(1, 1)}}), http.StatusUnprocessableEntity, "validation_failed")
	refuse(t, "a value off the scale", owner.patch(t, "/api/v1/requirements/"+s.riskKey, map[string]any{"baseRevision": 1,
		"risk": map[string]any{"before": side(7, 1)}}), http.StatusUnprocessableEntity, "validation_failed")

	revised := obj(t, want(t, owner.patch(t, "/api/v1/requirements/"+s.riskKey, map[string]any{"baseRevision": 1,
		"risk": map[string]any{"before": side(5, 3), "after": side(2, 3)}}), http.StatusOK, "rate it again"), "requirement")
	if revised["revision"] != float64(2) || levelName(ratingOf(t, revised)["beforeLevel"]) != "High" {
		t.Errorf("a new rating writes revision 2: %v", revised)
	}
	compared := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.riskKey+"/diff?from=1&to=2"), http.StatusOK, "compare"), "comparison")
	if diff := compared["risk"].([]any); len(diff) != 1 || diff[0].(map[string]any)["dimension"] != "severity" || diff[0].(map[string]any)["to"] != float64(5) {
		t.Errorf("the comparison reads %v", compared["risk"])
	}
	old := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.riskKey+"/revisions/1"), http.StatusOK, "read revision 1"), "revision")
	if fmt.Sprint(ratingOf(t, old)["before"].(map[string]any)["severity"]) != "4" {
		t.Errorf("revision 1 reads %v", old["risk"])
	}

	query := func(q string) []string {
		return rowKeys(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(q)), http.StatusOK, q))
	}
	sameList(t, "riskLevel = High", query(`riskLevel = High`), s.riskKey)
	sameList(t, "severity >= 5 AND risk = true", query(`severity >= 5 AND risk = true`), s.riskKey)
	sameList(t, "riskLevelAfter = Low", query(`riskLevelAfter = Low`))
	sameList(t, "rpn IS EMPTY", query(`rpn IS EMPTY`), s.plainKey)

	refuse(t, "a member setting the scheme", s.f.plain.put(t, "/api/v1/projects/"+s.key+"/risk-scheme", riskScheme(5, "matrix", false)),
		http.StatusForbidden, "forbidden")
	unfinished := riskScheme(5, "matrix", false)
	unfinished["matrix"] = unfinished["matrix"].([]any)[1:]
	refuse(t, "a matrix with a cell missing", owner.put(t, "/api/v1/projects/"+s.key+"/risk-scheme", unfinished),
		http.StatusUnprocessableEntity, "validation_failed")
	refuse(t, "a value a rating uses taken away", owner.put(t, "/api/v1/projects/"+s.key+"/risk-scheme", riskScheme(4, "matrix", false)),
		http.StatusConflict, "risk_scale_held")
	refuse(t, "a type of risks turned back", owner.patch(t, "/api/v1/projects/"+s.key+"/types/"+s.hazard, map[string]any{"role": "requirement"}),
		http.StatusConflict, "type_role_held")

	// The same five values judged by a priority number: 5 x 3 = 15 is medium from 20 no more, and the level moves at
	// once, as the scheme changed.
	want(t, owner.put(t, "/api/v1/projects/"+s.key+"/risk-scheme", riskScheme(5, "rpn", false)), http.StatusOK, "judge by priority number")
	if v := ratingOf(t, obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.riskKey), http.StatusOK, "read it again"), "requirement")); levelName(v["beforeLevel"]) != "Low" {
		t.Errorf("by priority number the rating reads %v", v)
	}
}

// Go and SQL work a level out alike, for every cell of a matrix and every priority number of a scheme.
func TestGoAndSQLGiveTheSameLevel(t *testing.T) {
	s := newRiskScene(t, "RSL")
	ctx := context.Background()
	var projectID uuid.UUID
	if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, s.key).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"matrix", "rpn"} {
		raw := want(t, s.f.owner.put(t, "/api/v1/projects/"+s.key+"/risk-scheme", riskScheme(5, method, method == "rpn")), http.StatusOK, "set "+method).Raw
		var body struct{ Scheme risk.Scheme }
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		for sev := 1; sev <= 5; sev++ {
			for prob := 1; prob <= 5; prob++ {
				for det := 1; det <= 5; det++ {
					sd := requirement.Side{Severity: &sev, Probability: &prob, Detection: &det}
					encoded, _ := json.Marshal(sd)
					var sqlLevel *string
					if err := s.f.h.super.QueryRow(ctx, `SELECT risk_level_ref(risk_level_of($1, $2))->>'name'`, projectID, encoded).Scan(&sqlLevel); err != nil {
						t.Fatal(err)
					}
					goLevel := body.Scheme.Level(sd)
					if (sqlLevel == nil) != (goLevel == nil) || (goLevel != nil && *sqlLevel != goLevel.Name) {
						t.Fatalf("%s %d %d %d: SQL says %v, Go %v", method, sev, prob, det, sqlLevel, goLevel)
					}
				}
			}
		}
	}
}

// A rating travels with its risk: the document, Word and Markdown show it, CSV and ReqIF carry it to another
// project, and a move to a project whose scales lack its values is refused.
func TestARatingTravelsWithItsRisk(t *testing.T) {
	s := newRiskScene(t, "RST")
	owner := s.f.owner
	word := unzipped(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/document.docx"), http.StatusOK, "export Word").Raw)["word/document.xml"]
	for _, wanted := range []string{"Severity", "Probability", "High", "Medium"} {
		if !bytes.Contains(word, []byte(wanted)) {
			t.Errorf("the Word document lacks %q", wanted)
		}
	}
	md := string(want(t, owner.get(t, "/api/v1/projects/"+s.key+"/document.md"), http.StatusOK, "export Markdown").Raw)
	if !strings.Contains(md, "**Risk**") || !strings.Contains(md, "| Before | 4 | 3 | 12 | High |") {
		t.Errorf("the Markdown reads %s", md)
	}

	to := projectKey(t, "RSU")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": to, "name": "Brakes again"}), http.StatusCreated, "make another project")
	want(t, owner.post(t, "/api/v1/projects/"+to+"/types", map[string]any{"name": "Hazard", "icon": "risk", "colour": "#dc2626", "role": "risk"}),
		http.StatusCreated, "a type of risks there")
	want(t, owner.put(t, "/api/v1/projects/"+to+"/risk-scheme", riskScheme(5, "matrix", false)), http.StatusOK, "the same scheme there")
	exported := want(t, owner.get(t, "/api/v1/projects/"+s.key+"/export.csv"), http.StatusOK, "export CSV")
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(exported.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if header := strings.Join(records[0], ","); !strings.Contains(header, "severity,probability,detection,severityAfter,probabilityAfter,detectionAfter") {
		t.Fatalf("a project of risks exports their ratings: %s", header)
	}
	made := obj(t, want(t, uploadCSV(t, owner, to, "risks.csv", exported.Raw), http.StatusCreated, "upload it"), "import")
	if problems := made["preview"].(map[string]any)["problems"].([]any); len(problems) != 0 {
		t.Fatalf("the file maps itself: %v", problems)
	}
	want(t, owner.post(t, "/api/v1/imports/"+made["id"].(string)+"/start", nil), http.StatusAccepted, "start it")
	if done := finished(t, owner, made["id"].(string)); done["state"] != "done" || len(done["refused"].([]any)) != 0 {
		t.Fatalf("the import: %v", done)
	}
	copied := ratingOf(t, obj(t, want(t, owner.get(t, "/api/v1/requirements/"+to+"-R2"), http.StatusOK, "read the copy"), "requirement"))
	if levelName(copied["beforeLevel"]) != "High" || fmt.Sprint(copied["after"].(map[string]any)["severity"]) != "2" {
		t.Errorf("the rating through CSV reads %v", copied)
	}

	reqif := projectKey(t, "RSV")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": reqif, "name": "Brakes once more"}), http.StatusCreated, "make a third project")
	want(t, owner.put(t, "/api/v1/projects/"+reqif+"/risk-scheme", riskScheme(5, "matrix", false)), http.StatusOK, "a scheme there")
	file := want(t, owner.get(t, "/api/v1/projects/"+s.key+"/export.reqif"), http.StatusOK, "export ReqIF").Raw
	if !bytes.Contains(file, []byte("before 4 3 -; after 2 3 -")) {
		t.Errorf("the ReqIF file carries no rating")
	}
	importReqIF(t, owner, reqif, "risks.reqif", file)
	again := ratingOf(t, obj(t, want(t, owner.get(t, "/api/v1/requirements/"+reqif+"-R2"), http.StatusOK, "read it there"), "requirement"))
	if levelName(again["beforeLevel"]) != "High" {
		t.Errorf("the rating through ReqIF reads %v", again)
	}

	small := projectKey(t, "RSW")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": small, "name": "Small scales"}), http.StatusCreated, "make a fourth project")
	want(t, owner.post(t, "/api/v1/projects/"+small+"/types", map[string]any{"name": "Hazard", "icon": "risk", "colour": "#dc2626", "role": "risk"}),
		http.StatusCreated, "a type of risks there too")
	want(t, owner.put(t, "/api/v1/projects/"+small+"/risk-scheme", riskScheme(3, "matrix", false)), http.StatusOK, "three values a scale")
	refuse(t, "a move to smaller scales", owner.post(t, "/api/v1/requirements/"+s.riskKey+"/move", map[string]any{"projectKey": small, "place": map[string]any{}}),
		http.StatusConflict, "ratings_do_not_fit")
}

// Straight through SQL as exciter_app: a rating goes on a risk alone with its project's values, a scale keeps the
// values ratings use, a type keeps its role, and only administrators set a scheme.
func TestRawSQLKeepsRatingsToRisks(t *testing.T) {
	s := newRiskScene(t, "RSX")
	ctx := context.Background()
	var projectID, plainID, riskID uuid.UUID
	if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, s.key).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	for number, id := range map[int]*uuid.UUID{1: &plainID, 2: &riskID} {
		if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM requirement WHERE number = $2 AND project_id = $1`, projectID, number).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	owner := s.f.h.appConn(t, s.f.owner.user)
	if _, err := owner.Exec(ctx, `UPDATE requirement SET risk = '{"before": {"severity": 1}}' WHERE id = $1`, plainID); err == nil {
		t.Error("a requirement of a type of requirements took a rating")
	}
	if _, err := owner.Exec(ctx, `UPDATE requirement SET risk = '{"before": {"severity": 9}}' WHERE id = $1`, riskID); err == nil {
		t.Error("a risk took a value off its scale")
	}
	if _, err := owner.Exec(ctx, `UPDATE requirement SET risk = '{"before": {"detection": 1}}' WHERE id = $1`, riskID); err == nil {
		t.Error("a risk took detection its scheme does not rate")
	}
	if _, err := owner.Exec(ctx, `DELETE FROM risk_scale_level WHERE project_id = $1 AND dimension = 'severity' AND value = 4`, projectID); err == nil {
		t.Error("a scale lost a value a revision uses")
	}
	if _, err := owner.Exec(ctx, `UPDATE requirement_type SET role = 'requirement' WHERE id = $1`, s.hazard); err == nil {
		t.Error("a type of risks turned back while its risk is rated")
	}
	if _, err := owner.Exec(ctx, `UPDATE requirement SET type_id = $2 WHERE id = $1`, riskID, s.plain); err == nil {
		t.Error("a risk kept its rating under a type of requirements")
	}
	member := s.f.h.appConn(t, s.f.plain.user)
	tag, err := member.Exec(ctx, `UPDATE risk_scheme SET method = 'rpn' WHERE project_id = $1`, projectID)
	if err == nil && tag.RowsAffected() != 0 {
		t.Error("a member changed the scheme")
	}
	tag, err = member.Exec(ctx, `DELETE FROM risk_scale_level WHERE project_id = $1 AND value = 5`, projectID)
	if err == nil && tag.RowsAffected() != 0 {
		t.Error("a member took a value off a scale")
	}
	if _, err := owner.Exec(ctx, `UPDATE risk_scheme SET detection = true WHERE project_id = $1`, projectID); err != nil {
		t.Fatalf("an administrator switches detection on: %v", err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO risk_scale_level (project_id, dimension, value, name) VALUES ($1, 'detection', 1, 'Sure')`, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `UPDATE requirement SET risk = '{"before": {"severity": 4, "probability": 3, "detection": 1}}' WHERE id = $1`, riskID); err != nil {
		t.Fatalf("a risk takes detection once its scheme rates it: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE risk_scheme SET detection = false WHERE project_id = $1`, projectID); err == nil {
		t.Error("detection went off while a rating gives it")
	}
}

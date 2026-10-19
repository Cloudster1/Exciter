//go:build integration

package test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mitigationScene is a risk scene whose plain requirements are asked to be verified, with a third requirement that
// verifies none yet.
type mitigationScene struct {
	*riskScene
	checkKey string
}

func newMitigationScene(t *testing.T, prefix string) *mitigationScene {
	t.Helper()
	s := &mitigationScene{riskScene: newRiskScene(t, prefix)}
	owner := s.f.owner
	want(t, owner.put(t, "/api/v1/projects/"+s.key+"/coverage-rules", map[string]any{"typeId": s.plain, "verified": true}),
		http.StatusOK, "ask the requirements to be verified")
	want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Check the brakes on ice"}),
		http.StatusCreated, "write a check")
	s.checkKey = s.key + "-R3"
	return s
}

func (s *mitigationScene) mitigations(t *testing.T) map[string]any {
	t.Helper()
	return want(t, s.f.owner.get(t, "/api/v1/requirements/"+s.riskKey+"/mitigations"), http.StatusOK, "read the mitigations").Body
}

func (s *mitigationScene) query(t *testing.T, q string) []string {
	t.Helper()
	return rowKeys(t, want(t, s.f.owner.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(q)), http.StatusOK, q))
}

// A mitigation is in effect once its requirement is verified, a level its rules do not ask counting as met, and its
// link is not suspect; the risk's level follows from the side in effect, and the link counts towards no coverage.
func TestAMitigationTakesEffectOnceItsRequirementIsVerified(t *testing.T) {
	s := newMitigationScene(t, "RMA")
	owner := s.f.owner
	if got := s.mitigations(t); got["state"] != "unmitigated" || len(got["mitigations"].([]any)) != 0 {
		t.Fatalf("a risk nothing mitigates reads %v", got)
	}
	refuse(t, "the mitigations of a requirement", owner.get(t, "/api/v1/requirements/"+s.plainKey+"/mitigations"), http.StatusNotFound, "not_found")
	before := s.query(t, "decomposed = na AND verified = none")

	refuse(t, "a risk mitigating a requirement", owner.post(t, "/api/v1/requirements/"+s.riskKey+"/links",
		map[string]any{"typeKey": "mitigates", "otherKey": s.plainKey}), http.StatusUnprocessableEntity, "validation_failed")
	refuse(t, "a requirement mitigating a requirement", owner.post(t, "/api/v1/requirements/"+s.plainKey+"/links",
		map[string]any{"typeKey": "mitigates", "otherKey": s.checkKey}), http.StatusUnprocessableEntity, "validation_failed")
	link := obj(t, want(t, owner.post(t, "/api/v1/requirements/"+s.plainKey+"/links", map[string]any{"typeKey": "mitigates", "otherKey": s.riskKey}),
		http.StatusCreated, "mitigate the risk"), "link")["id"].(string)

	got := s.mitigations(t)
	m := got["mitigations"].([]any)[0].(map[string]any)
	if got["state"] != "claimed" || m["verified"] != "none" || m["implemented"] != "na" || m["inEffect"] != false {
		t.Errorf("an unverified mitigation reads %v", got)
	}
	sameList(t, "mitigation = claimed", s.query(t, "mitigation = claimed"), s.riskKey)
	sameList(t, "a mitigation link changes no coverage", s.query(t, "decomposed = na AND verified = none"), before...)

	check := obj(t, want(t, owner.post(t, "/api/v1/requirements/"+s.checkKey+"/links", map[string]any{"typeKey": "verifies", "otherKey": s.plainKey}),
		http.StatusCreated, "verify the mitigating requirement"), "link")["id"].(string)
	got = s.mitigations(t)
	if got["state"] != "mitigated" || got["mitigations"].([]any)[0].(map[string]any)["inEffect"] != true {
		t.Fatalf("a verified mitigation reads %v", got)
	}
	r := ratingOf(t, obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.riskKey), http.StatusOK, "read the risk"), "requirement"))
	if r["state"] != "mitigated" || levelName(r["level"]) != "Medium" {
		t.Errorf("a mitigated risk stands at %v", r)
	}
	sameList(t, "riskLevel = Medium", s.query(t, "riskLevel = Medium"), s.riskKey)

	want(t, owner.patch(t, "/api/v1/requirements/"+s.plainKey, map[string]any{"baseRevision": 1, "statement": statementSaying("The vehicle stops sooner.")}),
		http.StatusOK, "change the mitigating requirement")
	got = s.mitigations(t)
	if got["state"] != "claimed" || got["mitigations"].([]any)[0].(map[string]any)["suspect"] != true {
		t.Errorf("a suspect mitigation reads %v", got)
	}
	// The change makes the check suspect too, so both are confirmed against it.
	want(t, owner.post(t, "/api/v1/links/"+link+"/clear", map[string]any{"sourceRevision": 2, "targetRevision": 1, "comment": "Still mitigates."}),
		http.StatusOK, "confirm the mitigation")
	want(t, owner.post(t, "/api/v1/links/"+check+"/clear", map[string]any{"sourceRevision": 1, "targetRevision": 2, "comment": "Still checks it."}),
		http.StatusOK, "confirm the check")
	if got := s.mitigations(t); got["state"] != "mitigated" {
		t.Errorf("a confirmed mitigation reads %v", got)
	}
	refuse(t, "a type of risks turned back while mitigated", owner.patch(t, "/api/v1/projects/"+s.key+"/types/"+s.hazard, map[string]any{"role": "requirement"}),
		http.StatusConflict, "type_role_held")
}

// The FMEA table lists the risks with their mitigations, narrowed to a cell of the matrix, and downloads as Excel;
// the matrix counts them before mitigation and as they stand.
func TestTheFMEATableAndTheRiskMatrix(t *testing.T) {
	s := newMitigationScene(t, "RMB")
	owner := s.f.owner
	want(t, owner.post(t, "/api/v1/requirements/"+s.plainKey+"/links", map[string]any{"typeKey": "mitigates", "otherKey": s.riskKey}),
		http.StatusCreated, "mitigate the risk")
	want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brakes squeal", "typeId": s.hazard}),
		http.StatusCreated, "write an unrated risk")
	risks := "/api/v1/projects/" + s.key + "/risks"

	table := want(t, owner.get(t, risks), http.StatusOK, "read the table").Body
	rows := table["risks"].([]any)
	if table["total"] != float64(2) || table["unrated"] != float64(1) || len(rows) != 2 {
		t.Fatalf("the table reads %v", table)
	}
	first := rows[0].(map[string]any)
	if first["risk"].(map[string]any)["key"] != s.riskKey || first["can"].(map[string]any)["edit"] != true ||
		first["mitigations"].([]any)[0].(map[string]any)["source"].(map[string]any)["key"] != s.plainKey {
		t.Errorf("the first row reads %v", first)
	}
	cell := want(t, owner.get(t, risks+"?side=before&severity=4&probability=3"), http.StatusOK, "a cell's risks").Body
	if cell["total"] != float64(1) {
		t.Errorf("the cell holds %v", cell)
	}
	if empty := want(t, owner.get(t, risks+"?severity=1&probability=1"), http.StatusOK, "an empty cell").Body; empty["total"] != float64(0) {
		t.Errorf("an empty cell holds %v", empty)
	}
	if hazards := want(t, owner.get(t, risks+"?query="+url.QueryEscape("rpn >= 10")), http.StatusOK, "a query").Body; hazards["total"] != float64(1) {
		t.Errorf("rpn >= 10 keeps %v", hazards)
	}
	refuse(t, "a query that does not compile", owner.get(t, risks+"?query=bogus"), http.StatusBadRequest, "bad_query")
	refuse(t, "a side that does not exist", owner.get(t, risks+"?side=sideways"), http.StatusUnprocessableEntity, "validation_failed")

	matrix := obj(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/risk-matrix?side=before"), http.StatusOK, "the matrix before"), "matrix")
	if len(matrix["cells"].([]any)) != 25 || matrix["unrated"] != float64(1) {
		t.Fatalf("the matrix reads %v", matrix)
	}
	count := func(m map[string]any, severity, probability int) (int, string) {
		for _, c := range m["cells"].([]any) {
			cell := c.(map[string]any)
			if cell["severity"] == float64(severity) && cell["probability"] == float64(probability) {
				return int(cell["count"].(float64)), levelName(cell["level"])
			}
		}
		return -1, ""
	}
	if n, level := count(matrix, 4, 3); n != 1 || level != "High" {
		t.Errorf("before mitigation the risk is in the cell 4, 3 %d times, at %s", n, level)
	}
	refuse(t, "a matrix of no side", owner.get(t, "/api/v1/projects/"+s.key+"/risk-matrix?side=sideways"), http.StatusUnprocessableEntity, "validation_failed")

	want(t, owner.post(t, "/api/v1/requirements/"+s.checkKey+"/links", map[string]any{"typeKey": "verifies", "otherKey": s.plainKey}),
		http.StatusCreated, "verify the mitigation")
	after := obj(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/risk-matrix"), http.StatusOK, "the matrix as it stands"), "matrix")
	if n, _ := count(after, 2, 3); n != 1 || after["side"] != "effective" {
		t.Errorf("once mitigated the risk is in the cell 2, 3 %d times: %v", n, after["side"])
	}

	workbook := unzipped(t, want(t, owner.get(t, risks+".xlsx"), http.StatusOK, "download the table").Raw)
	strs := string(workbook["xl/sharedStrings.xml"])
	for _, wanted := range []string{"Brakes fail on ice", s.plainKey + " Stop on ice", "Mitigated by", "High"} {
		if !strings.Contains(strs, wanted) {
			t.Errorf("the workbook lacks %q", wanted)
		}
	}
	if !bytes.Contains(workbook["xl/styles.xml"], []byte(`rgb="FFDC2626"`)) {
		t.Errorf("the workbook does not fill High in its colour")
	}
	refuse(t, "a download outside the project", s.f.outside.get(t, risks+".xlsx"), http.StatusNotFound, "not_found")

	refuse(t, "an edit against an older revision", owner.patch(t, "/api/v1/requirements/"+s.riskKey, map[string]any{"baseRevision": 0,
		"risk": map[string]any{"before": side(5, 3)}}), http.StatusConflict, "revision_conflict")
}

// A link type starts counting as mitigation only while its links join requirements to risks.
func TestALinkTypeCountsAsMitigationOnlyWhileItsLinksFit(t *testing.T) {
	s := newMitigationScene(t, "RMC")
	owner := s.f.owner
	guards := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/link-types", map[string]any{"key": "guards", "name": "Guards",
		"forward": "guards", "backward": "is guarded by", "suspectEnd": "source", "counts": "nothing"}), http.StatusCreated, "make a link type"), "linkType")
	want(t, owner.post(t, "/api/v1/requirements/"+s.plainKey+"/links", map[string]any{"typeKey": "guards", "otherKey": s.checkKey}),
		http.StatusCreated, "guard a requirement")
	refuse(t, "counting as mitigation over a requirement", owner.patch(t, "/api/v1/link-types/"+guards["id"].(string), map[string]any{"counts": "mitigation"}),
		http.StatusConflict, "link_type_held")
}

// Straight through SQL as exciter_app: a mitigation joins a requirement to a risk, and a link type counts as
// mitigation only while its links do.
func TestRawSQLKeepsMitigationsToRisks(t *testing.T) {
	s := newMitigationScene(t, "RMD")
	ctx := context.Background()
	ids := map[string]uuid.UUID{}
	for _, key := range []string{s.plainKey, s.riskKey, s.checkKey} {
		var id uuid.UUID
		number := key[strings.LastIndex(key, "R")+1:]
		if err := s.f.h.super.QueryRow(ctx, `SELECT r.id FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND r.number = $2::int`,
			s.key, number).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[key] = id
	}
	var mitigates, relates uuid.UUID
	if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM link_type WHERE key = 'mitigates' AND project_id IS NULL`).Scan(&mitigates); err != nil {
		t.Fatal(err)
	}
	if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM link_type WHERE key = 'relates' AND project_id IS NULL`).Scan(&relates); err != nil {
		t.Fatal(err)
	}
	owner := s.f.h.appConn(t, s.f.owner.user)
	insert := `INSERT INTO trace_link (type_id, source_id, target_id, created_by) VALUES ($1, $2, $3, $4)`
	if _, err := owner.Exec(ctx, insert, mitigates, ids[s.plainKey], ids[s.checkKey], s.f.owner.user); err == nil {
		t.Error("a mitigation joined two requirements")
	}
	if _, err := owner.Exec(ctx, insert, mitigates, ids[s.riskKey], ids[s.plainKey], s.f.owner.user); err == nil {
		t.Error("a risk mitigated a requirement")
	}
	if _, err := owner.Exec(ctx, insert, mitigates, ids[s.plainKey], ids[s.riskKey], s.f.owner.user); err != nil {
		t.Fatalf("a requirement mitigates a risk: %v", err)
	}
	var state string
	if err := owner.QueryRow(ctx, `SELECT risk_mitigation_state($1)`, ids[s.riskKey]).Scan(&state); err != nil || state != "claimed" {
		t.Errorf("the risk stands %q: %v", state, err)
	}
	if _, err := s.f.h.super.Exec(ctx, insert, relates, ids[s.plainKey], ids[s.checkKey], s.f.owner.user); err != nil {
		t.Fatal(err)
	}
	if _, err := s.f.h.super.Exec(ctx, `UPDATE link_type SET counts = 'mitigation' WHERE id = $1`, relates); err == nil {
		s.f.h.super.Exec(ctx, `UPDATE link_type SET counts = 'nothing' WHERE id = $1`, relates)
		t.Error("relates began to count as mitigation over two requirements")
	}
}

// The table and the matrix read a hundred risks of three mitigations each in good time.
func TestTheFMEATableReadsManyRisksInGoodTime(t *testing.T) {
	const risks, mitigationsEach = 100, 3
	const enough = 3 * time.Second
	s := newMitigationScene(t, "RME")
	owner := s.f.owner
	for i := 0; i < mitigationsEach; i++ {
		want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": fmt.Sprintf("Control %d", i)}), http.StatusCreated, "write a control")
	}
	for i := 0; i < risks; i++ {
		made := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": fmt.Sprintf("Hazard %d", i), "typeId": s.hazard,
			"risk": map[string]any{"before": side(i%5+1, i%3+1), "after": side(1, 1)}}), http.StatusCreated, "write a risk"), "requirement")
		for c := 0; c < mitigationsEach; c++ {
			want(t, owner.post(t, "/api/v1/requirements/"+fmt.Sprintf("%s-R%d", s.key, 4+c)+"/links", map[string]any{"typeKey": "mitigates", "otherKey": made["key"]}),
				http.StatusCreated, "mitigate it")
		}
	}
	for _, path := range []string{"/risks?limit=200", "/risk-matrix"} {
		start := time.Now()
		want(t, owner.get(t, "/api/v1/projects/"+s.key+path), http.StatusOK, path)
		if took := time.Since(start); took > enough {
			t.Errorf("%s took %v, more than %v", path, took, enough)
		}
	}
}

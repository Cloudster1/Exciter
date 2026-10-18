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

// testCaseScene is a project with a type marked as test cases, a requirement it verifies, and a test case with
// preconditions, two steps and a parameter of two values.
type testCaseScene struct {
	f        *projectFixture
	key      string
	testType string
	plain    string
}

func newTestCaseScene(t *testing.T, prefix string) *testCaseScene {
	t.Helper()
	f := newProjectFixture(t)
	s := &testCaseScene{f: f, key: projectKey(t, prefix)}
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": s.key, "name": "Brakes"}), http.StatusCreated, "make the project")
	s.testType = obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+s.key+"/types", map[string]any{
		"name": "Test case", "icon": "requirement", "colour": "#4f6bed", "role": "testCase"}), http.StatusCreated, "make a type of test cases"), "type")["id"].(string)
	s.plain = obj(t, want(t, f.owner.get(t, "/api/v1/projects/"+s.key), http.StatusOK, "read the project"), "project")["settings"].(map[string]any)["defaultTypeId"].(string)
	want(t, f.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Stop within 40 m", "statement": statementSaying("The vehicle stops.")}),
		http.StatusCreated, "write the requirement")
	want(t, f.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{
		"title": "Brake on a wet road", "typeId": s.testType, "statement": statementSaying("Braking is tested on a wet road."),
		"testCase": map[string]any{
			"preconditions": statementSaying("The car stands on a wet road."),
			"steps": []any{
				map[string]any{"action": statementSaying("Drive at the speed.")},
				map[string]any{"action": statementSaying("Press the pedal."), "expected": statementSaying("The car stops within 40 m.")},
			},
			"parameters": []any{map[string]any{"name": "Speed", "values": []string{"30", "50"}}},
		},
	}), http.StatusCreated, "write the test case")
	want(t, f.owner.post(t, "/api/v1/requirements/"+s.key+"-R2/links", map[string]any{"typeKey": "verifies", "otherKey": s.key + "-R1"}),
		http.StatusCreated, "the case verifies the requirement")
	return s
}

func testCaseOf(t *testing.T, r map[string]any) map[string]any {
	t.Helper()
	tc, ok := r["testCase"].(map[string]any)
	if !ok {
		t.Fatalf("%v carries no test case: %v", r["key"], r["testCase"])
	}
	return tc
}

func stepTexts(t *testing.T, tc map[string]any) []string {
	t.Helper()
	var out []string
	for _, each := range tc["steps"].([]any) {
		action := each.(map[string]any)["action"].(map[string]any)
		out = append(out, action["content"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string))
	}
	return out
}

// A test case carries its preconditions, steps and parameters in every revision: a change to a step is a revision,
// history compares steps one by one, a restore brings them back, and a type of no test cases drops them.
func TestATestCaseCarriesItsStepsInItsRevisions(t *testing.T) {
	s := newTestCaseScene(t, "TCA")
	owner := s.f.owner
	read := func() map[string]any {
		return obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.key+"-R2"), http.StatusOK, "read the case"), "requirement")
	}
	tc := testCaseOf(t, read())
	if sameList(t, "the steps", stepTexts(t, tc), "Drive at the speed.", "Press the pedal."); len(tc["parameters"].([]any)) != 1 || tc["preconditions"] == nil {
		t.Errorf("the case reads %v", tc)
	}
	if plainReq := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.key+"-R1"), http.StatusOK, "read the requirement"), "requirement"); plainReq["testCase"] != nil {
		t.Errorf("a requirement of another type carries %v", plainReq["testCase"])
	}

	swapped := want(t, owner.patch(t, "/api/v1/requirements/"+s.key+"-R2", map[string]any{"baseRevision": 1, "testCase": map[string]any{"steps": []any{
		map[string]any{"action": statementSaying("Press the pedal."), "expected": statementSaying("The car stops within 40 m.")},
		map[string]any{"action": statementSaying("Drive at the speed.")},
		map[string]any{"action": statementSaying("Release the pedal.")},
	}}}), http.StatusOK, "reorder the steps and add one")
	after := obj(t, swapped, "requirement")
	if after["revision"] != float64(2) || len(testCaseOf(t, after)["parameters"].([]any)) != 1 || testCaseOf(t, after)["preconditions"] == nil {
		t.Errorf("a change of the steps alone is revision 2 and keeps the rest: %v", after)
	}
	compared := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.key+"-R2/diff?from=1&to=2"), http.StatusOK, "compare"), "comparison")
	var changes []string
	for _, each := range compared["steps"].([]any) {
		changes = append(changes, each.(map[string]any)["change"].(string))
	}
	sameList(t, "the step changes", changes, "equal", "moved", "inserted")
	want(t, owner.post(t, "/api/v1/requirements/"+s.key+"-R2/revisions/1/restore", map[string]any{"baseRevision": 2}), http.StatusOK, "restore revision 1")
	sameList(t, "the restored steps", stepTexts(t, testCaseOf(t, read())), "Drive at the speed.", "Press the pedal.")

	refuseField := func(what string, got response, field string) {
		t.Helper()
		if msg := refuse422(t, what, got, field); msg == "" {
			t.Errorf("%s: no sentence on %s", what, field)
		}
	}
	refuseField("steps on a requirement of another type", owner.patch(t, "/api/v1/requirements/"+s.key+"-R1", map[string]any{
		"baseRevision": 1, "testCase": map[string]any{"steps": []any{map[string]any{"action": statementSaying("Look.")}}}}), "testCase")
	refuseField("parameters of uneven length", owner.patch(t, "/api/v1/requirements/"+s.key+"-R2", map[string]any{"baseRevision": 3,
		"testCase": map[string]any{"parameters": []any{map[string]any{"name": "a", "values": []string{"1", "2"}}, map[string]any{"name": "b", "values": []string{"1"}}}}}),
		"parameters[1].values")
	refuseField("a step without an action", owner.patch(t, "/api/v1/requirements/"+s.key+"-R2", map[string]any{"baseRevision": 3,
		"testCase": map[string]any{"steps": []any{map[string]any{"action": statementSaying("")}}}}), "steps[0].action")
	refuseField("a section with steps", owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"kind": "section", "title": "Tests",
		"testCase": map[string]any{"steps": []any{map[string]any{"action": statementSaying("Look.")}}}}), "testCase")
	refuse(t, "turning the type back while its cases hold steps", owner.patch(t, "/api/v1/projects/"+s.key+"/types/"+s.testType, map[string]any{"role": "requirement"}),
		http.StatusConflict, "type_role_held")

	moved := obj(t, want(t, owner.patch(t, "/api/v1/requirements/"+s.key+"-R2", map[string]any{"baseRevision": 3, "typeId": s.plain}),
		http.StatusOK, "give the case a type of requirements"), "requirement")
	if moved["testCase"] != nil || moved["revision"] != float64(4) {
		t.Errorf("a type of requirements drops the steps in a revision: %v", moved)
	}
	want(t, owner.patch(t, "/api/v1/projects/"+s.key+"/types/"+s.testType, map[string]any{"role": "requirement"}), http.StatusOK, "the type is free to turn back")
}

// A baseline holds a test case's steps as it was taken; the document, Word and Markdown show them; CSV and ReqIF
// carry them to another project.
func TestATestCaseIsBaselinedExportedAndExchanged(t *testing.T) {
	s := newTestCaseScene(t, "TCB")
	owner := s.f.owner
	baseline := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "Release 1"}), http.StatusCreated, "take a baseline"), "baseline")
	want(t, owner.patch(t, "/api/v1/requirements/"+s.key+"-R2", map[string]any{"baseRevision": 1,
		"testCase": map[string]any{"steps": []any{map[string]any{"action": statementSaying("Drive faster.")}}}}), http.StatusOK, "change the steps")
	doc := obj(t, want(t, owner.get(t, "/api/v1/baselines/"+baseline["id"].(string)+"/document"), http.StatusOK, "read the baseline"), "document")
	sameList(t, "the baseline's steps", stepTexts(t, testCaseOf(t, entryOf(t, doc, s.key+"-R2"))), "Drive at the speed.", "Press the pedal.")
	word := unzipped(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/document.docx"), http.StatusOK, "export Word").Raw)["word/document.xml"]
	for _, wanted := range []string{"Drive faster.", "Expected result", "The car stands on a wet road.", "Speed"} {
		if !bytes.Contains(word, []byte(wanted)) {
			t.Errorf("the Word document lacks %q", wanted)
		}
	}
	md := string(want(t, owner.get(t, "/api/v1/projects/"+s.key+"/document.md"), http.StatusOK, "export Markdown").Raw)
	if !strings.Contains(md, "| Drive faster. |") || !strings.Contains(md, "**Preconditions**") {
		t.Errorf("the Markdown reads %s", md)
	}

	to := projectKey(t, "TCC")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": to, "name": "Brakes again"}), http.StatusCreated, "make another project")
	want(t, owner.post(t, "/api/v1/projects/"+to+"/types", map[string]any{"name": "Test case", "icon": "requirement", "colour": "#4f6bed", "role": "testCase"}),
		http.StatusCreated, "a type of test cases there")
	exported := want(t, owner.get(t, "/api/v1/projects/"+s.key+"/export.csv"), http.StatusOK, "export CSV")
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(exported.Raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if header := strings.Join(records[0], ","); !strings.Contains(header, "preconditions,steps,parameters") {
		t.Fatalf("a project of test cases exports their columns: %s", header)
	}
	made := obj(t, want(t, uploadCSV(t, owner, to, "cases.csv", exported.Raw), http.StatusCreated, "upload it"), "import")
	if problems := made["preview"].(map[string]any)["problems"].([]any); len(problems) != 0 {
		t.Fatalf("the file maps itself: %v", problems)
	}
	want(t, owner.post(t, "/api/v1/imports/"+made["id"].(string)+"/start", nil), http.StatusAccepted, "start it")
	if done := finished(t, owner, made["id"].(string)); done["state"] != "done" || len(done["refused"].([]any)) != 0 {
		t.Fatalf("the import: %v", done)
	}
	copied := testCaseOf(t, obj(t, want(t, owner.get(t, "/api/v1/requirements/"+to+"-R2"), http.StatusOK, "read the copy"), "requirement"))
	if sameList(t, "the copied steps", stepTexts(t, copied), "Drive faster."); len(copied["parameters"].([]any)) != 1 || copied["preconditions"] == nil {
		t.Errorf("the copied case: %v", copied)
	}

	reqif := projectKey(t, "TCD")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": reqif, "name": "Brakes once more"}), http.StatusCreated, "make a third project")
	file := want(t, owner.get(t, "/api/v1/projects/"+s.key+"/export.reqif"), http.StatusOK, "export ReqIF").Raw
	if !bytes.Contains(file, []byte(`LONG-NAME="Steps"`)) {
		t.Errorf("the ReqIF file defines no steps")
	}
	importReqIF(t, owner, reqif, "cases.reqif", file)
	again := testCaseOf(t, obj(t, want(t, owner.get(t, "/api/v1/requirements/"+reqif+"-R2"), http.StatusOK, "read it there"), "requirement"))
	if sameList(t, "the steps through ReqIF", stepTexts(t, again), "Drive faster."); len(again["parameters"].([]any)) != 1 {
		t.Errorf("the case through ReqIF: %v", again)
	}
}

// Straight through SQL as exciter_app: steps go on a requirement of a type of test cases alone, and a type keeps
// its role while its requirements hold them, and only administrators change a type's role.
func TestRawSQLKeepsStepsToTestCases(t *testing.T) {
	s := newTestCaseScene(t, "TCE")
	ctx := context.Background()
	var requirementID, caseID uuid.UUID
	if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM requirement WHERE number = 1 AND project_id = (SELECT id FROM project WHERE key = $1)`, s.key).Scan(&requirementID); err != nil {
		t.Fatal(err)
	}
	if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM requirement WHERE number = 2 AND project_id = (SELECT id FROM project WHERE key = $1)`, s.key).Scan(&caseID); err != nil {
		t.Fatal(err)
	}
	owner := s.f.h.appConn(t, s.f.owner.user)
	if _, err := owner.Exec(ctx, `UPDATE requirement SET steps = '[{"action": {"type": "doc"}}]' WHERE id = $1`, requirementID); err == nil {
		t.Error("a requirement of a type of requirements took steps")
	}
	if _, err := owner.Exec(ctx, `UPDATE requirement_type SET role = 'requirement' WHERE id = $1`, s.testType); err == nil {
		t.Error("a type turned back while its case holds steps")
	}
	member := s.f.h.appConn(t, s.f.plain.user)
	tag, err := member.Exec(ctx, `UPDATE requirement_type SET role = 'testCase' WHERE id = $1`, s.plain)
	if err == nil && tag.RowsAffected() != 0 {
		t.Error("a member made a type one of test cases")
	}
	if _, err := owner.Exec(ctx, `UPDATE requirement SET type_id = $2 WHERE id = $1`, caseID, s.plain); err == nil {
		t.Error("a test case kept its steps under a type of requirements")
	}
}

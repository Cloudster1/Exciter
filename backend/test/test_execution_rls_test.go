//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// someHash is a well-formed sha256 for a file row whose bytes nobody reads.
const someHash = "0000000000000000000000000000000000000000000000000000000000000000"

// Straight through SQL: an execution keeps the plan, baseline, manager and exclusions it was started over, its
// testers are named as it starts alone, and an editor starts one as its manager in an open project only (#324).
func TestRawSQLKeepsAnExecutionToWhatItWasStartedOver(t *testing.T) {
	s := newExecutionScene(t, "TXQ")
	ctx := context.Background()
	owner := s.f.owner
	other := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/test-plans", map[string]any{"title": "Other", "scope": "cases"}),
		http.StatusCreated, "another plan"), "plan")["id"].(string)
	later := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "Release 2"}), http.StatusCreated,
		"another baseline"), "baseline")["id"].(string)
	manager := s.f.h.appConn(t, s.f.owner.user)
	changes := map[string]any{"plan_id": other, "baseline_id": later, "manager_id": s.f.plain.user, "excluded": `[]`}

	t.Run("the manager has no grant on what the execution was started over", func(t *testing.T) {
		for column, value := range changes {
			_, err := manager.Exec(ctx, `UPDATE test_execution SET `+column+` = $2 WHERE id = $1`, s.execution, value)
			if !isPermissionDenied(err) {
				t.Errorf("the manager changed %s: %v", column, err)
			}
		}
	})

	t.Run("and the guard refuses it even for the owner of the tables", func(t *testing.T) {
		for column, value := range changes {
			_, err := s.f.h.super.Exec(ctx, `UPDATE test_execution SET `+column+` = $2 WHERE id = $1`, s.execution, value)
			if !refusedBy(err, "test_execution_fixed") {
				t.Errorf("%s changed: %v", column, err)
			}
		}
		e := obj(t, want(t, owner.get(t, "/api/v1/test-executions/"+s.execution), http.StatusOK, "read it"), "execution")
		if e["planId"] != s.plan || e["baseline"].(map[string]any)["id"] != s.baseline || e["manager"].(map[string]any)["id"] != owner.user.String() ||
			len(e["excluded"].([]any)) != 1 {
			t.Errorf("the execution reads %v", e)
		}
	})

	t.Run("a tester is named as the execution starts and never after", func(t *testing.T) {
		_, err := manager.Exec(ctx, `INSERT INTO test_execution_tester (execution_id, subject_type, user_id) VALUES ($1, 'user', $2)`, s.execution, s.f.guest.user)
		if !isPolicyViolation(err) {
			t.Errorf("the manager named a tester after the start: %v", err)
		}
		if n := s.f.h.countRows(t, `SELECT count(*) FROM test_execution_tester WHERE execution_id = $1`, s.execution); n != 1 {
			t.Errorf("the execution has %d testers", n)
		}
	})

	t.Run("an editor starts one as its manager, in an open project", func(t *testing.T) {
		_, err := manager.Exec(ctx, `INSERT INTO test_execution (project_id, title, manager_id) VALUES ($1, 'Forged', $2)`, s.projectID(t), s.f.plain.user)
		if !isPolicyViolation(err) {
			t.Errorf("an editor started an execution in somebody else's name: %v", err)
		}
		archived := s.elsewhere(t, "TXQA")
		want(t, owner.put(t, "/api/v1/projects/"+archived.key+"/archive", nil), http.StatusOK, "archive the other project")
		var archivedID uuid.UUID
		if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, archived.key).Scan(&archivedID); err != nil {
			t.Fatal(err)
		}
		_, err = manager.Exec(ctx, `INSERT INTO test_execution (project_id, title, manager_id) VALUES ($1, 'Late', $2)`, archivedID, s.f.owner.user)
		if !isPolicyViolation(err) {
			t.Errorf("an execution was started in an archived project: %v", err)
		}
		if n := s.f.h.countRows(t, `SELECT count(*) FROM test_execution WHERE title IN ('Forged', 'Late') AND project_id IN ($1, $2)`, s.projectID(t), archivedID); n != 0 {
			t.Errorf("%d executions were started through SQL", n)
		}
	})
}

// Straight through SQL as a tester: a file's object is named by the database whatever the insert says and leaves a
// tombstone when deleted, and nothing is added to or deleted from a closed execution (#324).
func TestRawSQLFreezesWhatAClosedExecutionHolds(t *testing.T) {
	s := newExecutionScene(t, "TXZ")
	ctx := context.Background()
	tester := s.f.h.appConn(t, s.f.plain.user)
	caseID := s.reqID(t, 2)
	insertFile := func(name, objectKey string) (uuid.UUID, string, error) {
		var (
			id  uuid.UUID
			key string
		)
		err := tester.QueryRow(ctx, `INSERT INTO test_execution_file (execution_id, requirement_id, iteration, step, file_name, content_type, size, sha256,
			uploaded_by, object_key) VALUES ($1, $2, 1, 1, $3, 'image/png', 1, $4, $5, $6) RETURNING id, object_key`,
			s.execution, caseID, name, someHash, s.f.plain.user, objectKey).Scan(&id, &key)
		return id, key, err
	}

	t.Run("a file's object is named by the database, and a deleted file leaves a tombstone", func(t *testing.T) {
		id, key, err := insertFile("forged.png", "org/elsewhere/somebody-else")
		if err != nil {
			t.Fatalf("the tester puts a file on a step: %v", err)
		}
		wanted := "org/" + s.f.h.installation(t).ID.String() + "/execution/" + s.execution + "/" + id.String()
		if key != wanted {
			t.Errorf("the file's object is %q, want %q", key, wanted)
		}
		if tag, err := tester.Exec(ctx, `DELETE FROM test_execution_file WHERE id = $1`, id); err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("the uploader deletes the file: %v", err)
		}
		if n := s.f.h.countRows(t, `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`, wanted); n != 1 {
			t.Error("the deleted file left no tombstone for the reaper")
		}
	})

	kept, _, err := insertFile("kept.png", "")
	if err != nil {
		t.Fatal(err)
	}
	want(t, s.f.owner.post(t, "/api/v1/test-executions/"+s.execution+"/close", map[string]any{}), http.StatusOK, "close it")

	t.Run("after closing a tester adds no step, file or issue", func(t *testing.T) {
		_, err := tester.Exec(ctx, `INSERT INTO test_execution_step (execution_id, requirement_id, iteration, step, outcome, recorded_by)
			VALUES ($1, $2, 1, 1, 'passed', $3)`, s.execution, caseID, s.f.plain.user)
		if !refusedBy(err, "test_execution_closed") {
			t.Errorf("a step was recorded in a closed execution: %v", err)
		}
		if _, _, err := insertFile("late.png", ""); !refusedBy(err, "test_execution_closed") {
			t.Errorf("a file was put in a closed execution: %v", err)
		}
		_, err = tester.Exec(ctx, `INSERT INTO test_execution_issue (execution_id, requirement_id, iteration, issue_key, created_by)
			VALUES ($1, $2, 1, 'CP-1', $3)`, s.execution, caseID, s.f.plain.user)
		if !refusedBy(err, "test_execution_closed") {
			t.Errorf("an issue was filed in a closed execution: %v", err)
		}
	})

	t.Run("and deletes no file there", func(t *testing.T) {
		touchesNothing(t, tester, "the uploader deletes a file of a closed execution", `DELETE FROM test_execution_file WHERE id = $1`, kept)
		for table, want := range map[string]int{"test_execution_step": 0, "test_execution_file": 1, "test_execution_issue": 0} {
			if n := s.f.h.countRows(t, `SELECT count(*) FROM `+table+` WHERE execution_id = $1`, s.execution); n != want {
				t.Errorf("the closed execution holds %d rows of %s, want %d", n, table, want)
			}
		}
	})
}

// Straight through SQL: plans are made by the project's editors while it is open, and hold live test cases of
// their own project (#324).
func TestRawSQLKeepsPlansToTheirEditors(t *testing.T) {
	s := newExecutionScene(t, "TXK")
	ctx := context.Background()
	owner := s.f.owner
	fresh := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brake on gravel", "typeId": s.testType,
		"testCase": map[string]any{"steps": []any{map[string]any{"action": statementSaying("Brake.")}}}}), http.StatusCreated, "write a case"), "requirement")
	gone := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brake on sand", "typeId": s.testType,
		"testCase": map[string]any{"steps": []any{map[string]any{"action": statementSaying("Brake.")}}}}), http.StatusCreated, "write another case"), "requirement")
	want(t, owner.delete(t, "/api/v1/requirements/"+gone["key"].(string)), http.StatusNoContent, "trash it")
	other := s.elsewhere(t, "TXKO")
	var otherCase uuid.UUID
	if err := s.f.h.super.QueryRow(ctx, `SELECT r.id FROM requirement r JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND r.number = 1`,
		other.key).Scan(&otherCase); err != nil {
		t.Fatal(err)
	}
	editor := s.f.h.appConn(t, owner.user)
	reader := s.f.h.appConn(t, s.readerOf(t).user)
	project := s.projectID(t)
	addCase := `INSERT INTO test_plan_case (plan_id, requirement_id, position) VALUES ($1, $2, 9)`
	makePlan := `INSERT INTO test_plan (project_id, title, scope, created_by) VALUES ($1, 'Mine', 'cases', $2)`
	before := s.plans(t)

	t.Run("a reader makes no plan and adds no case", func(t *testing.T) {
		if _, err := reader.Exec(ctx, makePlan, project, s.f.guest.user); !isPolicyViolation(err) {
			t.Errorf("a reader made a plan: %v", err)
		}
		if _, err := reader.Exec(ctx, addCase, s.plan, fresh["id"]); !isPolicyViolation(err) {
			t.Errorf("a reader added a case: %v", err)
		}
	})

	t.Run("a plan holds no case in the trash and none of another project", func(t *testing.T) {
		if _, err := editor.Exec(ctx, addCase, s.plan, gone["id"]); !refusedBy(err, "test_plan_case_kept") {
			t.Errorf("a plan took a case in the trash: %v", err)
		}
		if _, err := editor.Exec(ctx, addCase, s.plan, otherCase); !refusedBy(err, "test_plan_case_kept") {
			t.Errorf("a plan took a case of another project: %v", err)
		}
	})

	t.Run("an editor of an archived project makes no plan and adds no case", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
		if _, err := editor.Exec(ctx, makePlan, project, owner.user); !isPolicyViolation(err) {
			t.Errorf("a plan was made in an archived project: %v", err)
		}
		if _, err := editor.Exec(ctx, addCase, s.plan, fresh["id"]); !isPolicyViolation(err) {
			t.Errorf("a case was added in an archived project: %v", err)
		}
	})

	if n := s.plans(t); n != before {
		t.Errorf("%d plans were made through SQL", n-before)
	}
	sameList(t, "the plan's cases", planKeys(t, owner, s.plan), s.key+"-R2", s.key+"-R3")
}

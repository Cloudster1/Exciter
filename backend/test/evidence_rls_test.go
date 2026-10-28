//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Straight through SQL as exciter_app, a run, its results, a scan, its tags and both settings stay what their rules
// allow, whatever the service would have refused first (#323).

// otherProjectsRequirement makes a project beside the scene's with one requirement, and answers that requirement's id.
func otherProjectsRequirement(t *testing.T, s *baselineScene, prefix string) string {
	t.Helper()
	key := projectKey(t, prefix)
	want(t, s.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Elsewhere"}), http.StatusCreated, "make "+key)
	return obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Doors lock at speed"}),
		http.StatusCreated, "write "+key+"-R1"), "requirement")["id"].(string)
}

func TestRawSQLKeepsARunAsItWasUploaded(t *testing.T) {
	s := newBaselineScene(t, "EQR")
	ctx := context.Background()
	landed := ran(t, s.bob, s.key, runForm("main"), junit([2]string{"engage [verifies " + s.req("1") + "]", "passed"}))["id"].(string)
	uploader := s.h.appConn(t, s.bobID)

	t.Run("what it was uploaded as never changes", func(t *testing.T) {
		// exciter_app may not update those columns at all; the trigger holds even a role that may.
		for _, c := range []struct{ what, sql string }{
			{"the branch moved", `UPDATE test_run SET branch = 'other' WHERE id = $1`},
			{"the commit changed", `UPDATE test_run SET commit_sha = 'abcdef0' WHERE id = $1`},
		} {
			if _, err := uploader.Exec(ctx, c.sql, landed); !isPermissionDenied(err) {
				t.Errorf("%s as the uploader: %v", c.what, err)
			}
			if _, err := s.h.super.Exec(ctx, c.sql, landed); !refusedBy(err, "test_run_fixed") {
				t.Errorf("%s as the superuser: %v", c.what, err)
			}
		}
	})

	t.Run("the cases it let go of do not come back", func(t *testing.T) {
		if _, err := uploader.Exec(ctx, `UPDATE test_run SET cases = '[{"name": "again"}]' WHERE id = $1`, landed); !refusedBy(err, "test_run_cases") {
			t.Errorf("the uploader gives a done run its cases back: %v", err)
		}
	})

	var branch, commit, cases string
	if err := s.h.super.QueryRow(ctx, `SELECT branch, commit_sha, cases::text FROM test_run WHERE id = $1`, landed).Scan(&branch, &commit, &cases); err != nil ||
		branch != "main" || commit != aCommit || cases != "[]" {
		t.Errorf("the run reads %s %s %s: %v", branch, commit, cases, err)
	}
}

func TestRawSQLLandsAResultOnlyOnARequirementOfTheRunsOwnProject(t *testing.T) {
	s := newBaselineScene(t, "EQT")
	ctx := context.Background()
	elsewhere := otherProjectsRequirement(t, s, "EQX")
	trashed := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Horn sounds"}), http.StatusCreated, "write R4"),
		"requirement")
	want(t, s.owner.delete(t, "/api/v1/requirements/"+trashed["key"].(string)), http.StatusNoContent, "trash R4")
	uploader := s.h.appConn(t, s.bobID)
	var run uuid.UUID
	if err := uploader.QueryRow(ctx, `INSERT INTO test_run (project_id, repository, commit_sha, branch, cases, uploaded_by) VALUES ($1, 'r', $2, 'main', '[]', $3) RETURNING id`,
		s.projectID, aCommit, s.bobID).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if _, err := uploader.Exec(ctx, `UPDATE test_run SET state = 'running' WHERE id = $1`, run); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO test_result (run_id, project_id, requirement_id, suite, name, outcome) VALUES ($1, $2, $3, 's', $4, 'passed')`
	for _, c := range []struct {
		what, requirement string
	}{
		{"a requirement of another project", elsewhere},
		{"a requirement in the trash", trashed["id"].(string)},
		{"a section", s.sectionID},
	} {
		if _, err := uploader.Exec(ctx, insert, run, s.projectID, c.requirement, c.what); !isPolicyViolation(err) {
			t.Errorf("a result lands on %s while the run runs: %v", c.what, err)
		}
	}
	if n := s.h.countRows(t, `SELECT count(*) FROM test_result WHERE run_id = $1`, run); n != 0 {
		t.Errorf("the refused results left %d rows", n)
	}
	// The same run takes a result on a requirement of its own project, so the refusals were the requirement's.
	if _, err := uploader.Exec(ctx, insert, run, s.projectID, s.r3ID, "lights"); err != nil {
		t.Errorf("a result on R3: %v", err)
	}
}

func TestRawSQLKeepsBothSettingsToTheAdministratorsOfAnOpenProject(t *testing.T) {
	s := newBaselineScene(t, "EQS")
	ctx := context.Background()
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/evidence-settings", map[string]any{"countingBranch": "release"}), http.StatusOK,
		"name the counting branch")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/code-tag-words", map[string]any{"implements": []string{"implements", "satisfies"},
		"verifies": []string{"verifies"}}), http.StatusOK, "set the words")
	updates := []struct{ what, sql string }{
		{"the counting branch", `UPDATE evidence_settings SET counting_branch = 'main', updated_by = $2 WHERE project_id = $1`},
		{"the tag words", `UPDATE code_tag_words SET implements = '{realises}', updated_by = $2 WHERE project_id = $1`},
	}
	touched := func(t *testing.T, who uuid.UUID) {
		t.Helper()
		conn := s.h.appConn(t, who)
		for _, u := range updates {
			if tag, err := conn.Exec(ctx, u.sql, s.projectID, who); err != nil || tag.RowsAffected() != 0 {
				t.Errorf("%s changed: %v, %d rows", u.what, err, tag.RowsAffected())
			}
		}
	}
	unchanged := func(t *testing.T) {
		t.Helper()
		var branch string
		var implements []string
		if err := s.h.super.QueryRow(ctx, `SELECT s.counting_branch, w.implements FROM evidence_settings s JOIN code_tag_words w USING (project_id)
			WHERE project_id = $1`, s.projectID).Scan(&branch, &implements); err != nil || branch != "release" || len(implements) != 2 {
			t.Errorf("the settings read %s and %v: %v", branch, implements, err)
		}
	}

	t.Run("an editor changes neither", func(t *testing.T) {
		touched(t, s.bobID)
		unchanged(t)
	})

	t.Run("the administrator of an archived project changes and adds neither", func(t *testing.T) {
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
		touched(t, s.ownerID)
		unchanged(t)
		owner := s.h.appConn(t, s.ownerID)
		for _, c := range []struct{ what, sql string }{
			{"the counting branch", `INSERT INTO evidence_settings (project_id, counting_branch, updated_by) VALUES ($1, 'main', $2)`},
			{"the tag words", `INSERT INTO code_tag_words (project_id, implements, verifies, updated_by) VALUES ($1, '{realises}', '{checks}', $2)`},
		} {
			if _, err := owner.Exec(ctx, c.sql, s.projectID, s.ownerID); !isPolicyViolation(err) {
				t.Errorf("%s is added to an archived project: %v", c.what, err)
			}
		}
	})
}

func TestRawSQLKeepsAScanAndItsTagsToItsOwnProject(t *testing.T) {
	s := newBaselineScene(t, "EQC")
	ctx := context.Background()
	elsewhere := otherProjectsRequirement(t, s, "EQY")
	uploaded := scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 12, "implements", s.req("1"))))["id"].(string)
	uploader := s.h.appConn(t, s.bobID)

	t.Run("within the transaction that made it, the scan's branch is fixed and its tags stay in its project", func(t *testing.T) {
		tx, err := uploader.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var scan uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT code_scan_replace($1, 'acme/brakes', $2, 'main', '')`, s.projectID, aCommit).Scan(&scan); err != nil {
			t.Fatal(err)
		}
		attempt := func(what, sql string, args ...any) error {
			t.Helper()
			if _, err := tx.Exec(ctx, `SAVEPOINT attempt`); err != nil {
				t.Fatal(err)
			}
			_, err := tx.Exec(ctx, sql, args...)
			if _, rbErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT attempt`); rbErr != nil {
				t.Fatalf("%s: %v", what, rbErr)
			}
			return err
		}
		// exciter_app may update only the counts, even in the transaction that made the scan.
		if err := attempt("move the branch", `UPDATE code_scan SET branch = 'other' WHERE id = $1`, scan); !isPermissionDenied(err) {
			t.Errorf("the uploader moves the new scan's branch: %v", err)
		}
		if err := attempt("count the tags", `UPDATE code_scan SET tags = 1 WHERE id = $1`, scan); err != nil {
			t.Errorf("the uploader counts the new scan's tags: %v", err)
		}
		insertTag := `INSERT INTO code_tag (scan_id, project_id, path, line, word, key, requirement_id, revision) VALUES ($1, $2, 'a.go', 1, 'implements', 'K-R1', $3, 1)`
		if err := attempt("tag another project's requirement", insertTag, scan, s.projectID, elsewhere); !isPolicyViolation(err) {
			t.Errorf("a tag names a requirement of another project: %v", err)
		}
		if err := attempt("tag a requirement of its own project", insertTag, scan, s.projectID, s.r3ID); err != nil {
			t.Errorf("a tag names R3: %v", err)
		}
	})

	t.Run("an uploaded scan stays on its branch for any role", func(t *testing.T) {
		if _, err := s.h.super.Exec(ctx, `UPDATE code_scan SET branch = 'other' WHERE id = $1`, uploaded); !refusedBy(err, "code_scan_fixed") {
			t.Errorf("the superuser moves a scan's branch: %v", err)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM code_scan WHERE id = $1 AND branch = 'main'`, uploaded); n != 1 {
			t.Error("the scan left its branch")
		}
	})
}

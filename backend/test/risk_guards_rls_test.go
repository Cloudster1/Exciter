//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// schemeRows counts what a project's scheme holds: its levels, its matrix's cells and its scales' values.
func (s *riskScene) schemeRows(t *testing.T) [3]int {
	t.Helper()
	id := s.projectID(t)
	return [3]int{
		s.f.h.countRows(t, `SELECT count(*) FROM risk_level WHERE project_id = $1`, id),
		s.f.h.countRows(t, `SELECT count(*) FROM risk_matrix_cell WHERE project_id = $1`, id),
		s.f.h.countRows(t, `SELECT count(*) FROM risk_scale_level WHERE project_id = $1`, id),
	}
}

// Straight through SQL as exciter_app, a member changes no part of a scheme, and an administrator none of an
// archived project's (#325).
func TestRawSQLKeepsEveryPartOfASchemeToItsAdministrators(t *testing.T) {
	s := newRiskScene(t, "RSQ")
	ctx := context.Background()
	projectID := s.projectID(t)
	var levelID uuid.UUID
	if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM risk_level WHERE project_id = $1 AND position = 0`, projectID).Scan(&levelID); err != nil {
		t.Fatal(err)
	}
	kept := s.schemeRows(t)
	refused := func(t *testing.T, as uuid.UUID, what, sql string, args ...any) {
		t.Helper()
		if _, err := s.f.h.appConn(t, as).Exec(ctx, sql, args...); !isPolicyViolation(err) {
			t.Errorf("%s: %v", what, err)
		}
	}
	const (
		addLevel = `INSERT INTO risk_level (project_id, position, name, colour) VALUES ($1, 7, 'Extreme', '#000000')`
		addCell  = `INSERT INTO risk_matrix_cell (project_id, severity, probability, level_id) VALUES ($1, 6, 6, $2)`
		addValue = `INSERT INTO risk_scale_level (project_id, dimension, value, name) VALUES ($1, 'detection', 1, 'Sure')`
	)

	t.Run("a member adds and removes nothing", func(t *testing.T) {
		member := s.f.plain.user
		refused(t, member, "a member adds a level", addLevel, projectID)
		touchesNothing(t, s.f.h.appConn(t, member), "a member removes the levels", `DELETE FROM risk_level WHERE project_id = $1`, projectID)
		refused(t, member, "a member adds a cell", addCell, projectID, levelID)
		refused(t, member, "a member adds a value", addValue, projectID)
		if got := s.schemeRows(t); got != kept {
			t.Errorf("the scheme holds %v, want %v", got, kept)
		}
	})

	t.Run("an administrator changes nothing of an archived project's scheme", func(t *testing.T) {
		want(t, s.f.owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
		owner := s.f.owner.user
		conn := s.f.h.appConn(t, owner)
		touchesNothing(t, conn, "the method changed", `UPDATE risk_scheme SET method = 'rpn' WHERE project_id = $1`, projectID)
		touchesNothing(t, conn, "a level renamed", `UPDATE risk_level SET name = 'Lowest' WHERE id = $1`, levelID)
		touchesNothing(t, conn, "the matrix cleared", `DELETE FROM risk_matrix_cell WHERE project_id = $1`, projectID)
		refused(t, owner, "a level added", addLevel, projectID)
		refused(t, owner, "a cell added", addCell, projectID, levelID)
		if got := s.schemeRows(t); got != kept {
			t.Errorf("the archived scheme holds %v, want %v", got, kept)
		}
		if n := s.f.h.countRows(t, `SELECT count(*) FROM risk_scheme WHERE project_id = $1 AND method = 'matrix'`, projectID); n != 1 {
			t.Error("the archived scheme's method changed")
		}
	})
}

// A mitigation keeps joining a requirement that is no risk to a risk when either end changes its type, through the
// API and straight through SQL (#325).
func TestAMitigationKeepsItsEndsWhenTheirTypesChange(t *testing.T) {
	s := newRiskScene(t, "RSM")
	owner := s.f.owner
	ctx := context.Background()
	unrated := s.writeRisk(t, "Brakes squeal", nil, nil)
	link := obj(t, want(t, owner.post(t, "/api/v1/requirements/"+s.plainKey+"/links", map[string]any{"typeKey": "mitigates", "otherKey": unrated}),
		http.StatusCreated, "mitigate the unrated risk"), "link")["id"].(string)
	id := func(key string) uuid.UUID {
		return uuid.MustParse(obj(t, want(t, owner.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement")["id"].(string))
	}
	riskID, controlID := id(unrated), id(s.plainKey)
	typeOf := func(key uuid.UUID) string {
		var typ string
		if err := s.f.h.super.QueryRow(ctx, `SELECT type_id::text FROM requirement WHERE id = $1`, key).Scan(&typ); err != nil {
			t.Fatal(err)
		}
		return typ
	}

	t.Run("through the API either end keeps its type", func(t *testing.T) {
		r := owner.patch(t, "/api/v1/requirements/"+unrated, map[string]any{"baseRevision": 1, "typeId": s.plain})
		refuse(t, "the risk made a requirement", r, http.StatusUnprocessableEntity, "validation_failed")
		fieldError(t, r, "typeId")
		r = owner.patch(t, "/api/v1/requirements/"+s.plainKey, map[string]any{"baseRevision": 1, "typeId": s.hazard})
		refuse(t, "the control made a risk", r, http.StatusUnprocessableEntity, "validation_failed")
		fieldError(t, r, "typeId")
		if typeOf(riskID) != s.hazard || typeOf(controlID) != s.plain {
			t.Error("a refused change of type went through")
		}
	})

	t.Run("straight through SQL either end keeps its type", func(t *testing.T) {
		conn := s.f.h.appConn(t, owner.user)
		for _, c := range []struct {
			what string
			id   uuid.UUID
			to   string
		}{{"the risk made a requirement", riskID, s.plain}, {"the control made a risk", controlID, s.hazard}} {
			if _, err := conn.Exec(ctx, `UPDATE requirement SET type_id = $2 WHERE id = $1`, c.id, c.to); !refusedBy(err, "requirement_mitigation_held") {
				t.Errorf("%s: %v", c.what, err)
			}
		}
		if _, err := conn.Exec(ctx, `UPDATE requirement SET type_id = type_id WHERE id = ANY ($1)`, []uuid.UUID{riskID, controlID}); err != nil {
			t.Errorf("both ends keep their types: %v", err)
		}
	})

	t.Run("a type with a mitigating requirement does not become one of risks", func(t *testing.T) {
		control := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/types", map[string]any{"name": "Control", "icon": "requirement", "colour": "#4f6bed"}),
			http.StatusCreated, "make a type of controls"), "type")["id"].(string)
		guard := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Pump the brakes", "typeId": control}),
			http.StatusCreated, "write a control"), "requirement")["key"].(string)
		want(t, owner.post(t, "/api/v1/requirements/"+guard+"/links", map[string]any{"typeKey": "mitigates", "otherKey": s.riskKey}),
			http.StatusCreated, "mitigate the rated risk")
		refuse(t, "controls marked as risks", owner.patch(t, "/api/v1/projects/"+s.key+"/types/"+control, map[string]any{"role": "risk"}),
			http.StatusConflict, "type_role_held")
		if _, err := s.f.h.appConn(t, owner.user).Exec(ctx, `UPDATE requirement_type SET role = 'risk' WHERE id = $1`, control); !refusedBy(err, "requirement_type_role_held") {
			t.Errorf("controls marked as risks through SQL: %v", err)
		}
	})

	t.Run("once the link is gone the risk may become a requirement", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/links/"+link), http.StatusNoContent, "remove the mitigation")
		want(t, owner.patch(t, "/api/v1/requirements/"+unrated, map[string]any{"baseRevision": 1, "typeId": s.plain}), http.StatusOK, "make the risk a requirement")
	})
}

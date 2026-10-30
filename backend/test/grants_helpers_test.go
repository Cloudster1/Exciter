//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// everyoneSQL names the built-in group that holds every member, inside a statement.
const everyoneSQL = `(SELECT id FROM groups WHERE builtin = 'everyone')`

// everyoneGroup is the id of the built-in group that holds every member.
func (h *harness) everyoneGroup(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM groups WHERE builtin = 'everyone'`).Scan(&id); err != nil {
		t.Fatalf("find Everyone: %v", err)
	}
	return id
}

// grantIn grants a role in a project to a person, behind the policies' back.
func (h *harness) grantIn(t *testing.T, project, user uuid.UUID, role string) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO role_assignment (role, project_id, user_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, role, project, user); err != nil {
		t.Fatalf("grant %s in the project: %v", role, err)
	}
	h.settle(t)
}

// subjectFields reads a subject as the tests write it, a user, a group or everyone, which is the Everyone group,
// as the fields a grant names it by.
func (c *client) subjectFields(t *testing.T, subject any) map[string]any {
	t.Helper()
	s, _ := subject.(map[string]any)
	switch s["type"] {
	case "everyone":
		return map[string]any{"groupId": c.api.h.everyoneGroup(t)}
	case "group":
		return map[string]any{"groupId": s["id"]}
	}
	return map[string]any{"userId": s["id"]}
}

// setGrants makes a project's grants the table given, rows of a subject and a role, through the API as the
// caller: each row is granted, and every other grant in the project is taken away, the caller's own last. It
// answers the first refusal, or the grants it made as "grants".
func (c *client) setGrants(t *testing.T, key string, rows []any) response {
	t.Helper()
	have := c.get(t, "/api/v1/role-assignments?limit=200&project="+url.QueryEscape(key))
	if have.Status != http.StatusOK {
		return have
	}
	var kept []string
	made := []any{}
	for _, r := range rows {
		row, _ := r.(map[string]any)
		body := c.subjectFields(t, row["subject"])
		body["role"], body["projectKey"] = row["role"], key
		got := c.post(t, "/api/v1/role-assignments", body)
		if got.Status != http.StatusCreated {
			return got
		}
		made = append(made, obj(t, got, "assignment"))
		kept = append(kept, fmt.Sprint(obj(t, got, "assignment")["id"]))
	}
	var gone, own []string
	for _, a := range list(t, have, "assignments") {
		row := a.(map[string]any)
		id := fmt.Sprint(row["id"])
		switch {
		case slices.Contains(kept, id):
		case row["userId"] == c.user.String():
			own = append(own, id)
		default:
			gone = append(gone, id)
		}
	}
	for _, id := range append(gone, own...) {
		if got := c.delete(t, "/api/v1/role-assignments/"+id); got.Status != http.StatusNoContent {
			return got
		}
	}
	return response{Status: http.StatusOK, Body: map[string]any{"grants": made}}
}

// grantsOf reads a project's grants as "role:subject" rows, Everyone named so, in the order the API lists them.
func (c *client) grantsOf(t *testing.T, key string) []string {
	t.Helper()
	var out []string
	for _, a := range list(t, want(t, c.get(t, "/api/v1/role-assignments?limit=200&project="+url.QueryEscape(key)), http.StatusOK, "list the grants"), "assignments") {
		row := a.(map[string]any)
		who, _ := row["userName"].(string)
		if g, _ := row["groupName"].(string); g != "" {
			who = "group " + g
		}
		out = append(out, fmt.Sprint(row["role"])+":"+who)
	}
	return out
}

// keepRoles puts the roles and the grants across the installation back as they were once the test ends: the
// roles it made go with their grants, the ones it changed come back, and so do the grants it gave or took.
func (h *harness) keepRoles(t *testing.T) {
	t.Helper()
	var roles, grants []byte
	if err := h.super.QueryRow(context.Background(), `
		SELECT (SELECT coalesce(jsonb_agg(r), '[]') FROM role r),
		       (SELECT coalesce(jsonb_agg(a), '[]') FROM role_assignment a WHERE a.project_id IS NULL)`).Scan(&roles, &grants); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `DELETE FROM role WHERE key NOT IN (SELECT e->>'key' FROM jsonb_array_elements($1::jsonb) e)`, roles)
		h.cleanupExec(t, h.super, `
			UPDATE role r SET name = s.name, description = s.description, installation_wide_only = s.installation_wide_only,
			       permissions = s.permissions, sort_order = s.sort_order
			FROM jsonb_populate_recordset(NULL::role, $1::jsonb) s
			WHERE r.key = s.key AND (r.name, r.description, r.installation_wide_only, r.permissions, r.sort_order)
			      IS DISTINCT FROM (s.name, s.description, s.installation_wide_only, s.permissions, s.sort_order)`, roles)
		h.cleanupExec(t, h.super, `
			DELETE FROM role_assignment WHERE project_id IS NULL
			   AND id NOT IN (SELECT (e->>'id')::uuid FROM jsonb_array_elements($1::jsonb) e)`, grants)
		h.cleanupExec(t, h.super, `
			INSERT INTO role_assignment (id, role, project_id, user_id, group_id, created_at)
			SELECT s.id, s.role, s.project_id, s.user_id, s.group_id, s.created_at
			FROM jsonb_populate_recordset(NULL::role_assignment, $1::jsonb) s
			WHERE (s.user_id IS NULL OR EXISTS (SELECT 1 FROM app_user WHERE id = s.user_id AND role IS NOT NULL))
			  AND (s.group_id IS NULL OR EXISTS (SELECT 1 FROM groups WHERE id = s.group_id))
			ON CONFLICT DO NOTHING`, grants)
		h.settle(t)
	})
}

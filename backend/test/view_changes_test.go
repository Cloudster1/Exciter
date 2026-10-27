//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// viewScene is a project where the plain member edits, a guest only views and the installation's owner
// administers, with a view of the member's shared and one of each kept to themselves.
type viewScene struct {
	f                          *projectFixture
	key                        string
	projectID, viewerID        uuid.UUID
	viewer                     *client
	shared, private, viewerOwn string
}

func newViewScene(t *testing.T, prefix string) *viewScene {
	t.Helper()
	f := newProjectFixture(t)
	s := &viewScene{f: f, key: projectKey(t, prefix)}
	made := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": s.key, "name": "Viewed"}), http.StatusCreated, "make a project"), "project")
	s.projectID = uuid.MustParse(made["id"].(string))
	s.viewerID = f.h.addPerson(t, "guest")
	f.h.grantAs(t, s.projectID, s.viewerID, "viewer")
	s.viewer = f.api.as(t, s.viewerID)
	save := func(c *client, body map[string]any) string {
		t.Helper()
		return obj(t, want(t, c.post(t, "/api/v1/projects/"+s.key+"/views", body), http.StatusCreated, "save "+body["name"].(string)), "view")["id"].(string)
	}
	s.shared = save(f.plain, map[string]any{"name": "Team", "shared": true, "columns": []string{"key", "title"}})
	s.private = save(f.plain, map[string]any{"name": "Mine"})
	s.viewerOwn = save(s.viewer, map[string]any{"name": "Glance"})
	return s
}

// row reads a view as it is stored, past every policy: its name, whether it is shared, and its columns.
func (s *viewScene) row(t *testing.T, id string) string {
	t.Helper()
	var name string
	var shared bool
	var columns []string
	err := s.f.h.super.QueryRow(context.Background(), `SELECT name, shared, columns FROM saved_view WHERE id = $1`, id).Scan(&name, &shared, &columns)
	if err == pgx.ErrNoRows {
		return "gone"
	}
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s shared=%t %v", name, shared, columns)
}

// unchanged fails unless each view reads as it did before the refusals.
func (s *viewScene) unchanged(t *testing.T, before map[string]string) {
	t.Helper()
	for id, was := range before {
		if now := s.row(t, id); now != was {
			t.Errorf("a refusal changed a view: %s became %s", was, now)
		}
	}
}

func (s *viewScene) rows(t *testing.T, ids ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, id := range ids {
		out[id] = s.row(t, id)
	}
	return out
}

// A view is refused when changed or deleted by who may not, and when what it would become breaks its rules; the
// refusal names its field and the view stays as it was (#311).
func TestAViewIsRefusedWhenChangedOrDeletedByWhoMayNot(t *testing.T) {
	s := newViewScene(t, "VWC")
	path := func(id string) string { return "/api/v1/views/" + id }
	before := s.rows(t, s.shared, s.private, s.viewerOwn)

	t.Run("a viewer shares nothing by changing a view, and deletes nobody's", func(t *testing.T) {
		refuse(t, "a viewer shares their own view", s.viewer.patch(t, path(s.viewerOwn), map[string]any{"shared": true}), http.StatusForbidden, "forbidden")
		refuse(t, "a viewer deletes a shared view", s.viewer.delete(t, path(s.shared)), http.StatusForbidden, "forbidden")
		refuse(t, "a viewer deletes a private view", s.viewer.delete(t, path(s.private)), http.StatusNotFound, "not_found")
		refuse(t, "the owner of the installation deletes a private view", s.f.owner.delete(t, path(s.private)), http.StatusNotFound, "not_found")
		s.unchanged(t, before)
		listed := []string{}
		for _, each := range list(t, want(t, s.viewer.get(t, "/api/v1/projects/"+s.key+"/views"), http.StatusOK, "the viewer lists"), "views") {
			listed = append(listed, each.(map[string]any)["name"].(string))
		}
		sameList(t, "the viewer's views", listed, "Glance", "Team")
	})

	t.Run("what a view would become is held to its rules on its field", func(t *testing.T) {
		mine := path(s.private)
		for field, body := range map[string]map[string]any{
			"name":    {"name": "TEAM"},
			"columns": {"columns": strings.Split(strings.Repeat("key,", 31), ",")[:31]},
		} {
			fieldError(t, s.f.plain.patch(t, mine, body), field)
		}
		if msg := fieldError(t, s.f.plain.patch(t, mine, map[string]any{"name": strings.Repeat("x", 101)}), "name"); !strings.Contains(msg, "100") {
			t.Errorf("a name too long is refused with %q", msg)
		}
		fieldError(t, s.f.plain.patch(t, mine, map[string]any{"name": " "}), "name")
		fieldError(t, s.f.plain.patch(t, mine, map[string]any{"filter": map[string]any{"priorities": []string{"urgent"}}}), "filter")
		fieldError(t, s.f.plain.patch(t, mine, map[string]any{"sort": map[string]any{"column": "colour"}}), "sort")
		// A view takes no input on DELETE: an id that is none is refused as every path's is, before anything is read.
		refuse(t, "a view that is no id", s.f.plain.delete(t, "/api/v1/views/nope"), http.StatusBadRequest, "bad_request")
		refuse(t, "a view nobody has", s.f.plain.delete(t, path(uuid.NewString())), http.StatusNotFound, "not_found")
		s.unchanged(t, before)
	})

	t.Run("a project administrator deletes somebody else's shared view", func(t *testing.T) {
		want(t, s.f.owner.delete(t, path(s.shared)), http.StatusNoContent, "the administrator deletes the shared view")
		if got := s.row(t, s.shared); got != "gone" {
			t.Errorf("the deleted view reads %s", got)
		}
		s.unchanged(t, s.rows(t, s.private, s.viewerOwn))
	})
}

// A shared view is kept by who edits its project: once its owner no longer does, or the project is archived, it
// stays as it was, and only taking it back is left (#311).
func TestASharedViewStaysWhenTheRightToEditIsLost(t *testing.T) {
	t.Run("an owner made a viewer", func(t *testing.T) {
		s := newViewScene(t, "VWV")
		if _, err := s.f.h.super.Exec(context.Background(), `UPDATE project_grant SET role = 'viewer' WHERE project_id = $1 AND subject_type = 'everyone'`, s.projectID); err != nil {
			t.Fatal(err)
		}
		s.f.h.settle(t)
		before := s.rows(t, s.shared)
		r := s.f.plain.patch(t, "/api/v1/views/"+s.shared, map[string]any{"name": "Renamed"})
		refuse(t, "the owner, now a viewer, renames it", r, http.StatusForbidden, "forbidden")
		if msg := refusalMessage(t, r); !strings.Contains(msg, "Stop sharing the view") {
			t.Errorf("the refusal says %q", msg)
		}
		s.unchanged(t, before)
		want(t, s.f.plain.patch(t, "/api/v1/views/"+s.shared, map[string]any{"shared": false}), http.StatusOK, "the owner takes it back")
	})

	t.Run("a project archived", func(t *testing.T) {
		s := newViewScene(t, "VWA")
		want(t, s.f.owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
		before := s.rows(t, s.shared, s.private)
		for who, c := range map[string]*client{"its owner": s.f.plain, "an administrator": s.f.owner} {
			r := c.patch(t, "/api/v1/views/"+s.shared, map[string]any{"name": "Renamed"})
			refuse(t, who+" renames a shared view of an archived project", r, http.StatusConflict, "project_archived")
			if msg := refusalMessage(t, r); !strings.Contains(msg, "Unarchive it") {
				t.Errorf("%s is told %q", who, msg)
			}
		}
		refuse(t, "sharing a view of an archived project", s.f.plain.patch(t, "/api/v1/views/"+s.private, map[string]any{"shared": true}),
			http.StatusConflict, "project_archived")
		refuse(t, "saving a shared view in an archived project", s.f.plain.post(t, "/api/v1/projects/"+s.key+"/views", map[string]any{"name": "Late", "shared": true}),
			http.StatusConflict, "project_archived")
		s.unchanged(t, before)
		want(t, s.f.plain.patch(t, "/api/v1/views/"+s.shared, map[string]any{"shared": false}), http.StatusOK, "the owner takes it back")
		want(t, s.f.plain.patch(t, "/api/v1/views/"+s.private, map[string]any{"name": "Still mine"}), http.StatusOK, "a private view is still its owner's")
	})
}

// Straight through SQL as exciter_app: a view never moves to another project, an administrator reaches only the
// shared views of others, and a token limited to one project reaches that project's views and requirements alone.
func TestRawSQLKeepsViewsToTheirProjectAndTokens(t *testing.T) {
	s := newViewScene(t, "VWS")
	ctx := context.Background()
	otherKey := projectKey(t, "VWO")
	other := uuid.MustParse(obj(t, want(t, s.f.owner.post(t, "/api/v1/projects", map[string]any{"key": otherKey, "name": "Elsewhere"}),
		http.StatusCreated, "make another project"), "project")["id"].(string))
	for key, title := range map[string]string{s.key: "Lamp " + strings.ToLower(mark(t)), otherKey: "Lamp " + strings.ToLower(mark(t)) + " elsewhere"} {
		want(t, s.f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": title}), http.StatusCreated, "write in "+key)
	}
	elsewhere := obj(t, want(t, s.f.owner.post(t, "/api/v1/projects/"+otherKey+"/views", map[string]any{"name": "Theirs", "shared": true}),
		http.StatusCreated, "share a view elsewhere"), "view")["id"].(string)
	own := obj(t, want(t, s.f.owner.post(t, "/api/v1/projects/"+s.key+"/views", map[string]any{"name": "Owner's"}),
		http.StatusCreated, "the owner saves a view here"), "view")["id"].(string)
	s.f.h.settle(t)
	member := s.f.h.appConn(t, s.f.plain.user)
	admin := s.f.h.appConn(t, s.f.home.user)

	t.Run("a view keeps its project", func(t *testing.T) {
		if _, err := member.Exec(ctx, `UPDATE saved_view SET project_id = $2 WHERE id = $1`, s.private, other); !isPermissionDenied(err) {
			t.Errorf("a view moved to another project: %v", err)
		}
	})

	t.Run("an administrator changes and deletes others' views only once shared", func(t *testing.T) {
		if n := affects(t, admin, `UPDATE saved_view SET name = 'Peeked' WHERE id = $1`, s.private); n != 0 {
			t.Error("an administrator renamed somebody's private view")
		}
		if n := affects(t, admin, `DELETE FROM saved_view WHERE id = $1`, s.private); n != 0 {
			t.Error("an administrator deleted somebody's private view")
		}
		if n := affects(t, admin, `UPDATE saved_view SET name = 'Everyone' WHERE id = $1`, s.shared); n != 1 {
			t.Error("an administrator could not rename a shared view")
		}
		if n := affects(t, admin, `DELETE FROM saved_view WHERE id = $1`, s.shared); n != 1 {
			t.Error("an administrator could not delete a shared view")
		}
	})

	t.Run("a token limited to one project reaches its views and requirements alone", func(t *testing.T) {
		limited := s.f.h.appConn(t, s.f.home.user)
		limitTo(t, limited, s.projectID)
		if n := seesRows(t, limited, `SELECT count(*) FROM saved_view WHERE id = $1`, elsewhere); n != 0 {
			t.Error("a limited token reads a view of another project")
		}
		if n := seesRows(t, admin, `SELECT count(*) FROM saved_view WHERE id = $1`, elsewhere); n != 1 {
			t.Error("the administrator does not read the shared view in a session")
		}
		if n := seesRows(t, limited, `SELECT count(*) FROM saved_view WHERE id = $1`, own); n != 1 {
			t.Error("a limited token does not read its owner's view in its project")
		}
		search := `SELECT count(*) FROM requirement_search_rows(websearch_to_tsquery('exciter_search', $1), '', 0)`
		word := "lamp " + strings.ToLower(mark(t))
		if n := seesRows(t, limited, search, word); n != 1 {
			t.Errorf("a limited token's search finds %d, want its own project's one", n)
		}
		if n := seesRows(t, admin, search, word); n != 2 {
			t.Errorf("the administrator's search finds %d, want both", n)
		}
		var mine, theirs *uuid.UUID
		if err := limited.QueryRow(ctx, `SELECT requirement_key_id($1, 1), requirement_key_id($2, 1)`, s.key, otherKey).Scan(&mine, &theirs); err != nil {
			t.Fatal(err)
		}
		if mine == nil || theirs != nil {
			t.Errorf("a limited token finds its own project's key: %v, and another's: %v", mine, theirs)
		}
	})
}

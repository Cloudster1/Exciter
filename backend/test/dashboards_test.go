//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
)

// Dashboards (#212): a person's own or shared with whoever reads the project, their widgets read live as the reader,
// each bucket a query the grid keeps the same requirements by.

type dashboardScene struct {
	*commentWorld
}

// newDashboardScene is a project of five requirements: two owned by bob, one by carol, two by nobody.
func newDashboardScene(t *testing.T, prefix string) *dashboardScene {
	t.Helper()
	s := &dashboardScene{newCommentWorld(t, prefix)}
	for _, r := range []struct {
		title string
		owner uuid.UUID
	}{{"Pedal travel", s.bobID}, {"Pedal force", s.bobID}, {"Brake lights", s.carolID}, {"Horn", uuid.Nil}} {
		body := map[string]any{"title": r.title}
		if r.owner != uuid.Nil {
			body["ownerId"] = r.owner
		}
		want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", body), http.StatusCreated, "write "+r.title)
	}
	return s
}

func (s *dashboardScene) dashboards() string { return "/api/v1/projects/" + s.key + "/dashboards" }

func (s *dashboardScene) make(t *testing.T, c *client, body map[string]any) map[string]any {
	t.Helper()
	return obj(t, want(t, c.post(t, s.dashboards(), body), http.StatusCreated, "make "+body["name"].(string)), "dashboard")
}

func (s *dashboardScene) widget(t *testing.T, c *client, dashboardID string, body map[string]any) string {
	t.Helper()
	return obj(t, want(t, c.post(t, "/api/v1/dashboards/"+dashboardID+"/widgets", body), http.StatusCreated, "add "+body["title"].(string)), "widget")["id"].(string)
}

func data(t *testing.T, c *client, widgetID string) map[string]any {
	t.Helper()
	return obj(t, want(t, c.get(t, "/api/v1/widgets/"+widgetID+"/data"), http.StatusOK, "read the widget"), "data")
}

// gridTotal is how many requirements the grid keeps for a query, as a reader opens a bucket there.
func (s *dashboardScene) gridTotal(t *testing.T, c *client, query string) float64 {
	t.Helper()
	return want(t, c.get(t, "/api/v1/projects/"+s.key+"/requirements?query="+url.QueryEscape(query+" AND kind = requirement")), http.StatusOK, "the grid").Body["total"].(float64)
}

func TestADashboardIsAPersonsOwnUntilSharedWithTheProject(t *testing.T) {
	s := newDashboardScene(t, "DBS")
	mine := s.make(t, s.carol, map[string]any{"name": "Mine"})
	t.Run("a reader makes a dashboard of their own, which nobody else reads", func(t *testing.T) {
		if mine["shared"] != false || mine["owner"].(map[string]any)["id"] != s.carolID.String() || mine["can"].(map[string]any)["share"] != false {
			t.Errorf("carol's dashboard reads %v", mine)
		}
		refuse(t, "bob reading carol's", s.bob.get(t, "/api/v1/dashboards/"+mine["id"].(string)), http.StatusNotFound, "not_found")
		refuse(t, "carol, a reader, sharing", s.carol.patch(t, "/api/v1/dashboards/"+mine["id"].(string), map[string]any{"shared": true}), http.StatusForbidden, "forbidden")
		refuse(t, "carol making a shared one", s.carol.post(t, s.dashboards(), map[string]any{"name": "Ours", "shared": true}), http.StatusForbidden, "forbidden")
		fieldError(t, s.carol.post(t, s.dashboards(), map[string]any{"name": "mine"}), "name")
		fieldError(t, s.carol.post(t, s.dashboards(), map[string]any{"name": " "}), "name")
	})
	ours := s.make(t, s.bob, map[string]any{"name": "Ours", "shared": true, "template": "overview"})
	ourID := ours["id"].(string)
	t.Run("shared, it is listed for every reader and changed only by its owner and the administrators", func(t *testing.T) {
		if len(ours["widgets"].([]any)) != 6 {
			t.Errorf("the overview starts with %v", ours["widgets"])
		}
		listed := list(t, want(t, s.carol.get(t, s.dashboards()), http.StatusOK, "carol's list"), "dashboards")
		if len(listed) != 2 || listed[0].(map[string]any)["name"] != "Mine" || listed[1].(map[string]any)["can"].(map[string]any)["edit"] != false {
			t.Errorf("carol lists %v", listed)
		}
		refuse(t, "carol renaming bob's", s.carol.patch(t, "/api/v1/dashboards/"+ourID, map[string]any{"name": "Theirs"}), http.StatusForbidden, "forbidden")
		refuse(t, "carol deleting bob's", s.carol.delete(t, "/api/v1/dashboards/"+ourID), http.StatusForbidden, "forbidden")
		renamed := obj(t, want(t, s.owner.patch(t, "/api/v1/dashboards/"+ourID, map[string]any{"name": "Brakes"}), http.StatusOK, "the owner renames it"), "dashboard")
		if renamed["name"] != "Brakes" {
			t.Errorf("renamed reads %v", renamed)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'dashboard.created' AND target_id = $1`, uuid.MustParse(ourID)); n != 1 {
			t.Errorf("%d audit entries for making it shared, want 1", n)
		}
	})
	t.Run("the project's home shows a shared dashboard, and loses it when it is no longer shared", func(t *testing.T) {
		project := "/api/v1/projects/" + s.key
		fieldError(t, s.owner.patch(t, project, map[string]any{"settings": map[string]any{"homeDashboardId": mine["id"]}}), "settings.homeDashboardId")
		set := obj(t, want(t, s.owner.patch(t, project, map[string]any{"settings": map[string]any{"homeDashboardId": ourID}}), http.StatusOK, "set the home"), "project")
		if set["settings"].(map[string]any)["homeDashboardId"] != ourID {
			t.Errorf("the project reads %v", set["settings"])
		}
		home := obj(t, want(t, s.carol.get(t, "/api/v1/dashboards/"+ourID), http.StatusOK, "carol reads the home"), "dashboard")
		if home["home"] != true {
			t.Errorf("the home dashboard reads %v", home)
		}
		want(t, s.bob.patch(t, "/api/v1/dashboards/"+ourID, map[string]any{"shared": false}), http.StatusOK, "bob takes it back")
		got := obj(t, want(t, s.owner.get(t, project), http.StatusOK, "the project"), "project")
		if got["settings"].(map[string]any)["homeDashboardId"] != nil {
			t.Errorf("the home is still %v", got["settings"])
		}
	})
	t.Run("a dashboard is deleted by whoever may change it", func(t *testing.T) {
		want(t, s.carol.delete(t, "/api/v1/dashboards/"+mine["id"].(string)), http.StatusNoContent, "carol deletes hers")
		refuse(t, "reading it after", s.carol.get(t, "/api/v1/dashboards/"+mine["id"].(string)), http.StatusNotFound, "not_found")
		refuse(t, "a dashboard that is not one", s.carol.get(t, "/api/v1/dashboards/nonsense"), http.StatusNotFound, "not_found")
		refuse(t, "a project nobody made", s.carol.get(t, "/api/v1/projects/NOPE"+s.h.suffix()+"/dashboards"), http.StatusNotFound, "not_found")
		refuse(t, "templates for nobody", s.carol.api.anonymous().get(t, "/api/v1/dashboard-templates"), http.StatusUnauthorized, "unauthorized")
		templates := want(t, s.carol.get(t, "/api/v1/dashboard-templates"), http.StatusOK, "the templates")
		if len(templates.Body["templates"].([]any)) < 2 || len(templates.Body["kinds"].([]any)) != 8 {
			t.Errorf("the templates read %v", templates.Body)
		}
	})
}

func TestAWidgetIsReadAsTheReaderAndOpensInTheGrid(t *testing.T) {
	s := newDashboardScene(t, "DBW")
	d := s.make(t, s.bob, map[string]any{"name": "Brakes", "shared": true})
	id := d["id"].(string)

	t.Run("a breakdown counts what the reader sees, each bucket the grid's count for its query", func(t *testing.T) {
		byOwner := s.widget(t, s.bob, id, map[string]any{"kind": "breakdown", "title": "By owner", "config": map[string]any{"groupBy": "owner"}})
		got := data(t, s.carol, byOwner)
		if got["total"] != float64(5) {
			t.Fatalf("the breakdown reads %v", got)
		}
		for _, each := range got["groups"].([]any)[0].(map[string]any)["buckets"].([]any) {
			b := each.(map[string]any)
			if total := s.gridTotal(t, s.carol, b["query"].(string)); total != b["count"] {
				t.Errorf("the bucket %v counts %v, the grid keeps %v", b["label"], b["count"], total)
			}
		}
	})
	t.Run("a query is read as whoever reads it", func(t *testing.T) {
		count := s.widget(t, s.bob, id, map[string]any{"kind": "count", "title": "Mine", "config": map[string]any{"source": map[string]any{"query": "owner = currentUser()"}}})
		if bob, carol := data(t, s.bob, count)["total"], data(t, s.carol, count)["total"]; bob != float64(2) || carol != float64(1) {
			t.Errorf("bob counts %v of his, carol %v of hers", bob, carol)
		}
		refuse(t, "a query that does not hold", s.bob.post(t, "/api/v1/dashboards/"+id+"/widgets", map[string]any{"kind": "count", "title": "Bad",
			"config": map[string]any{"source": map[string]any{"query": "nonsense ="}}}), http.StatusBadRequest, "bad_query")
	})
	t.Run("every kind reads", func(t *testing.T) {
		for _, kind := range []string{"list", "coverage", "review", "suspects", "changes", "quality"} {
			w := s.widget(t, s.bob, id, map[string]any{"kind": kind, "title": kind})
			got := data(t, s.carol, w)
			if got["kind"] != kind || got["total"] != float64(5) {
				t.Errorf("%s reads %v", kind, got)
			}
		}
		review := data(t, s.carol, s.widget(t, s.bob, id, map[string]any{"kind": "review", "title": "Review again"}))
		buckets := review["groups"].([]any)[0].(map[string]any)["buckets"].([]any)
		if fmt.Sprintf("%v %v", buckets[3].(map[string]any)["label"], buckets[3].(map[string]any)["count"]) != "none 5" {
			t.Errorf("the review reads %v", buckets)
		}
		preview := obj(t, want(t, s.carol.post(t, "/api/v1/projects/"+s.key+"/widget-preview", map[string]any{"kind": "breakdown", "config": map[string]any{"groupBy": "priority"}}),
			http.StatusOK, "preview a widget"), "data")
		if preview["total"] != float64(5) {
			t.Errorf("the preview reads %v", preview)
		}
		fieldError(t, s.carol.post(t, "/api/v1/projects/"+s.key+"/widget-preview", map[string]any{"kind": "gauge"}), "kind")
	})
	t.Run("a view must be shared to be read by a shared dashboard's readers", func(t *testing.T) {
		views := "/api/v1/projects/" + s.key + "/views"
		private := obj(t, want(t, s.bob.post(t, views, map[string]any{"name": "Bob's", "filter": map[string]any{"query": "owner = currentUser()"}}), http.StatusCreated, "a private view"), "view")
		fieldError(t, s.bob.post(t, "/api/v1/dashboards/"+id+"/widgets", map[string]any{"kind": "count", "title": "Private", "config": map[string]any{"source": map[string]any{"viewId": private["id"]}}}),
			"config.source.viewId")
		shared := obj(t, want(t, s.bob.post(t, views, map[string]any{"name": "Shared", "shared": true, "filter": map[string]any{"query": "title ~ pedal"}}), http.StatusCreated, "a shared view"), "view")
		w := s.widget(t, s.bob, id, map[string]any{"kind": "count", "title": "Pedals", "config": map[string]any{"source": map[string]any{"viewId": shared["id"]}}})
		if got := data(t, s.carol, w); got["total"] != float64(2) || got["viewId"] != shared["id"] {
			t.Errorf("the view's widget reads %v", got)
		}
	})
	t.Run("widgets are changed, ordered and removed by whoever may change the dashboard", func(t *testing.T) {
		widgets := obj(t, want(t, s.bob.get(t, "/api/v1/dashboards/"+id), http.StatusOK, "the dashboard"), "dashboard")["widgets"].([]any)
		first := widgets[0].(map[string]any)["id"].(string)
		refuse(t, "carol changing a widget", s.carol.patch(t, "/api/v1/widgets/"+first, map[string]any{"width": 3}), http.StatusForbidden, "forbidden")
		changed := obj(t, want(t, s.bob.patch(t, "/api/v1/widgets/"+first, map[string]any{"width": 3, "title": "Owners"}), http.StatusOK, "bob widens it"), "widget")
		if changed["width"] != float64(3) || changed["title"] != "Owners" {
			t.Errorf("the widget reads %v", changed)
		}
		var order []string
		for i := len(widgets) - 1; i >= 0; i-- {
			order = append(order, widgets[i].(map[string]any)["id"].(string))
		}
		fieldError(t, s.bob.put(t, "/api/v1/dashboards/"+id+"/order", map[string]any{"widgets": order[1:]}), "widgets")
		ordered := obj(t, want(t, s.bob.put(t, "/api/v1/dashboards/"+id+"/order", map[string]any{"widgets": order}), http.StatusOK, "reverse them"), "dashboard")
		if ordered["widgets"].([]any)[0].(map[string]any)["id"] != order[0] {
			t.Errorf("the order reads %v", ordered["widgets"])
		}
		refuse(t, "carol removing a widget", s.carol.delete(t, "/api/v1/widgets/"+first), http.StatusForbidden, "forbidden")
		want(t, s.bob.delete(t, "/api/v1/widgets/"+first), http.StatusNoContent, "bob removes it")
		refuse(t, "reading it after", s.carol.get(t, "/api/v1/widgets/"+first+"/data"), http.StatusNotFound, "not_found")
	})
	t.Run("a dashboard prints as the reader", func(t *testing.T) {
		pdf := want(t, s.carol.get(t, "/api/v1/dashboards/"+id+"/dashboard.pdf"), http.StatusOK, "print the dashboard")
		if string(pdf.Raw[:4]) != "%PDF" {
			t.Errorf("the print starts %q", pdf.Raw[:min(len(pdf.Raw), 8)])
		}
		refuse(t, "printing one that is not there", s.carol.get(t, "/api/v1/dashboards/"+uuid.NewString()+"/dashboard.pdf"), http.StatusNotFound, "not_found")
	})
}

// Straight through SQL as exciter_app: a dashboard is read by its owner or once shared, shared only with edit, and
// changed only by its owner or the project's administrators.
func TestRawSQLKeepsDashboardsToTheirOwnersAndReaders(t *testing.T) {
	s := newDashboardScene(t, "DBR")
	bobs := s.make(t, s.bob, map[string]any{"name": "Bob's"})
	ctx := context.Background()
	carol := s.h.appConn(t, s.carolID)
	var seen int
	if err := carol.QueryRow(ctx, `SELECT count(*)::int FROM dashboard WHERE id = $1`, bobs["id"]).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("carol sees %d of bob's private dashboards, %v", seen, err)
	}
	if _, err := carol.Exec(ctx, `INSERT INTO dashboard (project_id, owner_id, name, shared) VALUES ($1, $2, 'Ours', true)`, s.projectID, s.carolID); err == nil {
		t.Error("carol, a reader, made a shared dashboard")
	}
	if _, err := carol.Exec(ctx, `INSERT INTO dashboard (project_id, owner_id, name) VALUES ($1, $2, 'For bob')`, s.projectID, s.bobID); err == nil {
		t.Error("carol made a dashboard for bob")
	}
	bob := s.h.appConn(t, s.bobID)
	if _, err := bob.Exec(ctx, `UPDATE dashboard SET shared = true WHERE id = $1`, bobs["id"]); err != nil {
		t.Fatalf("bob shares his: %v", err)
	}
	tag, err := carol.Exec(ctx, `UPDATE dashboard SET name = 'Carol''s' WHERE id = $1`, bobs["id"])
	if err == nil && tag.RowsAffected() != 0 {
		t.Error("carol renamed bob's shared dashboard")
	}
	if _, err := carol.Exec(ctx, `INSERT INTO dashboard_widget (dashboard_id, kind, title, position, width) VALUES ($1, 'count', 'Mine', 0, 1)`, bobs["id"]); err == nil {
		t.Error("carol put a widget on bob's dashboard")
	}
	if _, err := bob.Exec(ctx, `UPDATE dashboard SET owner_id = $2 WHERE id = $1`, bobs["id"], s.carolID); err == nil {
		t.Error("bob gave his dashboard away")
	}
}

//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/dashboard"
)

// Dashboards refused (#319): every write a person may not make, past the limits or with settings its kind does not
// take is refused with its status and code, and the rows stay as they were.

// walledScene is a project only its grants reach: ann administers it, bob edits it and carol reads it.
type walledScene struct {
	h                              *harness
	api                            *apiServer
	key                            string
	projectID                      uuid.UUID
	ownerID, annID, bobID, carolID uuid.UUID
	owner, ann, bob, carol         *client
}

func newWalledScene(t *testing.T, prefix string) *walledScene {
	t.Helper()
	h := newHarness(t)
	s := &walledScene{h: h, api: newAPIServer(t, h), key: projectKey(t, prefix)}
	s.ownerID = h.owner(t, "walled").user
	s.annID, s.bobID, s.carolID = h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	s.owner, s.ann, s.bob, s.carol = s.api.as(t, s.ownerID), s.api.as(t, s.annID), s.api.as(t, s.bobID), s.api.as(t, s.carolID)
	project := obj(t, want(t, s.owner.post(t, "/api/v1/projects", map[string]any{"key": s.key, "name": "Walled"}), http.StatusCreated, "make a project"), "project")
	s.projectID = uuid.MustParse(project["id"].(string))
	s.grant(t, true)
	for _, title := range []string{"Pedal travel", "Pedal force"} {
		want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": title, "priority": "must"}), http.StatusCreated, "write "+title)
	}
	return s
}

// grant sets the project's grants through the API, with bob among them or without him.
func (s *walledScene) grant(t *testing.T, withBob bool) {
	t.Helper()
	rows := []any{
		grantRow(subjectOf("user", s.ownerID), "admin"),
		grantRow(subjectOf("user", s.annID), "admin"),
		grantRow(subjectOf("user", s.carolID), "viewer"),
	}
	if withBob {
		rows = append(rows, grantRow(subjectOf("user", s.bobID), "editor"))
	}
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/grants", map[string]any{"grants": rows}), http.StatusOK, "set the grants")
	s.h.settle(t)
}

func (s *walledScene) dashboard(t *testing.T, c *client, name string, shared bool) string {
	t.Helper()
	made := want(t, c.post(t, "/api/v1/projects/"+s.key+"/dashboards", map[string]any{"name": name, "shared": shared}), http.StatusCreated, "make "+name)
	return obj(t, made, "dashboard")["id"].(string)
}

func (s *walledScene) addWidget(t *testing.T, c *client, dashboardID, title string) string {
	t.Helper()
	body := map[string]any{"kind": "breakdown", "title": title, "config": map[string]any{"groupBy": "priority"}}
	return obj(t, want(t, c.post(t, "/api/v1/dashboards/"+dashboardID+"/widgets", body), http.StatusCreated, "add "+title), "widget")["id"].(string)
}

// widgetRow reads a widget as kept, past every policy: its title, width, position and settings.
func (s *walledScene) widgetRow(t *testing.T, id string) string {
	t.Helper()
	var row string
	if err := s.h.super.QueryRow(t.Context(), `SELECT title || '|' || width || '|' || position || '|' || config::text FROM dashboard_widget WHERE id = $1`, id).Scan(&row); err != nil {
		t.Fatalf("read the widget %s: %v", id, err)
	}
	return row
}

// dashboardRow reads a dashboard as kept, past every policy: its name, whether it is shared and its widgets in order.
func (s *walledScene) dashboardRow(t *testing.T, id string) string {
	t.Helper()
	var row string
	if err := s.h.super.QueryRow(t.Context(), `SELECT d.name || '|' || d.shared || '|' || COALESCE((SELECT string_agg(w.id::text, ',' ORDER BY w.position)
		FROM dashboard_widget w WHERE w.dashboard_id = d.id), '') FROM dashboard d WHERE d.id = $1`, id).Scan(&row); err != nil {
		t.Fatalf("read the dashboard %s: %v", id, err)
	}
	return row
}

func (s *walledScene) widgetCount(t *testing.T, dashboardID string) int {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM dashboard_widget WHERE dashboard_id = $1`, uuid.MustParse(dashboardID))
}

// refusedOn fails unless the answer is a 422 naming field.
func refusedOn(t *testing.T, what string, got response, field string) {
	t.Helper()
	if got.Status != http.StatusUnprocessableEntity {
		t.Errorf("%s: got %d, want 422 on %s: %s", what, got.Status, field, got.Raw)
		return
	}
	fieldError(t, got, field)
}

func TestDashboardWritesAreRefusedToWhoeverMayNotMakeThem(t *testing.T) {
	s := newWalledScene(t, "DRF")
	ours := s.dashboard(t, s.bob, "Brakes", true)
	first, second := s.addWidget(t, s.bob, ours, "First"), s.addWidget(t, s.bob, ours, "Second")
	mine := s.dashboard(t, s.bob, "Mine", false)
	outsider := s.api.as(t, s.h.addPerson(t, "member"))
	before := s.dashboardRow(t, ours)

	t.Run("a reader neither orders the widgets of a shared dashboard nor adds one", func(t *testing.T) {
		refuse(t, "carol reordering", s.carol.put(t, "/api/v1/dashboards/"+ours+"/order", map[string]any{"widgets": []string{second, first}}), http.StatusForbidden, "forbidden")
		refuse(t, "carol adding", s.carol.post(t, "/api/v1/dashboards/"+ours+"/widgets", map[string]any{"kind": "count", "title": "Carol's"}), http.StatusForbidden, "forbidden")
		if got := s.dashboardRow(t, ours); got != before {
			t.Errorf("the dashboard reads %s, was %s", got, before)
		}
	})
	t.Run("a preview is a read, so whoever may not read the project gets nothing to know it by", func(t *testing.T) {
		preview := "/api/v1/projects/" + s.key + "/widget-preview"
		refuse(t, "somebody outside previewing", outsider.post(t, preview, map[string]any{"kind": "count"}), http.StatusNotFound, "not_found")
		refuse(t, "a project nobody has", s.carol.post(t, "/api/v1/projects/NOPE"+s.h.suffix()+"/widget-preview", map[string]any{"kind": "count"}), http.StatusNotFound, "not_found")
		want(t, s.carol.post(t, preview, map[string]any{"kind": "count"}), http.StatusOK, "carol previews")
	})
	t.Run("a dashboard is renamed only to a name its owner has not used, and nothing is renamed meanwhile", func(t *testing.T) {
		path := "/api/v1/dashboards/" + ours
		refusedOn(t, "the name of bob's other dashboard", s.bob.patch(t, path, map[string]any{"name": " MINE "}), "name")
		refusedOn(t, "a blank name", s.bob.patch(t, path, map[string]any{"name": "  "}), "name")
		refusedOn(t, "a name too long", s.bob.patch(t, path, map[string]any{"name": strings.Repeat("n", dashboard.MaxNameLength+1)}), "name")
		if got := s.dashboardRow(t, ours); got != before {
			t.Errorf("the dashboard reads %s, was %s", got, before)
		}
		s.dashboard(t, s.ann, "Mine", false)
	})
	t.Run("a widget changed out of bounds is refused on the field and stays as it was", func(t *testing.T) {
		kept := s.widgetRow(t, first)
		path := "/api/v1/widgets/" + first
		refusedOn(t, "four columns", s.bob.patch(t, path, map[string]any{"width": dashboard.MaxWidth + 1}), "width")
		refusedOn(t, "no columns", s.bob.patch(t, path, map[string]any{"width": 0}), "width")
		refusedOn(t, "a blank title", s.bob.patch(t, path, map[string]any{"title": " "}), "title")
		refusedOn(t, "a title too long", s.bob.patch(t, path, map[string]any{"title": strings.Repeat("t", dashboard.MaxTitleLength+1)}), "title")
		if got := s.widgetRow(t, first); got != kept {
			t.Errorf("the widget reads %s, was %s", got, kept)
		}
	})
	t.Run("deleting takes no input, so what is not there is not found rather than refused", func(t *testing.T) {
		refuse(t, "a dashboard that is not an id", s.bob.delete(t, "/api/v1/dashboards/nonsense"), http.StatusNotFound, "not_found")
		refuse(t, "a dashboard nobody has", s.bob.delete(t, "/api/v1/dashboards/"+uuid.NewString()), http.StatusNotFound, "not_found")
		refuse(t, "a widget that is not an id", s.bob.delete(t, "/api/v1/widgets/nonsense"), http.StatusNotFound, "not_found")
		refuse(t, "a widget nobody has", s.bob.delete(t, "/api/v1/widgets/"+uuid.NewString()), http.StatusNotFound, "not_found")
		refuse(t, "carol deleting bob's private one", s.carol.delete(t, "/api/v1/dashboards/"+mine), http.StatusNotFound, "not_found")
		if got := s.dashboardRow(t, ours); got != before {
			t.Errorf("the dashboard reads %s, was %s", got, before)
		}
	})
	t.Run("an archived project's dashboards are not changed", func(t *testing.T) {
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
		refuse(t, "renaming", s.bob.patch(t, "/api/v1/dashboards/"+ours, map[string]any{"name": "Later"}), http.StatusConflict, "project_archived")
		refuse(t, "adding a widget", s.bob.post(t, "/api/v1/dashboards/"+ours+"/widgets", map[string]any{"kind": "count", "title": "Later"}), http.StatusConflict, "project_archived")
		refuse(t, "ordering", s.bob.put(t, "/api/v1/dashboards/"+ours+"/order", map[string]any{"widgets": []string{second, first}}), http.StatusConflict, "project_archived")
		refuse(t, "removing a widget", s.bob.delete(t, "/api/v1/widgets/"+first), http.StatusConflict, "project_archived")
		refuse(t, "deleting", s.bob.delete(t, "/api/v1/dashboards/"+ours), http.StatusConflict, "project_archived")
		refuse(t, "making one", s.bob.post(t, "/api/v1/projects/"+s.key+"/dashboards", map[string]any{"name": "Later"}), http.StatusConflict, "project_archived")
		if got := s.dashboardRow(t, ours); got != before {
			t.Errorf("the dashboard reads %s, was %s", got, before)
		}
	})
}

func TestOnePersonKeepsAtMostSoManyDashboardsAndADashboardSoManyWidgets(t *testing.T) {
	s := newWalledScene(t, "DLM")
	dashboards := "/api/v1/projects/" + s.key + "/dashboards"
	bobs := func() int {
		return s.h.countRows(t, `SELECT count(*) FROM dashboard WHERE project_id = $1 AND owner_id = $2`, s.projectID, s.bobID)
	}
	t.Run("the shared ones count towards the person's dashboards", func(t *testing.T) {
		s.dashboard(t, s.bob, "Shared", true)
		for i := 1; i < dashboard.MaxDashboards; i++ {
			s.dashboard(t, s.bob, fmt.Sprintf("Board %02d", i), false)
		}
		refuse(t, "one more", s.bob.post(t, dashboards, map[string]any{"name": "One too many"}), http.StatusConflict, "too_many_dashboards")
		refuse(t, "one more shared", s.bob.post(t, dashboards, map[string]any{"name": "One too many", "shared": true}), http.StatusConflict, "too_many_dashboards")
		if n := bobs(); n != dashboard.MaxDashboards {
			t.Errorf("bob keeps %d dashboards, want %d", n, dashboard.MaxDashboards)
		}
		s.dashboard(t, s.carol, "Carol's", false)
	})
	t.Run("a dashboard holds so many widgets and refuses the next", func(t *testing.T) {
		full := s.dashboard(t, s.carol, "Full", false)
		for i := range dashboard.MaxWidgets {
			want(t, s.carol.post(t, "/api/v1/dashboards/"+full+"/widgets", map[string]any{"kind": "count", "title": fmt.Sprintf("Count %02d", i)}),
				http.StatusCreated, "add a widget")
		}
		refuse(t, "one more", s.carol.post(t, "/api/v1/dashboards/"+full+"/widgets", map[string]any{"kind": "count", "title": "One too many"}),
			http.StatusConflict, "too_many_widgets")
		if n := s.widgetCount(t, full); n != dashboard.MaxWidgets {
			t.Errorf("the dashboard holds %d widgets, want %d", n, dashboard.MaxWidgets)
		}
	})
}

func TestAWidgetsSettingsAreRefusedOnTheFieldTheyAreWrongIn(t *testing.T) {
	s := newWalledScene(t, "DCF")
	other := newWalledScene(t, "DCO")
	id := s.dashboard(t, s.bob, "Settings", true)
	widgets := "/api/v1/dashboards/" + id + "/widgets"
	attribute := func(w *walledScene, name, kind string) string {
		made := want(t, w.owner.post(t, "/api/v1/projects/"+w.key+"/attributes", map[string]any{"name": name, "kind": kind}), http.StatusCreated, "make "+name)
		return obj(t, made, "attribute")["id"].(string)
	}
	notes := attribute(s, "Notes", "richText")
	theirs := attribute(other, "Risk", "number")
	ours := attribute(s, "Cost", "number")
	views := "/api/v1/projects/" + s.key + "/views"
	view := obj(t, want(t, s.bob.post(t, views, map[string]any{"name": "Pedals", "shared": true, "filter": map[string]any{"query": "title ~ pedal"}}),
		http.StatusCreated, "a shared view"), "view")["id"]

	t.Run("a new widget", func(t *testing.T) {
		for _, c := range []struct {
			what  string
			kind  string
			cfg   map[string]any
			field string
		}{
			{"counted by colour", "breakdown", map[string]any{"groupBy": "colour"}, "config.groupBy"},
			{"drawn as a pie", "breakdown", map[string]any{"shape": "pie"}, "config.shape"},
			{"counted by an attribute not named", "breakdown", map[string]any{"groupBy": "attribute"}, "config.attributeId"},
			{"counted by formatted text", "breakdown", map[string]any{"groupBy": "attribute", "attributeId": notes}, "config.attributeId"},
			{"counted by another project's attribute", "breakdown", map[string]any{"groupBy": "attribute", "attributeId": theirs}, "config.attributeId"},
			{"listing 51", "list", map[string]any{"rows": dashboard.MaxListRows + 1}, "config.rows"},
			{"listing none", "list", map[string]any{"rows": -1}, "config.rows"},
			{"looking back 731 days", "changes", map[string]any{"days": dashboard.MaxDays + 1}, "config.days"},
			{"per day", "changes", map[string]any{"interval": "day"}, "config.interval"},
			{"counting the deleted", "changes", map[string]any{"series": []string{"created", "deleted"}}, "config.series"},
			{"a query and a view", "count", map[string]any{"source": map[string]any{"query": "priority = must", "viewId": view}}, "config.source"},
		} {
			refusedOn(t, c.what, s.bob.post(t, widgets, map[string]any{"kind": c.kind, "title": c.what, "config": c.cfg}), c.field)
		}
		refusedOn(t, "four columns wide", s.bob.post(t, widgets, map[string]any{"kind": "count", "title": "Wide", "width": dashboard.MaxWidth + 1}), "width")
		if n := s.widgetCount(t, id); n != 0 {
			t.Errorf("%d widgets were added", n)
		}
	})
	t.Run("rows and days left at nought take their defaults", func(t *testing.T) {
		list := obj(t, want(t, s.bob.post(t, widgets, map[string]any{"kind": "list", "title": "List", "config": map[string]any{"rows": 0}}), http.StatusCreated, "a list"), "widget")
		if list["config"].(map[string]any)["rows"] != float64(dashboard.DefaultListRows) {
			t.Errorf("the list reads %v", list["config"])
		}
		changes := obj(t, want(t, s.bob.post(t, widgets, map[string]any{"kind": "changes", "title": "Changes", "config": map[string]any{"days": 0}}), http.StatusCreated, "changes"), "widget")
		if changes["config"].(map[string]any)["days"] != float64(dashboard.DefaultDays) {
			t.Errorf("the changes read %v", changes["config"])
		}
	})
	t.Run("a widget changed", func(t *testing.T) {
		byCost := s.bob.post(t, widgets, map[string]any{"kind": "breakdown", "title": "By cost", "config": map[string]any{"groupBy": "attribute", "attributeId": ours}})
		w := obj(t, want(t, byCost, http.StatusCreated, "count by an attribute of the project's"), "widget")["id"].(string)
		kept := s.widgetRow(t, w)
		path := "/api/v1/widgets/" + w
		refusedOn(t, "four columns", s.bob.patch(t, path, map[string]any{"width": dashboard.MaxWidth + 1}), "width")
		refusedOn(t, "formatted text", s.bob.patch(t, path, map[string]any{"config": map[string]any{"groupBy": "attribute", "attributeId": notes}}), "config.attributeId")
		refusedOn(t, "another project's", s.bob.patch(t, path, map[string]any{"config": map[string]any{"groupBy": "attribute", "attributeId": theirs}}), "config.attributeId")
		refusedOn(t, "a pie", s.bob.patch(t, path, map[string]any{"config": map[string]any{"groupBy": "status", "shape": "pie"}}), "config.shape")
		if got := s.widgetRow(t, w); got != kept {
			t.Errorf("the widget reads %s, was %s", got, kept)
		}
	})
}

func TestADashboardIsLostWithTheProjectAndKeptByItsAdministrators(t *testing.T) {
	s := newWalledScene(t, "DLP")
	mine := s.dashboard(t, s.bob, "Mine", false)
	mineWidget := s.addWidget(t, s.bob, mine, "Priorities")
	ours := s.dashboard(t, s.bob, "Ours", true)
	ourWidget := s.addWidget(t, s.bob, ours, "Shared priorities")

	t.Run("an administrator changes the widgets of a shared dashboard and never sees a private one", func(t *testing.T) {
		changed := obj(t, want(t, s.ann.patch(t, "/api/v1/widgets/"+ourWidget, map[string]any{"title": "By priority", "width": 2}), http.StatusOK, "ann changes it"), "widget")
		if changed["title"] != "By priority" || changed["width"] != float64(2) {
			t.Errorf("the widget reads %v", changed)
		}
		added := s.addWidget(t, s.ann, ours, "Ann's")
		want(t, s.ann.delete(t, "/api/v1/widgets/"+added), http.StatusNoContent, "ann removes hers")

		kept, before := s.widgetRow(t, mineWidget), s.dashboardRow(t, mine)
		refuse(t, "changing a widget of bob's own", s.ann.patch(t, "/api/v1/widgets/"+mineWidget, map[string]any{"title": "Ann's now"}), http.StatusNotFound, "not_found")
		refuse(t, "removing a widget of bob's own", s.ann.delete(t, "/api/v1/widgets/"+mineWidget), http.StatusNotFound, "not_found")
		refuse(t, "adding to bob's own", s.ann.post(t, "/api/v1/dashboards/"+mine+"/widgets", map[string]any{"kind": "count", "title": "Ann's"}), http.StatusNotFound, "not_found")
		refuse(t, "reading bob's own", s.ann.get(t, "/api/v1/widgets/"+mineWidget+"/data"), http.StatusNotFound, "not_found")
		if s.widgetRow(t, mineWidget) != kept || s.dashboardRow(t, mine) != before {
			t.Error("bob's private dashboard changed")
		}
	})
	t.Run("its owner, who does not administer the project, takes the home dashboard back and the home goes with it", func(t *testing.T) {
		home := s.dashboard(t, s.bob, "Home", true)
		project := "/api/v1/projects/" + s.key
		want(t, s.ann.patch(t, project, map[string]any{"settings": map[string]any{"homeDashboardId": home}}), http.StatusOK, "ann sets the home")
		want(t, s.bob.patch(t, "/api/v1/dashboards/"+home, map[string]any{"shared": false}), http.StatusOK, "bob takes it back")
		got := obj(t, want(t, s.carol.get(t, project), http.StatusOK, "carol reads the project"), "project")
		if v := got["settings"].(map[string]any)["homeDashboardId"]; v != nil {
			t.Errorf("the home is still %v", v)
		}
		if n := s.h.countRows(t, `SELECT count(*) FROM project WHERE id = $1 AND settings ? 'homeDashboardId'`, s.projectID); n != 0 {
			t.Error("the project still names a home dashboard")
		}
	})
	t.Run("whoever leaves the project no longer reads or changes even their own", func(t *testing.T) {
		before := s.dashboardRow(t, mine)
		s.grant(t, false)
		refuse(t, "bob reading his own", s.bob.get(t, "/api/v1/dashboards/"+mine), http.StatusNotFound, "not_found")
		refuse(t, "bob reading its widget", s.bob.get(t, "/api/v1/widgets/"+mineWidget+"/data"), http.StatusNotFound, "not_found")
		refuse(t, "bob renaming his own", s.bob.patch(t, "/api/v1/dashboards/"+mine, map[string]any{"name": "Still mine"}), http.StatusNotFound, "not_found")
		refuse(t, "bob listing them", s.bob.get(t, "/api/v1/projects/"+s.key+"/dashboards"), http.StatusNotFound, "not_found")
		if got := s.dashboardRow(t, mine); got != before {
			t.Errorf("bob's dashboard reads %s, was %s", got, before)
		}
		changed := obj(t, want(t, s.ann.patch(t, "/api/v1/dashboards/"+ours, map[string]any{"name": "Kept"}), http.StatusOK, "ann renames bob's shared one"), "dashboard")
		if changed["name"] != "Kept" || changed["owner"].(map[string]any)["id"] != s.bobID.String() {
			t.Errorf("the shared dashboard reads %v", changed)
		}
	})
}

// Straight through SQL as exciter_app: a widget of a shared dashboard is changed and removed only by whoever may
// change the dashboard, a dashboard never moves to another project, and the home is cleared only once it is not shared.
func TestRawSQLKeepsWidgetsAndTheHomeToWhoeverMayChangeThem(t *testing.T) {
	s := newWalledScene(t, "DSQ")
	elsewhere := newWalledScene(t, "DSE")
	ctx := context.Background()
	ours := s.dashboard(t, s.bob, "Ours", true)
	first, second := s.addWidget(t, s.bob, ours, "First"), s.addWidget(t, s.bob, ours, "Second")
	carol := s.h.appConn(t, s.carolID)

	t.Run("a reader changes and removes no widget of a shared dashboard", func(t *testing.T) {
		kept := s.widgetRow(t, first)
		touchesNothing(t, carol, "carol retitling", `UPDATE dashboard_widget SET title = 'Carol''s' WHERE id = $1`, first)
		touchesNothing(t, carol, "carol widening", `UPDATE dashboard_widget SET width = 3 WHERE dashboard_id = $1`, ours)
		touchesNothing(t, carol, "carol removing", `DELETE FROM dashboard_widget WHERE id = $1`, first)
		if got := s.widgetRow(t, first); got != kept || s.widgetCount(t, ours) != 2 {
			t.Errorf("the widget reads %s, was %s, of %d", got, kept, s.widgetCount(t, ours))
		}
	})
	t.Run("an administrator of the project changes and removes them", func(t *testing.T) {
		ann := s.h.appConn(t, s.annID)
		if tag, err := ann.Exec(ctx, `UPDATE dashboard_widget SET title = 'Ann''s' WHERE id = $1`, first); err != nil || tag.RowsAffected() != 1 {
			t.Errorf("ann retitles: %v, %v", tag, err)
		}
		if tag, err := ann.Exec(ctx, `DELETE FROM dashboard_widget WHERE id = $1`, second); err != nil || tag.RowsAffected() != 1 {
			t.Errorf("ann removes: %v, %v", tag, err)
		}
		if got := s.widgetRow(t, first); !strings.HasPrefix(got, "Ann's|") || s.widgetCount(t, ours) != 1 {
			t.Errorf("the widget reads %s, of %d", got, s.widgetCount(t, ours))
		}
	})
	t.Run("a dashboard stays in its project, by privilege and by its trigger", func(t *testing.T) {
		bob := s.h.appConn(t, s.bobID)
		_, err := bob.Exec(ctx, `UPDATE dashboard SET project_id = $2 WHERE id = $1`, ours, elsewhere.projectID)
		if !isPermissionDenied(err) {
			t.Errorf("bob moving his dashboard: %v, want no privilege", err)
		}
		tx, err := s.h.super.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `UPDATE dashboard SET project_id = $2 WHERE id = $1`, ours, elsewhere.projectID); !refusedBy(err, "dashboard_fixed") {
			t.Errorf("moving it past the policies: %v, want dashboard_fixed", err)
		}
	})
	t.Run("the home is cleared by nobody while its dashboard is still shared", func(t *testing.T) {
		home := func(w *walledScene) any {
			got := obj(t, want(t, w.owner.get(t, "/api/v1/projects/"+w.key), http.StatusOK, "the project"), "project")
			return got["settings"].(map[string]any)["homeDashboardId"]
		}
		theirs := elsewhere.dashboard(t, elsewhere.bob, "Theirs", true)
		want(t, s.owner.patch(t, "/api/v1/projects/"+s.key, map[string]any{"settings": map[string]any{"homeDashboardId": ours}}), http.StatusOK, "set our home")
		want(t, elsewhere.owner.patch(t, "/api/v1/projects/"+elsewhere.key, map[string]any{"settings": map[string]any{"homeDashboardId": theirs}}), http.StatusOK, "set their home")
		for _, project := range []uuid.UUID{s.projectID, elsewhere.projectID} {
			if _, err := carol.Exec(ctx, `SELECT project_clear_home_dashboard($1)`, project); err != nil {
				t.Errorf("carol clearing the home of %s: %v", project, err)
			}
		}
		if home(s) != ours || home(elsewhere) != theirs {
			t.Errorf("the homes read %v and %v", home(s), home(elsewhere))
		}
	})
}

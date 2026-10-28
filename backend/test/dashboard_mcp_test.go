//go:build integration

package test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// An assistant reads dashboards as the person whose token it holds (#319): list_dashboards and
// get_dashboard_widget answer what the API answers that person, and a private dashboard of somebody else's is refused.
func TestAnAssistantReadsDashboardsAsItsPerson(t *testing.T) {
	s := newWalledScene(t, "DMC")
	ours := s.dashboard(t, s.bob, "Ours", true)
	ourWidget := s.addWidget(t, s.bob, ours, "Priorities")
	mine := s.dashboard(t, s.bob, "Mine", false)
	mineWidget := s.addWidget(t, s.bob, mine, "Mine by priority")
	s.dashboard(t, s.carol, "Carol's", false)
	_, secret := makeToken(t, s.carol, map[string]any{"name": "assistant"})
	s.h.settle(t)
	a := connectAssistant(t, s.api, secret)

	t.Run("list_dashboards answers carol's list", func(t *testing.T) {
		got := a.call(t, "list_dashboards", map[string]any{"projectKey": s.key})["dashboards"]
		api := want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/dashboards"), http.StatusOK, "carol's list").Body["dashboards"]
		if !reflect.DeepEqual(got, api) {
			t.Errorf("the tool lists %v, the API %v", got, api)
		}
		if listed := got.([]any); len(listed) != 2 {
			t.Errorf("carol's assistant lists %d dashboards, want hers and the shared one", len(listed))
		}
	})
	t.Run("get_dashboard_widget answers what the widget shows carol", func(t *testing.T) {
		got := a.call(t, "get_dashboard_widget", map[string]any{"widgetID": ourWidget})["data"]
		api := want(t, s.carol.get(t, "/api/v1/widgets/"+ourWidget+"/data"), http.StatusOK, "carol reads the widget").Body["data"]
		if !reflect.DeepEqual(got, api) {
			t.Errorf("the tool reads %v, the API %v", got, api)
		}
		if got.(map[string]any)["total"] != float64(2) {
			t.Errorf("the widget counts %v", got)
		}
	})
	t.Run("a widget of bob's private dashboard is refused", func(t *testing.T) {
		if said := a.refused(t, "get_dashboard_widget", map[string]any{"widgetID": mineWidget}); !strings.Contains(said, "not found") {
			t.Errorf("the refusal says %q", said)
		}
		if said := a.refused(t, "list_dashboards", map[string]any{"projectKey": "NOPE" + s.h.suffix()}); said == "" {
			t.Error("a project nobody has is refused without a word")
		}
	})
}

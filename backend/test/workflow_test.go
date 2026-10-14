//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// transitionNamed finds a transition offered on a requirement by its name, and its id.
func transitionNamed(t *testing.T, c *client, key, name string) (string, bool) {
	t.Helper()
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+key+"/transitions"), http.StatusOK, "list transitions"), "transitions") {
		tr := each.(map[string]any)
		if tr["name"] == name {
			return tr["id"].(string), true
		}
	}
	return "", false
}

// plainID is the fixture's plain member, as their own answer about themselves names them.
func plainID(t *testing.T, f *projectFixture) uuid.UUID {
	t.Helper()
	return uuid.MustParse(obj(t, want(t, f.plain.get(t, "/api/v1/auth/me"), http.StatusOK, "who the member is"), "user")["id"].(string))
}

func statusOf(t *testing.T, c *client, key string) string {
	t.Helper()
	return obj(t, want(t, c.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement", "status")["name"].(string)
}

// Requirements move through their project's workflow by transitions alone: conditions decide who is offered
// what, validators ask for what a transition needs, and a new revision takes an approval back.
func TestWorkflowsOverTheAPI(t *testing.T) {
	wfKey := projectKey(t, "WF")
	f := newProjectFixture(t)
	project := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": wfKey, "name": "Flow"}), http.StatusCreated, "make a project"), "project")
	projectID := project["id"].(string)
	editor := f.plain
	reviewerID := f.h.addPerson(t, "guest")
	f.h.grantAs(t, uuid.MustParse(projectID), reviewerID, "reviewer")
	// The editor reviews too, so only not_author keeps them from approving what they wrote.
	f.h.grantAs(t, uuid.MustParse(projectID), plainID(t, f), "reviewer")
	reviewer := f.api.as(t, reviewerID)
	want(t, editor.post(t, "/api/v1/projects/"+wfKey+"/requirements", map[string]any{"title": "Pay"}), http.StatusCreated, "write "+wfKey+"-R1")
	want(t, editor.post(t, "/api/v1/projects/"+wfKey+"/requirements", map[string]any{"kind": "section", "title": "Scope"}), http.StatusCreated, "write a section")

	t.Run("a project starts with the default workflow, and a requirement in Draft", func(t *testing.T) {
		if n := len(list(t, want(t, editor.get(t, "/api/v1/projects/"+wfKey+"/statuses"), http.StatusOK, "list statuses"), "statuses")); n != 7 {
			t.Errorf("%d statuses, want the default seven", n)
		}
		flows := list(t, want(t, editor.get(t, "/api/v1/projects/"+wfKey+"/workflows"), http.StatusOK, "list workflows"), "workflows")
		if len(flows) != 1 || flows[0].(map[string]any)["projectDefault"] != true || len(flows[0].(map[string]any)["transitions"].([]any)) != 9 {
			t.Fatalf("one default workflow of nine transitions: %v", flows)
		}
		if got := statusOf(t, editor, wfKey+"-R1"); got != "Draft" {
			t.Errorf(wfKey+"-R1 starts in %s", got)
		}
		if n := len(list(t, want(t, editor.get(t, "/api/v1/requirements/"+wfKey+"-R2/transitions"), http.StatusOK, "a section's transitions"), "transitions")); n != 0 {
			t.Errorf("a section is offered %d transitions", n)
		}
		if n := len(list(t, want(t, editor.get(t, "/api/v1/workflow-rule-types"), http.StatusOK, "list rule types"), "ruleTypes")); n != 8 {
			t.Errorf("%d rule types, want 8", n)
		}
		refuse(t, "nobody asks for rule types", f.api.anonymous().get(t, "/api/v1/workflow-rule-types"), http.StatusUnauthorized, "unauthorized")
		refuse(t, "a guest reads the statuses", f.guest.get(t, "/api/v1/projects/"+wfKey+"/statuses"), http.StatusNotFound, "not_found")
		refuse(t, "a guest reads the workflows", f.guest.get(t, "/api/v1/projects/"+wfKey+"/workflows"), http.StatusNotFound, "not_found")
		refuse(t, "a guest reads the transitions", f.guest.get(t, "/api/v1/requirements/"+wfKey+"-R1/transitions"), http.StatusNotFound, "not_found")
	})

	t.Run("conditions decide who is offered what, and a stale transition is refused", func(t *testing.T) {
		submit, ok := transitionNamed(t, editor, wfKey+"-R1", "Submit for review")
		if !ok {
			t.Fatal("an editor is not offered Submit for review")
		}
		if _, ok := transitionNamed(t, reviewer, wfKey+"-R1", "Submit for review"); ok {
			t.Error("a reviewer is offered Submit for review")
		}
		want(t, editor.post(t, "/api/v1/requirements/"+wfKey+"-R1/transitions", map[string]any{"transitionId": submit}), http.StatusOK, "submit")
		refuse(t, "submitting again", editor.post(t, "/api/v1/requirements/"+wfKey+"-R1/transitions", map[string]any{"transitionId": submit}),
			http.StatusConflict, "transition_unavailable")
		approve, ok := transitionNamed(t, reviewer, wfKey+"-R1", "Approve")
		if !ok {
			t.Fatal("a reviewer is not offered Approve")
		}
		if _, ok := transitionNamed(t, editor, wfKey+"-R1", "Approve"); ok {
			t.Error("the author of the revision is offered Approve")
		}
		refuse(t, "the author approves", editor.post(t, "/api/v1/requirements/"+wfKey+"-R1/transitions", map[string]any{"transitionId": approve}),
			http.StatusForbidden, "forbidden")
		sendBack, _ := transitionNamed(t, reviewer, wfKey+"-R1", "Send back")
		refuse(t, "sending back without saying why", reviewer.post(t, "/api/v1/requirements/"+wfKey+"-R1/transitions", map[string]any{"transitionId": sendBack}),
			http.StatusUnprocessableEntity, "validation_failed")
		want(t, reviewer.post(t, "/api/v1/requirements/"+wfKey+"-R1/transitions", map[string]any{"transitionId": approve}), http.StatusOK, "approve")
		if got := statusOf(t, editor, wfKey+"-R1"); got != "Approved" {
			t.Errorf(wfKey+"-R1 is %s after the approval", got)
		}
		refuse(t, "a guest takes a transition", f.guest.post(t, "/api/v1/requirements/"+wfKey+"-R1/transitions", map[string]any{"transitionId": approve}),
			http.StatusNotFound, "not_found")
	})

	t.Run("a new revision takes an approved requirement back to Draft, and says why", func(t *testing.T) {
		want(t, editor.patch(t, "/api/v1/requirements/"+wfKey+"-R1", map[string]any{"title": "Pay now", "baseRevision": 1}), http.StatusOK, "revise")
		if got := statusOf(t, editor, wfKey+"-R1"); got != "Draft" {
			t.Errorf(wfKey+"-R1 is %s after a new revision, want Draft", got)
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM requirement_change c JOIN requirement r ON r.id = c.requirement_id
			WHERE r.project_id = $1 AND c.field = 'status' AND c.reason LIKE 'Revision 2 changed%'`, projectID); n != 1 {
			t.Error("the move back to Draft gives no reason")
		}
	})

	var deferred string
	t.Run("statuses are the administrators' to make, and stay while in use", func(t *testing.T) {
		made := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+wfKey+"/statuses", map[string]any{"name": "Deferred", "category": "closed"}), http.StatusCreated, "make a status"), "status")
		deferred = made["id"].(string)
		refuse(t, "a status named twice", f.owner.post(t, "/api/v1/projects/"+wfKey+"/statuses", map[string]any{"name": "draft"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a category that is none", f.owner.post(t, "/api/v1/projects/"+wfKey+"/statuses", map[string]any{"name": "Odd", "category": "maybe"}), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "an editor makes a status", editor.post(t, "/api/v1/projects/"+wfKey+"/statuses", map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
		want(t, f.owner.patch(t, "/api/v1/projects/"+wfKey+"/statuses/"+deferred, map[string]any{"description": "Not now."}), http.StatusOK, "describe a status")
		refuse(t, "an editor changes a status", editor.patch(t, "/api/v1/projects/"+wfKey+"/statuses/"+deferred, map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
		draft := ""
		for _, each := range list(t, want(t, editor.get(t, "/api/v1/projects/"+wfKey+"/statuses"), http.StatusOK, "list statuses"), "statuses") {
			if s := each.(map[string]any); s["name"] == "Draft" {
				draft = s["id"].(string)
			}
		}
		refuse(t, "deleting a status in use", f.owner.delete(t, "/api/v1/projects/"+wfKey+"/statuses/"+draft), http.StatusConflict, "in_use")
		spare := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+wfKey+"/statuses", map[string]any{"name": "Spare"}), http.StatusCreated, "a spare status"), "status")
		refuse(t, "an editor deletes a status", editor.delete(t, "/api/v1/projects/"+wfKey+"/statuses/"+spare["id"].(string)), http.StatusForbidden, "forbidden")
		want(t, f.owner.delete(t, "/api/v1/projects/"+wfKey+"/statuses/"+spare["id"].(string)), http.StatusNoContent, "delete an unused status")
	})

	t.Run("a workflow is saved whole, assigned with a mapping, and kept while followed", func(t *testing.T) {
		statuses := map[string]string{}
		for _, each := range list(t, want(t, editor.get(t, "/api/v1/projects/"+wfKey+"/statuses"), http.StatusOK, "list statuses"), "statuses") {
			s := each.(map[string]any)
			statuses[s["name"].(string)] = s["id"].(string)
		}
		graph := map[string]any{"name": "Simple", "steps": []any{
			map[string]any{"statusId": statuses["Draft"], "isInitial": true}, map[string]any{"statusId": deferred},
		}, "transitions": []any{map[string]any{"name": "Defer", "fromStatusId": statuses["Draft"], "toStatusId": deferred, "rules": []any{}}}}
		refuse(t, "an editor makes a workflow", editor.post(t, "/api/v1/projects/"+wfKey+"/workflows", graph), http.StatusForbidden, "forbidden")
		refuse(t, "a workflow without a start", f.owner.post(t, "/api/v1/projects/"+wfKey+"/workflows", map[string]any{"name": "Broken",
			"steps": []any{map[string]any{"statusId": deferred}}, "transitions": []any{}}), http.StatusUnprocessableEntity, "validation_failed")
		simple := obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+wfKey+"/workflows", graph), http.StatusCreated, "make a workflow"), "workflow")
		simpleID := simple["id"].(string)
		want(t, editor.get(t, "/api/v1/projects/"+wfKey+"/workflows/"+simpleID), http.StatusOK, "read it")
		refuse(t, "a workflow of nowhere", editor.get(t, "/api/v1/projects/"+wfKey+"/workflows/"+projectID), http.StatusNotFound, "not_found")
		want(t, editor.post(t, "/api/v1/projects/"+wfKey+"/requirements", map[string]any{"title": "Approved one"}), http.StatusCreated, "write "+wfKey+"-R3")
		submit, _ := transitionNamed(t, editor, wfKey+"-R3", "Submit for review")
		want(t, editor.post(t, "/api/v1/requirements/"+wfKey+"-R3/transitions", map[string]any{"transitionId": submit}), http.StatusOK, "submit "+wfKey+"-R3")
		refuse(t, "assigning without mapping In review", f.owner.put(t, "/api/v1/projects/"+wfKey+"/workflow", map[string]any{"workflowId": simpleID}),
			http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "an editor assigns", editor.put(t, "/api/v1/projects/"+wfKey+"/workflow", map[string]any{"workflowId": simpleID}), http.StatusForbidden, "forbidden")
		want(t, f.owner.put(t, "/api/v1/projects/"+wfKey+"/workflow", map[string]any{"workflowId": simpleID, "statusMapping": map[string]any{statuses["In review"]: deferred}}),
			http.StatusOK, "assign Simple")
		if got := statusOf(t, editor, wfKey+"-R3"); got != "Deferred" {
			t.Errorf(wfKey+"-R3 was mapped to %s, want Deferred", got)
		}
		graph["steps"] = []any{map[string]any{"statusId": deferred, "isInitial": true}}
		graph["transitions"] = []any{}
		refuse(t, "dropping a status in use", f.owner.put(t, "/api/v1/projects/"+wfKey+"/workflows/"+simpleID, graph), http.StatusConflict, "status_in_use")
		graph["steps"] = []any{map[string]any{"statusId": statuses["Draft"], "isInitial": true}, map[string]any{"statusId": deferred}}
		graph["name"] = "Simpler"
		want(t, f.owner.put(t, "/api/v1/projects/"+wfKey+"/workflows/"+simpleID, graph), http.StatusOK, "rename it")
		refuse(t, "an editor saves a workflow", editor.put(t, "/api/v1/projects/"+wfKey+"/workflows/"+simpleID, graph), http.StatusForbidden, "forbidden")
		refuse(t, "deleting a followed workflow", f.owner.delete(t, "/api/v1/projects/"+wfKey+"/workflows/"+simpleID), http.StatusConflict, "in_use")
		refuse(t, "an editor deletes a workflow", editor.delete(t, "/api/v1/projects/"+wfKey+"/workflows/"+simpleID), http.StatusForbidden, "forbidden")
		for _, each := range list(t, want(t, editor.get(t, "/api/v1/projects/"+wfKey+"/workflows"), http.StatusOK, "list workflows"), "workflows") {
			if w := each.(map[string]any); w["name"] == "Default workflow" {
				want(t, f.owner.delete(t, "/api/v1/projects/"+wfKey+"/workflows/"+w["id"].(string)), http.StatusNoContent, "delete the default one, followed no more")
			}
		}
		if n := f.h.countRows(t, `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action IN ('workflow.saved', 'workflow.assigned')`, f.home.user); n != 3 {
			t.Errorf("the log holds %d workflow entries, want 3", n)
		}
	})
}

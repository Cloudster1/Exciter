//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Every rule a transition carries, refusing or acting, the configurations a save refuses, and the walls between
// one project's statuses, workflows, types, attributes and field rules and another's (#322).

// flowScene is a project with an editor, a reader and the administrator who runs it, and a second project the same
// administrator runs.
type flowScene struct {
	f                *projectFixture
	key, other       string
	projectID        uuid.UUID
	editor, reader   *client
	editorID         uuid.UUID
	statuses, abroad map[string]string
}

func newFlowScene(t *testing.T, prefix string) *flowScene {
	t.Helper()
	f := newProjectFixture(t)
	s := &flowScene{f: f, key: projectKey(t, prefix), other: projectKey(t, prefix+"X")}
	made := obj(t, want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": s.key, "name": "Flow"}), http.StatusCreated, "make "+s.key), "project")
	s.projectID = uuid.MustParse(made["id"].(string))
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": s.other, "name": "Elsewhere"}), http.StatusCreated, "make "+s.other)
	s.editorID = f.h.addPerson(t, "member")
	readerID := f.h.addPerson(t, "guest")
	f.h.grantAs(t, s.projectID, readerID, "viewer")
	s.editor, s.reader = f.api.as(t, s.editorID), f.api.as(t, readerID)
	s.statuses, s.abroad = s.statusesOf(t, s.key), s.statusesOf(t, s.other)
	return s
}

func (s *flowScene) in(key, rest string) string { return "/api/v1/projects/" + key + rest }

func (s *flowScene) statusesOf(t *testing.T, key string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, each := range list(t, want(t, s.f.owner.get(t, s.in(key, "/statuses")), http.StatusOK, "list the statuses of "+key), "statuses") {
		st := each.(map[string]any)
		out[st["name"].(string)] = st["id"].(string)
	}
	return out
}

// workflowsOf names each workflow of a project by its id.
func (s *flowScene) workflowsOf(t *testing.T, key string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, each := range list(t, want(t, s.f.owner.get(t, s.in(key, "/workflows")), http.StatusOK, "list the workflows of "+key), "workflows") {
		w := each.(map[string]any)
		out[w["name"].(string)] = w
	}
	return out
}

func (s *flowScene) write(t *testing.T, title string) string {
	t.Helper()
	made := obj(t, want(t, s.editor.post(t, s.in(s.key, "/requirements"), map[string]any{"title": title}), http.StatusCreated, "write "+title), "requirement")
	return made["key"].(string)
}

func (s *flowScene) read(t *testing.T, key string) map[string]any {
	t.Helper()
	return obj(t, want(t, s.f.owner.get(t, "/api/v1/requirements/"+key), http.StatusOK, "read "+key), "requirement")
}

// take asks for the transition named on a requirement as c, which must be offered to whoever asks first.
func (s *flowScene) take(t *testing.T, c *client, key, transition string) response {
	t.Helper()
	return c.post(t, "/api/v1/requirements/"+key+"/transitions", map[string]any{"transitionId": s.transitionID(t, key, transition)})
}

// transitionID finds a transition of the workflow a requirement follows, offered or not, as the superuser sees it.
func (s *flowScene) transitionID(t *testing.T, key, name string) string {
	t.Helper()
	var id string
	if err := s.f.h.super.QueryRow(t.Context(), `
		SELECT t.id FROM workflow_transition t JOIN workflow w ON w.id = t.workflow_id
		JOIN project p ON p.id = w.project_id
		WHERE p.key = $1 AND t.name = $2 AND w.id = requirement_workflow(
			(SELECT r.type_id FROM requirement r WHERE r.project_id = p.id AND r.number = $3::int), p.id)`,
		s.key, name, strings.TrimPrefix(key, s.key+"-R")).Scan(&id); err != nil {
		t.Fatalf("find %s on %s: %v", name, key, err)
	}
	return id
}

func (s *flowScene) offered(t *testing.T, c *client, key string) []string {
	t.Helper()
	var names []string
	for _, each := range list(t, want(t, c.get(t, "/api/v1/requirements/"+key+"/transitions"), http.StatusOK, "list the transitions of "+key), "transitions") {
		names = append(names, each.(map[string]any)["name"].(string))
	}
	return names
}

func (s *flowScene) stays(t *testing.T, key, status string) {
	t.Helper()
	if got := s.read(t, key)["status"].(map[string]any)["name"]; got != status {
		t.Errorf("%s is in %v, want it still in %s", key, got, status)
	}
}

func flowStep(status string, initial bool) map[string]any {
	return map[string]any{"statusId": status, "isInitial": initial}
}

func flowEdge(name, from, to string, rules ...map[string]any) map[string]any {
	if rules == nil {
		rules = []map[string]any{}
	}
	e := map[string]any{"name": name, "toStatusId": to, "rules": rules}
	if from != "" {
		e["fromStatusId"] = from
	}
	return e
}

func flowRule(kind, typ string, config map[string]any) map[string]any {
	r := map[string]any{"kind": kind, "type": typ}
	if config != nil {
		r["config"] = config
	}
	return r
}

// The rules no other test puts on a transition, saved with the designer's whole graph, each refusing or acting; a
// refusal leaves the status where it was (#322).
func TestEveryRuleOnATransitionRefusesOrActs(t *testing.T) {
	s := newFlowScene(t, "WRU")
	st := s.statuses
	draft := st["Draft"]
	made := obj(t, want(t, s.f.owner.post(t, s.in(s.key, "/workflows"), map[string]any{"name": "Ruled", "steps": []any{flowStep(draft, true)}, "transitions": []any{}}),
		http.StatusCreated, "make the workflow"), "workflow")
	id := made["id"].(string)
	graph := map[string]any{"name": "Ruled", "steps": []any{flowStep(draft, true), flowStep(st["In review"], false), flowStep(st["Approved"], false),
		flowStep(st["Verified"], false), flowStep(st["Rejected"], false)}, "transitions": []any{
		flowEdge("Take over", draft, st["In review"], flowRule("condition", "condition.is_owner", nil)),
		flowEdge("Approve with an owner", draft, st["Approved"], flowRule("validator", "validator.owner_required", nil)),
		flowEdge("Verify when complete", draft, st["Verified"], flowRule("validator", "validator.required_attributes", nil)),
		flowEdge("Close when decomposed", draft, st["Rejected"], flowRule("validator", "validator.coverage_full", map[string]any{"dimension": "decomposed"})),
		flowEdge("Hand back", st["In review"], draft, flowRule("postfunction", "postfunction.clear_owner", nil)),
	}}
	saved := obj(t, want(t, s.f.owner.put(t, s.in(s.key, "/workflows/"+id), graph), http.StatusOK, "save the rules"), "workflow")
	if n := len(saved["transitions"].([]any)); n != 5 {
		t.Fatalf("the saved workflow has %d transitions, want 5", n)
	}
	want(t, s.f.owner.put(t, s.in(s.key, "/workflow"), map[string]any{"workflowId": id}), http.StatusOK, "the project follows it")

	t.Run("only the owner is offered or may take a transition for the owner, and handing it back clears the owner", func(t *testing.T) {
		key := s.write(t, "Brakes engage within 100 ms")
		want(t, s.editor.patch(t, "/api/v1/requirements/"+key, map[string]any{"ownerId": s.editorID.String()}), http.StatusOK, "the editor owns it")
		if got := s.offered(t, s.editor, key); !strings.Contains(strings.Join(got, "|"), "Take over") {
			t.Errorf("the owner is offered %v", got)
		}
		if got := s.offered(t, s.f.owner, key); strings.Contains(strings.Join(got, "|"), "Take over") {
			t.Errorf("an administrator who does not own it is offered %v", got)
		}
		r := s.take(t, s.f.owner, key, "Take over")
		refuse(t, "somebody else takes it over", r, http.StatusForbidden, "forbidden")
		if msg := refusalMessage(t, r); msg != "Take over is not yours to take. Only the requirement's owner may take it. Pick a transition you are offered." {
			t.Errorf("the refusal says %q", msg)
		}
		s.stays(t, key, "Draft")
		want(t, s.take(t, s.editor, key, "Take over"), http.StatusOK, "the owner takes it over")
		s.stays(t, key, "In review")
		back := obj(t, want(t, s.take(t, s.editor, key, "Hand back"), http.StatusOK, "hand it back"), "requirement")
		if back["status"].(map[string]any)["name"] != "Draft" || back["owner"] != nil {
			t.Errorf("handed back it reads %v owned by %v", back["status"], back["owner"])
		}
		if n := s.f.h.countRows(t, `SELECT count(*) FROM requirement_change c JOIN requirement r ON r.id = c.requirement_id
			WHERE r.project_id = $1 AND c.field = 'owner' AND c.to_value = ''`, s.projectID); n != 1 {
			t.Errorf("%d owner changes to nobody are logged, want 1", n)
		}
	})

	t.Run("a transition asking for an owner is refused on ownerId until it has one", func(t *testing.T) {
		key := s.write(t, "Brakes hold on a slope")
		if msg := refuse422(t, "approving without an owner", s.take(t, s.editor, key, "Approve with an owner"), "ownerId"); msg !=
			"Give the requirement an owner first; this transition asks for one." {
			t.Errorf("the refusal says %q", msg)
		}
		s.stays(t, key, "Draft")
		want(t, s.editor.patch(t, "/api/v1/requirements/"+key, map[string]any{"ownerId": s.editorID.String()}), http.StatusOK, "give it an owner")
		want(t, s.take(t, s.editor, key, "Approve with an owner"), http.StatusOK, "approve it with an owner")
		s.stays(t, key, "Approved")
	})

	t.Run("a transition asking for full coverage is refused until the requirement is covered", func(t *testing.T) {
		key := s.write(t, "Brakes warn when worn")
		want(t, s.f.owner.put(t, s.in(s.key, "/coverage-rules"), map[string]any{"decomposed": true}), http.StatusOK, "ask every type to be decomposed")
		if msg := refuse422(t, "closing what nothing decomposes", s.take(t, s.editor, key, "Close when decomposed"), "transition"); msg !=
			key+" is not fully decomposed yet. Cover it, then take this transition." {
			t.Errorf("the refusal says %q", msg)
		}
		s.stays(t, key, "Draft")
		part := s.write(t, "Pad sensor reports wear")
		want(t, s.editor.post(t, "/api/v1/requirements/"+part+"/links", map[string]any{"typeKey": "derives", "otherKey": key}), http.StatusCreated, "decompose it")
		want(t, s.take(t, s.editor, key, "Close when decomposed"), http.StatusOK, "close it, decomposed")
		s.stays(t, key, "Rejected")
	})

	t.Run("a transition asking for every required attribute names the one without a value", func(t *testing.T) {
		key := s.write(t, "Brakes cool within a minute")
		// Made required only now, so the requirement was written without it.
		risk := obj(t, want(t, s.f.owner.post(t, s.in(s.key, "/attributes"), map[string]any{"name": "Risk", "kind": "text"}), http.StatusCreated, "define Risk"), "attribute")
		riskID := risk["id"].(string)
		want(t, s.f.owner.patch(t, s.in(s.key, "/attributes/"+riskID), map[string]any{"required": true}), http.StatusOK, "make Risk required")
		if msg := refuse422(t, "verifying without Risk", s.take(t, s.editor, key, "Verify when complete"), "attributes."+riskID); msg !=
			"Give Risk a value first; this transition asks for every required attribute." {
			t.Errorf("the refusal says %q", msg)
		}
		s.stays(t, key, "Draft")
		want(t, s.editor.patch(t, "/api/v1/requirements/"+key, map[string]any{"attributes": map[string]any{riskID: "low"}, "baseRevision": 1}), http.StatusOK, "give Risk")
		want(t, s.take(t, s.editor, key, "Verify when complete"), http.StatusOK, "verify it, complete")
		s.stays(t, key, "Verified")
	})
}

// A rule whose configuration could never run is refused when the workflow is saved, on the transitions it sits on
// and naming the rule, and the workflow keeps what it had (#322).
func TestABadRuleConfigurationIsRefusedOnSave(t *testing.T) {
	s := newFlowScene(t, "WRC")
	st := s.statuses
	made := obj(t, want(t, s.f.owner.post(t, s.in(s.key, "/workflows"), map[string]any{"name": "Checked",
		"steps": []any{flowStep(st["Draft"], true), flowStep(st["In review"], false)}, "transitions": []any{flowEdge("Submit", st["Draft"], st["In review"])}}),
		http.StatusCreated, "make the workflow"), "workflow")
	path := s.in(s.key, "/workflows/"+made["id"].(string))
	foreign := obj(t, want(t, s.f.owner.post(t, s.in(s.other, "/link-types"), map[string]any{"key": "checks", "name": "Checks", "forward": "checks",
		"backward": "is checked by"}), http.StatusCreated, "a link type of the other project"), "linkType")["id"].(string)
	own := obj(t, want(t, s.f.owner.post(t, s.in(s.key, "/link-types"), map[string]any{"key": "proves", "name": "Proves", "forward": "proves",
		"backward": "is proved by"}), http.StatusCreated, "a link type of the project's own"), "linkType")["id"].(string)
	with := func(r map[string]any) map[string]any {
		return map[string]any{"name": "Checked", "steps": []any{flowStep(st["Draft"], true), flowStep(st["In review"], false)},
			"transitions": []any{flowEdge("Submit", st["Draft"], st["In review"], r)}}
	}

	for _, c := range []struct {
		what, says string
		rule       map[string]any
	}{
		{"a rule nobody knows", "Submit carries a rule this server does not know: validator.moon_phase.", flowRule("validator", "validator.moon_phase", nil)},
		{"a links rule naming no link type", "Submit: the rule Links of a type it names no link type; pick one.",
			flowRule("validator", "validator.links_exist", map[string]any{"direction": "outgoing"})},
		{"a links rule naming another project's link type", "Submit: the rule Links of a type names a link type this project does not have. Pick one of its link types.",
			flowRule("validator", "validator.links_exist", map[string]any{"linkTypeId": foreign, "direction": "incoming"})},
		{"a least score above the most", "Submit: the rule Quality score give the least score as a whole number from 0 to 100.",
			flowRule("validator", "validator.quality_min", map[string]any{"minimum": 101})},
		{"a least score below nothing", "Submit: the rule Quality score give the least score as a whole number from 0 to 100.",
			flowRule("validator", "validator.quality_min", map[string]any{"minimum": -1})},
		{"a role no project has", `Submit: the rule Project role "owner" is not a project role; pick viewer, reviewer, editor or admin.`,
			flowRule("condition", "condition.project_role", map[string]any{"roles": []string{"owner"}})},
	} {
		// The designer shows a graph's refusal on its transitions, so the field is theirs and the sentence names the rule.
		if msg := refuse422(t, c.what, s.f.owner.put(t, path, with(c.rule)), "transitions"); msg != c.says {
			t.Errorf("%s: the refusal says %q", c.what, msg)
		}
	}
	got := obj(t, want(t, s.f.owner.get(t, path), http.StatusOK, "read the workflow"), "workflow")
	edges := got["transitions"].([]any)
	if len(edges) != 1 || len(edges[0].(map[string]any)["rules"].([]any)) != 0 {
		t.Errorf("after the refusals the workflow reads %v", edges)
	}

	t.Run("a links rule naming the project's own link type, or one of every project's, is saved", func(t *testing.T) {
		want(t, s.f.owner.put(t, path, with(flowRule("validator", "validator.links_exist", map[string]any{"linkTypeId": own, "direction": "incoming"}))),
			http.StatusOK, "the project's own link type")
		var derives string
		if err := s.f.h.super.QueryRow(t.Context(), `SELECT id FROM link_type WHERE key = 'derives' AND project_id IS NULL`).Scan(&derives); err != nil {
			t.Fatal(err)
		}
		want(t, s.f.owner.put(t, path, with(flowRule("validator", "validator.links_exist", map[string]any{"linkTypeId": derives}))),
			http.StatusOK, "a link type of every project")
	})
}

// A workflow and a status are reached only through their own project, a graph names only its statuses, and an
// assignment maps only onto statuses the workflow has (#322).
func TestWorkflowsAndStatusesStayInTheirProject(t *testing.T) {
	s := newFlowScene(t, "WXP")
	owner := s.f.owner
	theirs := s.workflowsOf(t, s.other)["Default workflow"]
	theirsID := theirs["id"].(string)

	t.Run("another project's workflow is not found here, and stays as it was", func(t *testing.T) {
		at := s.in(s.key, "/workflows/"+theirsID)
		graph := map[string]any{"name": "Taken over", "steps": []any{flowStep(s.abroad["Draft"], true)}, "transitions": []any{}}
		refuse(t, "reading it here", owner.get(t, at), http.StatusNotFound, "not_found")
		refuse(t, "saving it here", owner.put(t, at, graph), http.StatusNotFound, "not_found")
		refuse(t, "deleting it here", owner.delete(t, at), http.StatusNotFound, "not_found")
		got := obj(t, want(t, owner.get(t, s.in(s.other, "/workflows/"+theirsID)), http.StatusOK, "read it at home"), "workflow")
		if got["name"] != "Default workflow" || len(got["steps"].([]any)) != len(theirs["steps"].([]any)) {
			t.Errorf("the other project's workflow reads %v", got["name"])
		}
	})

	t.Run("a graph naming another project's status is refused on steps", func(t *testing.T) {
		mine := s.workflowsOf(t, s.key)["Default workflow"]
		graph := map[string]any{"name": "Mixed", "steps": []any{flowStep(s.statuses["Draft"], true), flowStep(s.abroad["Approved"], false)}, "transitions": []any{}}
		refuse422(t, "a new workflow naming it", owner.post(t, s.in(s.key, "/workflows"), graph), "steps")
		graph["name"] = "Default workflow"
		refuse422(t, "a saved workflow naming it", owner.put(t, s.in(s.key, "/workflows/"+mine["id"].(string)), graph), "steps")
		if _, ok := s.workflowsOf(t, s.key)["Mixed"]; ok {
			t.Error("the workflow naming another project's status was made")
		}
		if n := s.f.h.countRows(t, `SELECT count(*) FROM workflow_step WHERE status_id = $1 AND project_id = $2`, s.abroad["Approved"], s.projectID); n != 0 {
			t.Errorf("%d steps here name the other project's status", n)
		}
	})

	t.Run("an assignment mapping onto a status the workflow lacks is refused, and nothing moves", func(t *testing.T) {
		key := s.write(t, "Brakes engage within 100 ms")
		want(t, s.take(t, s.editor, key, "Submit for review"), http.StatusOK, "submit it")
		short := obj(t, want(t, owner.post(t, s.in(s.key, "/workflows"), map[string]any{"name": "Short",
			"steps": []any{flowStep(s.statuses["Draft"], true), flowStep(s.statuses["Approved"], false)}, "transitions": []any{}}), http.StatusCreated, "make Short"), "workflow")
		r := owner.put(t, s.in(s.key, "/workflow"), map[string]any{"workflowId": short["id"], "statusMapping": map[string]any{s.statuses["In review"]: s.statuses["Verified"]}})
		if msg := refuse422(t, "mapping onto Verified", r, "statusMapping"); !strings.Contains(msg, "In review") {
			t.Errorf("the refusal says %q", msg)
		}
		s.stays(t, key, "In review")
		if flows := s.workflowsOf(t, s.key); flows["Default workflow"]["projectDefault"] != true || flows["Short"]["projectDefault"] != false {
			t.Error("the refused assignment changed which workflow the project follows")
		}
	})

	t.Run("a status is renamed only to a free name, kept while a workflow names it, and reached only here", func(t *testing.T) {
		statuses := s.in(s.key, "/statuses/")
		r := owner.patch(t, statuses+s.statuses["Obsolete"], map[string]any{"name": "approved"})
		if msg := refuse422(t, "a name another status has", r, "name"); msg != "Another status of this project is called that. Pick another name." {
			t.Errorf("the refusal says %q", msg)
		}
		refuse422(t, "a blank name", owner.patch(t, statuses+s.statuses["Obsolete"], map[string]any{"name": " "}), "name")
		// Obsolete is in the default workflow, and no requirement is in it.
		refuse(t, "deleting a status a workflow names", owner.delete(t, statuses+s.statuses["Obsolete"]), http.StatusConflict, "in_use")
		refuse(t, "renaming another project's status here", owner.patch(t, statuses+s.abroad["Obsolete"], map[string]any{"name": "Gone"}), http.StatusNotFound, "not_found")
		refuse(t, "deleting another project's status here", owner.delete(t, statuses+s.abroad["Obsolete"]), http.StatusNotFound, "not_found")
		refuse(t, "a status id that is none", owner.delete(t, statuses+"nope"), http.StatusBadRequest, "bad_request")
		if got, there := s.statusesOf(t, s.key), s.statusesOf(t, s.other); got["Obsolete"] == "" || there["Obsolete"] == "" || got["Gone"] != "" || there["Gone"] != "" {
			t.Errorf("the refusals changed the statuses: %v and %v", got, there)
		}
	})

	t.Run("a requirement's reader takes no transition", func(t *testing.T) {
		key := s.write(t, "Brakes hold on a slope")
		refuse(t, "a reader submits", s.take(t, s.reader, key, "Submit for review"), http.StatusForbidden, "forbidden")
		s.stays(t, key, "Draft")
		if got := s.offered(t, s.reader, key); len(got) != 0 {
			t.Errorf("a reader is offered %v", got)
		}
	})

	t.Run("a reader's bulk transition is refused on each requirement, and nothing moves", func(t *testing.T) {
		keys := []string{s.write(t, "Brakes hold in the wet"), s.write(t, "Brakes hold in the cold")}
		submit := s.transitionID(t, keys[0], "Submit for review")
		// The bulk route answers 200 as a whole and refuses each requirement as the single route would.
		results := list(t, want(t, s.reader.post(t, s.in(s.key, "/transitions"), map[string]any{"transitionId": submit, "keys": keys}),
			http.StatusOK, "a reader submits two"), "results")
		if len(results) != len(keys) {
			t.Fatalf("the reader's bulk transition answers %v", results)
		}
		for i, key := range keys {
			r := results[i].(map[string]any)
			refusal, _ := r["error"].(map[string]any)
			if r["key"] != key || r["outcome"] != "refused" || refusal == nil || refusal["code"] != "forbidden" {
				t.Errorf("the reader's result for %s is %v", key, r)
			}
			s.stays(t, key, "Draft")
		}
	})

	t.Run("a workflow id that is none is a bad address, and a followed one stays", func(t *testing.T) {
		refuse(t, "deleting a workflow id that is none", owner.delete(t, s.in(s.key, "/workflows/nope")), http.StatusBadRequest, "bad_request")
		mine := s.workflowsOf(t, s.key)["Default workflow"]
		refuse(t, "deleting the followed workflow", owner.delete(t, s.in(s.key, "/workflows/"+mine["id"].(string))), http.StatusConflict, "in_use")
		if _, ok := s.workflowsOf(t, s.key)["Default workflow"]; !ok {
			t.Error("the followed workflow went")
		}
	})
}

// Types and attributes are renamed only to free names and changed only by administrators, the archive takes no new
// shape, and field rules hold to the fixed fields and the project roles (#322).
func TestTypesAttributesAndFieldRulesRefuseWhatTheyCannotHold(t *testing.T) {
	s := newFlowScene(t, "WTA")
	owner := s.f.owner
	types, attrs := s.in(s.key, "/types/"), s.in(s.key, "/attributes/")
	iface := obj(t, want(t, owner.post(t, s.in(s.key, "/types"), map[string]any{"name": "Interface", "icon": "interface", "colour": "#4f6bed"}),
		http.StatusCreated, "make Interface"), "type")["id"].(string)
	notes := obj(t, want(t, owner.post(t, s.in(s.key, "/attributes"), map[string]any{"name": "Notes", "kind": "text"}), http.StatusCreated, "define Notes"),
		"attribute")["id"].(string)
	want(t, owner.post(t, s.in(s.key, "/attributes"), map[string]any{"name": "Risk", "kind": "text"}), http.StatusCreated, "define Risk")
	abroadType := obj(t, want(t, owner.post(t, s.in(s.other, "/types"), map[string]any{"name": "Hazard", "icon": "risk", "colour": "#dc2626"}),
		http.StatusCreated, "a type of the other project"), "type")["id"].(string)
	abroadAttr := obj(t, want(t, owner.post(t, s.in(s.other, "/attributes"), map[string]any{"name": "Cost", "kind": "number"}),
		http.StatusCreated, "an attribute of the other project"), "attribute")["id"].(string)
	typeNames := func(key string) []string {
		var out []string
		for _, each := range list(t, want(t, owner.get(t, s.in(key, "/types")), http.StatusOK, "list the types"), "types") {
			out = append(out, each.(map[string]any)["name"].(string))
		}
		return out
	}
	attrNames := func(key string) []string {
		var out []string
		for _, each := range list(t, want(t, owner.get(t, s.in(key, "/attributes")), http.StatusOK, "list the attributes"), "attributes") {
			out = append(out, each.(map[string]any)["name"].(string))
		}
		return out
	}

	t.Run("a rename to a name another has is refused on name, and an editor renames nothing", func(t *testing.T) {
		if msg := refuse422(t, "a type named as the default", owner.patch(t, types+iface, map[string]any{"name": "requirement"}), "name"); msg !=
			"Another type of this project is called that. Pick another name." {
			t.Errorf("the refusal says %q", msg)
		}
		refuse422(t, "an attribute named as another", owner.patch(t, attrs+notes, map[string]any{"name": "RISK"}), "name")
		refuse(t, "an editor renames a type", s.editor.patch(t, types+iface, map[string]any{"name": "Mine"}), http.StatusForbidden, "forbidden")
		sameList(t, "the types", typeNames(s.key), "Requirement", "Interface")
		sameList(t, "the attributes", attrNames(s.key), "Notes", "Risk")
	})

	t.Run("a type moved before the first is first", func(t *testing.T) {
		moved := obj(t, want(t, owner.patch(t, types+iface, map[string]any{"position": -3}), http.StatusOK, "move Interface before the first"), "type")
		if moved["position"] != float64(0) {
			t.Errorf("Interface is at %v", moved["position"])
		}
		sameList(t, "the types", typeNames(s.key), "Interface", "Requirement")
	})

	t.Run("another project's type and attribute are not found here, and a bad id is a bad address", func(t *testing.T) {
		refuse(t, "deleting another project's type here", owner.delete(t, types+abroadType), http.StatusNotFound, "not_found")
		refuse(t, "deleting another project's attribute here", owner.delete(t, attrs+abroadAttr), http.StatusNotFound, "not_found")
		refuse(t, "a type id that is none", owner.delete(t, types+"nope"), http.StatusBadRequest, "bad_request")
		refuse(t, "an attribute id that is none", owner.delete(t, attrs+"nope"), http.StatusBadRequest, "bad_request")
		sameList(t, "the other project's types", typeNames(s.other), "Requirement", "Hazard")
		sameList(t, "the other project's attributes", attrNames(s.other), "Cost")
	})

	t.Run("field rules hold to the fixed fields and the project roles, and are hidden from guests", func(t *testing.T) {
		rules := s.in(s.key, "/field-rules")
		refuse(t, "a field that is not fixed", owner.put(t, rules+"/colour", map[string]any{"editableBy": []any{}}), http.StatusNotFound, "not_found")
		if msg := refuse422(t, "a role nobody has", owner.put(t, rules+"/priority", map[string]any{"editableBy": []map[string]any{{"roles": []string{"boss"}}}}),
			"editableBy"); msg != `"boss" is not a project role; pick viewer, reviewer, editor or admin.` {
			t.Errorf("the refusal says %q", msg)
		}
		refuse(t, "a guest reads the field rules", s.f.guest.get(t, rules), http.StatusNotFound, "not_found")
		if n := s.f.h.countRows(t, `SELECT count(*) FROM field_rule WHERE project_id = $1`, s.projectID); n != 0 {
			t.Errorf("the refusals left %d field rules", n)
		}
	})

	t.Run("an archived project takes no new type, attribute or field rule", func(t *testing.T) {
		want(t, owner.put(t, s.in(s.key, "/archive"), nil), http.StatusOK, "archive the project")
		refuse(t, "a type", owner.post(t, s.in(s.key, "/types"), map[string]any{"name": "Late", "icon": "need", "colour": "#000000"}), http.StatusConflict, "project_archived")
		refuse(t, "an attribute", owner.post(t, s.in(s.key, "/attributes"), map[string]any{"name": "Late", "kind": "text"}), http.StatusConflict, "project_archived")
		refuse(t, "a field rule", owner.put(t, s.in(s.key, "/field-rules/owner"), map[string]any{"requiredFrom": s.statuses["In review"]}),
			http.StatusConflict, "project_archived")
		sameList(t, "the archived project's types", typeNames(s.key), "Interface", "Requirement")
		sameList(t, "the archived project's attributes", attrNames(s.key), "Notes", "Risk")
		if n := s.f.h.countRows(t, `SELECT count(*) FROM field_rule WHERE project_id = $1`, s.projectID); n != 0 {
			t.Errorf("the archived project holds %d field rules", n)
		}
	})
}

// An assistant lists the transitions its person is offered and takes one, and is told why it may not take another
// (#322).
func TestAnAssistantTakesATransition(t *testing.T) {
	s := newFlowScene(t, "WMC")
	key := s.write(t, "Brakes engage within 100 ms")
	_, secret := makeToken(t, s.editor, map[string]any{"name": "assistant"})
	s.f.h.settle(t)
	a := connectAssistant(t, s.f.api, secret)

	listed := a.call(t, "list_transitions", map[string]any{"reqKey": key})
	var names []string
	for _, each := range listed["transitions"].([]any) {
		names = append(names, each.(map[string]any)["name"].(string))
	}
	sameList(t, "the transitions an assistant lists", names, s.offered(t, s.editor, key)...)
	if len(names) == 0 {
		t.Fatal("the editor is offered nothing on a draft")
	}

	approve := s.transitionID(t, key, "Approve")
	got := a.call(t, "transition_requirement", map[string]any{"reqKey": key, "transitionId": s.transitionID(t, key, "Submit for review")})
	if got["requirement"].(map[string]any)["status"].(map[string]any)["name"] != "In review" {
		t.Errorf("the transition answered %v", got["requirement"])
	}
	// The default workflow leaves approving to reviewers and administrators, and the editor is neither.
	if msg := a.refused(t, "transition_requirement", map[string]any{"reqKey": key, "transitionId": approve}); msg !=
		"Approve is not yours to take. Only people holding one of these roles in the project may take it. Pick a transition you are offered." {
		t.Errorf("the refusal says %q", msg)
	}
	s.stays(t, key, "In review")
}

// Straight through SQL as exciter_app, an editor puts no rule on a transition and moves no step, and a project's
// administrator names no other project's status in a step (#322).
func TestRawSQLKeepsTransitionRulesAndStepsToTheAdministrators(t *testing.T) {
	s := newFlowScene(t, "WSQ")
	adminID := s.f.h.addPerson(t, "member")
	s.f.h.grantAs(t, s.projectID, adminID, "admin")
	ctx := t.Context()
	var flow, submit uuid.UUID
	if err := s.f.h.super.QueryRow(ctx, `SELECT w.id, t.id FROM workflow w JOIN workflow_transition t ON t.workflow_id = w.id
		WHERE w.project_id = $1 AND t.name = 'Submit for review'`, s.projectID).Scan(&flow, &submit); err != nil {
		t.Fatal(err)
	}
	rulesOn := func() int {
		return s.f.h.countRows(t, `SELECT count(*) FROM transition_rule WHERE transition_id = $1`, submit)
	}
	before := rulesOn()

	t.Run("an editor puts no rule on a transition and moves no step", func(t *testing.T) {
		editor := s.f.h.appConn(t, s.editorID)
		if _, err := editor.Exec(ctx, `INSERT INTO transition_rule (transition_id, kind, type, config, position)
			VALUES ($1, 'postfunction', 'postfunction.clear_owner', '{}', 9)`, submit); !isPolicyViolation(err) {
			t.Errorf("an editor put a rule on a transition: %v", err)
		}
		touchesNothing(t, editor, "an editor moves a step to another status",
			`UPDATE workflow_step SET status_id = $3 WHERE workflow_id = $1 AND status_id = $2`, flow, s.statuses["Obsolete"], s.statuses["Verified"])
		if got := rulesOn(); got != before {
			t.Errorf("the transition carries %d rules, want %d", got, before)
		}
		if n := s.f.h.countRows(t, `SELECT count(*) FROM workflow_step WHERE workflow_id = $1 AND status_id = $2`, flow, s.statuses["Obsolete"]); n != 1 {
			t.Error("the step of Obsolete moved")
		}
	})

	t.Run("a project administrator names no other project's status in a step", func(t *testing.T) {
		admin := s.f.h.appConn(t, adminID)
		if _, err := admin.Exec(ctx, `INSERT INTO workflow_step (workflow_id, status_id) VALUES ($1, $2)`, flow, s.abroad["Draft"]); !refusedBy(err,
			"workflow_step_status_of_project") {
			t.Errorf("a step named another project's status: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE workflow_step SET on_revision_status_id = $3 WHERE workflow_id = $1 AND status_id = $2`,
			flow, s.statuses["Approved"], s.abroad["Draft"]); !refusedBy(err, "workflow_step_on_revision_status_of_project") {
			t.Errorf("a step sent a new revision to another project's status: %v", err)
		}
		if n := s.f.h.countRows(t, `SELECT count(*) FROM workflow_step WHERE workflow_id = $1 AND (status_id = $2 OR on_revision_status_id = $2)`,
			flow, s.abroad["Draft"]); n != 0 {
			t.Errorf("%d steps name the other project's status", n)
		}
		// The same administrator still shapes the project's own steps.
		if n := affects(t, admin, `UPDATE workflow_step SET layout_x = 40 WHERE workflow_id = $1 AND status_id = $2`, flow, s.statuses["Draft"]); n != 1 {
			t.Errorf("the administrator moved %d steps on the canvas, want 1", n)
		}
	})
}

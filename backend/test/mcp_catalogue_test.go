//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cloudster1/exciter/backend/internal/stator"
)

// The tools of the catalogue no other test called through the protocol's client, and the refusals their items
// name (#329): each answer is held to its output schema by callTool and compared with the route it stands for.

// limitedTo makes the owner a token reaching one other project alone, and connects an assistant with it.
func limitedTo(t *testing.T, h *harness, owner *client, prefix string) (*assistant, string) {
	t.Helper()
	other := projectKey(t, prefix)
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": other}), http.StatusCreated, "make "+other)
	_, secret := makeToken(t, owner, map[string]any{"name": "elsewhere", "projects": []string{other}})
	h.settle(t)
	return connectAssistant(t, owner.api, secret), other
}

// refusedAsUnseen holds a tool to refusing a key outside the token's projects as one it cannot see.
func refusedAsUnseen(t *testing.T, a *assistant, name string, args map[string]any) {
	t.Helper()
	if said := a.refused(t, name, args); !strings.Contains(said, "not found") || !strings.HasSuffix(said, ".") {
		t.Errorf("%s outside the token's project says %q", name, said)
	}
}

// An assistant reads a project as its person does: ten reading tools answer what their routes answer, and a token
// limited to another project finds neither the project nor its tree.
func TestAnAssistantReadsAProjectAsTheRoutesDo(t *testing.T) {
	s := newBaselineScene(t, "MCR")
	owner, r1 := s.owner, s.req("1")
	project := "/api/v1/projects/" + s.key
	want(t, owner.post(t, "/api/v1/requirements/"+r1+"/comments", map[string]any{"bodyMarkdown": "Is 100 ms **enough**?"}), http.StatusCreated, "comment on R1")
	want(t, owner.post(t, project+"/terms", map[string]any{"name": "Brake pedal"}), http.StatusCreated, "define a term")
	dashboard := obj(t, want(t, owner.post(t, project+"/dashboards", map[string]any{"name": "Brakes", "shared": true}), http.StatusCreated, "make a dashboard"),
		"dashboard")["id"].(string)
	widget := obj(t, want(t, owner.post(t, "/api/v1/dashboards/"+dashboard+"/widgets", map[string]any{"kind": "breakdown", "title": "By priority",
		"config": map[string]any{"groupBy": "priority"}}), http.StatusCreated, "add a widget"), "widget")["id"].(string)
	name := "Quillon " + s.h.suffix()
	s.h.namedPerson(t, name)
	_, secret := makeToken(t, owner, map[string]any{"name": "reading assistant"})
	s.h.settle(t)
	a := connectAssistant(t, owner.api, secret)

	for _, c := range []struct {
		tool string
		args map[string]any
		path string
	}{
		{"get_project", map[string]any{"projectKey": s.key}, project},
		{"get_tree", map[string]any{"projectKey": s.key}, project + "/tree"},
		{"list_requirements", map[string]any{"projectKey": s.key, "q": "brake", "kind": []string{"requirement"}}, project + "/requirements?q=brake&kind=requirement"},
		{"search_requirements", map[string]any{"q": "brake", "project": []string{s.key}}, "/api/v1/search?q=brake&project=" + s.key},
		{"list_links", map[string]any{"reqKey": r1}, "/api/v1/requirements/" + r1 + "/links"},
		{"list_comments", map[string]any{"reqKey": r1}, "/api/v1/requirements/" + r1 + "/comments?format=markdown"},
		{"list_terms", map[string]any{"projectKey": s.key}, project + "/terms"},
		{"list_people", map[string]any{"q": name}, "/api/v1/people?q=" + url.QueryEscape(name)},
		{"list_dashboards", map[string]any{"projectKey": s.key}, project + "/dashboards"},
		{"get_dashboard_widget", map[string]any{"widgetID": widget}, "/api/v1/widgets/" + widget + "/data"},
	} {
		sameAnswer(t, c.tool, a.call(t, c.tool, c.args), want(t, owner.get(t, c.path), http.StatusOK, c.path))
	}

	// What each answer holds, so agreeing with an empty route proves nothing.
	if got := obj(t, response{Body: a.call(t, "get_project", map[string]any{"projectKey": s.key})}, "project"); got["key"] != s.key {
		t.Errorf("get_project answers %v", got)
	}
	if links := list(t, response{Body: a.call(t, "list_links", map[string]any{"reqKey": r1})}, "links"); len(links) != 1 {
		t.Errorf("R1 lists %d links, want the one R3 derives from it", len(links))
	}
	threads := list(t, response{Body: a.call(t, "list_comments", map[string]any{"reqKey": r1})}, "threads")
	if len(threads) != 1 || !strings.Contains(threads[0].(map[string]any)["comments"].([]any)[0].(map[string]any)["bodyMarkdown"].(string), "**enough**") {
		t.Errorf("R1's discussion reads %v", threads)
	}
	if people := list(t, response{Body: a.call(t, "list_people", map[string]any{"q": name})}, "people"); len(people) != 1 || people[0].(map[string]any)["name"] != name {
		t.Errorf("list_people finds %v for %q", people, name)
	}
	if hits := list(t, response{Body: a.call(t, "search_requirements", map[string]any{"q": "brake", "project": []string{s.key}})}, "hits"); len(hits) == 0 {
		t.Error("search_requirements finds nothing of brake")
	}

	t.Run("a token limited to another project", func(t *testing.T) {
		elsewhere, other := limitedTo(t, s.h, owner, "MCRO")
		refusedAsUnseen(t, elsewhere, "get_project", map[string]any{"projectKey": s.key})
		refusedAsUnseen(t, elsewhere, "get_tree", map[string]any{"projectKey": s.key})
		listed := list(t, response{Body: elsewhere.call(t, "list_projects", map[string]any{})}, "projects")
		if len(listed) != 1 || listed[0].(map[string]any)["key"] != other {
			t.Errorf("list_projects through the limited token lists %v, want %s alone", listed, other)
		}
		who := obj(t, response{Body: elsewhere.call(t, "whoami", map[string]any{})}, "token")
		if who["name"] != "elsewhere" || who["allProjects"] != false || len(who["projects"].([]any)) != 1 {
			t.Errorf("whoami through the limited token answers %v", who)
		}
	})
}

// server_info answers what GET /meta answers; list_audit_log lists to an administrator's token what the test just
// did, refuses a member's naming who may, and is not offered to a token limited to projects.
func TestAnAssistantReadsTheInstallation(t *testing.T) {
	key := projectKey(t, "MCI")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	_, adminSecret := makeToken(t, f.owner, map[string]any{"name": "administrator's assistant"})
	_, memberSecret := makeToken(t, f.plain, map[string]any{"name": "member's assistant"})
	f.h.settle(t)
	admin, member := connectAssistant(t, f.api, adminSecret), connectAssistant(t, f.api, memberSecret)

	info := admin.call(t, "server_info", map[string]any{})
	sameAnswer(t, "server_info", info, want(t, f.api.anonymous().get(t, "/api/v1/meta"), http.StatusOK, "read the meta"))
	if info["product"] == "" || info["apiVersion"] == "" {
		t.Errorf("server_info answers %v", info)
	}

	args := map[string]any{"action": "project.created", "actor": f.home.user.String()}
	logged := admin.call(t, "list_audit_log", args)
	sameAnswer(t, "list_audit_log", logged, want(t, f.owner.get(t, "/api/v1/audit?action=project.created&actor="+f.home.user.String()), http.StatusOK, "read the log"))
	found := false
	for _, each := range list(t, response{Body: logged}, "entries") {
		if e := each.(map[string]any); e["action"] == "project.created" && strings.Contains(string(mustJSON(t, e)), key) {
			found = true
		}
	}
	if !found {
		t.Errorf("list_audit_log does not list making %s: %v", key, logged["entries"])
	}
	if said := member.refused(t, "list_audit_log", map[string]any{}); !strings.Contains(said, "administrator") {
		t.Errorf("a member's list_audit_log says %q, want who may", said)
	}

	elsewhere, _ := limitedTo(t, f.h, f.owner, "MCIO")
	if _, offered := elsewhere.tools["list_audit_log"]; offered {
		t.Error("a token limited to projects is offered list_audit_log, which concerns the whole installation")
	}
	if _, offered := elsewhere.tools["get_project"]; !offered {
		t.Error("a token limited to projects is not offered get_project, so the check above proves nothing")
	}
}

// write_requirements is written with the project and the source in place, and names the tool that writes.
func TestTheWriteRequirementsPromptEmbedsItsSource(t *testing.T) {
	key := projectKey(t, "MCW")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	_, secret := makeToken(t, f.owner, map[string]any{"name": "drafting assistant"})
	f.h.settle(t)
	a := connectAssistant(t, f.api, secret)
	source := "The pump shall stop within 2 s of the float switch opening."
	for name, args := range map[string]map[string]string{
		"the whole project": {"projectKey": key, "source": source},
		"under a section":   {"projectKey": key, "source": source, "section": key + "-R7"},
	} {
		got, err := a.session.GetPrompt(a.ctx, &mcp.GetPromptParams{Name: "write_requirements", Arguments: args})
		if err != nil || len(got.Messages) != 1 {
			t.Fatalf("%s: write_requirements answers %+v, %v", name, got, err)
		}
		text := got.Messages[0].Content.(*mcp.TextContent).Text
		for _, wanted := range []string{"<source>\n" + source + "\n</source>", "call create_requirement with projectKey " + key, "project " + key} {
			if !strings.Contains(text, wanted) {
				t.Errorf("%s: the prompt lacks %q: %s", name, wanted, text)
			}
		}
		if under := strings.Contains(text, "under "+key+"-R7"); under != (args["section"] != "") {
			t.Errorf("%s: the prompt places the requirements under a section: %t", name, under)
		}
	}
}

// The evidence and trace tools answer a requirement with a result and a tag, and a token limited to another
// project gets a refusal from each.
func TestTheEvidenceToolsKeepToTheTokensProjects(t *testing.T) {
	s := newBaselineScene(t, "MCE")
	r1 := s.req("1")
	ran(t, s.bob, s.key, runForm("main"), junit([2]string{"engage [verifies " + r1 + "]", "passed"}))
	scanned(t, s.bob, s.key, scanBody("main", tag("brakes/engage.go", 12, "implements", r1)))
	_, secret := makeToken(t, s.owner, map[string]any{"name": "assistant"})
	s.h.drained(t)
	a := connectAssistant(t, s.owner.api, secret)
	elsewhere, _ := limitedTo(t, s.h, s.owner, "MCEO")

	for _, c := range []struct {
		tool, path, holds string
		args              map[string]any
	}{
		{"get_test_results", "/api/v1/requirements/" + r1 + "/test-results", "engage", map[string]any{"reqKey": r1}},
		{"get_code_links", "/api/v1/requirements/" + r1 + "/code-links", "brakes/engage.go", map[string]any{"reqKey": r1}},
		{"run_trace_check", "/api/v1/projects/" + s.key + "/trace-check", `"evidence":{"runs":1,"scans":1}`, map[string]any{"projectKey": s.key}},
		{"get_trace_graph", "/api/v1/requirements/" + r1 + "/graph", s.req("3"), map[string]any{"reqKey": r1}},
	} {
		got := a.call(t, c.tool, c.args)
		sameAnswer(t, c.tool, got, want(t, s.owner.get(t, c.path), http.StatusOK, c.path))
		if !strings.Contains(string(mustJSON(t, got)), c.holds) {
			t.Errorf("%s answers nothing of %s: %v", c.tool, c.holds, got)
		}
		refusedAsUnseen(t, elsewhere, c.tool, c.args)
	}
}

// The risk tools count a rated risk in its cell and name the requirement that mitigates it.
func TestTheRiskToolsCountTheCellAndNameTheMitigation(t *testing.T) {
	s := newRiskScene(t, "MCK")
	want(t, s.f.owner.post(t, "/api/v1/requirements/"+s.plainKey+"/links", map[string]any{"typeKey": "mitigates", "otherKey": s.riskKey}),
		http.StatusCreated, "mitigate the risk")
	_, secret := makeToken(t, s.f.owner, map[string]any{"name": "assistant"})
	s.f.h.settle(t)
	a := connectAssistant(t, s.f.api, secret)

	matrix := obj(t, response{Body: a.call(t, "get_risk_matrix", map[string]any{"projectKey": s.key, "side": "before"})}, "matrix")
	total := 0
	for _, each := range matrix["cells"].([]any) {
		cell := each.(map[string]any)
		n := int(cell["count"].(float64))
		total += n
		if cell["severity"] == float64(4) && cell["probability"] == float64(3) && n != 1 {
			t.Errorf("the cell 4, 3 counts %d, want the one risk", n)
		}
	}
	if total != 1 {
		t.Errorf("the matrix counts %d risks in all, want 1", total)
	}
	mitigations := a.call(t, "get_risk_mitigations", map[string]any{"reqKey": s.riskKey})["mitigations"].([]any)
	if len(mitigations) != 1 || mitigations[0].(map[string]any)["source"].(map[string]any)["key"] != s.plainKey {
		t.Errorf("get_risk_mitigations answers %v, want %s", mitigations, s.plainKey)
	}
	risks := list(t, response{Body: a.call(t, "list_risks", map[string]any{"projectKey": s.key})}, "risks")
	if len(risks) != 1 || risks[0].(map[string]any)["risk"].(map[string]any)["key"] != s.riskKey {
		t.Errorf("list_risks lists %v, want %s", risks, s.riskKey)
	}
	if scheme := obj(t, response{Body: a.call(t, "get_risk_scheme", map[string]any{"projectKey": s.key})}, "scheme"); scheme["method"] != "matrix" {
		t.Errorf("get_risk_scheme answers %v", scheme)
	}
	if report := a.call(t, "get_risk_report", map[string]any{"projectKey": s.key}); !strings.Contains(string(mustJSON(t, report)), s.riskKey) {
		t.Errorf("get_risk_report names nothing of %s: %v", s.riskKey, report)
	}
}

// A transition takes no baseRevision, so a call carrying one is refused naming it and moves nothing; the call
// without it moves the status.
func TestATransitionTakesNoBaseRevision(t *testing.T) {
	key := projectKey(t, "MCT")
	f := newProjectFixture(t)
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	reqKey := key + "-R1"
	path := "/api/v1/requirements/" + reqKey
	want(t, f.owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage"}), http.StatusCreated, "write R1")
	want(t, f.owner.patch(t, path, map[string]any{"title": "Brakes engage at once", "baseRevision": 1}), http.StatusOK, "write revision 2")
	_, secret := makeToken(t, f.owner, map[string]any{"name": "assistant"})
	f.h.settle(t)
	a := connectAssistant(t, f.api, secret)
	status := func() string {
		return obj(t, want(t, f.owner.get(t, path), http.StatusOK, "read R1"), "requirement")["status"].(map[string]any)["name"].(string)
	}
	before := status()
	var submit string
	for _, each := range list(t, response{Body: a.call(t, "list_transitions", map[string]any{"reqKey": reqKey})}, "transitions") {
		if tr := each.(map[string]any); tr["name"] == "Submit for review" {
			submit = tr["id"].(string)
		}
	}
	if submit == "" {
		t.Fatal("the owner is not offered Submit for review")
	}

	if said := a.refused(t, "transition_requirement", map[string]any{"reqKey": reqKey, "transitionId": submit, "baseRevision": 1}); !strings.Contains(said, `"baseRevision"`) {
		t.Errorf("a transition with a stale baseRevision says %q, want it to name baseRevision", said)
	}
	if now := status(); now != before {
		t.Errorf("the refused transition moved R1 from %s to %s", before, now)
	}
	moved := a.call(t, "transition_requirement", map[string]any{"reqKey": reqKey, "transitionId": submit})
	if got := moved["requirement"].(map[string]any)["status"].(map[string]any)["name"]; got != "In review" {
		t.Errorf("the transition answers %v", got)
	}
	f.h.settle(t)
	if now := status(); now != "In review" {
		t.Errorf("after the transition R1 reads %s", now)
	}
}

// A tester's read-only token records no result: record_test_result is a refusal saying it can only read, and the
// case reads as it did.
func TestAReadOnlyTokenRecordsNoTestResult(t *testing.T) {
	s := newExecutionScene(t, "MCX")
	r2 := s.key + "-R2"
	_, secret := makeToken(t, s.f.plain, map[string]any{"name": "reading assistant", "scopes": []string{"read"}})
	s.f.h.settle(t)
	a := connectAssistant(t, s.f.api, secret)
	if _, offered := a.tools["record_test_result"]; offered {
		t.Error("a read-only token is offered record_test_result")
	}
	if said := a.refused(t, "record_test_result", map[string]any{"executionID": s.execution, "reqKey": r2, "iteration": 1, "outcome": "passed"}); !strings.Contains(said, "can only read") {
		t.Errorf("record_test_result through a read-only token says %q", said)
	}
	if outcome, _, _ := runOf(t, s.f.plain, s.execution, r2, 1); outcome != nil {
		t.Errorf("the refused call recorded %v", outcome)
	}
	sameAnswer(t, "get_test_execution_case", a.call(t, "get_test_execution_case", map[string]any{"executionID": s.execution, "reqKey": r2, "iteration": 1}),
		want(t, s.f.plain.get(t, "/api/v1/test-executions/"+s.execution+"/cases/"+r2+"?iteration=1"), http.StatusOK, "read the case"))
}

// link_armature_issue links a requirement to an issue there is, which list_armature_links then lists as the route does.
func TestAnAssistantLinksAnArmatureIssue(t *testing.T) {
	key := projectKey(t, "MCA")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "mcp-armature-link").user)
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "the owner connects as bob")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	reqKey := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Sign-in takes a second"}), http.StatusCreated,
		"write R1"), "requirement")["key"].(string)
	_, secret := makeToken(t, owner, map[string]any{"name": "assistant"})
	h.settle(t)
	a := connectAssistant(t, api, secret)

	link := a.call(t, "link_armature_issue", map[string]any{"reqKey": reqKey, "issueKey": "CP-1", "relation": "implements"})["link"].(map[string]any)
	if link["issueKey"] != "CP-1" || link["relation"] != "implements" {
		t.Errorf("link_armature_issue answers %v", link)
	}
	h.settle(t)
	listed := a.call(t, "list_armature_links", map[string]any{"reqKey": reqKey})
	sameAnswer(t, "list_armature_links", listed, want(t, owner.get(t, "/api/v1/requirements/"+reqKey+"/armature-links"), http.StatusOK, "list the links"))
	if links := list(t, response{Body: listed}, "links"); len(links) != 1 || links[0].(map[string]any)["id"] != link["id"] {
		t.Errorf("the requirement lists %v, want the link the tool made", links)
	}
}

// clear_stator_link clears a link whose page Stator published again, against the version it now has.
func TestAnAssistantClearsASuspectStatorLink(t *testing.T) {
	key := projectKey(t, "MCS")
	h := newHarness(t)
	h.keepStator(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "mcp-stator-clear").user)
	tenant := stubTenant(t)
	statorStub(t, http.MethodDelete, tenant, nil)
	statorOffers(t)
	want(t, owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, owner.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "the owner connects as bob")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	reqKey := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Brakes engage within 100 ms"}), http.StatusCreated,
		"write R1"), "requirement")["key"].(string)
	page := statorPages(t, tenant)["Brake timing"]
	link := obj(t, want(t, owner.post(t, "/api/v1/requirements/"+reqKey+"/stator-links", map[string]any{"pageId": page, "relation": "specifies"}),
		http.StatusCreated, "link Brake timing"), "link")
	newer := int(link["pageVersion"].(float64)) + 1
	if status, code := api.deliverStator(t, statorHook{id: uuid.New(), topic: stator.TopicPagePublished, payload: pageRef(page, newer, link["spaceKey"].(string))},
		statorWebhookSecret); status != http.StatusNoContent {
		t.Fatalf("Stator publishing the page again answers %d %s", status, code)
	}
	_, secret := makeToken(t, owner, map[string]any{"name": "assistant"})
	h.settle(t)
	a := connectAssistant(t, api, secret)
	if l := list(t, response{Body: a.call(t, "list_stator_links", map[string]any{"reqKey": reqKey})}, "links")[0].(map[string]any); l["suspect"] != true {
		t.Fatalf("after version %d the link reads %v", newer, l)
	}

	cleared := a.call(t, "clear_stator_link", map[string]any{"linkID": link["id"], "pageVersion": newer, "comment": "The timing table still holds."})["link"].(map[string]any)
	if cleared["suspect"] != false || cleared["pageVersionSeen"] != float64(newer) {
		t.Errorf("clear_stator_link answers %v", cleared)
	}
	h.settle(t)
	if l := list(t, want(t, owner.get(t, "/api/v1/requirements/"+reqKey+"/stator-links"), http.StatusOK, "list the links"), "links")[0].(map[string]any); l["suspect"] != false {
		t.Errorf("the route still reads the link suspect: %v", l)
	}
}

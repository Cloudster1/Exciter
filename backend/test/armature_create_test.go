//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/events"
)

// Issues made from requirements (#28), against the stub: filed as the caller, each traced to its requirement at
// once and put on its issue by the worker, stopping at the first Armature refuses.

// stubIssue is an issue as the stub holds it, read as person, with what Exciter does not show.
func stubIssue(t *testing.T, tenant, person, key string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, os.Getenv(envArmatureStub)+"/api/v1/issues/"+key, nil)
	req.Header.Set("Authorization", "Bearer "+patFor(tenant, person))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Issue map[string]any `json:"issue"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || got.Issue == nil {
		t.Fatalf("the stub has no %s for %s: %v", key, person, err)
	}
	return got.Issue
}

// created reads a create's answer as the requirement keys and issue keys it made, in order.
func created(t *testing.T, r response) ([]map[string]any, map[string]any) {
	t.Helper()
	got := obj(t, want(t, r, http.StatusCreated, "make issues"))
	var made []map[string]any
	for _, each := range got["created"].([]any) {
		made = append(made, each.(map[string]any))
	}
	failed, _ := got["failed"].(map[string]any)
	return made, failed
}

func TestIssuesAreMadeFromRequirements(t *testing.T) {
	creKey := projectKey(t, "CRE")
	othKey := projectKey(t, "OTH")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-create")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	carol := api.as(t, h.addPerson(t, "member"))
	daveID := h.addPerson(t, "member")
	dave := api.as(t, daveID)
	eveID := h.addPerson(t, "member")
	eve := api.as(t, eveID)

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "bob")}), http.StatusOK, "bob connects")
	want(t, carol.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "carol")}), http.StatusOK, "carol connects")
	want(t, dave.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "reader")}), http.StatusOK, "dave connects with a read-only token")
	project := obj(t, want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": creKey, "name": "Created"}), http.StatusCreated, "make a project"), "project")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": othKey, "name": "Other"}), http.StatusCreated, "make another project")
	write := func(projectKey, title, kind string) map[string]any {
		t.Helper()
		body := map[string]any{"title": title}
		if kind != "" {
			body["kind"] = kind
		}
		return obj(t, want(t, owner.post(t, "/api/v1/projects/"+projectKey+"/requirements", body), http.StatusCreated, "write "+title), "requirement")
	}
	r1 := write(creKey, "Brakes engage within 100 ms", "")
	write(creKey, "Log every login", "")
	write(creKey, "Safety", "section")
	write(creKey, strings.Repeat("Long title ", 27), "")
	write(othKey, "Elsewhere", "")
	r1Key, r2Key, sectionKey, longKey := creKey+"-R1", creKey+"-R2", creKey+"-R3", creKey+"-R4"
	// Carol reads the project and edits none of it.
	if _, err := h.super.Exec(t.Context(), `UPDATE project_grant SET role = 'viewer' WHERE project_id = $1 AND subject_type = 'everyone'`, project["id"]); err != nil {
		t.Fatal(err)
	}
	for _, editor := range []uuid.UUID{bobID, daveID, eveID} {
		h.grantAs(t, uuid.MustParse(project["id"].(string)), editor, "editor")
	}
	h.runWorker(t, events.NewMux(nil).Route(armature.TopicLinks, h.linkSync(t)))
	issues := "/api/v1/projects/" + creKey + "/armature-issues"

	t.Run("the dialog reads the caller's projects and issue types", func(t *testing.T) {
		got := obj(t, want(t, bob.get(t, "/api/v1/armature/projects"), http.StatusOK, "bob's projects"))
		projects := got["projects"].([]any)
		if got["status"] != "ok" || len(projects) != 1 {
			t.Fatalf("bob's projects are %v", got)
		}
		if cp := projects[0].(map[string]any); cp["key"] != "CP" || cp["name"] != "Core platform" || cp["canCreate"] != true {
			t.Errorf("bob reads CP as %v", cp)
		}
		if cp := list(t, want(t, dave.get(t, "/api/v1/armature/projects"), http.StatusOK, "dave's projects"), "projects")[0].(map[string]any); cp["canCreate"] != false {
			t.Errorf("a read-only token may file in CP: %v", cp)
		}
		types := obj(t, want(t, bob.get(t, "/api/v1/armature/issue-types"), http.StatusOK, "the issue types"))
		var names []string
		for _, each := range types["issueTypes"].([]any) {
			names = append(names, each.(map[string]any)["name"].(string))
		}
		if types["status"] != "ok" || strings.Join(names, " ") != "Task Bug Story Epic" {
			t.Errorf("the issue types are %v", types)
		}
		if got := obj(t, want(t, eve.get(t, "/api/v1/armature/projects"), http.StatusOK, "eve's projects")); got["status"] != "not_connected" || len(got["projects"].([]any)) != 0 {
			t.Errorf("eve without a token reads %v", got)
		}
	})

	t.Run("one issue per requirement, each traced to it and put on it", func(t *testing.T) {
		made, failed := created(t, bob.post(t, issues, map[string]any{"requirementKeys": []string{strings.ToLower(r1Key), r2Key, r1Key},
			"armatureProject": "cp", "relation": "implements"}))
		if failed != nil || len(made) != 2 {
			t.Fatalf("made %v, failed %v", made, failed)
		}
		first := made[0]
		issue, link := first["issue"].(map[string]any), first["link"].(map[string]any)
		if first["requirementKey"] != r1Key || issue["key"] != "CP-6" || issue["summary"] != r1Key+" Brakes engage within 100 ms" || issue["type"] != "Task" ||
			!strings.HasSuffix(issue["url"].(string), "/issues/CP-6") {
			t.Errorf("the first issue is %v", first)
		}
		if link["issueKey"] != "CP-6" || link["relation"] != "implements" || link["statusCategory"] != "todo" || link["createdByName"] == "" {
			t.Errorf("the first link is %v", link)
		}
		if made[1]["requirementKey"] != r2Key || made[1]["issue"].(map[string]any)["key"] != "CP-7" {
			t.Errorf("the second is %v", made[1])
		}
		description, _ := json.Marshal(stubIssue(t, slug, "bob", "CP-6")["description"])
		if !strings.Contains(string(description), armature.RequirementPath+r1["id"].(string)+`"`) || !strings.Contains(string(description), "in Exciter") {
			t.Errorf("CP-6 is described as %s", description)
		}
		if reporter := stubIssue(t, slug, "bob", "CP-6")["reporter"].(map[string]any)["name"]; reporter != "Bob" {
			t.Errorf("CP-6 was filed by %v, want bob", reporter)
		}
		h.drained(t)
		url1 := testAppURL(t) + armature.RequirementPath + r1["id"].(string)
		if l := linksOn(t, slug, url1)["CP-6"]; l == nil || l["title"] != r1Key+" Brakes engage within 100 ms (implements)" || l["createdByName"] != "Bob" {
			t.Errorf("the link on CP-6 is %v", l)
		}
		if l := linkStates(t, carol, r1Key)["CP-6 implements"]; l == nil || l["sync"] != "synced" {
			t.Errorf("carol reads %s's link as %v", r1Key, l)
		}
	})

	t.Run("a type, verifies, and a title Armature's summary cannot hold", func(t *testing.T) {
		var bug string
		for _, each := range list(t, want(t, bob.get(t, "/api/v1/armature/issue-types"), http.StatusOK, "the issue types"), "issueTypes") {
			if each.(map[string]any)["name"] == "Bug" {
				bug = each.(map[string]any)["id"].(string)
			}
		}
		made, failed := created(t, bob.post(t, issues, map[string]any{"requirementKeys": []string{longKey}, "armatureProject": "CP", "typeId": bug, "relation": "verifies"}))
		if failed != nil || len(made) != 1 {
			t.Fatalf("made %v, failed %v", made, failed)
		}
		issue := made[0]["issue"].(map[string]any)
		summary := issue["summary"].(string)
		if issue["type"] != "Bug" || made[0]["link"].(map[string]any)["relation"] != "verifies" ||
			utf8.RuneCountInString(summary) != armature.MaxSummaryLength || !strings.HasPrefix(summary, longKey+" Long title") || !strings.HasSuffix(summary, "...") {
			t.Errorf("the long one was filed as %v", made[0])
		}
	})

	t.Run("a round stops at the first issue Armature refuses", func(t *testing.T) {
		stubControl(t, http.MethodPut, slug+"/refused-summary", map[string]any{"summary": armature.IssueSummary(r2Key, "Log every login")})
		t.Cleanup(func() { stubControl(t, http.MethodPut, slug+"/refused-summary", map[string]any{"summary": ""}) })
		before := len(linkStates(t, bob, longKey))
		made, failed := created(t, bob.post(t, issues, map[string]any{"requirementKeys": []string{r1Key, r2Key, longKey}, "armatureProject": "CP", "relation": "verifies"}))
		if len(made) != 1 || made[0]["requirementKey"] != r1Key {
			t.Errorf("made %v before the refusal", made)
		}
		if failed["requirementKey"] != r2Key || failed["code"] != "validation_failed" || failed["message"] != "Armature refuses this summary." {
			t.Errorf("the refusal reads %v", failed)
		}
		if after := len(linkStates(t, bob, longKey)); after != before {
			t.Errorf("%s gained a link after the round stopped", longKey)
		}
		if msg := fieldError(t, bob.post(t, issues, map[string]any{"requirementKeys": []string{r2Key}, "armatureProject": "CP", "relation": "verifies"}), "summary"); msg != "Armature refuses this summary." {
			t.Errorf("a first refusal is the answer, reading %q", msg)
		}
	})

	t.Run("what cannot be made is refused before Armature is asked", func(t *testing.T) {
		round := func(keys ...string) map[string]any {
			return map[string]any{"requirementKeys": keys, "armatureProject": "CP", "relation": "implements"}
		}
		refuse(t, "carol, who may only read", carol.post(t, issues, round(r1Key)), http.StatusForbidden, "forbidden")
		refuse(t, "a section", bob.post(t, issues, round(sectionKey)), http.StatusUnprocessableEntity, "validation_failed")
		if msg := fieldError(t, bob.post(t, issues, round(r1Key, othKey+"-R1")), "requirementKeys"); !strings.Contains(msg, othKey+"-R1") {
			t.Errorf("a requirement of another project is refused with %q", msg)
		}
		fieldError(t, bob.post(t, issues, round(creKey+"-R99")), "requirementKeys")
		refuse(t, "a project nobody has", bob.post(t, "/api/v1/projects/NOPE"+creKey+"/armature-issues", round(r1Key)), http.StatusNotFound, "not_found")
		fieldError(t, bob.post(t, issues, map[string]any{"requirementKeys": []string{r1Key}, "armatureProject": "CP", "relation": "relates"}), "relation")
		fieldError(t, bob.post(t, issues, round()), "requirementKeys")
		refuse(t, "eve without a token", eve.post(t, issues, round(r1Key)), http.StatusConflict, "armature_not_connected")
		refuse(t, "dave with a read-only token", dave.post(t, issues, round(r1Key)), http.StatusConflict, "armature_rejected")
	})

	t.Run("Armature's own refusals are relayed in its words", func(t *testing.T) {
		got := bob.post(t, issues, map[string]any{"requirementKeys": []string{r1Key}, "armatureProject": "SEC", "relation": "implements"})
		refuse(t, "a project bob may not see in Armature", got, http.StatusNotFound, "not_found")
		stubControl(t, http.MethodPut, slug+"/people/bob/read-only-projects", map[string]any{"projects": []string{"CP"}})
		t.Cleanup(func() {
			stubControl(t, http.MethodPut, slug+"/people/bob/read-only-projects", map[string]any{"projects": []string{}})
		})
		got = bob.post(t, issues, map[string]any{"requirementKeys": []string{r1Key}, "armatureProject": "CP", "relation": "implements"})
		refuse(t, "a project bob may only read", got, http.StatusForbidden, "forbidden")
		if msg := got.Body["error"].(map[string]any)["message"]; msg != "You may not file issues in this project." {
			t.Errorf("the refusal reads %q", msg)
		}
		if len(linkStates(t, bob, r1Key)) != 2 {
			t.Errorf("a refused round linked %s anyway: %v", r1Key, linkStates(t, bob, r1Key))
		}
	})
}

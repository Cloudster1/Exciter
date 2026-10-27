//go:build integration

package test

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/trace"
)

// The trace model's refusals, its chains paged and ending in pages or issues, the views kept of them, and the
// assistant's reading of the matrix and the chains (#315).

// chainScene is a comment world with needs N1 to N3 and systems S1 derived from N1 and S2 from N2.
type chainScene struct {
	*commentWorld
	types map[string]string
	keys  map[string]string
}

func newChainScene(t *testing.T, prefix string) *chainScene {
	t.Helper()
	s := &chainScene{commentWorld: newCommentWorld(t, prefix), types: map[string]string{}, keys: map[string]string{}}
	for _, name := range []string{"Need", "System", "Old"} {
		s.types[name] = obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/types", map[string]any{"name": name, "icon": "test", "colour": "#2255aa"}),
			http.StatusCreated, "make the type "+name), "type")["id"].(string)
	}
	for _, r := range []struct{ name, typ, title string }{
		{"N1", "Need", "Stop within 40 m"}, {"N2", "Need", "Sound a horn"}, {"N3", "Need", "Warn of ice"},
		{"S1", "System", "Brakes engage within 100 ms"}, {"S2", "System", "Horn sounds at 100 dB"},
	} {
		s.keys[r.name] = obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": r.title, "typeId": s.types[r.typ]}),
			http.StatusCreated, "write "+r.name), "requirement")["key"].(string)
	}
	for _, l := range [][2]string{{"S1", "N1"}, {"S2", "N2"}} {
		want(t, s.bob.post(t, "/api/v1/requirements/"+s.keys[l[0]]+"/links", map[string]any{"typeKey": "derives", "otherKey": s.keys[l[1]]}),
			http.StatusCreated, "derive "+l[0]+" from "+l[1])
	}
	return s
}

// installationLinkType is the id of a link type for all projects, by its key.
func (s *chainScene) installationLinkType(t *testing.T, key string) string {
	t.Helper()
	var id string
	if err := s.h.super.QueryRow(t.Context(), `SELECT id FROM link_type WHERE key = $1 AND project_id IS NULL`, key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// needToSystem is a model of needs reached by the links that derive systems from them.
func (s *chainScene) needToSystem(t *testing.T) map[string]any {
	return map[string]any{"levels": []map[string]any{
		{"name": "Need", "kind": "requirements", "typeIds": []string{s.types["Need"]}},
		{"name": "System", "kind": "requirements", "typeIds": []string{s.types["System"]},
			"steps": []map[string]any{{"typeId": s.installationLinkType(t, "derives"), "direction": "incoming"}}},
	}}
}

// savedModel reads the model as carol does, for comparing before and after a refusal.
func (s *chainScene) savedModel(t *testing.T) string {
	t.Helper()
	m := obj(t, want(t, s.carol.get(t, "/api/v1/projects/"+s.key+"/trace-model"), http.StatusOK, "read the model"), "model")
	return fmt.Sprint(m["levels"], m["updatedAt"])
}

// chainNames reads a chain report's lines as what they pass, a page by its id, and broken where one ends early.
func chainNames(t *testing.T, c map[string]any) []string {
	t.Helper()
	var out []string
	for _, each := range c["lines"].([]any) {
		line := each.(map[string]any)
		var names []string
		for _, n := range line["nodes"].([]any) {
			node := n.(map[string]any)
			switch {
			case node["requirement"] != nil:
				names = append(names, node["requirement"].(map[string]any)["key"].(string))
			case node["page"] != nil:
				names = append(names, node["page"].(map[string]any)["id"].(string))
			default:
				names = append(names, node["issueKey"].(string))
			}
		}
		if line["broken"] == true {
			names = append(names, "broken")
		}
		out = append(out, strings.Join(names, " "))
	}
	return out
}

// What the shape check cannot refuse alone is refused against the project, on the field of the level or step that
// names it, and the model saved before stays.
func TestATraceModelIsRefusedWhatTheProjectDoesNotHold(t *testing.T) {
	s := newChainScene(t, "TMX")
	model := "/api/v1/projects/" + s.key + "/trace-model"
	want(t, s.owner.put(t, model, s.needToSystem(t)), http.StatusOK, "set the model")
	before := s.savedModel(t)
	other := projectKey(t, "TMY")
	want(t, s.owner.post(t, "/api/v1/projects", map[string]any{"key": other, "name": "Other"}), http.StatusCreated, "make another project")
	theirs := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+other+"/link-types", map[string]any{
		"key": "checks", "name": "Checks", "forward": "checks", "backward": "is checked by",
	}), http.StatusCreated, "make the other project's own link type"), "linkType")["id"].(string)

	t.Run("a retired requirement type", func(t *testing.T) {
		want(t, s.owner.patch(t, "/api/v1/projects/"+s.key+"/types/"+s.types["Old"], map[string]any{"retired": true}), http.StatusOK, "retire Old")
		bad := s.needToSystem(t)
		bad["levels"].([]map[string]any)[1]["typeIds"] = []string{s.types["System"], s.types["Old"]}
		refuse422(t, "a level of a retired type", s.owner.put(t, model, bad), "levels[1].typeIds")
	})
	t.Run("another project's own link type", func(t *testing.T) {
		bad := s.needToSystem(t)
		bad["levels"].([]map[string]any)[1]["steps"] = []map[string]any{
			{"typeId": s.installationLinkType(t, "derives"), "direction": "incoming"}, {"typeId": theirs, "direction": "outgoing"},
		}
		refuse422(t, "a step of another project's type", s.owner.put(t, model, bad), "levels[1].steps[1]")
	})
	t.Run("a link type the project turned off", func(t *testing.T) {
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/link-types/"+s.installationLinkType(t, "refines"), map[string]any{"enabled": false}),
			http.StatusOK, "turn refines off here")
		bad := s.needToSystem(t)
		bad["levels"].([]map[string]any)[1]["steps"] = []map[string]any{{"typeId": s.installationLinkType(t, "refines"), "direction": "incoming"}}
		refuse422(t, "a step of a type turned off", s.owner.put(t, model, bad), "levels[1].steps[0]")
	})
	t.Run("more levels than a model holds", func(t *testing.T) {
		levels := []map[string]any{{"name": "Level 0", "kind": "requirements", "typeIds": []string{s.types["Need"]}}}
		for i := 1; i <= trace.MaxLevels; i++ {
			levels = append(levels, map[string]any{"name": fmt.Sprintf("Level %d", i), "kind": "requirements", "typeIds": []string{s.types["System"]},
				"steps": []map[string]any{{"typeId": s.installationLinkType(t, "derives"), "direction": "incoming"}}})
		}
		msg := refuse422(t, "a model one level too deep", s.owner.put(t, model, map[string]any{"levels": levels}), "levels")
		if !strings.Contains(msg, fmt.Sprint(trace.MaxLevels)) {
			t.Errorf("the refusal %q does not say how many levels a model holds", msg)
		}
	})
	if after := s.savedModel(t); after != before {
		t.Errorf("the refusals left the model %s, want %s", after, before)
	}
}

// Only an administrator of an open project sets the model: a reader is forbidden, and nobody changes the model of
// an archived project.
func TestATraceModelIsKeptFromReadersAndArchivedProjects(t *testing.T) {
	s := newChainScene(t, "TMZ")
	model := "/api/v1/projects/" + s.key + "/trace-model"
	want(t, s.owner.put(t, model, s.needToSystem(t)), http.StatusOK, "set the model")
	before := s.savedModel(t)
	cleared := map[string]any{"levels": []any{}}

	refuse(t, "carol, a reader", s.carol.put(t, model, cleared), http.StatusForbidden, "forbidden")
	if after := s.savedModel(t); after != before {
		t.Errorf("a reader's refusal left the model %s", after)
	}
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
	refuse(t, "the owner, once archived", s.owner.put(t, model, cleared), http.StatusConflict, "project_archived")
	if after := s.savedModel(t); after != before {
		t.Errorf("the archived project's model reads %s, want %s", after, before)
	}
	if can := obj(t, want(t, s.owner.get(t, model), http.StatusOK, "the owner reads the archived model"), "model")["can"].(map[string]any); can["edit"] != false {
		t.Errorf("the owner may still edit an archived project's model: %v", can)
	}
}

// The chains are paged by their roots with the total of every root kept, and a window past the largest is refused.
func TestTheChainsArePagedByTheirRoots(t *testing.T) {
	s := newChainScene(t, "TMP")
	chains := "/api/v1/projects/" + s.key + "/trace-chains"
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", s.needToSystem(t)), http.StatusOK, "set the model")
	page := obj(t, want(t, s.carol.get(t, chains+"?limit=1&offset=1"), http.StatusOK, "the second root"), "chains")
	roots := page["roots"].([]any)
	if len(roots) != 1 || roots[0].(map[string]any)["key"] != s.keys["N2"] || page["total"] != float64(3) || page["offset"] != float64(1) ||
		fmt.Sprint(chainNames(t, page)) != fmt.Sprint([]string{s.keys["N2"] + " " + s.keys["S2"]}) {
		t.Errorf("the second page of one root reads %v with %v", chainNames(t, page), page)
	}
	last := obj(t, want(t, s.carol.get(t, chains+"?limit=2&offset=2"), http.StatusOK, "the last page"), "chains")
	if fmt.Sprint(chainNames(t, last)) != fmt.Sprint([]string{s.keys["N3"] + " broken"}) || last["broken"] != float64(1) {
		t.Errorf("the last page reads %v", chainNames(t, last))
	}
	fieldError(t, s.carol.get(t, chains+fmt.Sprintf("?limit=%d", trace.ChainMaxLimit+1)), "limit")
	fieldError(t, s.carol.get(t, chains+"?limit=0"), "limit")
	fieldError(t, s.carol.get(t, chains+"?offset=-1"), "offset")
}

// requirementIDs reads the ids of the scene's needs, for rows written straight through SQL.
func (s *chainScene) requirementIDs(t *testing.T) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for _, name := range []string{"N1", "N2", "N3"} {
		ids[name] = obj(t, want(t, s.owner.get(t, "/api/v1/requirements/"+s.keys[name]), http.StatusOK, "read "+name), "requirement")["id"].(string)
	}
	return ids
}

// endIn sets a model of needs whose second and last level is of kind, reached by relation.
func (s *chainScene) endIn(t *testing.T, name, kind, relation string) {
	t.Helper()
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", map[string]any{"levels": []map[string]any{
		{"name": "Need", "kind": "requirements", "typeIds": []string{s.types["Need"]}},
		{"name": name, "kind": kind, "steps": []map[string]any{{"relation": relation}}},
	}}), http.StatusOK, "end the model in "+kind)
}

// A model ending in Stator pages follows a need to the page it specifies and breaks where none does, and the file
// names the page as each reader sees it in Stator.
func TestChainsEndInStatorPages(t *testing.T) {
	s := newChainScene(t, "TME")
	s.h.keepStator(t)
	tenant := stubTenant(t)
	statorOffers(t)
	want(t, s.owner.put(t, "/api/v1/stator/connection", map[string]any{"baseUrl": statorURL(t), "webhookSecret": statorWebhookSecret}), http.StatusOK, "connect Stator")
	want(t, s.bob.put(t, "/api/v1/stator/account/token", map[string]any{"token": statorPat(tenant, "bob")}), http.StatusOK, "bob connects")
	pages := statorPages(t, tenant)
	timing, braking := pages["Brake timing"], pages["Braking system"]
	ids := s.requirementIDs(t)
	for _, l := range []struct{ req, relation, page string }{{"N1", "specifies", timing}, {"N2", "describes", braking}} {
		if _, err := s.h.super.Exec(t.Context(), `INSERT INTO stator_page_link (requirement_id, relation, page_id, space_key, url, page_version, page_version_seen, reviewed_rev)
			VALUES ($1, $2, $3, 'ENG', $4, 1, 1, 1)`, ids[l.req], l.relation, l.page, statorURL(t)+"/s/ENG/p/"+l.page); err != nil {
			t.Fatal(err)
		}
	}
	// Written straight through SQL, so no read of the API's waits for it on a replica unless told to.
	s.h.settle(t)
	s.endIn(t, "Specification", "pages", "specifies")
	chains := "/api/v1/projects/" + s.key + "/trace-chains"
	c := obj(t, want(t, s.bob.get(t, chains), http.StatusOK, "the chains to pages"), "chains")
	if got := fmt.Sprint(chainNames(t, c)); got != fmt.Sprint([]string{s.keys["N1"] + " " + timing, s.keys["N2"] + " broken", s.keys["N3"] + " broken"}) ||
		c["broken"] != float64(2) {
		t.Errorf("the chains to pages read %s", got)
	}
	file := func(c *client) [][]string {
		t.Helper()
		return csvRecords(t, want(t, c.get(t, chains+".csv"), http.StatusOK, "the chain file"))
	}
	if bobs := file(s.bob); strings.Join(bobs[0], ",") != "Need,Specification,Breaks after" || !strings.Contains(bobs[1][1], "Brake timing") ||
		bobs[2][2] != "Need" {
		t.Errorf("bob's chain file reads %v", bobs)
	}
	if carols := file(s.carol); !strings.Contains(carols[1][1], "ENG/"+timing) || strings.Contains(carols[1][1], "Brake timing") {
		t.Errorf("carol, without a Stator token, has a chain file reading %v", carols)
	}
}

// A model ending in Armature issues follows a need to the issue that implements it and breaks where only another
// relation leads.
func TestChainsEndInArmatureIssues(t *testing.T) {
	s := newChainScene(t, "TMI")
	s.h.keepArmature(t)
	want(t, s.owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	ids := s.requirementIDs(t)
	for _, l := range []struct{ req, relation, issue string }{{"N1", "implements", "WEB-7"}, {"N2", "verifies", "WEB-8"}} {
		if _, err := s.h.super.Exec(t.Context(), `INSERT INTO armature_issue_link (requirement_id, relation, issue_key, status_category)
			VALUES ($1, $2, $3, 'done')`, ids[l.req], l.relation, l.issue); err != nil {
			t.Fatal(err)
		}
	}
	// Written straight through SQL, so no read of the API's waits for it on a replica unless told to.
	s.h.settle(t)
	s.endIn(t, "Work", "issues", "implements")
	chains := "/api/v1/projects/" + s.key + "/trace-chains"
	c := obj(t, want(t, s.carol.get(t, chains), http.StatusOK, "the chains to issues"), "chains")
	if got := fmt.Sprint(chainNames(t, c)); got != fmt.Sprint([]string{s.keys["N1"] + " WEB-7", s.keys["N2"] + " broken", s.keys["N3"] + " broken"}) {
		t.Errorf("the chains to issues read %s", got)
	}
	records := csvRecords(t, want(t, s.carol.get(t, chains+".csv"), http.StatusOK, "the chain file"))
	if strings.Join(records[1], ",") != s.keys["N1"]+" Stop within 40 m,WEB-7," || records[2][2] != "Need" {
		t.Errorf("the chain file reads %v", records)
	}
}

// Views of the matrix and the chains leave out a link type deleted or a level the model lost, naming each, and a
// shared chain view is listed to every member while a private one is its owner's.
func TestMatrixAndChainViewsDropWhatTheProjectLost(t *testing.T) {
	s := newChainScene(t, "TMV")
	views := "/api/v1/projects/" + s.key + "/views"
	listed := func(c *client, kind string) map[string]map[string]any {
		t.Helper()
		out := map[string]map[string]any{}
		for _, each := range list(t, want(t, c.get(t, views+"?kind="+kind), http.StatusOK, "the "+kind+" views"), "views") {
			v := each.(map[string]any)
			out[v["name"].(string)] = v
		}
		return out
	}
	t.Run("a link type deleted since", func(t *testing.T) {
		checks := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/link-types", map[string]any{
			"key": "checks", "name": "Checks", "forward": "checks", "backward": "is checked by",
		}), http.StatusCreated, "make a link type"), "linkType")["id"].(string)
		saved := obj(t, want(t, s.owner.post(t, views, map[string]any{"name": "Checked", "kind": "matrix",
			"settings": map[string]any{"linkTypes": []string{"checks", "derives"}}}), http.StatusCreated, "save a matrix view"), "view")
		if len(saved["dropped"].([]any)) != 0 {
			t.Errorf("a view of link types the project has dropped %v", saved["dropped"])
		}
		want(t, s.owner.delete(t, "/api/v1/link-types/"+checks), http.StatusNoContent, "delete the unused link type")
		read := listed(s.owner, "matrix")["Checked"]
		if fmt.Sprint(read["settings"].(map[string]any)["linkTypes"]) != "[derives]" || fmt.Sprint(read["dropped"]) != "[linkType:checks]" {
			t.Errorf("the matrix view once checks is gone reads %v", read)
		}
		again := obj(t, want(t, s.owner.post(t, views, map[string]any{"name": "Checked again", "kind": "matrix",
			"settings": map[string]any{"linkTypes": []string{"checks"}}}), http.StatusCreated, "save a view of the deleted type"), "view")
		if kept, _ := again["settings"].(map[string]any)["linkTypes"].([]any); len(kept) != 0 || fmt.Sprint(again["dropped"]) != "[linkType:checks]" {
			t.Errorf("a view saved naming the deleted type reads %v", again)
		}
	})
	t.Run("levels the model lost", func(t *testing.T) {
		three := s.needToSystem(t)
		three["levels"] = append(three["levels"].([]map[string]any),
			map[string]any{"name": "Work", "kind": "issues", "steps": []map[string]any{{"relation": "implements"}}})
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", three), http.StatusOK, "set a model of three levels")
		saved := obj(t, want(t, s.owner.post(t, views, map[string]any{"name": "Below needs", "kind": "chain", "shared": true,
			"settings": map[string]any{"levels": []int{1, 2}}}), http.StatusCreated, "save a shared chain view"), "view")
		want(t, s.owner.post(t, views, map[string]any{"name": "Mine", "kind": "chain", "settings": map[string]any{"onlyBroken": true}}),
			http.StatusCreated, "save a private chain view")
		if fmt.Sprint(saved["settings"].(map[string]any)["levels"]) != "[1 2]" || len(saved["dropped"].([]any)) != 0 {
			t.Errorf("the chain view reads %v", saved)
		}
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", s.needToSystem(t)), http.StatusOK, "drop the model's last level")
		carols := listed(s.carol, "chain")
		read := carols["Below needs"]
		if read == nil || fmt.Sprint(read["settings"].(map[string]any)["levels"]) != "[1]" || fmt.Sprint(read["dropped"]) != "[level:2]" || read["shared"] != true {
			t.Errorf("carol reads the shared chain view as %v", read)
		}
		if _, ok := carols["Mine"]; ok || len(carols) != 1 {
			t.Errorf("carol is listed the chain views %v; the owner's private one is not hers", carols)
		}
		if _, ok := listed(s.carol, "matrix")["Below needs"]; ok {
			t.Errorf("a chain view is listed with the matrix's")
		}
	})
}

// The assistant's get_matrix and list_trace_chains answer what the API's matrix and chains answer for the same
// question.
func TestAnAssistantReadsTheMatrixAndTheChainsAsTheAPIAnswersThem(t *testing.T) {
	s := newChainScene(t, "TMM")
	want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", s.needToSystem(t)), http.StatusOK, "set the model")
	want(t, s.bob.post(t, "/api/v1/requirements/"+s.keys["S1"]+"/links", map[string]any{"typeKey": "relates", "otherKey": s.keys["S2"]}),
		http.StatusCreated, "relate S1 to S2")
	_, secret := makeToken(t, s.bob, map[string]any{"name": "bob's assistant"})
	s.h.settle(t)
	assistant := connectAssistant(t, s.owner.api, secret)

	viaAPI := obj(t, want(t, s.bob.get(t, "/api/v1/projects/"+s.key+"/matrix?linkType=derives&direction=incoming"), http.StatusOK, "the matrix"), "matrix")
	viaTool := assistant.call(t, "get_matrix", map[string]any{"projectKey": s.key, "linkType": []string{"derives"}, "direction": "incoming"})["matrix"].(map[string]any)
	for _, part := range []string{"rows", "columns", "cells", "rowLinks", "columnLinks", "total"} {
		if !reflect.DeepEqual(viaTool[part], viaAPI[part]) {
			t.Errorf("get_matrix answers %s %v, the API %v", part, viaTool[part], viaAPI[part])
		}
	}
	if cells := matrixCells(t, viaTool); len(cells) != 2 || cells[s.keys["N1"]+":"+s.keys["S1"]] != "Derives/incoming" {
		t.Errorf("get_matrix lays out %v; only the derived systems count, incoming", cells)
	}

	chainsAPI := obj(t, want(t, s.bob.get(t, "/api/v1/projects/"+s.key+"/trace-chains"), http.StatusOK, "the chains"), "chains")
	chainsTool := assistant.call(t, "list_trace_chains", map[string]any{"projectKey": s.key})["chains"].(map[string]any)
	if !reflect.DeepEqual(chainsTool["lines"], chainsAPI["lines"]) || chainsTool["broken"] != chainsAPI["broken"] || chainsTool["broken"] != float64(1) {
		t.Errorf("list_trace_chains answers %v with %v broken, the API %v with %v", chainNames(t, chainsTool), chainsTool["broken"],
			chainNames(t, chainsAPI), chainsAPI["broken"])
	}
}

//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/baseline"
)

// mitigated mitigates the scene's risk with its requirement and verifies that by its check.
func (s *mitigationScene) mitigated(t *testing.T) {
	t.Helper()
	want(t, s.f.owner.post(t, "/api/v1/requirements/"+s.plainKey+"/links", map[string]any{"typeKey": "mitigates", "otherKey": s.riskKey}),
		http.StatusCreated, "mitigate the risk")
	want(t, s.f.owner.post(t, "/api/v1/requirements/"+s.checkKey+"/links", map[string]any{"typeKey": "verifies", "otherKey": s.plainKey}),
		http.StatusCreated, "verify the mitigation")
}

// A baseline records each risk as it stood, its levels, state and mitigations, so a scheme changed later changes
// nothing it recorded; Go and the database hash what it recorded alike.
func TestABaselineRecordsItsRisksAsTheyStood(t *testing.T) {
	s := newMitigationScene(t, "RRA")
	owner := s.f.owner
	s.mitigated(t)
	id := obj(t, want(t, owner.post(t, "/api/v1/projects/"+s.key+"/baselines", map[string]any{"name": "Release 1"}), http.StatusCreated, "take a baseline"),
		"baseline")["id"].(string)
	recorded := func() map[string]any {
		doc := obj(t, want(t, owner.get(t, "/api/v1/baselines/"+id+"/document"), http.StatusOK, "read the baseline"), "document")
		return ratingOf(t, entryOf(t, doc, s.riskKey))
	}
	if r := recorded(); levelName(r["beforeLevel"]) != "High" || levelName(r["level"]) != "Medium" || r["state"] != "mitigated" {
		t.Fatalf("the baseline recorded %v", r)
	}
	want(t, owner.put(t, "/api/v1/projects/"+s.key+"/risk-scheme", riskScheme(5, "rpn", false)), http.StatusOK, "judge by priority number")
	if now := ratingOf(t, obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.riskKey), http.StatusOK, "read the risk"), "requirement")); levelName(now["beforeLevel"]) != "Low" {
		t.Errorf("by priority number the risk stands at %v", now)
	}
	if r := recorded(); levelName(r["beforeLevel"]) != "High" {
		t.Errorf("a scheme changed later changed what the baseline recorded: %v", r)
	}

	ctx := context.Background()
	var projectID, ownerID uuid.UUID
	if err := s.f.h.super.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, s.key).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	ownerID = s.f.owner.user
	c := baseline.Content{BaselineID: uuid.MustParse(id), ProjectID: projectID}
	rows, err := s.f.h.appConn(t, ownerID).Query(ctx, `SELECT kind, a, b, c, revision, hash, path FROM baseline_content_rows($1)`, id)
	if err != nil {
		t.Fatal(err)
	}
	var risks, mitigations int
	for rows.Next() {
		var (
			kind     string
			a        uuid.UUID
			b, tt    *uuid.UUID
			revision *int
			hash     *string
			path     []int32
		)
		if err := rows.Scan(&kind, &a, &b, &tt, &revision, &hash, &path); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "item":
			item := baseline.ContentItem{RequirementID: a, Revision: *revision, Path: path}
			if hash != nil {
				item.Hash = *hash
			}
			c.Items = append(c.Items, item)
		case "link":
			c.Links = append(c.Links, baseline.ContentLink{TypeID: a, SourceID: *b, TargetID: *tt})
		case "evidence":
			c.Evidence = append(c.Evidence, baseline.ContentEvidence{RequirementID: a, Line: *hash})
			if strings.HasPrefix(*hash, "risk ") {
				risks++
			} else if strings.HasPrefix(*hash, "mitigation ") {
				mitigations++
			}
		case "computed":
			c.Computed = append(c.Computed, baseline.ContentEvidence{RequirementID: a, Line: *hash})
		}
	}
	rows.Close()
	var hash string
	if err := s.f.h.super.QueryRow(ctx, `SELECT baseline_content_hash($1)`, id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if risks != 1 || mitigations != 1 || baseline.ContentHash(c) != hash {
		t.Errorf("Go hashes the baseline as %s, the database as %s, over %d risks and %d mitigations", baseline.ContentHash(c), hash, risks, mitigations)
	}
}

// The trace graph and the chain report mark a risk with its level and state.
func TestRisksAreMarkedInTheGraphAndTheChains(t *testing.T) {
	s := newMitigationScene(t, "RRB")
	owner := s.f.owner
	want(t, owner.post(t, "/api/v1/requirements/"+s.plainKey+"/links", map[string]any{"typeKey": "mitigates", "otherKey": s.riskKey}),
		http.StatusCreated, "mitigate the risk")
	graph := obj(t, want(t, owner.get(t, "/api/v1/requirements/"+s.riskKey+"/graph"), http.StatusOK, "read the graph"), "graph")
	var mark map[string]any
	for _, n := range graph["nodes"].([]any) {
		node := n.(map[string]any)
		if r, ok := node["requirement"].(map[string]any); ok && r["key"] == s.riskKey {
			mark, _ = node["risk"].(map[string]any)
		}
	}
	if mark == nil || levelName(mark["level"]) != "High" || mark["state"] != "claimed" {
		t.Errorf("the graph marks the risk %v", mark)
	}

	mitigates := linkTypeID(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/link-types"), http.StatusOK, "the link types"), "mitigates")
	want(t, owner.put(t, "/api/v1/projects/"+s.key+"/trace-model", map[string]any{"levels": []map[string]any{
		{"name": "Hazards", "kind": "requirements", "typeIds": []string{s.hazard}},
		{"name": "Controls", "kind": "requirements", "typeIds": []string{s.plain}, "steps": []map[string]any{{"typeId": mitigates, "direction": "incoming"}}},
	}}), http.StatusOK, "model hazards and their controls")
	chains := obj(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/trace-chains"), http.StatusOK, "follow the chains"), "chains")
	first := chains["lines"].([]any)[0].(map[string]any)["nodes"].([]any)[0].(map[string]any)
	if m, _ := first["risk"].(map[string]any); m == nil || levelName(m["level"]) != "High" {
		t.Errorf("the chain marks its hazard %v", first)
	}
}

// The risk report lists every risk with its ratings and mitigations, and counts them at each level; it goes out as
// Word and, where the render service is set up, as PDF.
func TestTheRiskReport(t *testing.T) {
	s := newMitigationScene(t, "RRC")
	owner := s.f.owner
	s.mitigated(t)
	want(t, owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brakes squeal", "typeId": s.hazard}),
		http.StatusCreated, "write an unrated risk")
	report := obj(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/risk-report"), http.StatusOK, "read the report"), "report")
	if len(report["risks"].([]any)) != 2 || report["unrated"] != float64(1) {
		t.Fatalf("the report reads %v", report)
	}
	counts := map[string][2]float64{}
	for _, l := range report["levels"].([]any) {
		level := l.(map[string]any)
		counts[level["level"].(map[string]any)["name"].(string)] = [2]float64{level["before"].(float64), level["effective"].(float64)}
	}
	if counts["High"] != [2]float64{1, 0} || counts["Medium"] != [2]float64{0, 1} {
		t.Errorf("the report counts %v", counts)
	}
	word := unzipped(t, want(t, owner.get(t, "/api/v1/projects/"+s.key+"/risk-report.docx"), http.StatusOK, "the report as Word").Raw)["word/document.xml"]
	for _, wanted := range []string{"Risk report", "Brakes fail on ice", "Stop on ice", "Mitigated by"} {
		if !bytes.Contains(word, []byte(wanted)) {
			t.Errorf("the Word report lacks %q", wanted)
		}
	}
	refuse(t, "a report outside the project", s.f.outside.get(t, "/api/v1/projects/"+s.key+"/risk-report"), http.StatusNotFound, "not_found")
	refuse(t, "a Word report outside the project", s.f.outside.get(t, "/api/v1/projects/"+s.key+"/risk-report.docx"), http.StatusNotFound, "not_found")
	pdf := owner.get(t, "/api/v1/projects/"+s.key+"/risk-report.pdf")
	if pdf.Status != http.StatusOK && pdf.Status != http.StatusServiceUnavailable {
		t.Errorf("the PDF report answers %d", pdf.Status)
	}
	refuse(t, "a PDF report outside the project", s.f.outside.get(t, "/api/v1/projects/"+s.key+"/risk-report.pdf"), http.StatusNotFound, "not_found")

	bare := projectKey(t, "RRD")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": bare, "name": "No scheme"}), http.StatusCreated, "make a project without a scheme")
	refuse(t, "a report without a scheme", owner.get(t, "/api/v1/projects/"+bare+"/risk-report"), http.StatusConflict, "risk_scheme_missing")
}

// The medical device and functional safety templates make projects whose risks are rated at once, with their
// attributes on their types and their controls asked to be implemented and verified.
func TestTheRiskTemplatesSetUpTheirProjects(t *testing.T) {
	f := newProjectFixture(t)
	owner := f.owner
	for _, each := range []struct {
		template, prefix, riskType string
		attribute, attributeType   string
		rating                     map[string]any
		level                      string
	}{
		{"medical", "RTM", "Hazard", "Sequence of events", "Hazardous situation", map[string]any{"before": side(5, 4)}, "High"},
		{"safety", "RTS", "Failure mode", "Effect", "Failure mode",
			map[string]any{"before": map[string]any{"severity": 8, "probability": 5, "detection": 6}}, "High"},
	} {
		key := projectKey(t, each.prefix)
		want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": each.template, "template": each.template}), http.StatusCreated, "make it")
		if scheme := obj(t, want(t, owner.get(t, "/api/v1/projects/"+key+"/risk-scheme"), http.StatusOK, "its scheme"), "scheme"); scheme == nil {
			t.Fatalf("%s sets no scheme", each.template)
		}
		types := map[string]map[string]any{}
		for _, ty := range list(t, want(t, owner.get(t, "/api/v1/projects/"+key+"/types"), http.StatusOK, "its types"), "types") {
			typ := ty.(map[string]any)
			types[typ["name"].(string)] = typ
		}
		if types[each.riskType]["role"] != "risk" {
			t.Errorf("%s's %s is %v", each.template, each.riskType, types[each.riskType]["role"])
		}
		for _, a := range list(t, want(t, owner.get(t, "/api/v1/projects/"+key+"/attributes"), http.StatusOK, "its attributes"), "attributes") {
			attr := a.(map[string]any)
			if ids := attr["typeIds"].([]any); attr["name"] == each.attribute && (len(ids) != 1 || ids[0] != types[each.attributeType]["id"]) {
				t.Errorf("%s's %s shows on %v", each.template, each.attribute, ids)
			}
		}
		made := obj(t, want(t, owner.post(t, "/api/v1/projects/"+key+"/requirements", map[string]any{"title": "Rated at once",
			"typeId": types[each.riskType]["id"], "risk": each.rating}), http.StatusCreated, "rate a risk"), "requirement")
		if level := levelName(ratingOf(t, made)["beforeLevel"]); level != each.level {
			t.Errorf("%s rates it %q, want %q", each.template, level, each.level)
		}
		rules := list(t, want(t, owner.get(t, "/api/v1/projects/"+key+"/coverage-rules"), http.StatusOK, "its coverage rules"), "rules")
		if len(rules) != 1 || rules[0].(map[string]any)["implemented"] != true || rules[0].(map[string]any)["verified"] != true {
			t.Errorf("%s asks %v", each.template, rules)
		}
	}
}

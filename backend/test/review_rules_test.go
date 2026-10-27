//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
)

// Reviews through every role and rule (#317): each refusal writes nothing, a type's rule comes first, an author may
// approve where the rule lets them, and a signature keeps the language it was given in.

// reviewCount counts the reviews asked of a project's requirements.
func (s *reviewScene) reviewCount(t *testing.T) int {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM requirement_review v JOIN requirement r ON r.id = v.requirement_id WHERE r.project_id = $1`, s.projectID)
}

// ruleRows counts a project's rules and the approvers they name.
func (s *reviewScene) ruleRows(t *testing.T) (rules, approvers int) {
	t.Helper()
	return s.h.countRows(t, `SELECT count(*) FROM approval_rule WHERE project_id = $1`, s.projectID),
		s.h.countRows(t, `SELECT count(*) FROM approval_rule_subject x JOIN approval_rule a ON a.id = x.rule_id WHERE a.project_id = $1`, s.projectID)
}

func (s *reviewScene) bulk(t *testing.T, c *client, revision int) response {
	t.Helper()
	return c.post(t, "/api/v1/projects/"+s.key+"/reviews", map[string]any{"items": []any{map[string]any{"key": s.reqKey, "revision": revision}}})
}

func TestEveryReviewRefusalLeavesItAsItWas(t *testing.T) {
	s := newReviewScene(t, "RVF")
	s.setRules(t)

	t.Run("an editor sets and removes no rule", func(t *testing.T) {
		refuse(t, "an editor sets a rule", s.bob.put(t, s.rules, s.rule(1)), http.StatusForbidden, "forbidden")
		refuse(t, "an editor removes the rule", s.bob.delete(t, "/api/v1/approval-rules/"+s.ruleID), http.StatusForbidden, "forbidden")
		refuse(t, "a rule by no id", s.owner.delete(t, "/api/v1/approval-rules/not-a-rule"), http.StatusBadRequest, "bad_request")
		if rules, approvers := s.ruleRows(t); rules != 1 || approvers != 2 {
			t.Errorf("the refusals left %d rules naming %d approvers, want the one rule naming 2", rules, approvers)
		}
		got := list(t, want(t, s.carol.get(t, s.rules), http.StatusOK, "list the rules"), "rules")
		if len(got) != 1 || got[0].(map[string]any)["required"] != float64(2) {
			t.Errorf("the rules read %v", got)
		}
	})

	t.Run("a reader asking for many reviews has each refused, and none is asked", func(t *testing.T) {
		// A bulk request answers each requirement on its own, as a bulk transition does, so the refusal is per item.
		results := list(t, want(t, s.bulk(t, s.carol, 1), http.StatusOK, "a reader asks for many"), "results")
		if r := results[0].(map[string]any); r["outcome"] != "refused" || r["error"].(map[string]any)["code"] != "forbidden" {
			t.Errorf("a reader's bulk request reads %v", r)
		}
		refuse(t, "a bulk request in a project that is none", s.bob.post(t, "/api/v1/projects/NOPE"+s.key+"/reviews",
			map[string]any{"items": []any{map[string]any{"key": s.reqKey, "revision": 1}}}), http.StatusNotFound, "not_found")
		if n := s.reviewCount(t); n != 0 {
			t.Errorf("the refusals asked %d reviews", n)
		}
	})

	t.Run("a review by no id is withdrawn by nobody", func(t *testing.T) {
		made := obj(t, want(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "bob asks"), "review")
		refuse(t, "a withdrawal by no id", s.bob.post(t, "/api/v1/reviews/not-a-review/withdraw", nil), http.StatusBadRequest, "bad_request")
		refuse(t, "an approver withdraws", s.dave.post(t, "/api/v1/reviews/"+made["id"].(string)+"/withdraw", nil), http.StatusForbidden, "forbidden")
		if got := s.state(t, made["id"].(string)); got != "pending" {
			t.Errorf("the refused withdrawals left the review %s", got)
		}
		want(t, s.bob.post(t, "/api/v1/reviews/"+made["id"].(string)+"/withdraw", nil), http.StatusOK, "bob withdraws it")
	})

	t.Run("an archived project takes no rule and no review, and nothing is written", func(t *testing.T) {
		before := s.reviewCount(t)
		want(t, s.owner.put(t, "/api/v1/projects/"+s.key+"/archive", nil), http.StatusOK, "archive the project")
		refuse(t, "a rule set", s.owner.put(t, s.rules, s.rule(1)), http.StatusConflict, "project_archived")
		refuse(t, "a rule removed", s.owner.delete(t, "/api/v1/approval-rules/"+s.ruleID), http.StatusConflict, "project_archived")
		refuse(t, "a review asked", s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 1}), http.StatusConflict, "project_archived")
		refuse(t, "many reviews asked", s.bulk(t, s.bob, 1), http.StatusConflict, "project_archived")
		if rules, approvers := s.ruleRows(t); rules != 1 || approvers != 2 {
			t.Errorf("the archived project holds %d rules naming %d approvers", rules, approvers)
		}
		if got := list(t, want(t, s.carol.get(t, s.rules), http.StatusOK, "list the rules"), "rules"); got[0].(map[string]any)["required"] != float64(2) {
			t.Errorf("the archived project's rule reads %v", got[0])
		}
		if n := s.reviewCount(t); n != before {
			t.Errorf("the archived project holds %d reviews, had %d", n, before)
		}
	})
}

// subjectIDs are the people and groups a rule or review names.
func subjectIDs(review map[string]any) []string {
	var out []string
	for _, each := range review["approvers"].([]any) {
		a := each.(map[string]any)
		if id, ok := a["userId"].(string); ok {
			out = append(out, id)
		} else {
			out = append(out, a["groupId"].(string))
		}
	}
	return out
}

func TestARuleOfOneTypeComesBeforeTheRuleForEveryType(t *testing.T) {
	s := newReviewScene(t, "RVT")
	safety := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/types", map[string]any{"name": "Safety", "icon": "requirement", "colour": "#dc2626"}),
		http.StatusCreated, "a type of safety requirements"), "type")["id"].(string)
	safe := obj(t, want(t, s.owner.post(t, "/api/v1/projects/"+s.key+"/requirements", map[string]any{"title": "Brakes hold on ice", "typeId": safety}),
		http.StatusCreated, "write a safety requirement"), "requirement")["key"].(string)
	want(t, s.owner.put(t, s.rules, s.rule(2)), http.StatusOK, "the rule for every type")
	typed := obj(t, want(t, s.owner.put(t, s.rules, map[string]any{"typeId": safety, "required": 1,
		"approvers": []map[string]any{{"subjectType": "user", "userId": s.daveID}}}), http.StatusOK, "the rule of Safety"), "rule")
	if typed["typeId"] != safety || typed["typeName"] != "Safety" || typed["required"] != float64(1) {
		t.Fatalf("the rule of Safety reads %v", typed)
	}
	rules := list(t, want(t, s.carol.get(t, s.rules), http.StatusOK, "list the rules"), "rules")
	if len(rules) != 2 || rules[0].(map[string]any)["typeId"] != nil || rules[1].(map[string]any)["typeId"] != safety {
		t.Errorf("the rules read %v, want the rule for every type first", rules)
	}
	ask := func(key string) map[string]any {
		return obj(t, want(t, s.bob.post(t, "/api/v1/requirements/"+key+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "ask for "+key), "review")
	}

	safeReview := ask(safe)
	if safeReview["required"] != float64(1) || strings.Join(subjectIDs(safeReview), ",") != s.daveID.String() {
		t.Errorf("a safety requirement's review reads %v of %v, want 1 of dave", safeReview["required"], subjectIDs(safeReview))
	}
	other := ask(s.reqKey)
	if other["required"] != float64(2) || len(subjectIDs(other)) != 2 {
		t.Errorf("a requirement of another type takes %v of %v, want the rule for every type", other["required"], subjectIDs(other))
	}

	want(t, s.bob.post(t, "/api/v1/reviews/"+safeReview["id"].(string)+"/withdraw", nil), http.StatusOK, "withdraw the safety review")
	want(t, s.owner.delete(t, "/api/v1/approval-rules/"+typed["id"].(string)), http.StatusNoContent, "remove the rule of Safety")
	again := ask(safe)
	if again["required"] != float64(2) || len(subjectIDs(again)) != 2 {
		t.Errorf("once its rule went, a safety requirement takes %v of %v, want the rule for every type", again["required"], subjectIDs(again))
	}
	if got := obj(t, want(t, s.carol.get(t, "/api/v1/reviews/"+safeReview["id"].(string)), http.StatusOK, "read the withdrawn review"), "review"); len(subjectIDs(got)) != 1 {
		t.Errorf("the earlier review lost the approver of its rule: %v", got["approvers"])
	}
}

func TestAnAuthorApprovesWhereTheRuleLetsThem(t *testing.T) {
	s := newReviewScene(t, "RVA")
	rule := obj(t, want(t, s.owner.put(t, s.rules, map[string]any{"required": 1, "authorMayApprove": true,
		"approvers": []map[string]any{{"subjectType": "user", "userId": s.ownerID}}}), http.StatusOK, "the owner, who wrote R1, approves"), "rule")
	if rule["authorMayApprove"] != true {
		t.Fatalf("the rule reads %v", rule)
	}
	made := obj(t, want(t, s.bob.post(t, s.req+"/reviews", map[string]any{"revision": 1}), http.StatusCreated, "bob asks"), "review")
	id := made["id"].(string)
	waiting := list(t, want(t, s.owner.get(t, "/api/v1/reviews/waiting"), http.StatusOK, "the author's inbox"), "reviews")
	if len(waiting) != 1 || waiting[0].(map[string]any)["id"] != id || waiting[0].(map[string]any)["can"].(map[string]any)["decide"] != true {
		t.Fatalf("the author's inbox reads %v", waiting)
	}
	got := obj(t, want(t, s.owner.post(t, "/api/v1/reviews/"+id+"/decisions", map[string]any{"decision": "approve", "revision": 1}),
		http.StatusOK, "the author approves"), "review")
	if got["state"] != "approved" || got["approvals"] != float64(1) {
		t.Errorf("the author's approval leaves the review %v", got)
	}
	if approval := obj(t, want(t, s.carol.get(t, s.req+"/approval"), http.StatusOK, "read the approval"), "approval"); approval["current"] != true {
		t.Errorf("the approval reads %v", approval)
	}
}

func TestASignatureKeepsTheLanguageItWasGivenIn(t *testing.T) {
	s := signedReview(t, "SDE")
	s.h.givePassword(t, s.daveID, "dave-signs-here")
	want(t, s.dave.patch(t, "/api/v1/auth/me", map[string]any{"locale": "de"}), http.StatusOK, "dave reads German")
	s.h.settle(t)
	read := obj(t, want(t, s.dave.get(t, "/api/v1/reviews/"+s.reviewID), http.StatusOK, "dave reads the review"), "review")
	german := "Ich genehmige Revision 1 von " + s.reqKey + ", Brakes engage within 100 ms, als eine der genehmigenden Personen."
	if read["meanings"].(map[string]any)["approve"] != german {
		t.Fatalf("dave is shown %v", read["meanings"])
	}
	want(t, s.dave.post(t, "/api/v1/reviews/"+s.reviewID+"/decisions", map[string]any{"decision": "approve", "revision": 1, "password": "dave-signs-here"}),
		http.StatusOK, "dave signs in German")
	var stored string
	if err := s.h.super.QueryRow(t.Context(), `SELECT meaning FROM review_signature WHERE review_id = $1`, s.reviewID).Scan(&stored); err != nil || stored != german {
		t.Errorf("the signature keeps %q: %v", stored, err)
	}
	asRead := obj(t, want(t, s.owner.get(t, "/api/v1/reviews/"+s.reviewID), http.StatusOK, "the owner, in English, reads it"), "review")
	signature, _ := asRead["decisions"].([]any)[0].(map[string]any)["signature"].(map[string]any)
	if signature == nil || signature["meaning"] != german {
		t.Errorf("an English reader is shown the signature as %v", signature)
	}
}

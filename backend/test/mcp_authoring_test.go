//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/duplicates"
)

// The assistant's tools of the writing rules, the glossary and duplicates answer what the API answers, and a reader's
// assistant checks no draft (#320).
func TestTheAuthoringToolsAnswerWhatTheAPIAnswers(t *testing.T) {
	w := newCommentWorld(t, "MCQ")
	terms := "/api/v1/projects/" + w.key + "/terms"
	// A statement makes the two titles less alike than the default threshold asks.
	want(t, w.owner.put(t, "/api/v1/projects/"+w.key+"/duplicate-settings", map[string]any{"threshold": duplicates.MinThreshold}), http.StatusOK,
		"list everything kept")
	want(t, w.bob.post(t, terms, map[string]any{"name": "Brake pedal", "synonyms": []string{"foot brake"}, "definition": docOf("The lever the driver presses.")}),
		http.StatusCreated, "define Brake pedal")
	made := w.authored(t, map[string]any{"title": "Brakes engage within 100 milliseconds", "statement": docOf("The brake pedal shall respond quickly.")})
	key := made["key"].(string)
	_, bobSecret := makeToken(t, w.bob, map[string]any{"name": "Bob's assistant"})
	_, carolSecret := makeToken(t, w.carol, map[string]any{"name": "Carol's assistant"})
	w.h.drained(t)
	bob := connectAssistant(t, w.bob.api, bobSecret)

	t.Run("get_quality_findings", func(t *testing.T) {
		got := bob.call(t, "get_quality_findings", map[string]any{"reqKey": key})
		if f, _ := got["quality"].(map[string]any)["findings"].([]any); len(f) == 0 {
			t.Errorf("a vague requirement has no findings: %v", got)
		}
		sameAnswer(t, "get_quality_findings", got, want(t, w.bob.get(t, "/api/v1/requirements/"+key+"/quality"), http.StatusOK, "the findings"))
	})

	draft := map[string]any{"title": "Speed", "statementMarkdown": "The display shall be fast and/or bright, TBD."}
	t.Run("check_requirement_text", func(t *testing.T) {
		got := bob.call(t, "check_requirement_text", map[string]any{"projectKey": w.key, "title": draft["title"], "statementMarkdown": draft["statementMarkdown"]})
		if rules := fmtFindings(got["check"].(map[string]any)); !strings.Contains(rules, "and_or") || !strings.Contains(rules, "placeholder") {
			t.Errorf("the check finds %s", rules)
		}
		sameAnswer(t, "check_requirement_text", got, want(t, w.bob.post(t, "/api/v1/projects/"+w.key+"/quality-check", draft), http.StatusOK, "check the draft"))
	})

	t.Run("list_terms", func(t *testing.T) {
		got := bob.call(t, "list_terms", map[string]any{"projectKey": w.key})
		if listed := list(t, response{Body: got}, "terms"); len(listed) != 1 || listed[0].(map[string]any)["name"] != "Brake pedal" {
			t.Errorf("list_terms lists %v", listed)
		}
		sameAnswer(t, "list_terms", got, want(t, w.bob.get(t, terms), http.StatusOK, "list the terms"))
	})

	t.Run("get_similar_requirements", func(t *testing.T) {
		got := bob.call(t, "get_similar_requirements", map[string]any{"reqKey": key})
		if found := similarKeys(t, response{Body: got}); len(found) != 1 || found[0] != w.reqKey {
			t.Errorf("get_similar_requirements finds %v", found)
		}
		sameAnswer(t, "get_similar_requirements", got, want(t, w.bob.get(t, "/api/v1/requirements/"+key+"/similar"), http.StatusOK, "the similar"))
	})

	t.Run("a reader's assistant checks no draft", func(t *testing.T) {
		carol := connectAssistant(t, w.carol.api, carolSecret)
		if msg := carol.refused(t, "check_requirement_text", map[string]any{"projectKey": w.key, "title": "Speed"}); !strings.HasSuffix(msg, ".") || msg == "" {
			t.Errorf("the refusal says %q", msg)
		}
		// What a reader may read, the reader's assistant reads too.
		carol.call(t, "get_quality_findings", map[string]any{"reqKey": key})
	})
}

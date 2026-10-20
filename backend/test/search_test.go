//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/document"
)

// searchFixture is a project of five requirements, with values and labels to find them by, and a project the
// plain member may not view.
type searchFixture struct {
	*projectFixture
	risk, high, low, cost, due, note, detail string
}

func newSearchFixture(t *testing.T, key string) *searchFixture {
	hidKey := projectKey(t, "HID")
	t.Helper()
	f := &searchFixture{projectFixture: newProjectFixture(t)}
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": "Searched"}), http.StatusCreated, "make a project")
	want(t, f.owner.post(t, "/api/v1/projects", map[string]any{"key": hidKey, "name": "Hidden"}), http.StatusCreated, "make a hidden project")
	want(t, f.owner.put(t, "/api/v1/projects/"+hidKey+"/grants", map[string]any{"grants": []any{grantRow(subjectOf("user", f.home.user), "admin")}}),
		http.StatusOK, "keep the hidden project to its maker")
	attr := func(body map[string]any) map[string]any {
		return obj(t, want(t, f.owner.post(t, "/api/v1/projects/"+key+"/attributes", body), http.StatusCreated, "define "+body["name"].(string)), "attribute")
	}
	risk := attr(map[string]any{"name": "Risk", "kind": "select", "options": []any{map[string]any{"name": "Low"}, map[string]any{"name": "High"}}})
	f.risk, f.low, f.high = risk["id"].(string), optionID(t, risk, "Low"), optionID(t, risk, "High")
	f.cost = attr(map[string]any{"name": "Cost", "kind": "number"})["id"].(string)
	f.due = attr(map[string]any{"name": "Due", "kind": "date"})["id"].(string)
	f.note = attr(map[string]any{"name": "Note", "kind": "text"})["id"].(string)
	f.detail = attr(map[string]any{"name": "Detail", "kind": "richText"})["id"].(string)
	write := func(project string, body map[string]any) {
		want(t, f.plain.post(t, "/api/v1/projects/"+project+"/requirements", body), http.StatusCreated, "write "+body["title"].(string))
	}
	write(key, map[string]any{"title": "Pay the basket", "statement": statementSaying("The shopper pays for everything at once."),
		"priority": "must", "labels": []string{"checkout"},
		"attributes": map[string]any{f.risk: f.high, f.cost: 5, f.due: "2026-03-01", f.note: "Card or cash"}})
	write(key, map[string]any{"title": "Refund a payment", "statement": statementSaying("A refund goes back to the card."),
		"rationale": statementSaying("Shoppers change their minds."), "priority": "could",
		"attributes": map[string]any{f.risk: f.low, f.cost: 12, f.detail: statementSaying("Café crème is refunded too.")}})
	write(key, map[string]any{"kind": "section", "title": "Receipts"})
	write(key, map[string]any{"title": "Print the receipt", "statement": statementSaying("The till prints a receipt."), "labels": []string{"checkout", "paper"}})
	write(key, map[string]any{"title": "Mail the receipt", "statement": statementSaying("A receipt goes by mail when asked.")})
	want(t, f.owner.post(t, "/api/v1/projects/"+hidKey+"/requirements", map[string]any{"title": "Pay the secret basket"}), http.StatusCreated, "write a hidden one")
	return f
}

// hitKeys reads the keys of a search's hits, in order.
func hitKeys(t *testing.T, r response) []string {
	t.Helper()
	var keys []string
	for _, each := range list(t, r, "hits") {
		keys = append(keys, each.(map[string]any)["requirement"].(map[string]any)["key"].(string))
	}
	return keys
}

// matched reads the runs of a hit's title or snippet that matched.
func matched(segments any) []string {
	var out []string
	for _, each := range segments.([]any) {
		if s := each.(map[string]any); s["match"] == true {
			out = append(out, s["text"].(string))
		}
	}
	return out
}

// The plain text the index reads comes from SQL, and has to say what document.PlainText says, or a statement
// would be found by words it no longer holds.
func TestPlainTextInSQLIsDocumentPlainText(t *testing.T) {
	h := newHarness(t)
	docs := []string{
		`{"type":"doc","content":[]}`,
		`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"One "},{"type":"text","marks":[{"type":"bold"}],"text":"two"}]},{"type":"paragraph"}]}`,
		`{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Scope"}]},{"type":"codeBlock","content":[{"type":"text","text":"x := 1"}]}]}`,
		`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Ask "},{"type":"mention","attrs":{"id":"0195f000-0000-7000-8000-000000000001","label":"Ada"}},{"type":"hardBreak"},{"type":"text","text":"today"}]}]}`,
		`{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]},{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested"}]}]}]}]}]},{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]}]}`,
		`{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Case"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Result"}]},{"type":"paragraph","content":[{"type":"text","text":"line two"}]}]}]},{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph"}]},{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"ok"}]}]}]}]}]}`,
		`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Parts"}]},{"type":"diagram","attrs":{"language":"plantuml","source":"@startuml\n[Pump] --> [Valve]\n@enduml"}}]}`,
	}
	for _, raw := range docs {
		node, err := document.Parse(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		var inSQL string
		if err := h.super.QueryRow(t.Context(), `SELECT requirement_plain_text($1::jsonb)`, raw).Scan(&inSQL); err != nil {
			t.Fatal(err)
		}
		if want := document.PlainText(node); inSQL != want {
			t.Errorf("%s:\nSQL says  %q\nGo says   %q", raw, inSQL, want)
		}
	}
}

// Search finds requirements by key, title, statement, rationale and text values, ranks the title first, marks
// what matched, and counts only what the reader may view, out of the trash.
func TestSearchOverTheAPI(t *testing.T) {
	srcKey := projectKey(t, "SRC")
	hidKey := projectKey(t, "HID")
	f := newSearchFixture(t, srcKey)
	find := func(c *client, query string) response {
		t.Helper()
		return want(t, c.get(t, "/api/v1/search?"+query), http.StatusOK, "search "+query)
	}

	t.Run("words find what the reader may view, the title first", func(t *testing.T) {
		basket := find(f.plain, "q=basket")
		sameList(t, "the member's hits", hitKeys(t, basket), srcKey+"-R1")
		if basket.Body["total"] != float64(1) {
			t.Errorf("the total counts what the member sees: %v", basket.Body["total"])
		}
		hit := list(t, basket, "hits")[0].(map[string]any)
		if got := matched(hit["title"]); len(got) != 1 || got[0] != "basket" || hit["projectName"] != "Searched" {
			t.Errorf("the title marks its match: %v", hit)
		}
		if got := find(f.owner, "q=basket").Body["total"]; got != float64(2) {
			t.Errorf("the owner sees the hidden one too: %v", got)
		}
		sameList(t, "a statement beats a value", hitKeys(t, find(f.plain, "q=card")), srcKey+"-R2", srcKey+"-R1")
		refund := list(t, find(f.plain, "q=minds"), "hits")
		if len(refund) != 1 || !strings.Contains(strings.Join(matched(refund[0].(map[string]any)["snippet"]), ""), "minds") {
			t.Errorf("the rationale is found and its snippet marked: %v", refund)
		}
		sameList(t, "text values, accents ignored", hitKeys(t, find(f.plain, "q=cafe")), srcKey+"-R2")
		sameList(t, "a text value", hitKeys(t, find(f.plain, "q=cash")), srcKey+"-R1")
		sameList(t, "a phrase and a word left out", hitKeys(t, find(f.plain, "q="+url.QueryEscape(`"a receipt" -mail`))), srcKey+"-R4")
		sameList(t, "a key typed whole comes first", hitKeys(t, find(f.plain, "q="+strings.ToLower(srcKey)+"-r5")), srcKey+"-R5")
	})

	t.Run("filters narrow, and without words the latest change comes first", func(t *testing.T) {
		sameList(t, "by label", hitKeys(t, find(f.plain, "label=Checkout")), srcKey+"-R4", srcKey+"-R1")
		sameList(t, "by project and words", hitKeys(t, find(f.owner, "q=basket&project="+strings.ToLower(hidKey))), hidKey+"-R1")
		sameList(t, "by category", hitKeys(t, find(f.plain, "q=receipt&category=draft&sort=updated")), srcKey+"-R5", srcKey+"-R4")
		if got := find(f.plain, "q=receipt&category=approved").Body["total"]; got != float64(0) {
			t.Errorf("nothing is approved: %v", got)
		}
		if got := find(f.plain, "q=receipt&updatedBefore=2000-01-01").Body["total"]; got != float64(0) {
			t.Errorf("nothing changed before 2000: %v", got)
		}
		sameList(t, "by the latest change", hitKeys(t, find(f.plain, "q=receipt&sort=updated")), srcKey+"-R5", srcKey+"-R4")
		refuse(t, "a search too long", f.plain.get(t, "/api/v1/search?q="+strings.Repeat("x", 201)), http.StatusUnprocessableEntity, "validation_failed")
		refuse(t, "a category that is none", f.plain.get(t, "/api/v1/search?category=finished"), http.StatusUnprocessableEntity, "validation_failed")
		if got := find(f.outside, "q=basket").Body["total"]; got != float64(0) {
			t.Errorf("a guest no grant names found a requirement: %v", got)
		}
	})

	t.Run("quick search finds by the start of a key or of title words", func(t *testing.T) {
		quick := func(c *client, query string) []string {
			t.Helper()
			var keys []string
			for _, each := range list(t, want(t, c.get(t, "/api/v1/search/quick?"+query), http.StatusOK, "quick "+query), "requirements") {
				hit := each.(map[string]any)
				if hit["outline"] == "" {
					t.Errorf("a quick hit has no outline: %v", hit)
				}
				keys = append(keys, hit["requirement"].(map[string]any)["key"].(string))
			}
			return keys
		}
		sameList(t, "by key", quick(f.plain, "q="+strings.ToLower(srcKey)+"-r"), srcKey+"-R1", srcKey+"-R2", srcKey+"-R3", srcKey+"-R4", srcKey+"-R5")
		sameList(t, "one key", quick(f.plain, "q="+srcKey+"-R2"), srcKey+"-R2")
		sameList(t, "title words", quick(f.plain, "q=pa"), srcKey+"-R1", srcKey+"-R2")
		sameList(t, "every word a prefix", quick(f.plain, "q=re+ma"), srcKey+"-R5")
		sameList(t, "within a project", quick(f.owner, "q=pay&project="+strings.ToLower(hidKey)), hidKey+"-R1")
		sameList(t, "nothing typed", quick(f.plain, "q="))
		refuse(t, "too many at once", f.plain.get(t, "/api/v1/search/quick?q=pa&limit=21"), http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("the trash is never searched", func(t *testing.T) {
		want(t, f.plain.delete(t, "/api/v1/requirements/"+srcKey+"-R5"), http.StatusNoContent, "trash "+srcKey+"-R5")
		sameList(t, "without it", hitKeys(t, find(f.plain, "q=receipt")), srcKey+"-R4")
		if got := list(t, want(t, f.plain.get(t, "/api/v1/search/quick?q=mail"), http.StatusOK, "quick"), "requirements"); len(got) != 0 {
			t.Errorf("quick search found the trash: %v", got)
		}
	})
}

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

func TestEachTemplateResolvesAndNothingElseDoes(t *testing.T) {
	cases := map[string]resourceRead{
		"exciter://projects/CHK/spec":                        {host: hostProjects, key: "CHK"},
		"exciter://projects/CHK/spec?section=CHK-R4":         {host: hostProjects, key: "CHK", section: "CHK-R4"},
		"exciter://requirements/CHK-R4":                      {host: hostRequirements, key: "CHK-R4"},
		"exciter://baselines/0b5f6d1e-0000-4000-8000-1/spec": {host: hostBaselines, key: "0b5f6d1e-0000-4000-8000-1"},
	}
	for uri, want := range cases {
		if got, ok := parseResource(uri); !ok || got != want {
			t.Errorf("%s reads %+v, %v", uri, got, ok)
		}
	}
	for _, uri := range []string{
		"exciter://projects/CHK", "exciter://projects//spec", "exciter://projects/CHK/spec?section=", "exciter://projects/CHK/spec?other=1",
		"exciter://requirements/CHK-R4?section=CHK-R5", "exciter://requirements/CHK-R4/spec", "exciter://baselines/x", "https://projects/CHK/spec",
		"exciter://people/ada", "exciter://projects/CHK/spec#top",
	} {
		if got, ok := parseResource(uri); ok {
			t.Errorf("%s is read as %+v", uri, got)
		}
	}
	for _, template := range resourceTemplates {
		uri := strings.NewReplacer("{projectKey}", "CHK", "{reqKey}", "CHK-R4", "{baselineID}", "b1", "{?section}", "").Replace(template["uriTemplate"].(string))
		if _, ok := parseResource(uri); !ok || template["mimeType"] != markdownMIME {
			t.Errorf("the template %s does not resolve", template["uriTemplate"])
		}
	}
}

func TestADocumentPastTheLimitIsCutAfterTheLastEntryThatFits(t *testing.T) {
	doc := specdoc.Document{Kind: specdoc.KindProject, Title: "Big", ProjectKey: "BIG", ProjectName: "Big"}
	// Four entries fit beside the head and the closing sentence, and a fifth does not.
	long := strings.Repeat("word ", (MCPMaxResourceBytes/4-1000)/len("word "))
	for _, key := range []string{"BIG-R1", "BIG-R2", "BIG-R3", "BIG-R4", "BIG-R5", "BIG-R6", "BIG-R7", "BIG-R8"} {
		raw, _ := json.Marshal(map[string]any{"type": "doc", "content": []any{
			map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": long}}}}})
		doc.Entries = append(doc.Entries, specdoc.Entry{Key: key, Kind: "requirement", Outline: key[5:], Title: key, Statement: raw})
	}
	doc.Total = len(doc.Entries)
	md := doc.Markdown()
	got := cut(doc, md)
	if len(got) > MCPMaxResourceBytes || !strings.HasSuffix(got, "Read on with exciter://projects/BIG/spec?section=BIG-R5, the next entry and what is below it.\n") ||
		!strings.Contains(got, "after 4 entries") || !strings.Contains(got, "## 4 BIG-R4") || strings.Contains(got, "BIG-R5 BIG-R5") {
		t.Errorf("the cut document is %d bytes and ends %q", len(got), got[max(0, len(got)-200):])
	}
	whole := doc
	whole.Entries, whole.Total = doc.Entries[:1], 1
	if got := cut(whole, whole.Markdown()); got != whole.Markdown().String() {
		t.Errorf("a document that fits is cut: %q", got[max(0, len(got)-200):])
	}
	baseline := doc
	baseline.Kind = specdoc.KindBaseline
	baseline.Baseline = &specdoc.BaselineInfo{Name: "R1"}
	if got := cut(baseline, baseline.Markdown()); !strings.HasSuffix(got, "Read the rest with get_baseline_spec and offset 4.\n") {
		t.Errorf("a baseline cut ends %q", got[max(0, len(got)-200):])
	}
}

// The prompts name their arguments and the tools they send the model to, so a renamed tool or a slip in an
// argument shows here rather than in a conversation.
func TestEachPromptNamesItsArgumentsAndTheToolsItUses(t *testing.T) {
	for _, p := range prompts {
		if p.Title == "" || !strings.HasSuffix(p.Description, ".") || len(p.tools) == 0 || p.messages == nil {
			t.Errorf("%s is not described", p.Name)
		}
		required := 0
		for _, a := range p.Arguments {
			if a.Name == "" || !strings.HasSuffix(a.Description, ".") {
				t.Errorf("%s: the argument %q is not described", p.Name, a.Name)
			}
			if a.Required {
				required++
			}
		}
		if required == 0 {
			t.Errorf("%s requires no argument", p.Name)
		}
		for _, name := range p.tools {
			tool, ok := toolByName(name)
			if !ok || !tool.ReadOnly && !strings.Contains(name, "comment") && name != "create_requirement" {
				t.Errorf("%s names %s, which is not a tool, or one that writes what it should not", p.Name, name)
			}
		}
	}
}

func TestAPromptIsWrittenWithItsArgumentsInPlace(t *testing.T) {
	s := &Server{}
	get := func(params string) (string, *rpcError) {
		req := httptest.NewRequest(http.MethodPost, mcpPath, nil)
		got, err := s.dispatchMCP(req, rpcRequest{JSONRPC: "2.0", Method: "prompts/get", Params: json.RawMessage(params)})
		encoded, _ := json.Marshal(got)
		return string(encoded), err
	}
	text, err := get(`{"name":"review_section","arguments":{"projectKey":"CHK","section":"CHK-S3"}}`)
	if err != nil || !strings.Contains(text, "section CHK-S3 of project CHK since its last approval") ||
		!strings.Contains(text, "changes approved and onlyChanged true") || !strings.Contains(text, noDecision) {
		t.Errorf("review_section reads %s, %+v", text, err)
	}
	text, err = get(`{"name":"write_requirements","arguments":{"projectKey":"CHK","source":"The basket keeps its items."}}`)
	if err != nil || !strings.Contains(text, "The basket keeps its items.") || strings.Contains(text, "parentId") {
		t.Errorf("write_requirements reads %s, %+v", text, err)
	}
	refusals := map[string]string{
		`{"name":"review_section","arguments":{}}`:                                     "needs projectKey",
		`{"name":"review_section","arguments":{"projectKey":" "}}`:                     "needs projectKey",
		`{"name":"review_section","arguments":{"projectKey":"CHK","base":"baseline"}}`: "approved or previous",
		`{"name":"write_requirements","arguments":{"projectKey":"CHK"}}`:               "needs source",
		`{"name":"check_quality","arguments":{"reqKey":"CHK-R1","extra":"x"}}`:         "does not take extra",
		`{"name":"approve_everything"}`:                                                "no prompt approve_everything",
		`{"name":"check_quality","arguments":{"reqKey":3}}`:                            "object of strings",
	}
	for params, says := range refusals {
		if _, err := get(params); err == nil || err.Code != rpcInvalidParams || !strings.Contains(err.Message, says) {
			t.Errorf("%s answers %+v, want it to say %q", params, err, says)
		}
	}
	listed, _ := s.dispatchMCP(httptest.NewRequest(http.MethodPost, mcpPath, nil), rpcRequest{Method: "initialize"})
	if _, ok := listed.(map[string]any)["capabilities"].(map[string]any)["prompts"]; !ok {
		t.Error("initialize does not announce prompts")
	}
}

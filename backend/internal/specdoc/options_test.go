package specdoc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestATemplateKeepsTheFieldsItNamesInItsOrder(t *testing.T) {
	entry := func() Entry {
		return Entry{Revision: 4, Rationale: json.RawMessage(`{"type":"doc"}`), Properties: []Property{
			{Field: "type", Text: "Functional"}, {Field: "status", Text: "Draft"}, {Field: "owner", Text: "Ada"},
			{Name: "Cost", Text: "12"}, {Name: "Risk", Text: "high"},
		}}
	}
	e := entry()
	arrange(&e, []Choice{{Field: "attribute", Attribute: "risk"}, {Field: "status"}, {Field: FieldRevision}, {Field: "priority"}})
	var got []string
	for _, p := range e.Properties {
		got = append(got, p.Field+p.Name+"="+p.Text)
	}
	if strings.Join(got, ",") != "Risk=high,status=Draft,revision=4" {
		t.Errorf("the properties are %v", got)
	}
	if e.Rationale != nil {
		t.Error("the rationale stays though the template does not name it")
	}
	e = entry()
	arrange(&e, []Choice{{Field: FieldRationale}, {Field: "attribute", Attribute: "Gone"}})
	if e.Rationale == nil || len(e.Properties) != 0 || e.Properties == nil {
		t.Errorf("naming the rationale and an attribute the project lacks leaves %v and %s", e.Properties, e.Rationale)
	}
}

func TestChangesSinceABaselineAreMarkedAsInsertedAndDeleted(t *testing.T) {
	para := func(text string) string {
		return `{"type":"paragraph","content":[{"type":"text","text":"` + text + `"}]}`
	}
	old := json.RawMessage(`{"type":"doc","content":[` + para("Brake within 2 s.") + `,` + para("Dropped.") + `]}`)
	current := json.RawMessage(`{"type":"doc","content":[` + para("Brake within 1 s.") + `,` + para("Added.") + `,` + para("A new block.") + `]}`)
	got, err := marked(old, current)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{`"diffInsert"`, `"diffDelete"`, "Dropped", "Added", "Brake within "} {
		if !strings.Contains(text, want) {
			t.Errorf("the marked statement lacks %s: %s", want, text)
		}
	}
	var doc struct {
		Content []struct {
			Content []struct {
				Text  string `json:"text"`
				Marks []struct {
					Type string `json:"type"`
				} `json:"marks"`
			} `json:"content"`
		} `json:"content"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	marks := map[string]string{}
	for _, block := range doc.Content {
		for _, run := range block.Content {
			for _, m := range run.Marks {
				marks[strings.TrimSpace(run.Text)] = m.Type
			}
		}
	}
	if marks["2"] != "diffDelete" || marks["1"] != "diffInsert" || marks["Dropped"] != "diffDelete" || marks["Added"] != "diffInsert" {
		t.Errorf("the words changed are marked %v", marks)
	}
	if marks["A new block."] != "diffInsert" || marks["."] != "" {
		t.Errorf("a block added is not marked whole, or an unchanged word is: %v", marks)
	}
	if _, ok := marks["Brake within"]; ok {
		t.Errorf("unchanged words are marked: %v", marks)
	}
	same, err := marked(current, current)
	if err != nil || strings.Contains(string(same), "diff") {
		t.Errorf("an unchanged statement is marked: %s %v", same, err)
	}
}

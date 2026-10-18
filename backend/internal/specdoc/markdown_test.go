package specdoc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

func paragraphOf(text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}}})
	return raw
}

func sample() *Document {
	yes := true
	// A spanning cell is what Markdown cannot hold, so the rationale is written as its words.
	spanning := json.RawMessage(`{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[` +
		`{"type":"tableHeader","attrs":{"colspan":2,"rowspan":1,"colwidth":null},"content":[{"type":"paragraph","content":[{"type":"text","text":"Wide"}]}]}]}]}]}`)
	return &Document{Kind: KindProject, Title: "Checkout", ProjectKey: "CHK", ProjectName: "Checkout", ExportedBy: "Ada",
		ExportedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC), Total: 3, Entries: []Entry{
			{Key: "CHK-S1", Kind: requirement.KindSection, Outline: "1", Depth: 0, Title: "Payment", Statement: paragraphOf("How money moves.")},
			{Key: "CHK-R2", Kind: requirement.KindRequirement, Outline: "1.1", Depth: 1, Title: "Pay *fast*", Revision: 3,
				Statement: paragraphOf("Pay within 2 s."), Rationale: spanning,
				Properties: []Property{{Field: FieldStatus, Text: "Draft"}, {Name: "Safety", Flag: &yes}, {Name: "Note", Document: paragraphOf("Ask  the\nbank")}},
				Links:      []Link{{Phrase: "refines", Key: "CHK-R1", Title: "Pay"}}},
		}}
}

func TestADocumentIsWrittenAsMarkdownEntryByEntry(t *testing.T) {
	md := sample().Markdown()
	want := strings.Join([]string{
		"# Checkout",
		"Specification of Checkout (CHK). Exported by Ada on 2026-10-17.",
		"## 1 CHK-S1 Payment",
		"How money moves.",
		"### 1.1 CHK-R2 Pay \\*fast\\*",
		"- Revision: 3\n- Status: Draft\n- Safety: yes\n- Note: Ask the bank",
		"Pay within 2 s.",
		"**Rationale**",
		"*(as plain text)*",
		"Wide",
		"**Links**",
		"- refines CHK-R1 Pay",
		"This document holds the first 2 of the 3 requirements and sections. Read a section to hold the rest.",
	}, "\n\n") + "\n"
	if got := md.String(); got != want {
		t.Errorf("the document reads\n%s\nwant\n%s", got, want)
	}
	if len(md.Entries) != 2 || !strings.HasPrefix(md.Entries[1], "### 1.1") {
		t.Errorf("the entries are %q", md.Entries)
	}
	if got := md.Upto(1, "Cut."); !strings.HasSuffix(got, "How money moves.\n\nCut.\n") {
		t.Errorf("one entry and a closing sentence read %q", got)
	}
}

func TestABaselineSaysWhenItWasTakenAndLocked(t *testing.T) {
	doc := sample()
	locked := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	doc.Kind, doc.Total, doc.Baseline = KindBaseline, 2, &BaselineInfo{Name: "R1", Description: "For the audit.", TakenBy: "Bo",
		TakenAt: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), LockedAt: &locked}
	md := doc.Markdown()
	if !strings.Contains(md.Head, "Baseline of Checkout (CHK), taken by Bo on 2026-09-30. Locked on 2026-10-01; it never changes.") ||
		!strings.HasSuffix(md.Head, "\n\nFor the audit.") || md.Tail != "" {
		t.Errorf("the baseline's head reads %q, its tail %q", md.Head, md.Tail)
	}
}

func TestTheGlossaryFollowsTheEntries(t *testing.T) {
	doc := sample()
	doc.Glossary = []GlossaryTerm{
		{Name: "Brake pedal", Definition: paragraphOf("The pedal that slows the car."), Synonyms: []string{"pedal", "brake"}, Avoid: []string{"stopper"}},
		{Name: "ABS", Definition: paragraphOf("Anti-lock braking."), Synonyms: []string{}, Avoid: []string{}},
	}
	md := doc.Markdown()
	want := strings.Join([]string{
		"## Glossary",
		"**Brake pedal**",
		"The pedal that slows the car.",
		"- Synonyms: pedal, brake\n- Not to use: stopper",
		"**ABS**",
		"Anti-lock braking.",
	}, "\n\n")
	if md.Glossary != want {
		t.Errorf("the glossary reads\n%s\nwant\n%s", md.Glossary, want)
	}
	if got := md.String(); !strings.HasSuffix(got, "Read a section to hold the rest.\n\n"+want+"\n") {
		t.Errorf("the glossary does not end the document:\n%s", got)
	}
	if got := md.Upto(1, "Cut."); strings.Contains(got, "## Glossary") {
		t.Errorf("a document cut short carries its glossary: %s", got)
	}
	doc.Glossary = []GlossaryTerm{}
	if md := doc.Markdown(); md.Glossary != "" || strings.Contains(md.String(), "Glossary") {
		t.Errorf("a document without terms has a glossary: %q", md.Glossary)
	}
}

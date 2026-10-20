package demo

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/markdown"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

const featureBody = `## User story

As a tester, I want a story, so that it parses.

## Acceptance criteria

- [x] **Adding it:** an administrator adds the demo once.
- [ ] A criterion without a lead that runs on for a long while, well past the length a title should have, so that it is cut at a word and ends in three dots
  - it nests a point
- [ ] ` + "`code`" + ` in a criterion

## Tests

- [ ] Not a criterion.

## Depends on

- #12
- Cloudster1/Armature#300
- praetorianer777/Stator#457
`

func TestParseReadsTheFeatureTemplate(t *testing.T) {
	p := Parse(featureBody)
	if p.Story != "As a tester, I want a story, so that it parses." {
		t.Errorf("story %q", p.Story)
	}
	if len(p.Criteria) != 3 {
		t.Fatalf("got %d criteria, want 3: %+v", len(p.Criteria), p.Criteria)
	}
	if c := p.Criteria[0]; c.Title != "Adding it" || !strings.HasPrefix(c.Text, "**Adding it:**") {
		t.Errorf("a bold lead is the title and stays in the text: %+v", c)
	}
	if c := p.Criteria[1]; !strings.HasSuffix(c.Title, "...") || len(c.Title) > maxTitle || !strings.Contains(c.Text, "\n- it nests a point") {
		t.Errorf("a long criterion is cut for its title and keeps its nested lines: %+v", c)
	}
	if c := p.Criteria[2]; c.Title != "code in a criterion" {
		t.Errorf("code marks leave the title: %q", c.Title)
	}
	want := []Ref{{"", 12}, {"Cloudster1/Armature", 300}, {"praetorianer777/Stator", 457}}
	if !slices.Equal(p.DependsOn, want) {
		t.Errorf("depends on %v, want %v", p.DependsOn, want)
	}
}

func TestParseReadsDependenciesOnALineOfTheirOwn(t *testing.T) {
	p := Parse("## User story\n\nAs a tracker.\n\n## Tests\n\n- one\n\nDepends on: #210, #211, Cloudster1/Exciter#59.\n")
	want := []Ref{{"", 210}, {"", 211}, {"Cloudster1/Exciter", 59}}
	if !slices.Equal(p.DependsOn, want) {
		t.Errorf("depends on %v, want %v", p.DependsOn, want)
	}
	if p := Parse("## User story\n\nIt says that \"Depends on\" references become links, part of #9.\n"); len(p.DependsOn) != 0 {
		t.Errorf("a sentence that mentions dependencies is not one: %v", p.DependsOn)
	}
}

func fixture() Snapshot {
	return Snapshot{Tools: []Tool{
		{Repo: "Cloudster1/Armature", Key: "ARM", Name: "Armature", Taken: "2026-10-19", Issues: []Issue{
			{Number: 300, Title: "Suite demo issues", Closed: false, URL: "https://github.com/Cloudster1/Armature/issues/300",
				Body: "## User story\n\nAs a tracker user.\n\nDepends on: Cloudster1/Exciter#12, Cloudster1/Rotor#1."},
		}},
		{Repo: "Cloudster1/Exciter", Key: "EXC", Name: "Exciter", Taken: "2026-10-20", Issues: []Issue{
			{Number: 14, Title: "Backlog item", Milestone: "Backlog", Body: "## User story\n\nAs a later.\n"},
			{Number: 12, Title: "Later milestone", Milestone: "M10 Authoring quality", Closed: true, Body: "## User story\n\nAs a writer.\n"},
			{Number: 270, Title: "Suite demo", Milestone: "M2 Armature integration", Body: featureBody},
			{Number: 13, Title: "No milestone", Body: "## User story\n\nAs nobody.\n"},
		}},
	}}
}

func TestMakeOrdersMilestonesAndLinks(t *testing.T) {
	p := Make(fixture())
	if p.Taken != "2026-10-20" {
		t.Errorf("taken %q, want the latest", p.Taken)
	}
	exc := p.Projects[1]
	var titles []string
	for _, s := range exc.Sections {
		titles = append(titles, s.Title)
	}
	if want := []string{"M2 Armature integration", "M10 Authoring quality", Backlog, WithoutMilestone}; !slices.Equal(titles, want) {
		t.Errorf("sections %v, want %v", titles, want)
	}
	demo := exc.Sections[0].Items[0]
	if demo.Approved || len(demo.Children) != 3 || demo.Release != "M2 Armature integration" {
		t.Errorf("an open issue is a draft with its criteria: %+v", demo)
	}
	// #12 is known, Armature#300 is in the snapshot, Stator is not.
	if want := []Target{{"EXC", 12}, {"ARM", 300}}; !slices.Equal(demo.DependsOn, want) {
		t.Errorf("depends on %v, want %v", demo.DependsOn, want)
	}
	if want := []Page{{"demo-exc-docs-api-contract-m2", Specifies}}; !slices.Equal(demo.Pages, want) {
		t.Errorf("pages %v, want %v", demo.Pages, want)
	}
	if later := exc.Sections[1].Items[0]; !later.Approved {
		t.Errorf("a closed issue is approved: %+v", later)
	}
	if backlog := exc.Sections[2].Items[0]; len(backlog.Pages) != 0 {
		t.Errorf("the backlog has no contract: %v", backlog.Pages)
	}
	arm := p.Projects[0]
	if want := []Target{{"EXC", 12}}; !slices.Equal(arm.Sections[0].Items[0].DependsOn, want) {
		t.Errorf("a repository outside the demo is left out: %v", arm.Sections[0].Items[0].DependsOn)
	}
	if want := []Page{{"demo-arm-readme", Describes}, {"demo-arm-docs-architecture", Describes}}; !slices.Equal(arm.About.Pages, want) {
		t.Errorf("about pages %v, want %v", arm.About.Pages, want)
	}
	if key := (Target{"EXC", 261}).IssueKey(); key != "EXC-261" {
		t.Errorf("issue key %q", key)
	}
}

func TestCleanEscapesPlaceholdersOutsideCode(t *testing.T) {
	got := Clean("About <tool>, `<kept>` and ![logo](x.png) a < b")
	if want := "About \\<tool>, `<kept>` and  a < b"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	root, err := markdown.Convert(got, markdown.Resolve{})
	if err != nil {
		t.Fatal(err)
	}
	if text := document.PlainText(root); !strings.Contains(text, "About <tool>, <kept>") {
		t.Errorf("the reader sees %q", text)
	}
}

// Every statement of the snapshot this build carries goes in as a requirement's would, so a snapshot taken again
// that brings something a statement refuses fails here, not when an administrator adds the demo.
func TestTheEmbeddedSnapshotPlansStatementsThatAreTaken(t *testing.T) {
	s, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	p := Make(s)
	check := func(raw json.RawMessage) error { _, err := document.Parse(raw); return err }
	take := func(where, field, md string) {
		if _, err := requirement.FromMarkdown(field, &md, nil, markdown.Resolve{}, check); err != nil {
			t.Errorf("%s: %s refused: %v", where, field, err)
		}
	}
	items, children := 0, 0
	for _, pr := range p.Projects {
		all := []Item{pr.About}
		for _, sec := range pr.Sections {
			all = append(all, sec.Items...)
		}
		for _, it := range all {
			where := fmt.Sprintf("%s#%d", pr.Repo, it.Issue)
			if it.Title == "" || len([]rune(it.Title)) > requirement.MaxTitleLength {
				t.Errorf("%s: title %q", where, it.Title)
			}
			take(where, "statement", it.Statement)
			take(where, "rationale", it.Rationale)
			for i, c := range it.Children {
				if c.Title == "" {
					t.Errorf("%s criterion %d has no title", where, i+1)
				}
				take(fmt.Sprintf("%s criterion %d", where, i+1), "statement", c.Statement)
			}
			items++
			children += len(it.Children)
		}
	}
	t.Logf("%d requirements with %d criteria", items, children)
}

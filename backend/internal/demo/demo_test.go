package demo

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode"

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

const architectureFile = "commit: abc123\nsource: docs/architecture.md\n\nHow it is built.\n\n## Components\n\nThe parts.\n\n" +
	"```plantuml\n@startuml\ntitle Components\n## inside the fence\n[A] --> [B]\n@enduml\n```\n\n## Signing in\n\nThe flow.\n\n" +
	"```plantuml\n@startuml\nA -> B\n@enduml\n```\n"

func TestParseArchitectureReadsAViewUnderEachHeading(t *testing.T) {
	a, err := ParseArchitecture(architectureFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Commit != "abc123" || a.Source != "docs/architecture.md" || a.Intro != "How it is built." {
		t.Errorf("the head reads %+v", a)
	}
	if len(a.Views) != 2 || a.Views[0].Title != "Components" || a.Views[1].Title != "Signing in" {
		t.Fatalf("views %+v", a.Views)
	}
	if !strings.HasPrefix(a.Views[0].Statement, "The parts.") || !strings.Contains(a.Views[0].Statement, "## inside the fence") {
		t.Errorf("a heading in a fence stays in the view: %q", a.Views[0].Statement)
	}
	sources, err := DiagramSources(a.Views[1].Statement)
	if err != nil || len(sources) != 1 || !strings.Contains(sources[0], "A -> B") {
		t.Errorf("the view's diagram is %q (%v)", sources, err)
	}
	for name, md := range map[string]string{
		"no commit":  strings.Replace(architectureFile, "commit: abc123\n", "", 1),
		"no source":  strings.Replace(architectureFile, "source: docs/architecture.md\n", "", 1),
		"no view":    "commit: a\nsource: b\n\nOnly an introduction.\n",
		"open fence": "commit: a\nsource: b\n\n## View\n\n```plantuml\n@startuml\n",
		"nothing":    "",
	} {
		if _, err := ParseArchitecture(md); err == nil {
			t.Errorf("%s was read", name)
		}
	}
}

func TestMakeFilesTheArchitectureWithItsSource(t *testing.T) {
	s := fixture()
	a, err := ParseArchitecture(architectureFile)
	if err != nil {
		t.Fatal(err)
	}
	s.Tools[1].Architecture = &a
	p := Make(s)
	if p.Projects[0].Architecture != nil {
		t.Errorf("a tool without drawings has no architecture section: %+v", p.Projects[0].Architecture)
	}
	sec := p.Projects[1].Architecture
	if sec == nil || sec.Title != SectionArchitecture || sec.Intro != "How it is built." || len(sec.Items) != 2 {
		t.Fatalf("the section is %+v", sec)
	}
	view := sec.Items[1]
	if view.Title != "Signing in" || !view.Approved || view.Issue != 0 {
		t.Errorf("a view is an approved requirement of no issue: %+v", view)
	}
	if want := []Page{{"demo-exc-docs-architecture", Describes}}; !slices.Equal(view.Pages, want) {
		t.Errorf("pages %v, want %v", view.Pages, want)
	}
	if !strings.Contains(view.Rationale, "(https://github.com/Cloudster1/Exciter/blob/abc123/docs/architecture.md)") {
		t.Errorf("the rationale links the document at its commit: %q", view.Rationale)
	}
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

func TestARunMakesEveryProjectBeforeItLinksAny(t *testing.T) {
	p := Make(fixture())
	build := stepsOf(p, KindBuild)
	// Armature: its project and one section; Exciter: its project and four; a link step each; the baselines.
	want := []step{
		{stepProject, 0, 0}, {stepSection, 0, 0},
		{stepProject, 1, 0}, {stepSection, 1, 0}, {stepSection, 1, 1}, {stepSection, 1, 2}, {stepSection, 1, 3},
		{stepLinks, 0, 0}, {stepLinks, 1, 0}, {stepBaseline, 0, 0},
	}
	if !slices.Equal(build, want) {
		t.Errorf("a build runs %v, want %v", build, want)
	}
	if links := stepsOf(p, KindLinks); !slices.Equal(links, want[len(want)-3:]) || StepCount(p, KindLinks) != 3 {
		t.Errorf("linking again runs %v", links)
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
		checkArchitecture(t, pr, take)
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

// minViews is what every tool's architecture draws at least: its parts, its deployment, signing in and the suite.
const minViews = 4

// checkArchitecture holds each view to one diagram in plain ASCII, as the repository's prose is, under a title the
// requirement takes.
func checkArchitecture(t *testing.T, pr Project, take func(where, field, md string)) {
	t.Helper()
	sec := pr.Architecture
	if sec == nil || len(sec.Items) < minViews || sec.Intro == "" {
		t.Errorf("%s: the architecture is %+v, want an introduction and at least %d views", pr.Repo, sec, minViews)
		return
	}
	take(pr.Repo+" architecture", "statement", sec.Intro)
	seen := map[string]bool{}
	for _, it := range sec.Items {
		where := pr.Repo + " architecture view " + it.Title
		if it.Title == "" || len([]rune(it.Title)) > requirement.MaxTitleLength || seen[it.Title] {
			t.Errorf("%s: title %q is empty, too long or twice", where, it.Title)
		}
		seen[it.Title] = true
		for i, r := range it.Statement + it.Title + sec.Intro {
			if r > unicode.MaxASCII {
				t.Errorf("%s: %q at %d is not ASCII", where, r, i)
				break
			}
		}
		take(where, "statement", it.Statement)
		take(where, "rationale", it.Rationale)
		if sources, err := DiagramSources(it.Statement); err != nil || len(sources) != 1 {
			t.Errorf("%s holds %d diagrams (%v), want one", where, len(sources), err)
		}
	}
}

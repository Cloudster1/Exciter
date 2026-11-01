package demo

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

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

const boardsFile = "# Functional requirements / Boards\n\nHow work moves across a board.\n\n" +
	"### FR-BRD-1 Move an issue by dragging its card\ntype: Functional\npriority: must\nverification: Test\ncomponent: Boards\n" +
	"refines: SN-3\nrelates: FR-BRD-2\nissues: ARM-112, ARM-140\nguide: boards\n\n" +
	"When a person drags a card, Armature shall move the issue.\n\n| Column | Status |\n|---|---|\n| Done | Done |\n\n" +
	"```text\n### not a heading\n\nRationale: not one either\n```\n\nRationale: Moving a card is how a team reports progress.\n\n" +
	"## Swimlanes\n\nLanes split columns.\n\n" +
	"### FR-BRD-2 Configure swimlanes\ntype: Functional\npriority: should\nverification: Inspection\nguide: ../exciter/traceability\n\n" +
	"Armature shall keep each board's lanes apart.\n"

const testsFile = "# Test specification / Boards\n\n" +
	"### TC-BRD-1 Drag a card\ntype: Test case\nverifies: FR-BRD-1, FR-BRD-2\nautomated: yes\nresult: failed\nnote: Fails as in ARM-345.\n\n" +
	"Preconditions: A scrum project with a board\nwhose columns map to statuses.\n\n" +
	"| Action | Expected |\n|---|---|\n| Drag the card to `In progress`. | The card stays there. |\n| Reload \\| look. | |\n\n" +
	"Parameters: column = In progress, Done; who = alice, bob\n"

func TestParseSpecReadsSectionsItemsAndTestCases(t *testing.T) {
	f, err := ParseSpec("spec/armature/10-boards.md", boardsFile)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "10-boards.md" || len(f.Sections) != 2 {
		t.Fatalf("the file reads %+v", f)
	}
	head, lanes := f.Sections[0], f.Sections[1]
	if !slices.Equal(head.Path, []string{"Functional requirements", "Boards"}) || head.Intro != "How work moves across a board." {
		t.Errorf("the head section is %v %q", head.Path, head.Intro)
	}
	if !slices.Equal(lanes.Path, []string{"Functional requirements", "Boards", "Swimlanes"}) || lanes.Intro != "Lanes split columns." {
		t.Errorf("a subsection sits inside the head: %v %q", lanes.Path, lanes.Intro)
	}
	it := head.Items[0]
	if it.ID != "FR-BRD-1" || it.Title != "Move an issue by dragging its card" || it.Type != TypeFunctional || it.Priority != "must" ||
		it.Verification != "Test" || it.Component != "Boards" || it.Guide != "boards" || it.Line != 5 {
		t.Errorf("the fields read %+v", it)
	}
	if !slices.Equal(it.Refines, []string{"SN-3"}) || !slices.Equal(it.Relates, []string{"FR-BRD-2"}) || !slices.Equal(it.Issues, []string{"ARM-112", "ARM-140"}) {
		t.Errorf("the links read %v %v %v", it.Refines, it.Relates, it.Issues)
	}
	if !strings.Contains(it.Statement, "| Column | Status |") || !strings.Contains(it.Statement, "### not a heading") || len(it.Steps) != 0 {
		t.Errorf("a requirement's table and fence stay in its statement: %q", it.Statement)
	}
	if it.Rationale != "Moving a card is how a team reports progress." {
		t.Errorf("the rationale is %q", it.Rationale)
	}

	f, err = ParseSpec("spec/armature/80-tests-boards.md", testsFile)
	if err != nil {
		t.Fatal(err)
	}
	tc := f.Sections[0].Items[0]
	if !tc.IsTestCase() || tc.IsRequirement() || tc.Result != "failed" || tc.Note != "Fails as in ARM-345." || tc.Automated != "yes" {
		t.Errorf("the test case reads %+v", tc)
	}
	if tc.Preconditions != "A scrum project with a board\nwhose columns map to statuses." || tc.Statement != "" {
		t.Errorf("the preconditions are %q and the statement %q", tc.Preconditions, tc.Statement)
	}
	want := []SpecStep{{"Drag the card to `In progress`.", "The card stays there."}, {"Reload | look.", ""}}
	if !slices.Equal(tc.Steps, want) {
		t.Errorf("the steps are %+v", tc.Steps)
	}
	if len(tc.Parameters) != 2 || tc.Parameters[0].Name != "column" || !slices.Equal(tc.Parameters[1].Values, []string{"alice", "bob"}) {
		t.Errorf("the parameters are %+v", tc.Parameters)
	}
}

func TestParseSpecSaysWhereAndWhatToFix(t *testing.T) {
	for name, c := range map[string]struct{ md, says string }{
		"no path":           {"Text.\n", ":1: the first line is the section path"},
		"empty title":       {"# A /  / B\n", "empty title"},
		"a second path":     {"# A\n\n# B\n", ":3: only the first line"},
		"no ID":             {"# A\n\n### move a card\ntype: Functional\n", "which is no ID"},
		"unknown field":     {"# A\n\n### FR-1 T\ntype: Functional\nowner: me\n", `:5: FR-1: "owner: me" is not a field`},
		"field twice":       {"# A\n\n### FR-1 T\ntype: Functional\ntype: Functional\n", "type is given twice"},
		"bad priority":      {"# A\n\n### FR-1 T\ntype: Functional\npriority: high\n", `priority is "high". Write one of must`},
		"no type":           {"# A\n\n### FR-1 T\npriority: must\n\nIt shall.\n", "there is no type"},
		"no verification":   {"# A\n\n### FR-1 T\ntype: Functional\npriority: must\n\nIt shall.\n", "says how it is verified"},
		"no statement":      {"# A\n\n### SN-1 T\ntype: Stakeholder need\npriority: must\n", "there is no statement"},
		"result on a need":  {"# A\n\n### SN-1 T\ntype: Stakeholder need\npriority: must\nresult: passed\n\nA need.\n", "only a test case takes `result:`"},
		"bad issue":         {"# A\n\n### SN-1 T\ntype: Stakeholder need\npriority: must\nissues: #12\n\nA need.\n", `issues names "#12"`},
		"bad guide":         {"# A\n\n### SN-1 T\ntype: Stakeholder need\npriority: must\nguide: Boards Page\n\nA need.\n", "lower case"},
		"other tool":        {"# A\n\n### SN-1 T\ntype: Stakeholder need\npriority: must\nguide: ../jira/boards\n\nA need.\n", "../<tool>/<name>"},
		"rationale early":   {"# A\n\n### SN-1 T\ntype: Stakeholder need\npriority: must\n\nRationale: why.\n\nA need.\n", "not the last paragraph"},
		"open fence":        {"# A\n\n```\ncode\n", "fence is left open"},
		"case without":      {"# A\n\n### TC-1 T\ntype: Test case\n", "says what it verifies"},
		"no steps":          {"# A\n\n### TC-1 T\ntype: Test case\nverifies: FR-1\n", "a test case has steps"},
		"other columns":     {"# A\n\n### TC-1 T\ntype: Test case\nverifies: FR-1\n\n| Do | See |\n|---|---|\n| a | b |\n", "the steps are not a table"},
		"three cells":       {"# A\n\n### TC-1 T\ntype: Test case\nverifies: FR-1\n\n| Action | Expected |\n|---|---|\n| a | b | c |\n", "step 1 has 3 cells"},
		"uneven parameters": {"# A\n\n### TC-1 T\ntype: Test case\nverifies: FR-1\n\n| Action | Expected |\n|---|---|\n| a | b |\n\nParameters: x = 1, 2; y = 3\n", "the parameter y has 1 values"},
	} {
		_, err := ParseSpec("spec/exciter/10-x.md", c.md)
		var se *SpecError
		if err == nil || !errors.As(err, &se) || !strings.Contains(err.Error(), c.says) || !strings.HasPrefix(err.Error(), "spec/exciter/10-x.md:") {
			t.Errorf("%s: got %v, want it to say %q", name, err, c.says)
		}
	}
}

func TestGuideLabelNamesThePageOfItsTool(t *testing.T) {
	for guide, want := range map[string]string{"boards": "demo-arm-guide-boards", "../exciter/links-to-armature": "demo-exc-guide-links-to-armature",
		"../rotor/concepts": "demo-rot-guide-concepts"} {
		if got, _, err := GuideLabel("ARM", guide); err != nil || got != want {
			t.Errorf("%s labels %q (%v), want %q", guide, got, err, want)
		}
	}
}

// specFixture is two tools' files as Lint reads them, with every mistake it should find.
var specFixture = fstest.MapFS{
	"spec/armature/02-needs.md": {Data: []byte("# Stakeholder needs\n\n### SN-1 A need\ntype: Stakeholder need\npriority: must\n\nA lead needs it.\n")},
	"spec/armature/10-boards.md": {Data: []byte("# Functional requirements / Boards\n\n" +
		"### FR-1 Covered\ntype: Functional\npriority: must\nverification: Test\nrefines: SN-1\nissues: ARM-1\n\nIt shall.\n\n" +
		"### FR-2 Unverified\ntype: Functional\npriority: must\nverification: Test\nrefines: SN-9\nissues: ARM-2\n\nIt shall too.\n\n" +
		"### FR-1 Twice\ntype: Functional\npriority: must\nverification: Test\n\nAgain.\n")},
	"spec/armature/80-tests.md": {Data: []byte("# Test specification\n\n" +
		"### TC-1 A case\ntype: Test case\nverifies: FR-1, TC-2\nresult: blocked\n\n| Action | Expected |\n|---|---|\n| a | b |\n\n" +
		"### TC-2 Another\ntype: Test case\nverifies: SN-1\nresult: passed\n\n| Action | Expected |\n|---|---|\n| <b>bold</b> | b |\n")},
	"spec/exciter/01-bad.md": {Data: []byte("No heading.\n")},
}

func TestLintSaysWhichFileAndIDToFix(t *testing.T) {
	snap := Snapshot{Tools: []Tool{
		{Key: "ARM", Name: "Armature", Issues: []Issue{{Number: 1, Closed: true}}},
		{Key: "EXC", Name: "Exciter"},
	}}
	var got []string
	for _, err := range Lint(specFixture, snap) {
		got = append(got, err.Error())
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{
		"spec/armature/10-boards.md:21: FR-1: the ID is taken by spec/armature/10-boards.md line 3",
		"spec/armature/10-boards.md:12: FR-2: refines names SN-9, which no file of armature defines",
		"spec/armature/10-boards.md:12: FR-2: issues names ARM-2, which the snapshot does not hold",
		"spec/armature/10-boards.md:12: FR-2: no test case verifies this requirement",
		"spec/armature/80-tests.md:3: TC-1: verifies names TC-2, which is a test case",
		"spec/armature/80-tests.md:3: TC-1: the result is blocked and says nothing about it",
		"spec/armature/80-tests.md:12: TC-2: Exciter refuses the step 1's action: step 1's actionMarkdown: A requirement holds no HTML; write it as Markdown instead. Write",
		"spec/exciter/01-bad.md:1: the first line is the section path",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the lint does not say %q; it says\n%s", want, all)
		}
	}
	if strings.Contains(all, "FR-1: no test case") || strings.Contains(all, "SN-1: no test case") {
		t.Errorf("a verified requirement or a need is flagged:\n%s", all)
	}
	if len(got) != 8 {
		t.Errorf("the lint found %d mistakes, want 8:\n%s", len(got), all)
	}
}

// The specifications this build carries are written by hand, so every mistake in them fails here, each saying which
// file and ID to fix, rather than when an administrator adds the demo.
func TestTheShippedSpecificationsPassTheLint(t *testing.T) {
	for _, err := range LintEmbedded() {
		t.Error(err)
	}
	s, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range s.Tools {
		if len(tool.Spec) == 0 {
			t.Errorf("%s has no specification in spec/%s", tool.Name, toolDirs[tool.Key])
		}
	}
}

func specTool(t *testing.T) Tool {
	t.Helper()
	tool := Tool{Repo: "Cloudster1/Armature", Key: "ARM", Name: "Armature", Commit: "abc", Taken: "2026-10-20",
		Issues: []Issue{{Number: 112, Closed: true}, {Number: 140, Closed: true}}}
	for name, md := range map[string]string{"spec/armature/10-boards.md": boardsFile, "spec/armature/80-tests-boards.md": testsFile} {
		f, err := ParseSpec(name, md)
		if err != nil {
			t.Fatal(err)
		}
		tool.Spec = append(tool.Spec, f)
	}
	slices.SortFunc(tool.Spec, func(a, b SpecFile) int { return strings.Compare(a.Name, b.Name) })
	return tool
}

func TestMakePlansTheSpecificationWithStatusesTracesAndPages(t *testing.T) {
	tool := specTool(t)
	p := Make(Snapshot{Tools: []Tool{tool, {Key: "EXC", Name: "Exciter", Taken: "2026-10-21"}}})
	if p.Taken != "2026-10-21" || len(p.Projects) != 2 {
		t.Fatalf("the plan is %+v", p)
	}
	arm := p.Projects[0]
	if arm.Commit != "abc" || len(arm.Sections) != 3 {
		t.Fatalf("the project is %+v", arm)
	}
	moved := arm.Sections[0].Items[0]
	// Its issues are closed, and its one test case failed.
	if moved.Status != StatusImplemented || moved.Priority != "must" || moved.Verification != "Test" || moved.Component != "Boards" {
		t.Errorf("the requirement is %+v", moved)
	}
	if want := []Trace{{"refines", "SN-3"}, {"relates", "FR-BRD-2"}}; !slices.Equal(moved.Traces, want) {
		t.Errorf("traces %v, want %v", moved.Traces, want)
	}
	if !slices.Equal(moved.Issues, []string{"ARM-112", "ARM-140"}) {
		t.Errorf("issues %v", moved.Issues)
	}
	if want := []Page{{"demo-arm-guide-boards", Describes}}; !slices.Equal(moved.Pages, want) {
		t.Errorf("pages %v, want %v", moved.Pages, want)
	}
	if lanes := arm.Sections[1].Items[0]; lanes.Status != StatusApproved || !slices.Equal(lanes.Pages, []Page{{"demo-exc-guide-traceability", Describes}}) {
		t.Errorf("a requirement without issues is approved, with another tool's page: %+v", lanes)
	}
	tc := arm.Sections[2].Items[0]
	if tc.Status != StatusApproved || tc.TestCase == nil || len(tc.TestCase.Steps) != 2 || tc.TestCase.Iterations() != 2 || tc.Automated == nil || !*tc.Automated {
		t.Fatalf("the test case is %+v", tc)
	}
	if s := tc.TestCase.Steps[1]; *s.ActionMarkdown != "Reload | look." || s.ExpectedMarkdown != nil {
		t.Errorf("a step without an expected result expects nothing: %+v", s)
	}
	if want := []Trace{{"verifies", "FR-BRD-1"}, {"verifies", "FR-BRD-2"}}; !slices.Equal(tc.Traces, want) {
		t.Errorf("the test case traces %v", tc.Traces)
	}

	tool.Spec[1].Sections[0].Items[0].Result = resultPassed
	if got := Make(Snapshot{Tools: []Tool{tool}}).Projects[0].Sections[0].Items[0].Status; got != StatusVerified {
		t.Errorf("implemented and passed is %s, want Verified", got)
	}
	tool.Issues[1].Closed = false
	if got := Make(Snapshot{Tools: []Tool{tool}}).Projects[0].Sections[0].Items[0].Status; got != StatusApproved {
		t.Errorf("an open issue leaves it %s, want Approved", got)
	}
}

func TestMakeFilesTheArchitectureWithItsSource(t *testing.T) {
	s := Snapshot{Tools: []Tool{{Repo: "Cloudster1/Armature", Key: "ARM", Name: "Armature"}, {Repo: "Cloudster1/Exciter", Key: "EXC", Name: "Exciter"}}}
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
	if sec == nil || sec.Title() != SectionArchitecture || sec.Intro != "How it is built." || len(sec.Items) != 2 {
		t.Fatalf("the section is %+v", sec)
	}
	view := sec.Items[1]
	if view.Title != "Signing in" || view.Status != StatusApproved || view.ID != "" {
		t.Errorf("a view is an approved requirement of no ID: %+v", view)
	}
	if want := []Page{{"demo-exc-docs-architecture", Describes}}; !slices.Equal(view.Pages, want) {
		t.Errorf("pages %v, want %v", view.Pages, want)
	}
	if !strings.Contains(view.Rationale, "(https://github.com/Cloudster1/Exciter/blob/abc123/docs/architecture.md)") {
		t.Errorf("the rationale links the document at its commit: %q", view.Rationale)
	}
}

func TestARunMakesEveryProjectBeforeItLinksAnyAndTestsLast(t *testing.T) {
	p := Make(Snapshot{Tools: []Tool{specTool(t), {Key: "EXC", Name: "Exciter"}}})
	build := stepsOf(p, KindBuild)
	// Armature: its project and three sections; Exciter: its project alone; a link step each; a test run each.
	want := []step{
		{stepProject, 0, 0}, {stepSection, 0, 0}, {stepSection, 0, 1}, {stepSection, 0, 2},
		{stepProject, 1, 0},
		{stepLinks, 0, 0}, {stepLinks, 1, 0}, {stepTests, 0, 0}, {stepTests, 1, 0},
	}
	if !slices.Equal(build, want) {
		t.Errorf("a build runs %v, want %v", build, want)
	}
	if links := stepsOf(p, KindLinks); !slices.Equal(links, want[len(want)-4:]) || StepCount(p, KindLinks) != 4 {
		t.Errorf("linking again runs %v", links)
	}
}

func TestTheBlueprintAddsNeedsAndTestsToTheSoftwareTemplate(t *testing.T) {
	b := Blueprint()
	types := map[string]string{}
	for _, ts := range b.TypeSpecs {
		types[ts.Name] = ts.Role
	}
	for _, name := range itemTypes {
		if _, ok := types[name]; !ok {
			t.Errorf("the blueprint has no type %s: %v", name, types)
		}
	}
	if types[TypeTestCase] != string(attribute.RoleTestCase) || b.TypeSpecs[0].Name != TypeFunctional || len(b.Sections) != 0 {
		t.Errorf("the blueprint is %+v", b)
	}
	var automated []string
	for _, a := range b.AttributeSpecs {
		if a.Name == attributeAutomated {
			automated = a.Types
		}
	}
	if !slices.Contains(automated, TypeTestCase) || slices.Contains(automated, TypeFunctional) {
		t.Errorf("Automated shows on %v", automated)
	}
}

// Every view of the architectures this build carries goes in as a requirement's would, so a drawing a statement
// refuses fails here, not when an administrator adds the demo.
func TestTheEmbeddedArchitecturesAreTaken(t *testing.T) {
	s, err := embeddedIssues()
	if err != nil {
		t.Fatal(err)
	}
	p := Make(s)
	take := func(where, field, md string) {
		if errs := lintMarkdown(where, 0, "", field, md); len(errs) > 0 {
			t.Error(errs[0])
		}
	}
	for _, pr := range p.Projects {
		checkArchitecture(t, pr, take)
	}
	if len(p.Projects) != len(Keys) {
		t.Errorf("the snapshot holds %d tools, want %d", len(p.Projects), len(Keys))
	}
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

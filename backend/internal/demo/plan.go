package demo

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Relations an item takes to a Stator page, as stator.Relation names them.
const (
	Specifies = "specifies"
	Describes = "describes"
)

// The statuses the demo sets, as the template's workflow names them.
const (
	StatusApproved    = "Approved"
	StatusImplemented = "Implemented"
	StatusVerified    = "Verified"
)

var labelUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// Plan is the demo in the order it is made: one project per tool.
type Plan struct {
	Taken    string
	Projects []Project
}

// Project is one tool's specification. Tool is its key in Armature and Stator.
type Project struct {
	Tool   string
	Name   string
	Repo   string
	Commit string
	// Architecture is the section of drawings after the introduction, nil for a tool that has none.
	Architecture *Section
	// Sections are the specification's, in the order of its files; each is filed in a step of its own.
	Sections []Section
}

// Section is a section of the specification by its titles from the top, or the architecture's under its one title.
type Section struct {
	Path  []string
	Intro string
	Items []Item
}

// Title is the section's own title.
func (s Section) Title() string { return s.Path[len(s.Path)-1] }

// Item is a requirement or a test case of the specification, or a view of the architecture when ID is empty.
type Item struct {
	ID           string
	Type         string
	Title        string
	Statement    string
	Rationale    string
	Status       string
	Priority     string
	Verification string
	Component    string
	Automated    *bool
	TestCase     *requirement.TestCase
	// Traces are the trace links it starts, by link type key and the IDs of their targets.
	Traces []Trace
	Issues []string
	Pages  []Page
	Result string
	Note   string
}

// Trace is a trace link from an item to another of its project.
type Trace struct {
	Type string
	To   string
}

// Page is a Stator page the item links to, found by the label the demo space gives it.
type Page struct {
	Label    string
	Relation string
}

// Label is the label Stator's demo gives the page of a file in a tool's repository.
func Label(tool, path string) string {
	path = strings.TrimSuffix(strings.ToLower(path), ".md")
	return "demo-" + strings.ToLower(tool) + "-" + strings.Trim(labelUnsafe.ReplaceAllString(path, "-"), "-")
}

// Make plans the demo from a snapshot and the specifications it carries.
func Make(s Snapshot) Plan {
	closed := map[string]bool{}
	for _, t := range s.Tools {
		for _, i := range t.Issues {
			closed[fmt.Sprintf("%s-%d", t.Key, i.Number)] = i.Closed
		}
	}
	p := Plan{Taken: s.Taken()}
	for _, t := range s.Tools {
		p.Projects = append(p.Projects, planProject(t, closed))
	}
	return p
}

func planProject(t Tool, closed map[string]bool) Project {
	p := Project{Tool: t.Key, Name: t.Name, Repo: t.Repo, Commit: t.Commit, Architecture: architectureSection(t)}
	// A requirement is verified when every test case that verifies it passed.
	verifiedBy := map[string][]string{}
	for _, f := range t.Spec {
		for _, sec := range f.Sections {
			for _, it := range sec.Items {
				for _, id := range it.Verifies {
					verifiedBy[id] = append(verifiedBy[id], it.Result)
				}
			}
		}
	}
	for _, f := range t.Spec {
		for _, sec := range f.Sections {
			out := Section{Path: sec.Path, Intro: sec.Intro}
			for _, it := range sec.Items {
				out.Items = append(out.Items, specItem(t.Key, it, status(it, closed, verifiedBy[it.ID])))
			}
			p.Sections = append(p.Sections, out)
		}
	}
	return p
}

// status is Implemented for a requirement whose issues are all closed, Verified when its test cases passed as well,
// and Approved for everything else.
func status(it SpecItem, closed map[string]bool, results []string) string {
	if !it.IsRequirement() || len(it.Issues) == 0 {
		return StatusApproved
	}
	for _, key := range it.Issues {
		if !closed[key] {
			return StatusApproved
		}
	}
	if len(results) == 0 {
		return StatusImplemented
	}
	for _, r := range results {
		if r != resultPassed {
			return StatusImplemented
		}
	}
	return StatusVerified
}

// resultPassed is the result of a test case that did what it expects.
const resultPassed = "passed"

func specItem(tool string, it SpecItem, status string) Item {
	out := Item{ID: it.ID, Type: it.Type, Title: it.Title, Statement: it.Statement, Rationale: it.Rationale, Status: status,
		Priority: it.Priority, Verification: it.Verification, Component: it.Component, Issues: it.Issues, Result: it.Result, Note: it.Note}
	if it.Automated != "" {
		automated := it.Automated == "yes"
		out.Automated = &automated
	}
	for _, link := range []struct {
		kind string
		ids  []string
	}{{"refines", it.Refines}, {"relates", it.Relates}, {"verifies", it.Verifies}} {
		for _, id := range link.ids {
			out.Traces = append(out.Traces, Trace{Type: link.kind, To: id})
		}
	}
	if it.Guide != "" {
		if label, _, err := GuideLabel(tool, it.Guide); err == nil {
			out.Pages = []Page{{Label: label, Relation: Describes}}
		}
	}
	if it.IsTestCase() {
		tc := &requirement.TestCase{Steps: []requirement.Step{}, Parameters: it.Parameters}
		if it.Preconditions != "" {
			tc.PreconditionsMarkdown = &it.Preconditions
		}
		for _, s := range it.Steps {
			step := requirement.Step{ActionMarkdown: &s.Action}
			if s.Expected != "" {
				step.ExpectedMarkdown = &s.Expected
			}
			tc.Steps = append(tc.Steps, step)
		}
		if tc.Parameters == nil {
			tc.Parameters = []requirement.Parameter{}
		}
		out.TestCase = tc
	}
	return out
}

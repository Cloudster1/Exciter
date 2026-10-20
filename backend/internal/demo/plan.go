package demo

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Relations an item takes to a Stator page, as stator.Relation names them.
const (
	Specifies = "specifies"
	Describes = "describes"
)

// Section titles beside the milestones.
const (
	Backlog          = "Backlog"
	WithoutMilestone = "Without a milestone"
)

// The order of the sections that are not a numbered milestone.
const (
	orderOther = 1000 + iota
	orderBacklog
	orderWithout
)

var (
	milestoneNumber = regexp.MustCompile(`^M([0-9]+)\b`)
	labelUnsafe     = regexp.MustCompile(`[^a-z0-9]+`)
	image           = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	tagLike         = regexp.MustCompile(`<([A-Za-z/!?])`)
)

// about says what each tool is, for the requirement that opens its project.
var about = map[string]string{
	"ARM": "Armature is the suite's issue tracker. Its projects, boards and sprints carry the work, and each requirement here links to the issue that built it.",
	"EXC": "Exciter is the suite's requirements management. It keeps requirements in a tree, traces them to each other, to Armature issues and to Stator pages, and keeps baselines of them.",
	"STA": "Stator is the suite's wiki. Its spaces hold the documentation, which the requirements here cite as their specification.",
}

// Plan is the demo in the order it is made: one project per tool.
type Plan struct {
	Taken    string
	Projects []Project
}

// Project is one tool's requirements. Tool is its key in Armature and Stator.
type Project struct {
	Tool     string
	Name     string
	Repo     string
	About    Item
	Sections []Section
}

// Section holds the issues of one milestone.
type Section struct {
	Title string
	Items []Item
}

// Item is a requirement taken from one issue, or the one that says what the tool is when Issue is 0.
type Item struct {
	Issue     int
	Title     string
	Statement string
	Rationale string
	Approved  bool
	Release   string
	Children  []Child
	DependsOn []Target
	Pages     []Page
}

// Child is an acceptance criterion; it is approved when its issue is.
type Child struct {
	Title     string
	Statement string
}

// Target is an issue of the demo: Tool's key and its number.
type Target struct {
	Tool   string
	Number int
}

// Page is a Stator page the item links to, found by the label the demo space gives it.
type Page struct {
	Label    string
	Relation string
}

// IssueKey is the issue's key in Armature's demo.
func (t Target) IssueKey() string { return fmt.Sprintf("%s-%d", t.Tool, t.Number) }

// Label is the label Stator's demo gives the page of a file in a tool's repository.
func Label(tool, path string) string {
	path = strings.TrimSuffix(strings.ToLower(path), ".md")
	return "demo-" + strings.ToLower(tool) + "-" + strings.Trim(labelUnsafe.ReplaceAllString(path, "-"), "-")
}

// Make plans the demo from a snapshot.
func Make(s Snapshot) Plan {
	known := map[Target]bool{}
	byRepo := map[string]string{}
	for _, t := range s.Tools {
		byRepo[strings.ToLower(t.Repo)] = t.Key
		for _, i := range t.Issues {
			known[Target{t.Key, i.Number}] = true
		}
	}
	p := Plan{Taken: s.Taken()}
	for _, t := range s.Tools {
		p.Projects = append(p.Projects, planProject(t, s.Taken(), byRepo, known))
	}
	return p
}

func planProject(t Tool, taken string, byRepo map[string]string, known map[Target]bool) Project {
	p := Project{Tool: t.Key, Name: t.Name, Repo: t.Repo, About: Item{
		Title: "About " + t.Name,
		Statement: about[t.Key] + "\n\nThe requirements of this project are " + t.Name + "'s own, taken on " + taken +
			" from the user stories and acceptance criteria of its issues.",
		Rationale: fmt.Sprintf("Taken from [%s](https://github.com/%s).", t.Repo, t.Repo),
		Approved:  true,
		Pages:     []Page{{Label(t.Key, "README.md"), Describes}, {Label(t.Key, "docs/architecture.md"), Describes}},
	}}
	sections := map[string]*Section{}
	var order []string
	for _, issue := range t.Issues {
		title := cmp.Or(issue.Milestone, WithoutMilestone)
		sec, ok := sections[title]
		if !ok {
			sec = &Section{Title: title}
			sections[title] = sec
			order = append(order, title)
		}
		sec.Items = append(sec.Items, item(t, issue, byRepo, known))
	}
	slices.SortStableFunc(order, func(a, b string) int {
		return cmp.Or(cmp.Compare(sectionOrder(a), sectionOrder(b)), cmp.Compare(a, b))
	})
	for _, title := range order {
		p.Sections = append(p.Sections, *sections[title])
	}
	return p
}

func sectionOrder(title string) int {
	switch title {
	case Backlog:
		return orderBacklog
	case WithoutMilestone:
		return orderWithout
	}
	if n, ok := milestone(title); ok {
		return n
	}
	return orderOther
}

func milestone(title string) (int, bool) {
	m := milestoneNumber.FindStringSubmatch(title)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

func item(t Tool, issue Issue, byRepo map[string]string, known map[Target]bool) Item {
	parsed := Parse(issue.Body)
	it := Item{
		Issue:     issue.Number,
		Title:     shorten(issue.Title),
		Statement: Clean(parsed.Story),
		Rationale: fmt.Sprintf("Taken from [%s#%d](%s).", t.Repo, issue.Number, issue.URL),
		Approved:  issue.Closed,
		Release:   issue.Milestone,
	}
	for _, c := range parsed.Criteria {
		it.Children = append(it.Children, Child{Title: c.Title, Statement: Clean(c.Text)})
	}
	self := Target{t.Key, issue.Number}
	for _, ref := range parsed.DependsOn {
		tool := t.Key
		if ref.Repo != "" {
			if tool = byRepo[strings.ToLower(ref.Repo)]; tool == "" {
				continue
			}
		}
		if target := (Target{tool, ref.Number}); target != self && known[target] {
			it.DependsOn = append(it.DependsOn, target)
		}
	}
	if n, ok := milestone(issue.Milestone); ok {
		it.Pages = []Page{{Label(t.Key, fmt.Sprintf("docs/api-contract-m%d.md", n)), Specifies}}
	}
	return it
}

// Clean keeps an issue's Markdown to what a requirement's statement takes. Images stay on GitHub, and a placeholder
// such as <tool> outside code is escaped, since a statement refuses what reads as HTML.
func Clean(md string) string {
	md = image.ReplaceAllString(md, "")
	parts := strings.Split(md, "`")
	for i := 0; i < len(parts); i += 2 {
		parts[i] = tagLike.ReplaceAllString(parts[i], `\<$1`)
	}
	return strings.TrimSpace(strings.Join(parts, "`"))
}

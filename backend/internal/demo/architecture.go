package demo

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/markdown"
)

// The drawings of each tool's architecture (#286), written by hand from its documentation, travel with the binary as
// the snapshot does.
//
//go:embed architecture/*.md
var architectureFiles embed.FS

// SectionArchitecture is the top-level section the drawings are filed in, after the introduction.
const SectionArchitecture = "Architecture"

const (
	fieldCommit = "commit: "
	fieldSource = "source: "
	viewHeading = "## "
	fence       = "```"
)

// Architecture is one tool's architecture drawn as views, from Source in its repository at Commit.
type Architecture struct {
	Commit string
	Source string
	Intro  string
	Views  []View
}

// View is one drawing: its title and a statement of a few sentences around a plantuml fence.
type View struct {
	Title     string
	Statement string
}

// ParseArchitecture reads a file of architecture/: the commit and source lines, an introduction, then a view under
// each second-level heading. A heading inside a fence belongs to the fence.
func ParseArchitecture(md string) (Architecture, error) {
	var (
		a       Architecture
		intro   []string
		body    []string
		inFence bool
	)
	flush := func() {
		if n := len(a.Views); n > 0 {
			a.Views[n-1].Statement = strings.TrimSpace(strings.Join(body, "\n"))
		} else {
			a.Intro = strings.TrimSpace(strings.Join(intro, "\n"))
		}
		body = nil
	}
	for line := range strings.Lines(md) {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, fence):
			inFence = !inFence
		case inFence:
		case a.Commit == "" && strings.HasPrefix(line, fieldCommit):
			a.Commit = strings.TrimSpace(strings.TrimPrefix(line, fieldCommit))
			continue
		case a.Source == "" && strings.HasPrefix(line, fieldSource):
			a.Source = strings.TrimSpace(strings.TrimPrefix(line, fieldSource))
			continue
		case strings.HasPrefix(line, viewHeading):
			flush()
			a.Views = append(a.Views, View{Title: strings.TrimSpace(strings.TrimPrefix(line, viewHeading))})
			continue
		}
		if len(a.Views) == 0 {
			intro = append(intro, line)
		} else {
			body = append(body, line)
		}
	}
	flush()
	switch {
	case a.Commit == "" || a.Source == "":
		return a, errors.New("an architecture file starts with its commit and source lines")
	case len(a.Views) == 0:
		return a, errors.New("an architecture file holds at least one view")
	case inFence:
		return a, errors.New("an architecture file leaves a fence open")
	}
	return a, nil
}

// embeddedArchitecture reads the drawings of the tool a snapshot file names, if there are any.
func embeddedArchitecture(name string) (*Architecture, error) {
	raw, err := architectureFiles.ReadFile("architecture/" + name + ".md")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a, err := ParseArchitecture(string(raw))
	if err != nil {
		return nil, fmt.Errorf("the %s architecture: %w", name, err)
	}
	return &a, nil
}

// architectureSection plans the section of a tool's drawings, each traced to the page of the document they are
// drawn from.
func architectureSection(t Tool) *Section {
	a := t.Architecture
	if a == nil {
		return nil
	}
	doc := fmt.Sprintf("[%s](https://github.com/%s/blob/%s/%s)", a.Source, t.Repo, a.Commit, a.Source)
	sec := &Section{Title: SectionArchitecture, Intro: a.Intro}
	for _, v := range a.Views {
		sec.Items = append(sec.Items, Item{
			Title:     v.Title,
			Statement: v.Statement,
			Rationale: "Drawn from " + doc + " and the code beside it.",
			Approved:  true,
			Pages:     []Page{{Label(t.Key, a.Source), Describes}},
		})
	}
	return sec
}

// DiagramSources is the source of each diagram a statement's Markdown becomes, in order.
func DiagramSources(statement string) ([]string, error) {
	root, err := markdown.Convert(statement, markdown.Resolve{})
	if err != nil {
		return nil, err
	}
	var (
		out  []string
		walk func(nodes []document.Node)
	)
	walk = func(nodes []document.Node) {
		for _, n := range nodes {
			if n.Type == document.NodeDiagram {
				src, _ := n.Attrs["source"].(string)
				out = append(out, src)
			}
			walk(n.Content)
		}
	}
	walk(root.Content)
	return out, nil
}

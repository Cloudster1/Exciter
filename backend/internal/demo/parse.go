package demo

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxTitle keeps a criterion's title to a line; its statement holds all of it.
const maxTitle = 120

var (
	heading    = regexp.MustCompile(`^##[ \t]+(.+?)[ \t]*$`)
	checkbox   = regexp.MustCompile(`^[-*][ \t]+\[([ xX])\][ \t]+(.*)$`)
	lead       = regexp.MustCompile(`^\*\*(.+?)\*\*[ \t]*`)
	inlineDeps = regexp.MustCompile(`(?i)^\**depends on\**:?\**[ \t]*(.*)$`)
	reference  = regexp.MustCompile(`([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?#([0-9]+)`)
)

// Parsed is what an issue's body says as requirements: its user story, its acceptance criteria and the issues it
// depends on.
type Parsed struct {
	Story     string
	Criteria  []Criterion
	DependsOn []Ref
}

// Criterion is one acceptance criterion.
type Criterion struct {
	Title string
	Text  string
}

// Ref names an issue: Repo is "owner/name", or empty for the issue's own repository.
type Ref struct {
	Repo   string
	Number int
}

// Parse reads an issue body written to the feature template, and the shorter forms some issues use.
func Parse(body string) Parsed {
	sections := map[string][]string{}
	var name string
	var outside []string
	for line := range strings.SplitSeq(body, "\n") {
		if m := heading.FindStringSubmatch(line); m != nil {
			name = strings.ToLower(m[1])
			continue
		}
		if name == "" {
			outside = append(outside, line)
		} else {
			sections[name] = append(sections[name], line)
		}
	}
	p := Parsed{Story: strings.TrimSpace(strings.Join(sections["user story"], "\n"))}
	p.Criteria = criteria(sections["acceptance criteria"])
	deps := sections["depends on"]
	// Issues without the section say it on a line of their own, wherever it falls.
	for sec, lines := range sections {
		if sec != "depends on" {
			deps = append(deps, inlineDependencies(lines)...)
		}
	}
	deps = append(deps, inlineDependencies(outside)...)
	p.DependsOn = references(deps)
	return p
}

func inlineDependencies(lines []string) []string {
	var out []string
	for _, line := range lines {
		if m := inlineDeps.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// criteria reads the checklist; the lines indented under an item belong to it.
func criteria(lines []string) []Criterion {
	var (
		out  []Criterion
		text []string
	)
	flush := func() {
		if len(text) > 0 {
			out = append(out, criterion(strings.TrimSpace(strings.Join(text, "\n"))))
			text = nil
		}
	}
	for _, line := range lines {
		if m := checkbox.FindStringSubmatch(line); m != nil {
			flush()
			text = []string{m[2]}
			continue
		}
		if len(text) > 0 && strings.TrimSpace(line) != "" && (line[0] == ' ' || line[0] == '\t') {
			text = append(text, dedent(line))
			continue
		}
		flush()
	}
	flush()
	return out
}

// dedent takes off the indentation that put a line under its item, keeping what nests it further.
func dedent(line string) string {
	const itemIndent = 2
	trimmed := strings.TrimLeft(line, " \t")
	indent := len(line) - len(trimmed)
	return strings.Repeat(" ", max(indent-itemIndent, 0)) + trimmed
}

func criterion(text string) Criterion {
	first, _, _ := strings.Cut(text, "\n")
	title := first
	if m := lead.FindStringSubmatch(first); m != nil {
		title = strings.TrimRight(m[1], ":. ")
	}
	return Criterion{Title: shorten(plain(title)), Text: text}
}

// plain takes the emphasis and code marks off text that becomes a title.
func plain(s string) string {
	return strings.TrimSpace(strings.NewReplacer("**", "", "`", "", "__", "").Replace(s))
}

// shorten cuts a title at a word before maxTitle.
func shorten(s string) string {
	if utf8.RuneCountInString(s) <= maxTitle {
		return s
	}
	const ellipsis = "..."
	cut := string([]rune(s)[:maxTitle-len(ellipsis)])
	if i := strings.LastIndexByte(cut, ' '); i > maxTitle/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:.") + ellipsis
}

func references(lines []string) []Ref {
	var out []Ref
	seen := map[Ref]bool{}
	for _, line := range lines {
		for _, m := range reference.FindAllStringSubmatch(line, -1) {
			n, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			r := Ref{Repo: m[1], Number: n}
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out
}

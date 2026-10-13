package csvio

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Choices are the answers a person gave to the words and people of the file.
type Choices struct {
	// Values map a word of the file to a name the project has, per target.
	Values map[string]map[string]string `json:"values,omitempty"`
	// People choose a member, or nobody, for a name the file uses.
	People map[string]PersonChoice `json:"people,omitempty"`
}

// priorityWords are how a file may spell each priority.
var priorityWords = map[string]requirement.Priority{
	"must": requirement.PriorityMust, "should": requirement.PriorityShould, "could": requirement.PriorityCould,
	"wont": requirement.PriorityWont, "won't": requirement.PriorityWont, "won't have": requirement.PriorityWont,
	"must have": requirement.PriorityMust, "should have": requirement.PriorityShould, "could have": requirement.PriorityCould,
}

// builtInWordTargets are the targets of every project whose values name something it has.
var builtInWordTargets = []string{TargetType, TargetStatus, TargetPriority}

// wordTargets are the targets whose values name something the project has, its options after the rest.
func (w *world) wordTargets(m Mapping) []string {
	var options []string
	for target := range m {
		if d := w.def(target); d != nil && (d.Kind == attribute.Select || d.Kind == attribute.MultiSelect) {
			options = append(options, target)
		}
	}
	slices.Sort(options)
	return append(append([]string{}, builtInWordTargets...), options...)
}

// cellWords are the words one cell of a target holds: one, or several for a multi-select.
func (w *world) cellWords(target, cell string) []string {
	if d := w.def(target); d != nil && d.Kind == attribute.MultiSelect {
		return splitList(cell)
	}
	return []string{cell}
}

// meaning is what a word of a target names here, after the person's answer: its name and id, ok false when
// it names nothing.
func (w *world) meaning(target, word string, c Choices) (string, uuid.UUID, bool) {
	if chosen, ok := c.Values[target][word]; ok {
		word = chosen
	}
	lower := strings.ToLower(strings.TrimSpace(word))
	switch target {
	case TargetType:
		n, ok := w.types[lower]
		return n.Name, n.ID, ok
	case TargetStatus:
		n, ok := w.statuses[lower]
		return n.Name, n.ID, ok
	case TargetPriority:
		p, ok := priorityWords[lower]
		return string(p), uuid.Nil, ok
	}
	if d := w.def(target); d != nil {
		for _, o := range d.Options {
			if !o.Retired && strings.EqualFold(o.Name, lower) {
				return o.Name, o.ID, true
			}
		}
	}
	return "", uuid.Nil, false
}

// words lists the distinct words of the file with what each names, and a problem for each that names nothing.
func (w *world) words(rows []Row, m Mapping, c Choices, headers []string) ([]Word, []Problem) {
	out := []Word{}
	var problems []Problem
	for _, target := range w.wordTargets(m) {
		counts := map[string]int{}
		var order []string
		for _, row := range rows {
			for _, cell := range row.values(m, target) {
				for _, word := range w.cellWords(target, cell) {
					if counts[word] == 0 {
						order = append(order, word)
					}
					counts[word]++
				}
			}
		}
		for _, word := range order {
			means, _, ok := w.meaning(target, word, c)
			out = append(out, Word{Target: target, Value: word, Count: counts[word], Means: means})
			if !ok {
				problems = append(problems, Problem{Column: columnName(m, target, headers), Message: fmt.Sprintf(
					"%s %q matches nothing in %s. Map it to one the project has, or change the file.", targetLabel(target), word, w.project.Key)})
			}
		}
	}
	return out, problems
}

// personTargets are the targets whose values name people.
func (w *world) personTargets(m Mapping) []string {
	targets := []string{TargetOwner}
	for target := range m {
		if d := w.def(target); d != nil && d.Kind == attribute.User {
			targets = append(targets, target)
		}
	}
	return targets
}

// person is who a name of the file is: a member, nobody, or not answered yet.
func (w *world) person(name string, c Choices) (member *named, answered bool) {
	if choice, ok := c.People[name]; ok {
		if choice.Member == nil {
			return nil, true
		}
		for _, n := range w.members {
			if n.ID == *choice.Member {
				return &n, true
			}
		}
		return nil, false
	}
	if n, ok := w.members[strings.ToLower(strings.TrimSpace(name))]; ok {
		return &n, true
	}
	return nil, false
}

// people lists the distinct people the file names, with the member each matched, and a problem for each name
// nobody answered for.
func (w *world) people(rows []Row, m Mapping, c Choices) ([]PersonFound, []Problem) {
	counts := map[string]int{}
	var order []string
	for _, target := range w.personTargets(m) {
		for _, row := range rows {
			if name := row.value(m, target); name != "" {
				if counts[name] == 0 {
					order = append(order, name)
				}
				counts[name]++
			}
		}
	}
	out := []PersonFound{}
	var problems []Problem
	for _, name := range order {
		found := PersonFound{Name: name, Count: counts[name]}
		member, answered := w.person(name, c)
		if member != nil {
			found.Member, found.Email = &member.ID, member.Name
		}
		out = append(out, found)
		if !answered {
			problems = append(problems, Problem{Message: fmt.Sprintf(
				"Nobody who may view %s has the address %s. Choose who it is, or nobody.", w.project.Key, name)})
		}
	}
	return out, problems
}

// columnName is the header of the first column a target is mapped to, for a problem to point at.
func columnName(m Mapping, target string, headers []string) string {
	if at := m[target]; len(at) > 0 && at[0] < len(headers) {
		return headers[at[0]]
	}
	return ""
}

// targetLabel is a target as a sentence names it.
func targetLabel(target string) string {
	if name, ok := strings.CutPrefix(target, AttributePrefix); ok {
		return "The value of " + name
	}
	return "The " + target
}

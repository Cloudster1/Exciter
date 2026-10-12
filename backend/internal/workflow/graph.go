package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// InvalidError is a workflow the designer sent that could not work, said in a sentence on the field it is about.
type InvalidError struct {
	Field, Message string
}

func (e *InvalidError) Error() string { return e.Message }

func invalid(field, format string, args ...any) error {
	return &InvalidError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// ValidateGraph checks a whole workflow as Armature's does: statuses each once, one initial, every transition
// named, inside and not to its own start, names unique per start, and every rule one the registry runs.
func ValidateGraph(in GraphInput, statuses map[uuid.UUID]string) error {
	if strings.TrimSpace(in.Name) == "" {
		return invalid("name", "Give the workflow a name.")
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Name)) > MaxNameLength {
		return invalid("name", "Keep the name to %d characters.", MaxNameLength)
	}
	if len(in.Steps) == 0 {
		return invalid("steps", "A workflow needs at least one status.")
	}
	if len(in.Steps) > MaxStatuses {
		return invalid("steps", "A workflow holds at most %d statuses.", MaxStatuses)
	}
	if len(in.Transitions) > MaxTransitions {
		return invalid("transitions", "A workflow holds at most %d transitions.", MaxTransitions)
	}
	inside := map[uuid.UUID]bool{}
	initial := ""
	for _, s := range in.Steps {
		name, known := statuses[s.StatusID]
		if !known {
			return invalid("steps", "A status named is not one of this project's. Pick them from its statuses.")
		}
		if inside[s.StatusID] {
			return invalid("steps", "%s is in the workflow twice.", name)
		}
		inside[s.StatusID] = true
		if s.IsInitial {
			if initial != "" {
				return invalid("steps", "New requirements start in one status, and both %s and %s are marked.", initial, name)
			}
			initial = name
		}
	}
	if initial == "" {
		return invalid("steps", "Mark the status new requirements start in.")
	}
	for _, s := range in.Steps {
		if s.OnRevisionStatusID != nil && !inside[*s.OnRevisionStatusID] {
			return invalid("steps", "%s sends a new revision to a status this workflow does not contain.", statuses[s.StatusID])
		}
	}
	type edge struct {
		from uuid.UUID
		name string
	}
	named := map[edge]bool{}
	for _, t := range in.Transitions {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			return invalid("transitions", "Give every transition a name.")
		}
		if !inside[t.ToStatusID] {
			return invalid("transitions", "%s leads to a status this workflow does not contain.", name)
		}
		from := uuid.Nil
		if t.FromStatusID != nil {
			if !inside[*t.FromStatusID] {
				return invalid("transitions", "%s leaves a status this workflow does not contain.", name)
			}
			if *t.FromStatusID == t.ToStatusID {
				return invalid("transitions", "%s goes from %s back to itself.", name, statuses[t.ToStatusID])
			}
			from = *t.FromStatusID
		}
		key := edge{from, strings.ToLower(name)}
		if named[key] {
			if from == uuid.Nil {
				return invalid("transitions", "Two transitions offered from every status are called %s.", name)
			}
			return invalid("transitions", "Two transitions leaving %s are called %s.", statuses[from], name)
		}
		named[key] = true
		if len(t.Rules) > MaxRulesPerEdge {
			return invalid("transitions", "%s carries more than %d rules.", name, MaxRulesPerEdge)
		}
		for _, r := range t.Rules {
			known, ok := lookup(r.Kind, r.Type)
			if !ok {
				return invalid("transitions", "%s carries a rule this server does not know: %s.", name, r.Type)
			}
			if err := known.check(json.RawMessage(r.Config)); err != nil {
				return invalid("transitions", "%s: the rule %s %s.", name, known.Label, err.Error())
			}
		}
	}
	return nil
}

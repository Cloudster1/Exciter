package requirement

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// A statement as the editor sends it, and as jsonb answers it: keys by length then name, a space after each
// colon and comma.
const (
	statementAsSent   = `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Pay","marks":[{"type":"bold"}]}]}]}`
	statementAsStored = `{"type": "doc", "content": [{"type": "paragraph", "content": [{"text": "Pay", "type": "text", "marks": [{"type": "bold"}]}]}]}`
)

func priorityOf(p Priority) *Priority { return &p }

// changed decides which field rules a save meets, so it names only what a save really changes (#309).
func TestChangedNamesOnlyWhatASaveChanges(t *testing.T) {
	t.Run("the same statement with its keys in another order is no change", func(t *testing.T) {
		before := fields{statement: json.RawMessage(statementAsStored)}
		after := fields{statement: json.RawMessage(statementAsSent)}
		if got := changed(before, after); len(got) != 0 {
			t.Errorf("an unchanged statement reads as changed in %v", got)
		}
	})

	t.Run("other words are a change", func(t *testing.T) {
		before := fields{statement: json.RawMessage(statementAsStored)}
		after := fields{statement: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Pay later"}]}]}`)}
		if got := changed(before, after); !slices.Equal(got, []string{"statement"}) {
			t.Errorf("new words are %v", got)
		}
	})

	t.Run("new attribute keys are named in a stable order", func(t *testing.T) {
		after := fields{attributes: map[string]json.RawMessage{}}
		for _, key := range []string{"e", "a", "d", "b", "c"} {
			after.attributes[key] = json.RawMessage(`"x"`)
		}
		before := fields{attributes: map[string]json.RawMessage{"c": json.RawMessage(`"y"`)}}
		// A map is walked in a new order every time, so an order that holds over many runs is a stable one.
		const runs = 50
		for range runs {
			if got := changed(before, after); !slices.Equal(got, []string{"a", "b", "c", "d", "e"}) {
				t.Fatalf("the attributes changed are named as %v", got)
			}
		}
	})

	t.Run("a missing rationale against an empty one is no change", func(t *testing.T) {
		if got := changed(fields{rationale: nil}, fields{rationale: json.RawMessage{}}); len(got) != 0 {
			t.Errorf("nil against empty reads as %v", got)
		}
		if got := changed(fields{rationale: nil}, fields{rationale: json.RawMessage(`{"type":"doc"}`)}); !slices.Equal(got, []string{"rationale"}) {
			t.Errorf("a rationale given reads as %v", got)
		}
	})

	t.Run("the owner and the priority are compared by what they name", func(t *testing.T) {
		ada := uuid.New()
		same := ada
		if got := changed(fields{owner: &ada, priority: priorityOf(PriorityMust)}, fields{owner: &same, priority: priorityOf(PriorityMust)}); len(got) != 0 {
			t.Errorf("the same owner and priority read as %v", got)
		}
		if got := changed(fields{owner: &ada, priority: priorityOf(PriorityMust)}, fields{}); !slices.Equal(got, []string{"owner", "priority"}) {
			t.Errorf("a cleared owner and priority read as %v", got)
		}
	})
}

// ownerAfter and priorityAfter read a save's owner and priority, keeping the current one where the save names none
// or names one badly (#309).
func TestOwnerAndPriorityAfterASave(t *testing.T) {
	ada, bob := uuid.New(), uuid.New()
	cur := &loaded{Requirement: Requirement{Owner: &PersonRef{ID: ada, Name: "Ada"}, Priority: priorityOf(PriorityShould)}}

	for _, c := range []struct {
		what  string
		raw   string
		owner *uuid.UUID
	}{
		{"absent", "", &ada},
		{"null", "null", nil},
		{"malformed", `"not a person"`, &ada},
		{"another person", `"` + bob.String() + `"`, &bob},
	} {
		got := ownerAfter(cur, json.RawMessage(c.raw))
		if (got == nil) != (c.owner == nil) || (got != nil && *got != *c.owner) {
			t.Errorf("an owner %s reads as %v, want %v", c.what, got, c.owner)
		}
	}

	for _, c := range []struct {
		what     string
		raw      string
		priority *Priority
	}{
		{"absent", "", priorityOf(PriorityShould)},
		{"null", "null", nil},
		{"malformed", `42`, priorityOf(PriorityShould)},
		{"another", `"must"`, priorityOf(PriorityMust)},
	} {
		if got := priorityAfter(cur, json.RawMessage(c.raw)); priorityText(got) != priorityText(c.priority) {
			t.Errorf("a priority %s reads as %q, want %q", c.what, priorityText(got), priorityText(c.priority))
		}
	}
}

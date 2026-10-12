package workflow

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// sample is the heart of the default workflow: Draft, In review and Approved, a submit, an approve that keeps
// authors from approving their own words, a reject from anywhere that asks why, and an approve that takes ownership.
func sample() (*Workflow, map[string]uuid.UUID) {
	ids := map[string]uuid.UUID{"draft": uuid.New(), "review": uuid.New(), "approved": uuid.New(), "rejected": uuid.New(),
		"submit": uuid.New(), "approve": uuid.New(), "reject": uuid.New()}
	step := func(name string, initial bool) Step {
		return Step{Status: Status{ID: ids[name], Name: name}, IsInitial: initial}
	}
	roles := func(r ...string) json.RawMessage { b, _ := json.Marshal(map[string]any{"roles": r}); return b }
	draft, review := ids["draft"], ids["review"]
	w := &Workflow{
		Steps: []Step{step("draft", true), step("review", false), step("approved", false), step("rejected", false)},
		Transitions: []Transition{
			{ID: ids["submit"], Name: "Submit", FromStatusID: &draft, ToStatusID: review, Position: 0,
				Rules: []Rule{{Kind: KindCondition, Type: "condition.project_role", Config: roles("editor", "admin")}}},
			{ID: ids["approve"], Name: "Approve", FromStatusID: &review, ToStatusID: ids["approved"], Position: 1, Rules: []Rule{
				{Kind: KindCondition, Type: "condition.project_role", Config: roles("reviewer", "admin")},
				{Kind: KindCondition, Type: "condition.not_author"},
				{Kind: KindPostFunction, Type: "postfunction.assign_to_actor"},
			}},
			{ID: ids["reject"], Name: "Reject", ToStatusID: ids["rejected"], Position: 2, Rules: []Rule{
				{Kind: KindCondition, Type: "condition.project_role", Config: roles("reviewer")},
				{Kind: KindValidator, Type: "validator.comment_required"},
			}},
		},
	}
	return w, ids
}

func names(offered []Available) []string {
	out := []string{}
	for _, a := range offered {
		out = append(out, a.Name)
	}
	return out
}

func TestConditionsDecideWhatIsOffered(t *testing.T) {
	w, ids := sample()
	draft, review := ids["draft"], ids["review"]
	editor := Actor{UserID: uuid.New(), Roles: []string{"editor"}}
	reviewer := Actor{UserID: uuid.New(), Roles: []string{"reviewer"}}
	if got := names(Offer(w, Subject{StatusID: &draft}, editor)); len(got) != 1 || got[0] != "Submit" {
		t.Errorf("an editor in Draft is offered %v, want Submit", got)
	}
	if got := names(Offer(w, Subject{StatusID: &review}, reviewer)); len(got) != 2 || got[0] != "Approve" || got[1] != "Reject" {
		t.Errorf("a reviewer in review is offered %v, want Approve and Reject", got)
	}
	author := reviewer.UserID
	if got := names(Offer(w, Subject{StatusID: &review, LatestAuthor: &author}, reviewer)); len(got) != 1 || got[0] != "Reject" {
		t.Errorf("the author of the latest revision is offered %v, want only Reject", got)
	}
	rejected := ids["rejected"]
	if got := names(Offer(w, Subject{StatusID: &rejected}, reviewer)); len(got) != 0 {
		t.Errorf("a transition from anywhere is offered into the status it leads to: %v", got)
	}
}

func TestATakenTransitionRunsItsValidatorsAndPostFunctions(t *testing.T) {
	w, ids := sample()
	draft, review := ids["draft"], ids["review"]
	reviewer := Actor{UserID: uuid.New(), Roles: []string{"reviewer"}}
	if _, _, err := Decide(w, ids["approve"], Subject{StatusID: &draft}, reviewer, Input{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("approving from Draft: %v, want unavailable", err)
	}
	if _, _, err := Decide(w, ids["submit"], Subject{StatusID: &draft}, reviewer, Input{}); !errors.Is(err, ErrNotYours) {
		t.Errorf("a reviewer submits: %v, want not theirs", err)
	}
	var field *FieldError
	if _, _, err := Decide(w, ids["reject"], Subject{StatusID: &review}, reviewer, Input{Comment: "  "}); !errors.As(err, &field) || field.Field != "comment" {
		t.Errorf("a reject without a comment: %v, want refused on comment", err)
	}
	_, effect, err := Decide(w, ids["approve"], Subject{StatusID: &review}, reviewer, Input{})
	if err != nil || !effect.SetOwner || effect.Owner == nil || *effect.Owner != reviewer.UserID {
		t.Errorf("the approver becomes the owner: %+v %v", effect, err)
	}
	if _, _, err := Decide(w, uuid.New(), Subject{StatusID: &review}, reviewer, Input{}); !errors.Is(err, ErrNoTransition) {
		t.Errorf("a transition of no workflow: %v", err)
	}
}

func TestAGraphThatCouldNotWorkIsRefused(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	statuses := map[uuid.UUID]string{a: "Draft", b: "Done"}
	ok := GraphInput{Name: "Flow", Steps: []StepInput{{StatusID: a, IsInitial: true}, {StatusID: b}},
		Transitions: []TransitionInput{{Name: "Finish", FromStatusID: &a, ToStatusID: b}}}
	if err := ValidateGraph(ok, statuses); err != nil {
		t.Fatalf("a sound graph was refused: %v", err)
	}
	other := uuid.New()
	for what, change := range map[string]func(*GraphInput){
		"no name":           func(g *GraphInput) { g.Name = " " },
		"no initial":        func(g *GraphInput) { g.Steps[0].IsInitial = false },
		"two initials":      func(g *GraphInput) { g.Steps[1].IsInitial = true },
		"a status twice":    func(g *GraphInput) { g.Steps = append(g.Steps, StepInput{StatusID: a}) },
		"a stranger status": func(g *GraphInput) { g.Steps = append(g.Steps, StepInput{StatusID: other}) },
		"a loop":            func(g *GraphInput) { g.Transitions[0].ToStatusID = a },
		"a nameless edge":   func(g *GraphInput) { g.Transitions[0].Name = "" },
		"an edge outside":   func(g *GraphInput) { g.Transitions[0].ToStatusID = other },
		"two names from one": func(g *GraphInput) {
			g.Transitions = append(g.Transitions, TransitionInput{Name: "finish", FromStatusID: &a, ToStatusID: b})
		},
		"an unknown rule": func(g *GraphInput) {
			g.Transitions[0].Rules = []RuleInput{{Kind: KindCondition, Type: "condition.moon"}}
		},
		"a role that is none": func(g *GraphInput) {
			g.Transitions[0].Rules = []RuleInput{{Kind: KindCondition, Type: "condition.project_role", Config: json.RawMessage(`{"roles":["boss"]}`)}}
		},
		"a revision outside": func(g *GraphInput) { g.Steps[1].OnRevisionStatusID = &other },
	} {
		g := ok
		g.Steps = append([]StepInput(nil), ok.Steps...)
		g.Transitions = append([]TransitionInput(nil), ok.Transitions...)
		change(&g)
		var invalid *InvalidError
		if err := ValidateGraph(g, statuses); !errors.As(err, &invalid) {
			t.Errorf("%s was not refused: %v", what, err)
		}
	}
}

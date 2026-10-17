package workflow

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The trace conditions (#206) keep a transition offered, then refuse it with a sentence naming the requirement and
// what is missing, until the requirement has it.
func TestTraceConditionsRefuseWhatIsMissingAndNameIt(t *testing.T) {
	verifies := uuid.New()
	links, _ := json.Marshal(map[string]any{"linkTypeId": verifies, "direction": "incoming"})
	dimension, _ := json.Marshal(map[string]any{"dimension": "verified"})
	draft, approved, release := uuid.New(), uuid.New(), uuid.New()
	w := &Workflow{
		Steps: []Step{{Status: Status{ID: draft, Name: "Draft"}, IsInitial: true}, {Status: Status{ID: approved, Name: "Approved"}}},
		Transitions: []Transition{{ID: release, Name: "Release", FromStatusID: &draft, ToStatusID: approved, Rules: []Rule{
			{Kind: KindValidator, Type: "validator.links_exist", Config: links},
			{Kind: KindValidator, Type: "validator.no_suspect"},
			{Kind: KindValidator, Type: "validator.coverage_full", Config: dimension},
			{Kind: KindValidator, Type: "validator.threads_resolved"},
		}}},
	}
	who := Actor{UserID: uuid.New(), Roles: []string{"editor"}}
	if offered := Offer(w, Subject{StatusID: &draft}, who); len(offered) != 1 {
		t.Fatalf("a transition with trace conditions is offered as %v", names(offered))
	}
	s := Subject{StatusID: &draft, Key: "CHK-R4", Links: []LinkCount{{TypeID: verifies, TypeName: "verifies", Outgoing: 1}},
		Suspects: 2, OpenThreads: 1, Coverage: map[string]string{"verified": "partial"}}
	steps := []struct {
		want string
		fix  func()
	}{
		{"CHK-R4 has no verifies link to it yet.", func() { s.Links[0].Incoming = 1 }},
		{"CHK-R4 has 2 suspect links.", func() { s.Suspects = 0 }},
		{"CHK-R4 is not fully verified yet.", func() { s.Coverage["verified"] = "full" }},
		{"CHK-R4 has 1 open comment thread.", func() { s.OpenThreads = 0 }},
	}
	for _, step := range steps {
		var field *FieldError
		_, _, err := Decide(w, release, s, who, Input{})
		if !errors.As(err, &field) || field.Field != "transition" || !strings.HasPrefix(field.Message, step.want) {
			t.Fatalf("Decide = %v, want a refusal saying %q", err, step.want)
		}
		step.fix()
	}
	if _, _, err := Decide(w, release, s, who, Input{}); err != nil {
		t.Errorf("with every condition met: %v", err)
	}
	s.Coverage["verified"] = "na"
	if _, _, err := Decide(w, release, s, who, Input{}); err != nil {
		t.Errorf("a dimension its type does not ask for refused the transition: %v", err)
	}
}

func TestTraceConditionsCheckWhatTheyAreGiven(t *testing.T) {
	for _, c := range []struct {
		typ, config string
		ok          bool
	}{
		{"validator.links_exist", `{}`, false},
		{"validator.links_exist", `{"linkTypeId": "` + uuid.NewString() + `", "direction": "sideways"}`, false},
		{"validator.links_exist", `{"linkTypeId": "` + uuid.NewString() + `"}`, true},
		{"validator.coverage_full", `{"dimension": "tested"}`, false},
		{"validator.coverage_full", `{"dimension": "decomposed"}`, true},
	} {
		r, _ := lookup(KindValidator, c.typ)
		if err := r.check(json.RawMessage(c.config)); (err == nil) != c.ok {
			t.Errorf("%s %s: %v, want ok %v", c.typ, c.config, err, c.ok)
		}
	}
}

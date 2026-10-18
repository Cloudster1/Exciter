package requirement

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/markdown"
)

func stepDoc(text string) json.RawMessage {
	return json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + text + `"}]}]}`)
}

// A requirement that is no test case hashes as it did before test cases, so no hash and no signature changes.
func TestARequirementWithoutStepsKeepsItsHash(t *testing.T) {
	n := Normative{Title: "Stop  within 40 m", Statement: stepDoc("The vehicle stops."), Attributes: map[string]json.RawMessage{"a": json.RawMessage(`"x"`)}}
	before, _ := json.Marshal(map[string]any{
		"title": collapse(n.Title), "statement": canonical(n.Statement), "rationale": canonical(n.Rationale),
		"typeId": nil, "attributes": map[string]any{"a": "x"},
	})
	sum := sha256.Sum256(before)
	if n.Hash() != hex.EncodeToString(sum[:]) {
		t.Error("the hash of a requirement without steps changed")
	}
	n.Test = TestCase{Steps: []Step{{Action: stepDoc("Press the pedal.")}}}
	if n.Hash() == hex.EncodeToString(sum[:]) {
		t.Error("a step is not part of the hash")
	}
	spaced := n
	spaced.Test = TestCase{Steps: []Step{{Action: stepDoc("Press  the pedal. ")}}}
	if spaced.Hash() != n.Hash() {
		t.Error("white space in a step changes the hash")
	}
}

func TestATestCaseIsCheckedAsItIsWritten(t *testing.T) {
	action := "Press the **pedal**."
	got, err := checkTestCase(TestCase{
		Steps:      []Step{{ActionMarkdown: &action}, {Action: stepDoc("Release it."), Expected: stepDoc("")}},
		Parameters: []Parameter{{Name: " Speed ", Values: []string{"30", " 50 "}}, {Name: "Road", Values: []string{"dry", "wet"}}},
	}, markdown.Resolve{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps) != 2 || !strings.Contains(string(got.Steps[0].Action), `"bold"`) || got.Steps[1].Expected != nil {
		t.Errorf("the steps read %+v", got.Steps)
	}
	if got.Parameters[0].Name != "Speed" || got.Parameters[0].Values[1] != "50" || got.Iterations() != 2 || got.Values(2)["Road"] != "wet" {
		t.Errorf("the parameters read %+v", got.Parameters)
	}
	for name, tc := range map[string]struct {
		in    TestCase
		field string
	}{
		"a step without an action": {TestCase{Steps: []Step{{Action: stepDoc("")}}}, "steps[0].action"},
		"uneven parameters": {TestCase{Parameters: []Parameter{{Name: "a", Values: []string{"1", "2"}}, {Name: "b", Values: []string{"1"}}}},
			"parameters[1].values"},
		"a name twice":      {TestCase{Parameters: []Parameter{{Name: "a", Values: []string{"1"}}, {Name: "A", Values: []string{"2"}}}}, "parameters[1].name"},
		"a parameter empty": {TestCase{Parameters: []Parameter{{Name: "a"}}}, "parameters[0].values"},
		"too many steps":    {TestCase{Steps: make([]Step, MaxSteps+1)}, "steps"},
	} {
		_, err := checkTestCase(tc.in, markdown.Resolve{})
		var field *FieldError
		if !errors.As(err, &field) || field.Field != tc.field || !strings.HasSuffix(field.Message, ".") {
			t.Errorf("%s: %v, want a sentence on %s", name, err, tc.field)
		}
	}
}

func TestStepsAreComparedOneByOne(t *testing.T) {
	a, b, c, d := Step{Action: stepDoc("A")}, Step{Action: stepDoc("B")}, Step{Action: stepDoc("C")}, Step{Action: stepDoc("D")}
	changed := Step{Action: stepDoc("B2")}
	diffs, err := stepDiff([]Step{a, b, c, d}, []Step{c, a, changed, Step{Action: stepDoc("E")}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range diffs {
		from, to := "-", "-"
		if x.From != nil {
			from = string(rune('0' + *x.From))
		}
		if x.To != nil {
			to = string(rune('0' + *x.To))
		}
		got = append(got, string(x.Change)+":"+from+">"+to)
	}
	if strings.Join(got, " ") != "equal:3>1 moved:1>2 modified:2>3 modified:4>4" {
		t.Errorf("the steps compare as %v", got)
	}
	diffs, _ = stepDiff([]Step{a, b}, []Step{a})
	if len(diffs) != 2 || diffs[1].Change != DiffDeleted || *diffs[1].From != 2 {
		t.Errorf("a step removed: %+v", diffs)
	}
	params := parameterDiff([]Parameter{{Name: "x", Values: []string{"1"}}, {Name: "gone", Values: []string{"2"}}}, []Parameter{{Name: "x", Values: []string{"9"}}, {Name: "new", Values: []string{"3"}}})
	if len(params) != 3 || params[0].Change != DiffModified || params[1].Change != DiffInserted || params[2].Change != DiffDeleted {
		t.Errorf("the parameters compare as %+v", params)
	}
}

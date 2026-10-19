package requirement

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func value(n int) *int { return &n }

// A requirement that is no risk hashes as it did before risks, so no hash and no signature changes.
func TestARequirementWithoutARatingKeepsItsHash(t *testing.T) {
	n := Normative{Title: "Stop  within 40 m", Statement: stepDoc("The vehicle stops."), Attributes: map[string]json.RawMessage{"a": json.RawMessage(`"x"`)}}
	before, _ := json.Marshal(map[string]any{
		"title": collapse(n.Title), "statement": canonical(n.Statement), "rationale": canonical(n.Rationale),
		"typeId": nil, "attributes": map[string]any{"a": "x"},
	})
	sum := sha256.Sum256(before)
	if n.Hash() != hex.EncodeToString(sum[:]) {
		t.Fatal("the hash of a requirement without a rating changed")
	}
	n.Risk = Rating{After: Side{}}
	if n.Hash() != hex.EncodeToString(sum[:]) {
		t.Error("a rating without a value changes the hash")
	}
	n.Risk = Rating{Before: Side{Severity: value(4), Probability: value(2)}}
	rated := n.Hash()
	if rated == hex.EncodeToString(sum[:]) {
		t.Error("a rating is not part of the hash")
	}
	n.Risk.Before.Detection = nil
	if n.Hash() != rated {
		t.Error("a value left unknown changes the hash")
	}
	n.Risk.After = Side{Severity: value(4), Probability: value(1)}
	if n.Hash() == rated {
		t.Error("the rating after mitigation is not part of the hash")
	}
}

func TestARatingIsCheckedAsItIsWritten(t *testing.T) {
	var field *FieldError
	if err := checkRating(Rating{Before: Side{Severity: value(11)}}); !errors.As(err, &field) || field.Field != "risk.before.severity" {
		t.Errorf("a severity of 11 is %v", err)
	}
	if err := checkRating(Rating{After: Side{Detection: value(0)}}); !errors.As(err, &field) || field.Field != "risk.after.detection" {
		t.Errorf("a detection of 0 is %v", err)
	}
	if err := checkRating(Rating{Before: Side{Severity: value(1), Probability: value(10)}}); err != nil {
		t.Errorf("values from 1 to 10 are refused: %v", err)
	}
	if !(Rating{}).Empty() || (Rating{After: Side{Probability: value(1)}}).Empty() {
		t.Error("a rating is empty while it gives no value, and only then")
	}
	if col, err := (Rating{}).column(); err != nil || col != nil {
		t.Errorf("an empty rating is kept as %v, want null", col)
	}
}

func TestARatingIsComparedValueByValue(t *testing.T) {
	a := Rating{Before: Side{Severity: value(4), Probability: value(2)}}
	b := Rating{Before: Side{Severity: value(4), Probability: value(3)}, After: Side{Severity: value(2)}}
	got := riskDiff(a, b)
	if len(got) != 2 || got[0].Side != "before" || got[0].Dimension != "probability" || *got[0].From != 2 || *got[0].To != 3 ||
		got[1].Side != "after" || got[1].Dimension != "severity" || got[1].From != nil || *got[1].To != 2 {
		t.Errorf("the diff is %+v", got)
	}
	if len(riskDiff(a, a)) != 0 {
		t.Error("a rating differs from itself")
	}
}

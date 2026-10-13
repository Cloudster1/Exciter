package trace

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func ptr[T any](v T) *T { return &v }

func TestKeyFromName(t *testing.T) {
	for name, want := range map[string]string{
		"Derives":               "derives",
		"Is tested by":          "is_tested_by",
		"  Hazard / Control":    "hazard_control",
		"2nd level":             "nd_level",
		"Übergeordnet":          "bergeordnet",
		"!!":                    "",
		strings.Repeat("a", 50): strings.Repeat("a", MaxKeyLength),
	} {
		if got := KeyFromName(name); got != want {
			t.Errorf("KeyFromName(%q) = %q, want %q", name, got, want)
		}
	}
}

func fieldOf(err error) string {
	var field *FieldError
	if errors.As(err, &field) {
		return field.Field
	}
	return ""
}

func TestApplyTypeMakes(t *testing.T) {
	var lt LinkType
	in := LinkTypeInput{Name: ptr(" Mitigates "), Forward: ptr("mitigates"), Backward: ptr("is mitigated by")}
	if err := applyType(&lt, in, true); err != nil {
		t.Fatal(err)
	}
	if lt.Key != "mitigates" || lt.Name != "Mitigates" || lt.SuspectEnd != SuspectNone || lt.Counts != CountsNothing || lt.Acyclic {
		t.Errorf("made %+v", lt)
	}

	refusals := map[string]LinkTypeInput{
		"name":       {Forward: ptr("a"), Backward: ptr("b")},
		"forward":    {Name: ptr("A"), Backward: ptr("b")},
		"backward":   {Name: ptr("A"), Forward: ptr("a")},
		"key":        {Name: ptr("A"), Forward: ptr("a"), Backward: ptr("b"), Key: "Bad-Key"},
		"suspectEnd": {Name: ptr("Ab"), Forward: ptr("a"), Backward: ptr("b"), SuspectEnd: ptr(SuspectEnd("middle"))},
		"counts":     {Name: ptr("Ab"), Forward: ptr("a"), Backward: ptr("b"), Counts: ptr(Counts("all"))},
	}
	for field, in := range refusals {
		var lt LinkType
		if got := fieldOf(applyType(&lt, in, true)); got != field {
			t.Errorf("refusing %s: got the field %q", field, got)
		}
	}
	long := LinkTypeInput{Name: ptr(strings.Repeat("n", MaxNameLength+1)), Forward: ptr("a"), Backward: ptr("b")}
	if got := fieldOf(applyType(&LinkType{}, long, true)); got != "name" {
		t.Errorf("a long name: got the field %q", got)
	}
	short := LinkTypeInput{Name: ptr("X"), Forward: ptr("a"), Backward: ptr("b")}
	if got := fieldOf(applyType(&LinkType{}, short, true)); got != "key" {
		t.Errorf("a name too short for a key: got the field %q", got)
	}
}

func TestApplyTypeChanges(t *testing.T) {
	lt := LinkType{Key: "derives", Name: "Derives", Forward: "derives from", Backward: "is derived into", SuspectEnd: SuspectTarget, Counts: CountsDecomposition}
	if err := applyType(&lt, LinkTypeInput{Key: "derives", Retired: ptr(true), Acyclic: ptr(true)}, false); err != nil {
		t.Fatal(err)
	}
	if !lt.Retired || !lt.Acyclic || lt.Name != "Derives" {
		t.Errorf("changed %+v", lt)
	}
	if got := fieldOf(applyType(&lt, LinkTypeInput{Key: "derived"}, false)); got != "key" {
		t.Errorf("a new key: got the field %q", got)
	}
	if got := fieldOf(applyType(&lt, LinkTypeInput{Name: ptr("  ")}, false)); got != "name" {
		t.Errorf("an empty name: got the field %q", got)
	}
}

func TestSymmetricAndPhrase(t *testing.T) {
	derives := LinkType{Forward: "derives from", Backward: "is derived into"}
	relates := LinkType{Forward: "relates to", Backward: "relates to"}
	if derives.Symmetric() || !relates.Symmetric() {
		t.Error("only a type that reads the same both ways is symmetric")
	}
	if derives.phrase(Outgoing) != "derives from" || derives.phrase(Incoming) != "is derived into" {
		t.Error("a link reads forward from its source and backward from its target")
	}
}

func TestAllows(t *testing.T) {
	need, spec, test := uuid.New(), uuid.New(), uuid.New()
	rules := []TypeRule{{SourceTypeID: &spec, TargetTypeID: &need}, {SourceTypeID: &test, TargetTypeID: nil}}
	cases := []struct {
		name           string
		rules          []TypeRule
		source, target *uuid.UUID
		same, want     bool
	}{
		{"no rule is any pair", nil, &need, &test, true, true},
		{"a pair a rule names", rules, &spec, &need, true, true},
		{"the pair the other way round", rules, &need, &spec, true, false},
		{"a null target is any type", rules, &test, &spec, true, true},
		{"an untyped source", rules, nil, &need, true, false},
		{"another project's target is not judged", rules, &spec, &test, false, true},
		{"another project's target still needs an allowed source", rules, &need, &test, false, false},
	}
	for _, c := range cases {
		if got := Allows(c.rules, c.source, c.target, c.same); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestCheckRules(t *testing.T) {
	a, b, stranger := uuid.New(), uuid.New(), uuid.New()
	types := map[uuid.UUID]string{a: "Need", b: "Spec"}
	got, err := checkRules([]TypeRule{{SourceTypeID: &a, TargetTypeID: &b}, {SourceTypeID: &a, TargetTypeID: &b}, {SourceTypeID: nil, TargetTypeID: &b}}, types)
	if err != nil || len(got) != 2 {
		t.Errorf("a repeated rule is dropped: %v %v", got, err)
	}
	if _, err := checkRules([]TypeRule{{SourceTypeID: &stranger}}, types); fieldOf(err) != "rules" {
		t.Errorf("another project's type: %v", err)
	}
	many := make([]TypeRule, 0, MaxRules+1)
	for range MaxRules + 1 {
		id := uuid.New()
		types[id] = "x"
		many = append(many, TypeRule{SourceTypeID: &id})
	}
	if _, err := checkRules(many, types); fieldOf(err) != "rules" {
		t.Errorf("too many rules: %v", err)
	}
}

func TestCycleSentence(t *testing.T) {
	derives := LinkType{Forward: "derives from"}
	got := cycleSentence(derives, "CHK-R7", "SYS-R2", []string{"CHK-R3"})
	if !strings.HasPrefix(got, "CHK-R7 already derives from SYS-R2 through CHK-R3, ") {
		t.Errorf("got %q", got)
	}
	if got := cycleSentence(derives, "CHK-R7", "SYS-R2", nil); !strings.HasPrefix(got, "CHK-R7 already derives from SYS-R2, ") {
		t.Errorf("got %q", got)
	}
}

func TestCheckLinkInput(t *testing.T) {
	in := LinkInput{TypeKey: "derives", OtherKey: " chk-r2 "}
	if err := checkLinkInput(&in); err != nil || in.OtherKey != "CHK-R2" || in.Direction != Outgoing {
		t.Errorf("got %+v, %v", in, err)
	}
	for field, in := range map[string]LinkInput{
		"otherKey":  {TypeKey: "derives"},
		"direction": {TypeKey: "derives", OtherKey: "CHK-R2", Direction: "sideways"},
		"typeId":    {OtherKey: "CHK-R2"},
	} {
		if got := fieldOf(checkLinkInput(&in)); got != field {
			t.Errorf("refusing %s: got the field %q", field, got)
		}
	}
}

package attribute

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute/formula"
)

// The worker's pure helpers (#319): the order formulas are worked out in, whether a value moved, what a computed
// attribute refuses, how kinds cross into the formula language, and which formulas read themselves round.

var (
	verifies = formula.LinkType{ID: uuid.MustParse("0195f000-0000-7000-8000-0000000000b1"), Key: "verifies", Acyclic: true}
	relates  = formula.LinkType{ID: uuid.MustParse("0195f000-0000-7000-8000-0000000000b2"), Key: "relates"}
)

func typed(name string, kind Kind) Def { return Def{ID: uuid.New(), Name: name, Kind: kind} }

func computedDef(name string) Def {
	text := ""
	return Def{ID: uuid.New(), Name: name, Kind: Number, Formula: &text}
}

// compiled checks each formula as the project would, every other attribute in the catalogue and the one defined as
// itself, failing the test on a formula that does not hold.
func compiled(t *testing.T, defs []Def, links []formula.LinkType, formulas map[uuid.UUID]string) map[uuid.UUID]*formula.Program {
	t.Helper()
	out := map[uuid.UUID]*formula.Program{}
	for id, text := range formulas {
		p, err := formula.Compile(text, catalogOf(defs, links, id))
		if err != nil {
			t.Fatalf("%s: %s", text, err.Message)
		}
		out[id] = p
	}
	return out
}

func names(defs []Def) string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return strings.Join(out, ",")
}

func TestSelfOrderWorksOutWhatAFormulaReadsFirst(t *testing.T) {
	weight := typed("Weight", Number)
	total, score, rank, broken := computedDef("Total"), computedDef("Score"), computedDef("Rank"), computedDef("Broken")
	defs := []Def{rank, weight, total, score, broken}
	programs := compiled(t, defs, nil, map[uuid.UUID]string{
		rank.ID:  `"Score" + "Total"`,
		score.ID: `"Total" * 2`,
		total.ID: `"Weight" + 1`,
	})
	if got := names(selfOrder(defs, programs)); got != "Total,Score,Rank" {
		t.Errorf("the formulas are worked out as %s, want Total,Score,Rank", got)
	}

	sums := computedDef("Sum")
	programs = compiled(t, []Def{sums, weight}, nil, map[uuid.UUID]string{sums.ID: `sum(children, "Sum") + "Weight"`})
	if got := names(selfOrder([]Def{sums, weight}, programs)); got != "Sum" {
		t.Errorf("a formula reading itself over the children is worked out as %s", got)
	}
	if got := selfOrder([]Def{weight}, map[uuid.UUID]*formula.Program{}); len(got) != 0 {
		t.Errorf("a project without formulas works out %v", got)
	}
}

func TestSameValuesComparesWhatTheValuesSay(t *testing.T) {
	raw := func(pairs ...string) map[string]json.RawMessage {
		out := map[string]json.RawMessage{}
		for i := 0; i < len(pairs); i += 2 {
			out[pairs[i]] = json.RawMessage(pairs[i+1])
		}
		return out
	}
	for _, c := range []struct {
		name string
		a, b map[string]json.RawMessage
		same bool
	}{
		{"both empty", raw(), nil, true},
		{"a number spelled two ways", raw("a", `1`), raw("a", `1.0`), true},
		{"an object with its keys in another order", raw("a", `{"x":1,"y":"z"}`), raw("a", `{"y":"z","x":1}`), true},
		{"spaces in the JSON", raw("a", `[1, 2]`), raw("a", `[1,2]`), true},
		{"a value moved", raw("a", `1`), raw("a", `2`), false},
		{"a value gone", raw("a", `1`, "b", `true`), raw("a", `1`), false},
		{"as many values under other keys", raw("a", `1`), raw("b", `1`), false},
		{"null against nothing", raw("a", `null`), raw("b", `null`), false},
		{"text against a number", raw("a", `"1"`), raw("a", `1`), false},
		{"a value that does not read", raw("a", `{`), raw("a", `{`), false},
	} {
		if got := sameValues(c.a, c.b); got != c.same {
			t.Errorf("%s: sameValues = %v, want %v", c.name, got, c.same)
		}
	}
}

func TestCheckComputedRefusesWhatOnlyATypedAttributeHas(t *testing.T) {
	yes, no := true, false
	someone := uuid.New()
	for _, c := range []struct {
		name  string
		in    DefInput
		field string
	}{
		{"nothing typed", DefInput{}, ""},
		{"not required", DefInput{Required: &no}, ""},
		{"required", DefInput{Required: &yes}, "required"},
		{"required from a status", DefInput{RequiredFrom: json.RawMessage(`"` + someone.String() + `"`)}, "requiredFrom"},
		{"required from no status", DefInput{RequiredFrom: json.RawMessage(`null`)}, ""},
		{"choices narrowed", DefInput{DependsOn: json.RawMessage(`{"attributeId":"` + someone.String() + `"}`)}, "dependsOn"},
		{"choices narrowed by nothing", DefInput{DependsOn: json.RawMessage(`null`)}, ""},
		{"options", DefInput{Options: &[]OptionInput{{Name: "Low"}}}, "options"},
		{"no options", DefInput{Options: &[]OptionInput{}}, ""},
		{"edited by somebody", DefInput{EditableBy: &[]Permit{{}}}, "editableBy"},
		{"edited by nobody named", DefInput{EditableBy: &[]Permit{}}, ""},
	} {
		err := checkComputed(c.in)
		var field *FieldError
		switch {
		case c.field == "" && err != nil:
			t.Errorf("%s: refused with %v", c.name, err)
		case c.field != "" && (!errors.As(err, &field) || field.Field != c.field):
			t.Errorf("%s: %v, want a refusal on %s", c.name, err, c.field)
		case c.field != "" && !strings.HasSuffix(field.Message, "."):
			t.Errorf("%s: the refusal %q is not a sentence", c.name, field.Message)
		}
	}
}

func TestKindsCrossIntoTheFormulaLanguageAndBack(t *testing.T) {
	for _, k := range ComputedKinds {
		if back := kindOf(formulaKind(k)); back != k {
			t.Errorf("%s reads as %s and keeps as %s", k, formulaKind(k), back)
		}
	}
	for _, k := range formula.Kinds {
		if back := formulaKind(kindOf(k)); back != k {
			t.Errorf("a formula giving %s keeps it as %s, which reads as %s", k, kindOf(k), back)
		}
	}
	for _, k := range []Kind{RichText, Select, MultiSelect, User, URL} {
		if got := formulaKind(k); got != formula.Text {
			t.Errorf("%s reads as %s, want its words", k, got)
		}
	}
	if got := kindOf("unknown"); got != Text {
		t.Errorf("a kind the language does not name keeps as %s, want text", got)
	}
}

func TestCyclesOfRefusesFormulasThatReadThemselvesRound(t *testing.T) {
	links := []formula.LinkType{verifies, relates}
	weight := typed("Weight", Number)
	refusal := func(err error) string {
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "formula" {
			return ""
		}
		return field.Message
	}

	score, rank := computedDef("Score"), computedDef("Rank")
	defs := []Def{weight, score, rank}
	round := compiled(t, defs, links, map[uuid.UUID]string{score.ID: `"Rank" + "Weight"`, rank.ID: `"Score" * 2`})
	if got := refusal(cyclesOf(defs, links, round)); !strings.HasPrefix(got, "Score reads Rank, which reads Score.") && !strings.HasPrefix(got, "Rank reads Score, which reads Rank.") {
		t.Errorf("two formulas reading each other: %q", got)
	}

	straight := compiled(t, defs, links, map[uuid.UUID]string{score.ID: `"Weight" * 2`, rank.ID: `"Score" + 1`})
	if err := cyclesOf(defs, links, straight); err != nil {
		t.Errorf("one formula reading another: %v", err)
	}
	delete(round, rank.ID)
	if err := cyclesOf(defs, links, round); err != nil {
		t.Errorf("a formula that did not compile is no part of a cycle: %v", err)
	}

	chain := computedDef("Chain")
	defs = []Def{chain}
	along := func(key string) map[uuid.UUID]*formula.Program {
		return compiled(t, defs, links, map[uuid.UUID]string{chain.ID: `sum(links('` + key + `', out), "Chain")`})
	}
	if err := cyclesOf(defs, links, along("verifies")); err != nil {
		t.Errorf("reading itself along an acyclic link type: %v", err)
	}
	if got := refusal(cyclesOf(defs, links, along("relates"))); !strings.HasPrefix(got, "Chain reads Chain.") {
		t.Errorf("reading itself along a link type that may cycle: %q", got)
	}
}

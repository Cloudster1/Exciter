package formula

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	weightID   = uuid.MustParse("0195f000-0000-7000-8000-000000000001")
	costID     = uuid.MustParse("0195f000-0000-7000-8000-000000000002")
	dueID      = uuid.MustParse("0195f000-0000-7000-8000-000000000003")
	scoreID    = uuid.MustParse("0195f000-0000-7000-8000-000000000004")
	verifiesID = uuid.MustParse("0195f000-0000-7000-8000-0000000000a1")
	relatesID  = uuid.MustParse("0195f000-0000-7000-8000-0000000000a2")
)

var catalog = Catalog{
	Attributes: []Attribute{
		{ID: weightID, Name: "Weight", Kind: Number},
		{ID: costID, Name: "Cost", Kind: Number},
		{ID: dueID, Name: "Due", Kind: Date},
		{ID: scoreID, Name: "Score", Kind: Number},
	},
	LinkTypes: []LinkType{{ID: verifiesID, Key: "verifies", Acyclic: true}, {ID: relatesID, Key: "relates"}},
	Self:      scoreID,
}

// item is a requirement as the tests give it: its fields by name and attributes by id.
type item struct {
	fields map[string]Value
	attrs  map[uuid.UUID]Value
}

func (i item) Field(name string) Value {
	if v, ok := i.fields[name]; ok {
		return v
	}
	return None
}

func (i item) Attribute(id uuid.UUID) Value {
	if v, ok := i.attrs[id]; ok {
		return v
	}
	return None
}

type scope struct {
	item
	children []Item
	linked   map[uuid.UUID][]Item
}

func (s scope) Children() []Item { return s.children }
func (s scope) Linked(typeID uuid.UUID, out bool) []Item {
	if !out {
		return nil
	}
	return s.linked[typeID]
}
func (scope) Today() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }

func child(category string, cost float64) Item {
	return item{fields: map[string]Value{"category": TextValue(category)}, attrs: map[uuid.UUID]Value{costID: NumberValue(cost)}}
}

func world() scope {
	return scope{
		item: item{
			fields: map[string]Value{"priority": TextValue("must"), "title": TextValue("Brakes"), "revision": NumberValue(3)},
			attrs:  map[uuid.UUID]Value{weightID: NumberValue(4), dueID: DayValue(time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC))},
		},
		children: []Item{child("approved", 10), child("draft", 5), child("approved", 2.5)},
		linked: map[uuid.UUID][]Item{verifiesID: {
			item{fields: map[string]Value{"testResult": TextValue("passed")}},
			item{fields: map[string]Value{"testResult": TextValue("failed")}},
		}},
	}
}

func eval(t *testing.T, text string) Value {
	t.Helper()
	p, err := Compile(text, catalog)
	if err != nil {
		t.Fatalf("%s: %s at %d-%d", text, err.Message, err.From, err.To)
	}
	return p.Eval(world())
}

func TestFormulasWorkOutArithmeticConditionsTextAndDates(t *testing.T) {
	for text, want := range map[string]string{
		`"Weight" * 2 + if(priority = 'must', 10, 0)`: "18",
		`round(10 / 3, 2)`:                            "3.33",
		`"Cost" + 1`:                                  "",
		`coalesce("Cost", 7)`:                         "7",
		`1 / 0`:                                       "",
		`concat(title, ' r', revision)`:               `"Brakes r3"`,
		`upper(title) = 'BRAKES' and not (revision < 2)`:   "true",
		`days(today(), "Due")`:                             "10",
		`"Due" > today()`:                                  "true",
		`max(1, "Weight", 2) - min(3, -1)`:                 "5",
		`len('Bremse') + abs(-1) + floor(2.7) + ceil(0.2)`: "10",
		`"Cost" = null`:                                    "true",
		`if("Cost" > 1, 'big', 'small')`:                   `"small"`,
		`PRIORITY = 'must'`:                                "",
	} {
		if text == `PRIORITY = 'must'` {
			if _, err := Compile(text, catalog); err == nil {
				t.Errorf("%s was taken; fields are spelled as they are", text)
			}
			continue
		}
		if got := string(eval(t, text).JSON()); got != want {
			t.Errorf("%s = %s, want %s", text, got, want)
		}
	}
}

func TestAggregatesReadTheChildrenAndTheLinkedRequirements(t *testing.T) {
	for text, want := range map[string]string{
		`count(children)`: "3",
		`count(children where category = 'approved') / count(children)`: "0.6666666666666666",
		`sum(children, "Cost")`:                                                      "17.5",
		`sum(children, "Cost" where category = 'approved')`:                          "12.5",
		`avg(children, "Cost")`:                                                      "5.833333333333333",
		`min(children, "Cost")`:                                                      "2.5",
		`max(children, "Cost")`:                                                      "10",
		`all(links('verifies', out), testResult = 'passed')`:                         "false",
		`any(links('verifies', out), testResult = 'passed')`:                         "true",
		`count(links('verifies', in))`:                                               "0",
		`all(links('verifies', in), testResult = 'passed')`:                          "true",
		`avg(links('verifies', in), revision)`:                                       "",
		`if(all(links('verifies', out), testResult = 'passed'), 'verified', 'open')`: `"open"`,
	} {
		if got := string(eval(t, text).JSON()); got != want {
			t.Errorf("%s = %s, want %s", text, got, want)
		}
	}
}

func TestAFormulaKnowsWhatItReadsAndIsKeptByID(t *testing.T) {
	p, err := Compile(`"weight" + sum(children, "Cost") + count(links('verifies', out) where "Due" > today())`, catalog)
	if err != nil {
		t.Fatal(err.Message)
	}
	if p.Kind != Number || len(p.Reads.Self) != 1 || p.Reads.Self[0] != weightID || !p.Reads.ReadsChildren ||
		len(p.Reads.Children) != 1 || len(p.Reads.Links) != 1 || p.Reads.Links[0].Attributes[0] != dueID || !p.Reads.Links[0].Out || !p.Reads.Today {
		t.Errorf("reads = %+v", p.Reads)
	}
	if timeless, err := Compile(`"weight" * 2`, catalog); err != nil || timeless.Reads.Today {
		t.Errorf("a formula without today() reads %+v, %v", timeless.Reads, err)
	}
	if strings.Contains(p.Stored(), "weight") || !strings.Contains(p.Stored(), `"@`+weightID.String()+`"`) || !strings.Contains(p.Stored(), `'@`+verifiesID.String()+`'`) {
		t.Errorf("kept as %s", p.Stored())
	}
	renamed := catalog
	renamed.Attributes = append([]Attribute{{ID: weightID, Name: "Gewicht", Kind: Number}}, catalog.Attributes[1:]...)
	shown := Display(p.Stored(), renamed)
	if shown != `"Gewicht" + sum(children, "Cost") + count(links('verifies', out) where "Due" > today())` {
		t.Errorf("shown as %s", shown)
	}
	again, err := Compile(shown, renamed)
	if err != nil || again.Stored() != p.Stored() {
		t.Errorf("read back as %v, %v", again, err)
	}
}

func TestAFormulaThatCannotHoldSaysWhereAndWhy(t *testing.T) {
	for text, want := range map[string]string{
		``:                                    "Write a formula",
		`"Weight" +`:                          "ends too early",
		`"Weight" * 'two'`:                    "This is text, where a number belongs",
		`"Height"`:                            `no attribute "Height"`,
		`"Score" + 1`:                         "cannot read its own attribute",
		`frobnicate(1)`:                       "no function frobnicate",
		`round()`:                             "round takes 1 to 2 arguments",
		`sum(children)`:                       "sum needs what to read",
		`count(links('blocks', out))`:         `no link type "blocks"`,
		`count(links('verifies', up))`:        "Say out for the links",
		`sum(children, count(children))`:      "cannot read inside another",
		`'open`:                               "never closed",
		`1 2`:                                 "should end before",
		`if(1, 2, 3)`:                         "This is a number, where true or false belongs",
		`null`:                                "must give a value",
		`"Weight" ; 1`:                        "has no meaning",
		strings.Repeat("(", MaxDepth+1) + "1": "nests too deeply",
	} {
		_, err := Compile(text, catalog)
		if err == nil || !strings.Contains(err.Message, want) {
			t.Errorf("%q: %v, want %q", text, err, want)
		}
	}
	_, err := Compile(`"Weight" * 'two'`, catalog)
	if err.From != 12 || err.To != 16 {
		t.Errorf("the mistake is placed at %d-%d", err.From, err.To)
	}
}

func TestCyclesAreRefusedWithTheAttributesNamedUnlessTheyDescend(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	acyclic := func(id uuid.UUID) bool { return id == verifiesID }
	cases := []struct {
		name string
		defs []Def
		ok   bool
	}{
		{"a reads b of the same requirement, b reads a", []Def{
			{a, "Score", Reads{Self: []uuid.UUID{b}}}, {b, "Weight", Reads{Self: []uuid.UUID{a}}}}, false},
		{"a sums itself over the children", []Def{{a, "Total", Reads{ReadsChildren: true, Children: []uuid.UUID{a}}}}, true},
		{"a reads b, which sums a over the children", []Def{
			{a, "Total", Reads{Self: []uuid.UUID{b}}}, {b, "Rolled", Reads{ReadsChildren: true, Children: []uuid.UUID{a}}}}, true},
		{"a reads itself along an acyclic type", []Def{
			{a, "Chain", Reads{Links: []LinkRead{{TypeID: verifiesID, Out: true, Attributes: []uuid.UUID{a}}}}}}, true},
		{"a reads itself along a type that may cycle", []Def{
			{a, "Chain", Reads{Links: []LinkRead{{TypeID: relatesID, Out: true, Attributes: []uuid.UUID{a}}}}}}, false},
		{"a reads itself along an acyclic type both ways", []Def{
			{a, "Chain", Reads{Links: []LinkRead{{TypeID: verifiesID, Out: true, Attributes: []uuid.UUID{a}}, {TypeID: verifiesID, Attributes: []uuid.UUID{a}}}}}}, false},
		{"children and links mixed", []Def{
			{a, "Up", Reads{ReadsChildren: true, Children: []uuid.UUID{b}}},
			{b, "Down", Reads{Links: []LinkRead{{TypeID: verifiesID, Out: true, Attributes: []uuid.UUID{a}}}}}}, false},
		{"three round on one requirement", []Def{
			{a, "A", Reads{Self: []uuid.UUID{b}}}, {b, "B", Reads{Self: []uuid.UUID{c}}}, {c, "C", Reads{Self: []uuid.UUID{a}}}}, false},
	}
	for _, tc := range cases {
		err := Cycles(tc.defs, acyclic)
		if (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	err := Cycles([]Def{{a, "Score", Reads{Self: []uuid.UUID{b}}}, {b, "Weight", Reads{Self: []uuid.UUID{a}}}}, acyclic)
	var cycle *CycleError
	if !errors.As(err, &cycle) || !strings.HasPrefix(err.Error(), "Score reads Weight, which reads Score.") {
		t.Errorf("the cycle is said as %v", err)
	}
}

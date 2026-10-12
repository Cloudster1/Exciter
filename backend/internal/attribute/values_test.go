package attribute

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func def(kind Kind, options ...Option) Def {
	return Def{ID: uuid.New(), Name: "Field", Kind: kind, Options: options}
}

func anyone(uuid.UUID) (bool, error) { return true, nil }

// one normalizes a single value as Merge would, answering the stored form or the refusal.
func one(t *testing.T, d Def, raw string) (json.RawMessage, error) {
	t.Helper()
	out, err := Merge([]Def{d}, nil, false, nil, map[string]json.RawMessage{d.ID.String(): json.RawMessage(raw)}, anyone)
	if err != nil {
		return nil, err
	}
	return out[d.ID.String()], nil
}

func TestEachKindTakesItsOwnShape(t *testing.T) {
	low, high := Option{ID: uuid.New(), Name: "Low"}, Option{ID: uuid.New(), Name: "High"}
	for _, c := range []struct {
		def  Def
		in   string
		want string
	}{
		{def(Text), `"  a line  "`, `"a line"`},
		{def(Number), `2.5`, `2.5`},
		{def(Date), `"2026-01-31"`, `"2026-01-31"`},
		{def(Boolean), `false`, `false`},
		{def(URL), `"https://example.test/x"`, `"https://example.test/x"`},
		{def(Select, low, high), `"` + high.ID.String() + `"`, `"` + high.ID.String() + `"`},
		// Kept in the attribute's order, so the same choice is stored and hashed the same way.
		{def(MultiSelect, low, high), `["` + high.ID.String() + `","` + low.ID.String() + `"]`, `["` + low.ID.String() + `","` + high.ID.String() + `"]`},
		{def(RichText), `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}`, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}`},
	} {
		got, err := one(t, c.def, c.in)
		if err != nil || string(got) != c.want {
			t.Errorf("%s %s = %s, %v; want %s", c.def.Kind, c.in, got, err, c.want)
		}
	}
}

func TestAValueOfTheWrongShapeIsRefusedOnItsAttribute(t *testing.T) {
	option := Option{ID: uuid.New(), Name: "Low"}
	for _, c := range []struct {
		def Def
		in  string
	}{
		{def(Text), `12`},
		{def(Number), `"twelve"`},
		{def(Date), `"2026-02-30"`},
		{def(Boolean), `"yes"`},
		{def(URL), `"ftp://example.test"`},
		{def(URL), `"example.test"`},
		{def(Select, option), `"` + uuid.NewString() + `"`},
		{def(MultiSelect, option), `"` + option.ID.String() + `"`},
		{def(RichText), `{"type":"doc","content":[{"type":"panel"}]}`},
		{def(User), `"not an id"`},
	} {
		_, err := one(t, c.def, c.in)
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "attributes."+c.def.ID.String() {
			t.Errorf("%s %s was not refused on its attribute: %v", c.def.Kind, c.in, err)
		}
	}
}

func TestEmptyClearsAndARetiredOptionStaysButIsNotChosenAgain(t *testing.T) {
	retired := Option{ID: uuid.New(), Name: "Old", Retired: true}
	d := def(Select, retired, Option{ID: uuid.New(), Name: "New"})
	key := d.ID.String()
	had := map[string]json.RawMessage{key: json.RawMessage(`"` + retired.ID.String() + `"`)}
	for _, empty := range []string{`null`, `""`, `[]`} {
		out, err := Merge([]Def{d}, nil, false, had, map[string]json.RawMessage{key: json.RawMessage(empty)}, anyone)
		if err != nil || len(out) != 0 {
			t.Errorf("%s did not clear the value: %v %v", empty, out, err)
		}
	}
	if _, err := Merge([]Def{d}, nil, false, had, map[string]json.RawMessage{key: had[key]}, anyone); err != nil {
		t.Errorf("keeping a retired option was refused: %v", err)
	}
	if _, err := one(t, d, string(had[key])); err == nil {
		t.Error("a retired option was newly chosen")
	}
}

func TestTypesDecideWhereAnAttributeShowsAndWhatIsRequired(t *testing.T) {
	functional, interface_ := uuid.New(), uuid.New()
	d := def(Text)
	d.TypeIDs = []uuid.UUID{functional}
	d.Required = true
	key := d.ID.String()
	value := map[string]json.RawMessage{key: json.RawMessage(`"x"`)}
	if _, err := Merge([]Def{d}, &interface_, false, nil, value, anyone); err == nil {
		t.Error("a value for an attribute that does not show on the type was taken")
	}
	if err := Missing([]Def{d}, &functional, nil); err == nil {
		t.Error("a required attribute was left out")
	}
	if err := Missing([]Def{d}, &interface_, nil); err != nil {
		t.Errorf("an attribute required on another type was asked for: %v", err)
	}
	out, err := Merge([]Def{d}, &interface_, true, value, nil, anyone)
	if err != nil || len(out) != 0 {
		t.Errorf("a value stays on a type its attribute does not show on: %v %v", out, err)
	}
}

func TestOptionsAreReplacedKeepingTheirIds(t *testing.T) {
	low := Option{ID: uuid.New(), Name: "Low"}
	gone := Option{ID: uuid.New(), Name: "Gone"}
	out, err := mergeOptions(Select, []Option{low, gone}, []OptionInput{{ID: &low.ID, Name: "Lowest"}, {Name: "High"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[0].ID != low.ID || out[0].Name != "Lowest" || out[2].ID != gone.ID || !out[2].Retired {
		t.Errorf("options = %+v", out)
	}
	if _, err := mergeOptions(Select, nil, []OptionInput{{Name: "a"}, {Name: "A"}}); err == nil {
		t.Error("two options named alike were taken")
	}
	if _, err := mergeOptions(Text, nil, []OptionInput{{Name: "a"}}); err == nil {
		t.Error("a text attribute took options")
	}
}

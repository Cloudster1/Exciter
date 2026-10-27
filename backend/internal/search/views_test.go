package search

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/rql"
)

// refusedOn fails unless err is a field error on field.
func refusedOn(t *testing.T, what string, err error, field string) {
	t.Helper()
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Errorf("%s: %v, want a refusal on %s", what, err, field)
	}
}

// A view's name, filter, order and columns are held to the grid's rules before it is saved (#311).
func TestCheckViewHoldsAViewToTheGridsRules(t *testing.T) {
	grid := func(change func(*View)) View {
		v := View{Name: "Mine", Kind: ViewGrid, Columns: []string{"key", "title"}}
		change(&v)
		return v
	}

	t.Run("the name is trimmed, and the labels come back as the grid reads them", func(t *testing.T) {
		name, f, err := checkView(grid(func(v *View) {
			v.Name = "  Checkout  "
			v.Filter.Labels = []string{"Safety Critical", "paper"}
			v.Columns = append(v.Columns, "labels", "outline", costID.String())
		}), gridDef)
		if err != nil {
			t.Fatal(err)
		}
		if name != "Checkout" || !slices.Equal(f.Labels, []string{"safety-critical", "paper"}) {
			t.Errorf("checked as %q with labels %v", name, f.Labels)
		}
	})

	t.Run("a name of the most characters is kept", func(t *testing.T) {
		long := strings.Repeat("é", MaxViewNameLength)
		if name, _, err := checkView(grid(func(v *View) { v.Name = long }), gridDef); err != nil || name != long {
			t.Errorf("a name of %d characters: %q %v", MaxViewNameLength, name, err)
		}
	})

	columns := make([]string, MaxColumns+1)
	for i := range columns {
		columns[i] = "key"
	}
	for what, c := range map[string]struct {
		view  View
		field string
	}{
		"a blank name":                               {grid(func(v *View) { v.Name = " \t " }), "name"},
		"a name one character long":                  {grid(func(v *View) { v.Name = strings.Repeat("x", MaxViewNameLength+1) }), "name"},
		"one column too many":                        {grid(func(v *View) { v.Columns = columns }), "columns"},
		"a column that is none":                      {grid(func(v *View) { v.Columns = []string{"key", "colour"} }), "columns"},
		"an attribute the project lacks as a column": {grid(func(v *View) { v.Columns = []string{uuid.NewString()} }), "columns"},
		"a priority that is none":                    {grid(func(v *View) { v.Filter.Priorities = []string{"urgent"} }), "filter"},
		"a kind that is none":                        {grid(func(v *View) { v.Filter.Kinds = []requirement.Kind{"chapter"} }), "filter"},
		"a condition of an attribute that is none": {grid(func(v *View) {
			v.Filter.Attributes = []AttributeCondition{{AttributeID: uuid.New(), Op: OpIs, Value: "x"}}
		}), "filter"},
		"an order by a column that is none":     {grid(func(v *View) { v.Sort = Sort{Column: "colour"} }), "sort"},
		"a grid view with a matrix's settings":  {grid(func(v *View) { v.Settings.Gaps = "rows" }), "settings"},
		"a matrix view with the grid's columns": {grid(func(v *View) { v.Kind = ViewMatrix }), "columns"},
	} {
		_, _, err := checkView(c.view, gridDef)
		refusedOn(t, what, err, c.field)
	}
}

// keepKnown keeps what is known in the order it was named, and notes each one dropped under its prefix.
func TestKeepKnownNotesWhatItDrops(t *testing.T) {
	var dropped []string
	if got := keepKnown([]string{"verifies", "gone", "relates"}, []string{"relates", "verifies"}, "linkType:", &dropped); !slices.Equal(got, []string{"verifies", "relates"}) {
		t.Errorf("kept %v", got)
	}
	lost := uuid.New()
	ids := keepKnown([]uuid.UUID{riskID, lost}, []uuid.UUID{riskID}, "columnType:", &dropped)
	if !slices.Equal(ids, []uuid.UUID{riskID}) || !slices.Equal(dropped, []string{"linkType:gone", "columnType:" + lost.String()}) {
		t.Errorf("kept %v, dropped %v", ids, dropped)
	}
	if got := keepKnown(nil, []string{"verifies"}, "linkType:", &dropped); got == nil || len(got) != 0 {
		t.Errorf("nothing named keeps an empty list, not %v", got)
	}
}

// firstWords is the start of a statement for a hit its filters alone found.
func TestFirstWordsKeepsTheStart(t *testing.T) {
	for _, c := range []struct {
		text string
		n    int
		want string
	}{
		{"The shopper pays for everything at once.", 3, "The shopper pays"},
		{"  spread \n over\tlines  ", 5, "spread over lines"},
		{"Short.", 30, "Short."},
		{"", 30, ""},
	} {
		if got := firstWords(c.text, c.n); got != c.want {
			t.Errorf("firstWords(%q, %d) = %q, want %q", c.text, c.n, got, c.want)
		}
	}
}

// detailOf names the kind of word a completion offers, as the list under the query box shows it.
func TestDetailOfNamesEachKindOfWord(t *testing.T) {
	for _, c := range []struct {
		slot rql.Slot
		word string
		want string
	}{
		{rql.SlotField, "status", "field"},
		{rql.SlotField, `"Risk level"`, "attribute"},
		{rql.SlotOperator, ">=", "operator"},
		{rql.SlotValue, "draft", "value"},
		{rql.SlotValue, "currentUser()", "function"},
		{rql.SlotKeyword, "AND", "keyword"},
		{rql.SlotNone, "", "keyword"},
	} {
		if got := detailOf(c.slot, c.word); got != c.want {
			t.Errorf("detailOf(%s, %q) = %q, want %q", c.slot, c.word, got, c.want)
		}
	}
}

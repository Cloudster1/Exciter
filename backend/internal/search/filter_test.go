package search

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

var (
	riskID  = uuid.MustParse("0199a5e0-0000-7000-8000-0000000000a1")
	highID  = uuid.MustParse("0199a5e0-0000-7000-8000-0000000000a2")
	costID  = uuid.MustParse("0199a5e0-0000-7000-8000-0000000000a3")
	dueID   = uuid.MustParse("0199a5e0-0000-7000-8000-0000000000a4")
	noteID  = uuid.MustParse("0199a5e0-0000-7000-8000-0000000000a5")
	safeID  = uuid.MustParse("0199a5e0-0000-7000-8000-0000000000a6")
	gridDef = []attribute.Def{
		{ID: riskID, Name: "Risk", Kind: attribute.Select, Options: []attribute.Option{{ID: highID, Name: "High"}}},
		{ID: costID, Name: "Cost", Kind: attribute.Number},
		{ID: dueID, Name: "Due", Kind: attribute.Date},
		{ID: noteID, Name: "Note", Kind: attribute.Text},
		{ID: safeID, Name: "Safe", Kind: attribute.Boolean},
	}
)

func TestParseGridReadsTheContractsWords(t *testing.T) {
	f, sort, err := ParseGrid(url.Values{
		"q": {" pay "}, "kind": {"requirement"}, "label": {"Safety Critical"}, "priority": {"must"},
		"attr": {riskID.String() + ":is:" + highID.String(), noteID.String() + ":contains:a:b"},
		"sort": {"title"}, "desc": {"true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Q != "pay" || len(f.Attributes) != 2 || f.Attributes[1].Value != "a:b" || sort != (Sort{Column: "title", Desc: true}) {
		t.Fatalf("parsed %+v %+v", f, sort)
	}
	checked, err := check(f, sort, gridDef)
	if err != nil || checked.Labels[0] != "safety-critical" {
		t.Fatalf("checked %+v: %v", checked, err)
	}
	for field, v := range map[string]url.Values{
		"owner": {"owner": {"bob"}},
		"attr":  {"attr": {"nope"}},
		"desc":  {"desc": {"maybe"}},
	} {
		var fe *FieldError
		if _, _, err := ParseGrid(v); !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%v: %v, want a refusal on %s", v, err, field)
		}
	}
}

// Every condition the contract allows builds, and each it does not is refused in words on attr.
func TestConditionsHoldToEachKind(t *testing.T) {
	bind := func(any) string { return "$9" }
	for _, c := range []AttributeCondition{
		{riskID, OpIs, highID.String()}, {riskID, OpNot, highID.String()}, {costID, OpGte, "2.5"}, {dueID, OpLte, "2026-01-31"},
		{noteID, OpContains, "basket"}, {noteID, OpIs, "Pay"}, {safeID, OpIs, "true"}, {dueID, OpSet, ""}, {costID, OpUnset, ""},
	} {
		if sql, err := condition(c, gridDef, bind); err != nil || sql == "" {
			t.Errorf("%+v: %q %v", c, sql, err)
		}
	}
	for _, c := range []AttributeCondition{
		{uuid.New(), OpIs, "x"}, {riskID, OpGte, highID.String()}, {riskID, OpIs, uuid.NewString()}, {costID, OpIs, "many"},
		{dueID, OpGte, "tomorrow"}, {safeID, OpIs, "yes"}, {noteID, OpSet, "x"}, {noteID, OpIs, ""}, {riskID, Op("like"), "x"},
	} {
		_, err := condition(c, gridDef, bind)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "attr" || !strings.HasSuffix(fe.Message, ".") {
			t.Errorf("%+v: %v, want a sentence on attr", c, err)
		}
	}
	not, _ := condition(AttributeCondition{riskID, OpNot, highID.String()}, gridDef, bind)
	if !strings.HasPrefix(not, "NOT COALESCE(") {
		t.Errorf("not equal takes in what has no value: %s", not)
	}
}

func TestCheckRefusesWhatTheGridDoesNotTake(t *testing.T) {
	many := make([]AttributeCondition, MaxAttributeFilters+1)
	for i := range many {
		many[i] = AttributeCondition{costID, OpSet, ""}
	}
	for field, f := range map[string]Filter{
		"kind":     {Kinds: []requirement.Kind{"chapter"}},
		"category": {Categories: []string{"finished"}},
		"priority": {Priorities: []string{"urgent"}},
		"label":    {Labels: []string{"#1"}},
		"attr":     {Attributes: many},
		"q":        {Q: strings.Repeat("x", MaxQueryLength+1)},
	} {
		var fe *FieldError
		if _, err := check(f, Sort{}, gridDef); !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: %v", field, err)
		}
	}
	var fe *FieldError
	if _, err := check(Filter{}, Sort{Column: "colour"}, gridDef); !errors.As(err, &fe) || fe.Field != "sort" {
		t.Errorf("an unknown column sorted: %v", err)
	}
	if _, err := check(Filter{}, Sort{Column: costID.String()}, gridDef); err != nil {
		t.Errorf("an attribute did not sort: %v", err)
	}
}

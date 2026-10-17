package requirement

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestATitleIsComparedWordByWord(t *testing.T) {
	blocks, err := Diff(titleDoc("Pay"), titleDoc("Pay by card"))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Change != DiffModified || !strings.Contains(string(blocks[0].Node), "diffInsert") {
		t.Errorf("a longer title is one paragraph with its new words marked: %+v", blocks)
	}
	same, err := Diff(titleDoc("Pay"), titleDoc("Pay"))
	if err != nil || len(same) != 1 || same[0].Change != DiffEqual {
		t.Errorf("an unchanged title is one equal paragraph: %+v %v", same, err)
	}
	if _, err := Diff(titleDoc(""), titleDoc("Pay")); err != nil {
		t.Errorf("the empty requirement's title compares too: %v", err)
	}
}

func TestASpecQueryNamesWhatItComparesWith(t *testing.T) {
	for name, tc := range map[string]struct {
		q     SpecQuery
		field string
	}{
		"an unknown base":             {SpecQuery{Changes: "lost"}, "changes"},
		"a baseline left out":         {SpecQuery{Changes: BaseBaseline}, "baseline"},
		"only changed without a base": {SpecQuery{OnlyChanged: true}, "onlyChanged"},
	} {
		var field *FieldError
		if err := tc.q.check(); !errors.As(err, &field) || field.Field != tc.field {
			t.Errorf("%s: %v, want a refusal on %s", name, err, tc.field)
		}
	}
	for _, q := range []SpecQuery{{}, {Changes: BasePrevious, OnlyChanged: true}, {Changes: BaseApproved}, {Changes: BaseBaseline, Baseline: uuid.New()}} {
		if err := q.check(); err != nil {
			t.Errorf("%+v: %v", q, err)
		}
	}
}

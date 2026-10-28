package workflow

import (
	"errors"
	"strings"
	"testing"
)

func given[T any](v T) *T { return &v }

// A status's fields are each held to their shape before anything is written (#322).
func TestAStatusTakesOnlyWhatItsFieldsHold(t *testing.T) {
	for _, c := range []struct {
		what  string
		in    StatusInput
		field string
	}{
		{"a blank name", StatusInput{Name: given("  ")}, "name"},
		{"a name too long", StatusInput{Name: given(strings.Repeat("n", MaxNameLength+1))}, "name"},
		{"a category that is none", StatusInput{Category: given(Category("parked"))}, "category"},
		{"a description too long", StatusInput{Description: given(strings.Repeat("d", MaxDescriptionLength+1))}, "description"},
	} {
		st := Status{Name: "Draft", Category: CategoryDraft}
		var invalid *InvalidError
		if err := applyStatus(&st, c.in); !errors.As(err, &invalid) || invalid.Field != c.field {
			t.Errorf("%s: %v, want a refusal on %s", c.what, err, c.field)
			continue
		}
		if !strings.HasSuffix(invalid.Message, ".") {
			t.Errorf("%s: the refusal is no sentence: %q", c.what, invalid.Message)
		}
		if st.Name != "Draft" || st.Category != CategoryDraft {
			t.Errorf("%s changed the status to %+v", c.what, st)
		}
	}

	t.Run("what it takes is trimmed, a negative position is the first, and what is left out stays", func(t *testing.T) {
		st := Status{Name: "Draft", Category: CategoryDraft, Description: "Being written.", Position: 2}
		if err := applyStatus(&st, StatusInput{Name: given("  Parked  "), Position: given(-3), Locks: given(true)}); err != nil {
			t.Fatal(err)
		}
		if st.Name != "Parked" || st.Position != 0 || !st.Locks || st.Category != CategoryDraft || st.Description != "Being written." {
			t.Errorf("the status reads %+v", st)
		}
		if err := applyStatus(&st, StatusInput{Category: given(CategoryClosed), Description: given("  Not now.  ")}); err != nil {
			t.Fatal(err)
		}
		if st.Category != CategoryClosed || st.Description != "Not now." || st.Name != "Parked" {
			t.Errorf("the status reads %+v", st)
		}
	})
}

package requirement

import (
	"errors"
	"fmt"
	"testing"
)

func TestABulkRequestNamesEachRequirementOnceAndNotTooMany(t *testing.T) {
	many := make([]string, MaxBulk+1)
	for i := range many {
		many[i] = fmt.Sprintf("CHK-R%d", i+1)
	}
	for name, keys := range map[string][]string{
		"none":                  {},
		"one past the limit":    many,
		"a key twice":           {"CHK-R1", "CHK-R2", "CHK-R1"},
		"a key twice, any case": {"CHK-R1", "chk-r1"},
	} {
		var field *FieldError
		if err := CheckBulk("keys", keys); !errors.As(err, &field) || field.Field != "keys" {
			t.Errorf("%s: %v, want a refusal on keys", name, err)
		}
	}
	if err := CheckBulk("keys", many[:MaxBulk]); err != nil {
		t.Errorf("as many as the limit: %v", err)
	}
}

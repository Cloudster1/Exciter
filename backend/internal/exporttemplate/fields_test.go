package exporttemplate

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// The fields a template shows are refused on fields when one is unknown, nameless, twice or past MaxFields, and a
// field that is no attribute forgets an attribute's name (#318).
func TestATemplateShowsEachKnownFieldOnce(t *testing.T) {
	tooMany := make([]FieldRef, MaxFields+1)
	for i := range tooMany {
		tooMany[i] = FieldRef{Field: FieldAttribute, Attribute: "Attribute " + strings.Repeat("x", i+1)}
	}
	for _, c := range []struct {
		what   string
		fields []FieldRef
		says   string
	}{
		{"an unknown field", []FieldRef{{Field: "colour"}}, `"colour" is not a field a template shows.`},
		{"an attribute without its name", []FieldRef{{Field: FieldAttribute, Attribute: "  "}}, "Name the attribute a field shows."},
		{"the same field twice", []FieldRef{{Field: FieldStatus}, {Field: FieldOwner}, {Field: FieldStatus}}, "Show each field once."},
		{"the same attribute twice", []FieldRef{{Field: FieldAttribute, Attribute: "Risk"}, {Field: FieldAttribute, Attribute: " Risk "}}, "Show each field once."},
		{"a field twice once its attribute name is dropped", []FieldRef{{Field: FieldType}, {Field: FieldType, Attribute: "Risk"}}, "Show each field once."},
		{"more than MaxFields", tooMany, "A template shows at most 60 fields."},
	} {
		_, err := checkFields(c.fields)
		var refusal *FieldError
		if !errors.As(err, &refusal) || refusal.Field != "fields" || !strings.HasPrefix(refusal.Message, c.says) {
			t.Errorf("%s: %v", c.what, err)
		}
	}

	t.Run("MaxFields fields are taken, and a field that is no attribute drops its attribute name", func(t *testing.T) {
		if got, err := checkFields(tooMany[:MaxFields]); err != nil || len(got) != MaxFields {
			t.Fatalf("%d fields: %d taken, %v", MaxFields, len(got), err)
		}
		got, err := checkFields([]FieldRef{{Field: FieldPriority, Attribute: "Risk"}, {Field: FieldAttribute, Attribute: " Risk "}})
		want := []FieldRef{{Field: FieldPriority}, {Field: FieldAttribute, Attribute: "Risk"}}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("the fields read %v, %v", got, err)
		}
		if got, err := checkFields(nil); err != nil || got == nil || len(got) != 0 {
			t.Errorf("no fields read %v, %v", got, err)
		}
	})
}

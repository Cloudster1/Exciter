package attribute

import (
	"errors"
	"strings"
	"testing"
)

func given[T any](v T) *T { return &v }

// typeRefusal applies in to a type that has an icon and a colour, and answers the field it was refused on.
func typeRefusal(t *testing.T, in TypeInput) string {
	t.Helper()
	ty := Type{Icon: "requirement", Colour: "#4f6bed"}
	var field *FieldError
	if err := applyType(&ty, in); !errors.As(err, &field) {
		t.Fatalf("%+v was taken: %v", in, err)
	}
	if !strings.HasSuffix(field.Message, ".") {
		t.Errorf("the refusal on %s is no sentence: %q", field.Field, field.Message)
	}
	return field.Field
}

// A type's fields are each held to their shape before anything is written (#322).
func TestATypeTakesOnlyWhatItsFieldsHold(t *testing.T) {
	for _, c := range []struct {
		what  string
		in    TypeInput
		field string
	}{
		{"a blank name", TypeInput{Name: given("   ")}, "name"},
		{"a name too long", TypeInput{Name: given(strings.Repeat("n", MaxNameLength+1))}, "name"},
		{"a description too long", TypeInput{Description: given(strings.Repeat("d", MaxDescriptionLength+1))}, "description"},
		{"an icon that is no icon's name", TypeInput{Icon: given("Rocket ship!")}, "icon"},
		{"an empty icon", TypeInput{Icon: given("")}, "icon"},
		{"a colour by name", TypeInput{Colour: given("red")}, "colour"},
		{"a colour of three digits", TypeInput{Colour: given("#abc")}, "colour"},
		{"a role no type has", TypeInput{Role: given(TypeRole("epic"))}, "role"},
	} {
		if got := typeRefusal(t, c.in); got != c.field {
			t.Errorf("%s was refused on %s, want %s", c.what, got, c.field)
		}
	}

	t.Run("a new type without an icon or a colour is refused", func(t *testing.T) {
		var field *FieldError
		if err := applyType(&Type{}, TypeInput{Name: given("Interface"), Colour: given("#4f6bed")}); !errors.As(err, &field) || field.Field != "icon" {
			t.Errorf("no icon: %v", err)
		}
		if err := applyType(&Type{}, TypeInput{Name: given("Interface"), Icon: given("interface")}); !errors.As(err, &field) || field.Field != "colour" {
			t.Errorf("no colour: %v", err)
		}
	})

	t.Run("what it takes is trimmed, a colour lowered, and the role a requirement's unless named", func(t *testing.T) {
		ty := Type{}
		// An icon the web client does not draw yet is still a name it may learn, so only its shape is held.
		if err := applyType(&ty, TypeInput{Name: given("  Interface  "), Description: given("  Between parts.  "), Icon: given("gearbox"),
			Colour: given("  #4F6BED ")}); err != nil {
			t.Fatal(err)
		}
		if ty.Name != "Interface" || ty.Description != "Between parts." || ty.Icon != "gearbox" || ty.Colour != "#4f6bed" || ty.Role != RoleRequirement {
			t.Errorf("the type reads %+v", ty)
		}
		if err := applyType(&ty, TypeInput{Role: given(RoleRisk)}); err != nil || ty.Role != RoleRisk || ty.Name != "Interface" {
			t.Errorf("a role alone changes only the role: %+v, %v", ty, err)
		}
	})
}

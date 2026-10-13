package project_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// What the database takes of a type's icon and colour, migration 00140's checks.
var (
	iconShape   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	colourShape = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

func TestEveryTemplateMakesWhatTheContractNamesAndTheDatabaseTakes(t *testing.T) {
	var keys []string
	for _, summary := range project.Templates() {
		b, ok := project.BlueprintOf(summary.Key)
		if !ok || slices.Contains(keys, summary.Key) {
			t.Fatalf("the template %q is not found by its key once", summary.Key)
		}
		keys = append(keys, summary.Key)
		if len(b.TypeSpecs) == 0 || len(b.Sections) == 0 || summary.Name == "" || summary.Description == "" {
			t.Errorf("%s makes no types or sections, or is not named: %+v", summary.Key, b)
		}
		if len(summary.Types) != len(b.TypeSpecs) || len(summary.Attributes) != len(b.AttributeSpecs) || len(summary.Statuses) == 0 {
			t.Errorf("%s: the summary does not name what it makes: %+v", summary.Key, summary)
		}
		for _, each := range b.TypeSpecs {
			if !iconShape.MatchString(each.Icon) || !colourShape.MatchString(each.Colour) || len(each.Name) > attribute.MaxNameLength {
				t.Errorf("%s: the type %+v would be refused", summary.Key, each)
			}
		}
		for _, each := range b.AttributeSpecs {
			kind := attribute.Kind(each.Kind)
			hasOptions := kind == attribute.Select || kind == attribute.MultiSelect
			if !slices.Contains(attribute.Kinds, kind) || hasOptions != (len(each.Options) > 0) {
				t.Errorf("%s: the attribute %+v would be refused", summary.Key, each)
			}
		}
	}
	if !slices.Equal(keys, []string{"system", "software", "test"}) {
		t.Errorf("the templates are %v, want the contract's system, software and test", keys)
	}
	if _, ok := project.BlueprintOf("nope"); ok {
		t.Error("a key nobody made names a template")
	}
	if b, _ := project.BlueprintOf("test"); !slices.Equal(b.LinkTypeKeys, []string{"verifies", "relates"}) {
		t.Errorf("the test template uses %v", b.LinkTypeKeys)
	}
}

package project

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Built-in project templates (#24), shaped after Stator's space templates: what a new project starts with, kept
// in code and copied once, so changing a template never changes the projects made from it.

// TypeSpec is a requirement type a template makes; the first is the project's default.
type TypeSpec struct {
	Name, Icon, Colour string
}

// AttributeSpec is an attribute a template makes, shown on every type.
type AttributeSpec struct {
	Name, Kind string
	Options    []string
}

// Blueprint is what a template makes; LinkTypes names the organization's link types the project uses, every
// one when empty.
type Blueprint struct {
	Template
	TypeSpecs      []TypeSpec
	AttributeSpecs []AttributeSpec
	LinkTypeKeys   []string
}

// The attribute kinds a template uses, as internal/attribute names them.
const (
	kindText     = "text"
	kindRichText = "richText"
	kindSelect   = "select"
	kindBoolean  = "boolean"
)

// The statuses every template uses: the default workflow's (#17).
var defaultStatuses = []string{"Draft", "In review", "Approved", "Implemented", "Verified", "Rejected", "Obsolete"}

// verification is how a requirement is shown to be met, which both specification templates ask.
var verification = AttributeSpec{Name: "Verification method", Kind: kindSelect, Options: []string{"Test", "Analysis", "Inspection", "Demonstration"}}

// blueprints are the built-in templates, in the order the chooser offers them.
var blueprints = []Blueprint{
	{
		Template: Template{Key: "system", Name: "System specification",
			Description: "Stakeholder needs refined into system requirements, interfaces and constraints, each verified by a method.",
			Sections:    []string{"Introduction", "Stakeholder needs", "System requirements", "Interfaces", "Constraints"}},
		TypeSpecs: []TypeSpec{
			{"Stakeholder need", "need", "#7c3aed"}, {"System requirement", "requirement", "#4f6bed"},
			{"Interface", "interface", "#0f9d8a"}, {"Constraint", "constraint", "#d97706"},
		},
		AttributeSpecs: []AttributeSpec{{Name: "Source", Kind: kindText}, verification, {Name: "Safety relevant", Kind: kindBoolean}},
	},
	{
		Template: Template{Key: "software", Name: "Software specification",
			Description: "Functional and non-functional requirements, interfaces and use cases of a piece of software, by component and release.",
			Sections:    []string{"Introduction", "Functional requirements", "Non-functional requirements", "Interfaces"}},
		TypeSpecs: []TypeSpec{
			{"Functional", "function", "#4f6bed"}, {"Non-functional", "requirement", "#64748b"},
			{"Interface", "interface", "#0f9d8a"}, {"Use case", "data", "#db2777"},
		},
		AttributeSpecs: []AttributeSpec{{Name: "Component", Kind: kindText}, verification, {Name: "Release", Kind: kindText}},
	},
	{
		Template: Template{Key: "test", Name: "Test specification",
			Description: "Test cases and procedures that verify the requirements of other projects, with what each expects.",
			Sections:    []string{"Test strategy", "Test cases"}},
		TypeSpecs: []TypeSpec{{"Test case", "test", "#c2410c"}, {"Test procedure", "test", "#d97706"}},
		AttributeSpecs: []AttributeSpec{
			{Name: "Preconditions", Kind: kindRichText}, {Name: "Expected result", Kind: kindRichText}, {Name: "Automated", Kind: kindBoolean},
		},
		LinkTypeKeys: []string{"verifies", "relates"},
	},
}

// Templates are the built-in templates, each naming what it makes for the chooser to show.
func Templates() []Template {
	out := make([]Template, len(blueprints))
	for i, b := range blueprints {
		out[i] = b.summary()
	}
	return out
}

func (b Blueprint) summary() Template {
	t := b.Template
	t.Types, t.Attributes = make([]string, len(b.TypeSpecs)), make([]string, len(b.AttributeSpecs))
	for i, each := range b.TypeSpecs {
		t.Types[i] = each.Name
	}
	for i, each := range b.AttributeSpecs {
		t.Attributes[i] = each.Name
	}
	t.Statuses = defaultStatuses
	t.LinkTypes = b.LinkTypeKeys
	if len(t.LinkTypes) == 0 {
		t.LinkTypes = []string{}
	}
	return t
}

// BlueprintOf is the template a key names.
func BlueprintOf(key string) (Blueprint, bool) {
	for _, b := range blueprints {
		if b.Key == key {
			return b, true
		}
	}
	return Blueprint{}, false
}

// Seeder makes what a template holds in a new project, in the transaction that made it; the requirement
// package makes the sections, which this package cannot.
type Seeder func(ctx context.Context, tx db.DBTX, actor, projectID uuid.UUID, b Blueprint) error

// WithSeeder has every project made from a template get what it holds.
func (s *Service) WithSeeder(seed Seeder) *Service {
	s.seed = seed
	return s
}

package project

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Built-in project templates (#24), shaped after Stator's space templates: what a new project starts with, kept
// in code and copied once, so changing a template never changes the projects made from it.

// TypeSpec is a requirement type a template makes; the first is the project's default. Role is the type's
// role as internal/attribute names it, a plain requirement type when empty.
type TypeSpec struct {
	Name, Icon, Colour, Role string
}

// AttributeSpec is an attribute a template makes, shown on the types Types names, every type when it names none.
type AttributeSpec struct {
	Name, Kind string
	Options    []string
	Types      []string
}

// RiskSpec is the risk scheme a template sets (#261), as internal/risk takes it; Level names the level of each cell
// of a matrix, and a priority number leaves it nil.
type RiskSpec struct {
	Method    string
	Detection bool
	Scales    []ScaleSpec
	Levels    []LevelSpec
	Level     func(severity, probability int) string
}

// ScaleSpec is one scale of a template's scheme: its dimension, label, and the names of its values from 1 up.
type ScaleSpec struct {
	Dimension, Label string
	Names            []string
}

// LevelSpec is one level of a template's scheme, from the least to the most severe.
type LevelSpec struct {
	Name, Colour string
	Acceptable   bool
	MinRPN       *int
}

// CoverageSpec is what a template's coverage rules ask of the requirements of one of its types (#38).
type CoverageSpec struct {
	Type                  string
	Implemented, Verified bool
}

// Blueprint is what a template makes; LinkTypes names the installation's link types the project uses, every
// one when empty.
type Blueprint struct {
	Template
	TypeSpecs      []TypeSpec
	AttributeSpecs []AttributeSpec
	LinkTypeKeys   []string
	Risk           *RiskSpec
	Coverage       []CoverageSpec
}

// The attribute kinds a template uses, as internal/attribute names them.
const (
	kindText     = "text"
	kindRichText = "richText"
	kindSelect   = "select"
	kindBoolean  = "boolean"
)

// roleTestCase and roleRisk are the roles of types whose requirements are test cases (#250) and risks (#259), as
// internal/attribute names them.
const (
	roleTestCase = "testCase"
	roleRisk     = "risk"
)

// The colours of the templates' risk levels, from acceptable to the most severe.
const (
	colourLow    = "#0f9d8a"
	colourMedium = "#d97706"
	colourHigh   = "#c2410c"
)

// The thresholds of the medical device matrix by the product of severity and probability, and of the functional
// safety priority number.
const (
	medicalMedium = 5
	medicalHigh   = 12
	safetyMedium  = 100
	safetyHigh    = 200
)

func threshold(n int) *int { return &n }

// medicalRisk is a five by five matrix of severity and probability, as a medical device's risk file rates harms.
var medicalRisk = RiskSpec{
	Method: "matrix",
	Scales: []ScaleSpec{
		{Dimension: "severity", Label: "Severity", Names: []string{"Negligible", "Minor", "Serious", "Critical", "Catastrophic"}},
		{Dimension: "probability", Label: "Probability", Names: []string{"Improbable", "Remote", "Occasional", "Probable", "Frequent"}},
	},
	Levels: []LevelSpec{{Name: "Low", Colour: colourLow, Acceptable: true}, {Name: "Medium", Colour: colourMedium}, {Name: "High", Colour: colourHigh}},
	Level: func(severity, probability int) string {
		switch product := severity * probability; {
		case product >= medicalHigh:
			return "High"
		case product >= medicalMedium:
			return "Medium"
		}
		return "Low"
	},
}

// safetyRisk is a priority number of severity, occurrence and detection from 1 to 10 each, as a failure mode
// analysis rates them.
var safetyRisk = RiskSpec{
	Method:    "rpn",
	Detection: true,
	Scales: []ScaleSpec{
		{Dimension: "severity", Label: "Severity", Names: []string{"No effect", "Very minor", "Minor", "Very low", "Low", "Moderate", "High", "Very high",
			"Hazardous with warning", "Hazardous without warning"}},
		{Dimension: "probability", Label: "Occurrence", Names: []string{"Remote", "Very low", "Low", "Low to moderate", "Moderate", "Moderately high", "High",
			"Very high", "Extremely high", "Almost certain"}},
		{Dimension: "detection", Label: "Detection", Names: []string{"Almost certain", "Very high", "High", "Moderately high", "Moderate", "Low", "Very low",
			"Remote", "Very remote", "Absolutely uncertain"}},
	},
	Levels: []LevelSpec{{Name: "Low", Colour: colourLow, Acceptable: true}, {Name: "Medium", Colour: colourMedium, MinRPN: threshold(safetyMedium)},
		{Name: "High", Colour: colourHigh, MinRPN: threshold(safetyHigh)}},
}

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
			{Name: "Stakeholder need", Icon: "need", Colour: "#7c3aed"}, {Name: "System requirement", Icon: "requirement", Colour: "#4f6bed"},
			{Name: "Interface", Icon: "interface", Colour: "#0f9d8a"}, {Name: "Constraint", Icon: "constraint", Colour: "#d97706"},
		},
		AttributeSpecs: []AttributeSpec{{Name: "Source", Kind: kindText}, verification, {Name: "Safety relevant", Kind: kindBoolean}},
	},
	{
		Template: Template{Key: "software", Name: "Software specification",
			Description: "Functional and non-functional requirements, interfaces and use cases of a piece of software, by component and release.",
			Sections:    []string{"Introduction", "Functional requirements", "Non-functional requirements", "Interfaces"}},
		TypeSpecs: []TypeSpec{
			{Name: "Functional", Icon: "function", Colour: "#4f6bed"}, {Name: "Non-functional", Icon: "requirement", Colour: "#64748b"},
			{Name: "Interface", Icon: "interface", Colour: "#0f9d8a"}, {Name: "Use case", Icon: "data", Colour: "#db2777"},
		},
		AttributeSpecs: []AttributeSpec{{Name: "Component", Kind: kindText}, verification, {Name: "Release", Kind: kindText}},
	},
	{
		Template: Template{Key: "test", Name: "Test specification",
			Description: "Test cases and procedures that verify the requirements of other projects, with what each expects.",
			Sections:    []string{"Test strategy", "Test cases"}},
		TypeSpecs: []TypeSpec{
			{Name: "Test case", Icon: "test", Colour: "#c2410c", Role: roleTestCase}, {Name: "Test procedure", Icon: "test", Colour: "#d97706"},
		},
		// A test case holds its preconditions and expected results itself, so the template adds no attributes for them.
		AttributeSpecs: []AttributeSpec{{Name: "Automated", Kind: kindBoolean}},
		LinkTypeKeys:   []string{"verifies", "relates"},
	},
	{
		Template: Template{Key: "medical", Name: "Medical device risk file",
			Description: "Hazards leading to hazardous situations and harms, rated by severity and probability, each controlled by requirements implemented and verified.",
			Sections:    []string{"Introduction", "Hazards", "Risk controls"}},
		TypeSpecs: []TypeSpec{
			{Name: "Risk control", Icon: "constraint", Colour: "#0f9d8a"}, {Name: "Hazard", Icon: "risk", Colour: colourHigh, Role: roleRisk},
			{Name: "Hazardous situation", Icon: "risk", Colour: colourMedium, Role: roleRisk}, {Name: "Harm", Icon: "risk", Colour: "#db2777", Role: roleRisk},
		},
		AttributeSpecs: []AttributeSpec{
			{Name: "Sequence of events", Kind: kindRichText, Types: []string{"Hazardous situation"}},
			{Name: verification.Name, Kind: verification.Kind, Options: verification.Options, Types: []string{"Risk control"}},
		},
		LinkTypeKeys: []string{"mitigates", "leads_to", "verifies", "relates"},
		Risk:         &medicalRisk,
		Coverage:     []CoverageSpec{{Type: "Risk control", Implemented: true, Verified: true}},
	},
	{
		Template: Template{Key: "safety", Name: "Functional safety analysis",
			Description: "Failure modes with their effects and causes, rated by severity, occurrence and detection into a priority number, each mitigated by safety requirements implemented and verified.",
			Sections:    []string{"Item definition", "Failure modes", "Safety requirements"}},
		TypeSpecs: []TypeSpec{
			{Name: "Safety requirement", Icon: "requirement", Colour: "#4f6bed"}, {Name: "Failure mode", Icon: "risk", Colour: colourHigh, Role: roleRisk},
		},
		AttributeSpecs: []AttributeSpec{
			{Name: "Effect", Kind: kindText, Types: []string{"Failure mode"}}, {Name: "Cause", Kind: kindText, Types: []string{"Failure mode"}},
			{Name: verification.Name, Kind: verification.Kind, Options: verification.Options, Types: []string{"Safety requirement"}},
		},
		LinkTypeKeys: []string{"mitigates", "leads_to", "verifies", "refines", "relates"},
		Risk:         &safetyRisk,
		Coverage:     []CoverageSpec{{Type: "Safety requirement", Implemented: true, Verified: true}},
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

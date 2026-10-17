// Package workflow is the state machine requirements move through (#17), a
// slimmed Armature workflow; docs/api-contract-m1.md says how.
package workflow

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Limits on a workflow, matching the designer's.
const (
	MaxNameLength   = 60
	MaxStatuses     = 50
	MaxTransitions  = 200
	MaxRulesPerEdge = 20
)

// Category is the coarse bucket a status falls into; coverage, filters and
// every "is it settled" question key on it rather than on a status's name.
type Category string

const (
	CategoryDraft    Category = "draft"
	CategoryReview   Category = "review"
	CategoryApproved Category = "approved"
	CategoryDone     Category = "done"
	// CategoryClosed is rejected or obsolete, which coverage leaves out.
	CategoryClosed Category = "closed"
)

// Categories lists every Category, in the order a requirement passes them.
var Categories = []Category{CategoryDraft, CategoryReview, CategoryApproved, CategoryDone, CategoryClosed}

// Status is a state a requirement of the project can be in.
type Status struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Category    Category  `json:"category"`
	Description string    `json:"description"`
	Position    int       `json:"position"`
	// Requirements counts those in it now, which keep it from being deleted.
	Requirements int `json:"requirements"`
	// Locks says its requirements take no new revision, move, link or trash while in it (#205).
	Locks bool `json:"locks"`
}

// StatusInput makes a status, or changes one where nil leaves a field alone.
type StatusInput struct {
	Name        *string   `json:"name,omitempty"`
	Category    *Category `json:"category,omitempty"`
	Description *string   `json:"description,omitempty"`
	Position    *int      `json:"position,omitempty"`
	Locks       *bool     `json:"locks,omitempty"`
}

// Point is where the designer drew a status, in canvas pixels.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Step is a status's place in one workflow.
type Step struct {
	Status    Status `json:"status"`
	IsInitial bool   `json:"isInitial"`
	// OnRevisionStatusID is where a requirement in this status goes when a
	// new revision is saved, null to stay; Approved goes back to Draft.
	OnRevisionStatusID *uuid.UUID `json:"onRevisionStatusId"`
	// Layout is null until somebody has placed the status on the canvas.
	Layout *Point `json:"layout"`
}

// RuleKind separates the three points at which a rule can intervene.
type RuleKind string

const (
	// KindCondition decides whether a transition is offered at all.
	KindCondition RuleKind = "condition"
	// KindValidator checks the input of a chosen transition, with a sentence on failure.
	KindValidator RuleKind = "validator"
	// KindPostFunction runs after the status has changed.
	KindPostFunction RuleKind = "postfunction"
)

// RuleKinds lists every RuleKind, for the API document.
var RuleKinds = []RuleKind{KindCondition, KindValidator, KindPostFunction}

// Rule is a configured rule on a transition.
type Rule struct {
	ID       uuid.UUID       `json:"id"`
	Kind     RuleKind        `json:"kind"`
	Type     string          `json:"type"`
	Config   json.RawMessage `json:"config"`
	Position int             `json:"position"`
}

// Transition is an edge of the workflow graph.
type Transition struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// FromStatusID is null for a transition offered from every status.
	FromStatusID *uuid.UUID `json:"fromStatusId"`
	ToStatusID   uuid.UUID  `json:"toStatusId"`
	Description  string     `json:"description"`
	Position     int        `json:"position"`
	Rules        []Rule     `json:"rules"`
}

// Workflow is a set of steps and the transitions between them.
type Workflow struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Steps       []Step       `json:"steps"`
	Transitions []Transition `json:"transitions"`
	// ProjectDefault says the project follows it; TypeIDs are the types with it as their own.
	ProjectDefault bool        `json:"projectDefault"`
	TypeIDs        []uuid.UUID `json:"typeIds"`
	UpdatedAt      time.Time   `json:"updatedAt"`
}

// StepInput is one status of a workflow as the designer saves it.
type StepInput struct {
	StatusID           uuid.UUID  `json:"statusId"`
	IsInitial          bool       `json:"isInitial"`
	OnRevisionStatusID *uuid.UUID `json:"onRevisionStatusId,omitempty"`
	Layout             *Point     `json:"layout,omitempty"`
}

// RuleInput is one rule as the designer saves it.
type RuleInput struct {
	Kind   RuleKind        `json:"kind"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config,omitempty"`
}

// TransitionInput is one transition as the designer saves it; one with the id
// of an existing transition is that transition.
type TransitionInput struct {
	ID           *uuid.UUID  `json:"id,omitempty"`
	Name         string      `json:"name"`
	Description  string      `json:"description,omitempty"`
	FromStatusID *uuid.UUID  `json:"fromStatusId,omitempty"`
	ToStatusID   uuid.UUID   `json:"toStatusId"`
	Rules        []RuleInput `json:"rules"`
}

// GraphInput is a whole workflow as the designer saves it, in one request.
type GraphInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Steps       []StepInput       `json:"steps"`
	Transitions []TransitionInput `json:"transitions"`
}

// AssignInput makes a workflow the project's, or a type's, and moves the
// requirements whose status it lacks.
type AssignInput struct {
	WorkflowID uuid.UUID `json:"workflowId"`
	// TypeID names the type to assign it to, left out for the project's default.
	TypeID *uuid.UUID `json:"typeId,omitempty"`
	// StatusMapping maps each status in use that the workflow lacks to one it has.
	StatusMapping map[string]uuid.UUID `json:"statusMapping,omitempty"`
}

// OptionKind is the shape of one setting of a rule type.
type OptionKind string

const (
	OptionText  OptionKind = "text"
	OptionRoles OptionKind = "roles"
)

// OptionKinds lists every OptionKind, for the API document.
var OptionKinds = []OptionKind{OptionText, OptionRoles}

// Option is one setting a rule type takes in its config.
type Option struct {
	Name     string     `json:"name"`
	Label    string     `json:"label"`
	Kind     OptionKind `json:"kind"`
	Required bool       `json:"required"`
	Choices  []string   `json:"choices"`
}

// RuleType is one entry of the registry the designer offers.
type RuleType struct {
	Kind        RuleKind `json:"kind"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Options     []Option `json:"options"`
}

// Available is a transition the caller may take now, and what it asks for.
type Available struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	ToStatus Status    `json:"toStatus"`
	// NeedsComment says a validator refuses it without a comment.
	NeedsComment bool `json:"needsComment"`
}

// TransitionRequest takes a transition.
type TransitionRequest struct {
	TransitionID uuid.UUID `json:"transitionId"`
	Comment      string    `json:"comment,omitempty"`
}

package requirement

import (
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/workflow"
)

const (
	// MaxBulk bounds the requirements one bulk read, transition or review request names (#167).
	MaxBulk = 100
	// MaxSpecChangesLimit bounds one window of the specification with changes, each entry carrying its comparison.
	MaxSpecChangesLimit = 50
)

// BulkOutcome says what a bulk operation did with one requirement.
type BulkOutcome string

const (
	BulkTaken     BulkOutcome = "taken"
	BulkRequested BulkOutcome = "requested"
	BulkRefused   BulkOutcome = "refused"
)

// BulkOutcomes lists every BulkOutcome, for the API document.
var BulkOutcomes = []BulkOutcome{BulkTaken, BulkRequested, BulkRefused}

// BulkRefusal is what the single operation would have answered for one requirement of a bulk one.
type BulkRefusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RequirementTransitions is what the caller may take now on one requirement of a bulk read.
type RequirementTransitions struct {
	Key         string               `json:"key"`
	Transitions []workflow.Available `json:"transitions"`
}

// BulkTransitionInput takes one transition on several requirements of a project, each on its own.
type BulkTransitionInput struct {
	TransitionID uuid.UUID `json:"transitionId"`
	Comment      string    `json:"comment,omitempty"`
	Keys         []string  `json:"keys"`
}

// BulkTransitionResult is one requirement's outcome: the requirement as it is now, or the refusal.
type BulkTransitionResult struct {
	Key         string       `json:"key"`
	Outcome     BulkOutcome  `json:"outcome"`
	Requirement *Requirement `json:"requirement"`
	Error       *BulkRefusal `json:"error"`
}

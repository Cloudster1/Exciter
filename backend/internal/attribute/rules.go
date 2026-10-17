package attribute

import "github.com/google/uuid"

// MaxPermits bounds the role and status pairs that say who changes one field (#206).
const MaxPermits = 20

// FixedFields are a requirement's own fields that take rules, beside its attributes.
var FixedFields = []string{"title", "statement", "rationale", "type", "owner", "priority"}

// Permit lets the holders of any of Roles change a field while the requirement is in one of Statuses, every status
// when empty.
type Permit struct {
	Roles    []string    `json:"roles"`
	Statuses []uuid.UUID `json:"statuses"`
}

// Dependency narrows a select attribute's options by the option another select attribute holds; an option of the
// other that Choices does not name allows every option.
type Dependency struct {
	AttributeID uuid.UUID              `json:"attributeId"`
	Choices     map[string][]uuid.UUID `json:"choices"`
}

// FieldRule is when a fixed field is required and who changes it.
type FieldRule struct {
	Field string `json:"field"`
	// RequiredFrom is the status from which on it needs a value, in the project's status order; null for never.
	RequiredFrom *uuid.UUID `json:"requiredFrom"`
	// EditableBy empty leaves the field to whoever may edit the requirement.
	EditableBy []Permit `json:"editableBy"`
}

// FieldRuleInput sets a fixed field's rule whole.
type FieldRuleInput struct {
	RequiredFrom *uuid.UUID `json:"requiredFrom"`
	EditableBy   []Permit   `json:"editableBy"`
}

// Package attribute keeps a project's requirement types and the attributes
// each carries (#14); docs/api-contract-m1.md says how.
package attribute

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Limits on what a person types, matching the web client's.
const (
	MaxNameLength        = 60
	MaxDescriptionLength = 500
	MaxOptions           = 100
	MaxTextLength        = 500
	MaxMultiSelect       = 50
	MaxTypes             = 50
	MaxAttributes        = 100
)

// Type is a kind of requirement a project distinguishes, such as Functional or Interface.
type Type struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	// Icon names one of the web client's type icons; Colour is a #rrggbb.
	Icon     string `json:"icon"`
	Colour   string `json:"colour"`
	Position int    `json:"position"`
	// WorkflowID is the type's own workflow, null to follow the project's.
	WorkflowID *uuid.UUID `json:"workflowId"`
	// Retired types are kept for the requirements and revisions that have
	// them, and new requirements cannot take them.
	Retired      bool      `json:"retired"`
	Requirements int       `json:"requirements"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// TypeInput makes a type, or changes one where nil leaves a field alone. The
// workflow is set with PUT /projects/{projectKey}/workflow, which maps statuses.
type TypeInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	Colour      *string `json:"colour,omitempty"`
	// Position moves it among the project's types, from 0.
	Position *int  `json:"position,omitempty"`
	Retired  *bool `json:"retired,omitempty"`
}

// Kind is what shape an attribute's values take.
type Kind string

const (
	// Text is a string of at most MaxTextLength characters.
	Text Kind = "text"
	// RichText is a document of the shared editor.
	RichText Kind = "richText"
	// Number is a JSON number.
	Number Kind = "number"
	// Select is the id of one option.
	Select Kind = "select"
	// MultiSelect is an array of option ids, in the attribute's order.
	MultiSelect Kind = "multiSelect"
	// Date is a day, YYYY-MM-DD.
	Date Kind = "date"
	// User is the id of a member of the installation.
	User Kind = "user"
	// Boolean is true or false.
	Boolean Kind = "boolean"
	// URL is an absolute http or https address.
	URL Kind = "url"
)

// Kinds lists every Kind, in the order the chooser offers them.
var Kinds = []Kind{Text, RichText, Number, Select, MultiSelect, Date, User, Boolean, URL}

// KindInfo describes a kind for the client, which draws the control from it.
type KindInfo struct {
	Kind        Kind   `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// HasOptions says the attribute is defined with a list of choices.
	HasOptions bool `json:"hasOptions"`
}

// Option is one choice of a select or multi-select attribute; values name it by id.
type Option struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Retired options stay on the values that chose them and cannot be chosen again.
	Retired bool `json:"retired"`
}

// OptionInput is one option as the form sends it; without an id it is new.
type OptionInput struct {
	ID      *uuid.UUID `json:"id,omitempty"`
	Name    string     `json:"name"`
	Retired bool       `json:"retired,omitempty"`
}

// Def is one attribute a project records about its requirements.
type Def struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Kind        Kind      `json:"kind"`
	// Required refuses a save of a requirement of a type it shows on without a value.
	Required bool     `json:"required"`
	Options  []Option `json:"options"`
	// TypeIDs are the types it shows on; empty is every type.
	TypeIDs  []uuid.UUID `json:"typeIds"`
	Position int         `json:"position"`
	// Retired attributes leave forms and lists; their values stay on requirements and revisions.
	Retired bool `json:"retired"`
	// RequiredFrom is the status from which on it needs a value, in the project's status order (#206); null for none.
	RequiredFrom *uuid.UUID `json:"requiredFrom"`
	// DependsOn narrows a select's options by another select's option, null when it depends on none.
	DependsOn *Dependency `json:"dependsOn"`
	// EditableBy empty leaves it to whoever may edit the requirement.
	EditableBy []Permit  `json:"editableBy"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// DefInput makes an attribute, or changes one where nil leaves a field alone;
// the kind is set once.
type DefInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Kind        Kind    `json:"kind,omitempty"`
	Required    *bool   `json:"required,omitempty"`
	// Options replace the list; an option left out keeps its id and is retired.
	Options  *[]OptionInput `json:"options,omitempty"`
	TypeIDs  *[]uuid.UUID   `json:"typeIds,omitempty"`
	Position *int           `json:"position,omitempty"`
	Retired  *bool          `json:"retired,omitempty"`
	// RequiredFrom and DependsOn set to null clear them (#206).
	RequiredFrom json.RawMessage `json:"requiredFrom,omitempty"`
	DependsOn    json.RawMessage `json:"dependsOn,omitempty"`
	EditableBy   *[]Permit       `json:"editableBy,omitempty"`
}

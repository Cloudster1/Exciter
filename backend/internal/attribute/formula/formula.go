// Package formula is the language a computed attribute is written in (#213): arithmetic, conditions, text and dates
// over a requirement's own fields and attributes, and aggregates over its children or linked requirements.
package formula

import (
	"encoding/json"
)

// Agreed limits of a formula (#242).
const (
	MaxLength = 2000
	// MaxDepth bounds how deeply parentheses, calls and operators nest.
	MaxDepth = 32
	// MaxSteps bounds how often one value is worked out again while what it reads keeps changing, as on a cycle of
	// links an acyclic type let through before it was acyclic.
	MaxSteps = 64
)

// Kind is the kind of value a formula gives.
type Kind string

const (
	Number  Kind = "number"
	Text    Kind = "text"
	Boolean Kind = "boolean"
	Date    Kind = "date"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{Number, Text, Boolean, Date}

// Aggregates are the functions that read many requirements.
var Aggregates = []string{"count", "sum", "min", "max", "avg", "any", "all"}

// Error is what is wrong with a formula, and where: from and to count characters from 1.
type Error struct {
	From    int    `json:"from"`
	To      int    `json:"to"`
	Message string `json:"message"`
}

// CheckInput is a formula as it is written, and the requirement to try it on.
type CheckInput struct {
	Formula string `json:"formula"`
	// OnSections says it is worked out for sections too, which have no type and no attributes of their own.
	OnSections bool `json:"onSections"`
	// AttributeID names the computed attribute being changed, which a formula may not read through itself.
	AttributeID string `json:"attributeId,omitempty"`
	// RequirementKey names a requirement or section of the project to work it out for.
	RequirementKey string `json:"requirementKey,omitempty"`
}

// Check says whether a formula holds, what kind of value it gives and what it reads, and its value for the
// requirement it was tried on.
type Check struct {
	// Formula is the formula as it will be kept and shown, with names spelled as the project spells them.
	Formula string  `json:"formula"`
	Kind    Kind    `json:"kind,omitempty"`
	Errors  []Error `json:"errors"`
	// Reads are the attributes it reads, by name, and the link types it follows, by key.
	Reads     []string `json:"reads"`
	LinkTypes []string `json:"linkTypes"`
	// Value is its value for the requirement tried, null when it has none; ValueError says why it could not be had.
	Value      json.RawMessage `json:"value"`
	ValueError string          `json:"valueError,omitempty"`
}

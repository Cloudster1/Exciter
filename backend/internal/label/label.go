// Package label puts the organization's labels on requirements (#19), as
// Stator puts them on pages; docs/api-contract-m1.md says how.
package label

import (
	"time"

	"github.com/google/uuid"
)

const (
	MaxNameLength      = 40
	MaxPerRequirement  = 50
	DefaultSuggestions = 10
	MaxSuggestions     = 50
	DefaultLimit       = 20
	MaxLimit           = 100
)

// Suggestion is a label in use, and on how many requirements the caller may view.
type Suggestion struct {
	Name         string `json:"name"`
	Requirements int    `json:"requirements"`
}

// Labelled is a requirement that carries a label, with all of its labels.
type Labelled struct {
	ID            uuid.UUID `json:"id"`
	Key           string    `json:"key"`
	ProjectKey    string    `json:"projectKey"`
	Title         string    `json:"title"`
	Labels        []string  `json:"labels"`
	UpdatedByName string    `json:"updatedByName"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Input names a label to add; it is normalized first, so "Safety Critical" is safety-critical.
type Input struct {
	Name string `json:"name"`
}

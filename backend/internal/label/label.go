// Package label keeps the organization's labels on requirements (#19), as Stator keeps them on pages; a label
// exists while some requirement carries it. docs/api-contract-m1.md says how.
package label

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits on a name, on a requirement and on how much one answer carries.
const (
	// MaxNameLength keeps a label a word, as Stator does.
	MaxNameLength      = 40
	MaxPerRequirement  = 50
	DefaultSuggestions = 10
	MaxSuggestions     = 50
	DefaultLimit       = 20
	MaxLimit           = 100
)

var (
	// ErrBadName refuses a name Normalize cannot make a label of.
	ErrBadName = errors.New("use only letters, digits, hyphens, underscores and dots in a label, and start it with a letter or a digit")
	// ErrTooLong refuses a name longer than MaxNameLength.
	ErrTooLong = fmt.Errorf("keep a label to %d characters", MaxNameLength)
	// ErrTooMany refuses a label past MaxPerRequirement on one requirement.
	ErrTooMany = fmt.Errorf("a requirement carries at most %d labels; remove one first", MaxPerRequirement)
)

// FieldError is a refusal of one field of the request, shown next to it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// fieldError shows err beside field as the sentence a person reads.
func fieldError(field string, err error) *FieldError {
	text := err.Error()
	return &FieldError{Field: field, Message: strings.ToUpper(text[:1]) + text[1:] + "."}
}

// Normalize makes a label of what was typed: trimmed, lower case, spaces as hyphens, then only letters,
// digits, - _ and ., led by a letter or digit.
func Normalize(raw string) (string, error) {
	name := strings.Join(strings.Fields(strings.ToLower(raw)), "-")
	if name == "" {
		return "", ErrBadName
	}
	for i, r := range name {
		word := unicode.IsLetter(r) || unicode.IsDigit(r)
		if i == 0 && !word {
			return "", ErrBadName
		}
		if !word && r != '-' && r != '_' && r != '.' {
			return "", ErrBadName
		}
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return "", ErrTooLong
	}
	return name, nil
}

// Field normalizes a label sent on field, refusing it there; labels on a new requirement and a route's name
// are both read this way.
func Field(field, raw string) (string, error) {
	name, err := Normalize(raw)
	if err != nil {
		return "", fieldError(field, err)
	}
	return name, nil
}

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
	ProjectName   string    `json:"projectName"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	Labels        []string  `json:"labels"`
	UpdatedByName string    `json:"updatedByName"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Input names a label to add; it is normalized first, so "Safety Critical" is safety-critical.
type Input struct {
	Name string `json:"name"`
}

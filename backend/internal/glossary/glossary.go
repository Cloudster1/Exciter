// Package glossary keeps a project's terms (#210), each defined once with its synonyms and the words not to use
// instead, and the requirements that use them.
package glossary

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Agreed limits of the glossary (#233).
const (
	MaxTerms      = 5000
	MaxNameLength = 120
	// MaxSynonyms bounds a term's synonyms, MaxAvoid the words it says not to use instead.
	MaxSynonyms     = 20
	MaxAvoid        = 20
	MaxCommentChars = 500
	// MaxLibraries bounds the library projects whose glossaries one project uses.
	MaxLibraries = 20
	// PreviewChars bounds the definition a mark shows on hover.
	PreviewChars = 300
	DefaultLimit = 50
	MaxLimit     = 500
)

// Status is where a term stands; a deprecated term is still marked, and left out of exports.
type Status string

const (
	StatusProposed   Status = "proposed"
	StatusApproved   Status = "approved"
	StatusDeprecated Status = "deprecated"
)

// Statuses lists every Status, for the API document.
var Statuses = []Status{StatusProposed, StatusApproved, StatusDeprecated}

// TermRef is a term as a requirement or a hit names it.
type TermRef struct {
	ID         uuid.UUID `json:"id"`
	ProjectKey string    `json:"projectKey"`
	Name       string    `json:"name"`
	Status     Status    `json:"status"`
}

// Term is one term of a glossary.
type Term struct {
	TermRef
	// Definition is a document of the shared editor.
	Definition json.RawMessage `json:"definition"`
	Synonyms   []string        `json:"synonyms"`
	Avoid      []string        `json:"avoid"`
	Revision   int             `json:"revision"`
	// DefinitionRevision is the revision its name or definition last changed in, which makes its uses suspect.
	DefinitionRevision int                   `json:"definitionRevision"`
	Uses               int                   `json:"uses"`
	CreatedBy          requirement.PersonRef `json:"createdBy"`
	UpdatedBy          requirement.PersonRef `json:"updatedBy"`
	CreatedAt          time.Time             `json:"createdAt"`
	UpdatedAt          time.Time             `json:"updatedAt"`
}

// TermInput makes a term.
type TermInput struct {
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
	Synonyms   []string        `json:"synonyms"`
	Avoid      []string        `json:"avoid"`
	// Status is proposed when absent.
	Status Status `json:"status,omitempty"`
}

// TermPatch changes a term as a new revision; a field left out keeps its value.
type TermPatch struct {
	BaseRevision int              `json:"baseRevision"`
	Name         *string          `json:"name,omitempty"`
	Definition   *json.RawMessage `json:"definition,omitempty"`
	Synonyms     *[]string        `json:"synonyms,omitempty"`
	Avoid        *[]string        `json:"avoid,omitempty"`
	Status       *Status          `json:"status,omitempty"`
	Comment      string           `json:"comment,omitempty"`
}

// TermRevision is one revision of a term.
type TermRevision struct {
	Number     int                   `json:"number"`
	Name       string                `json:"name"`
	Definition json.RawMessage       `json:"definition"`
	Synonyms   []string              `json:"synonyms"`
	Avoid      []string              `json:"avoid"`
	Status     Status                `json:"status"`
	Comment    string                `json:"comment"`
	CreatedBy  requirement.PersonRef `json:"createdBy"`
	CreatedAt  time.Time             `json:"createdAt"`
}

// Use is a requirement that uses a term, as the term lists it.
type Use struct {
	Requirement requirement.Ref `json:"requirement"`
	// Explicit says an author linked words of it to the term.
	Explicit bool `json:"explicit"`
	// SeenRevision is the term's revision its owner last confirmed; Suspect says the definition changed since.
	SeenRevision int  `json:"seenRevision"`
	Suspect      bool `json:"suspect"`
}

// RequirementTerm is a term a requirement uses, as the requirement lists it.
type RequirementTerm struct {
	Term               TermRef `json:"term"`
	Explicit           bool    `json:"explicit"`
	SeenRevision       int     `json:"seenRevision"`
	DefinitionRevision int     `json:"definitionRevision"`
	Suspect            bool    `json:"suspect"`
}

// ClearInput confirms that a requirement still holds under a term's current definition.
type ClearInput struct {
	TermRevision int    `json:"termRevision"`
	Comment      string `json:"comment"`
}

// Library is a library project whose glossary a project uses.
type Library struct {
	ProjectKey string `json:"projectKey"`
	Name       string `json:"name"`
	Position   int    `json:"position"`
	Terms      int    `json:"terms"`
}

// GlossariesInput names the library projects whose glossaries a project uses, the first winning a word two share.
type GlossariesInput struct {
	Libraries []string `json:"libraries"`
}

// Glossaries are the library glossaries a project uses.
type Glossaries struct {
	Libraries []Library  `json:"libraries"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// Hit is a term a search finds.
type Hit struct {
	TermRef
	Snippet string `json:"snippet"`
}

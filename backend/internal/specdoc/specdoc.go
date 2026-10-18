// Package specdoc reads a specification as one document to sign and send (#45): a project, a section and what is
// below it, or a baseline as it was taken. The web client's print view draws it for PDF; the Word writer writes it.
package specdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// MaxEntries bounds the requirements one document holds; past it the document says so and ends.
const MaxEntries = 1000

// Kind says what a document is of.
type Kind string

const (
	KindProject  Kind = "project"
	KindSection  Kind = "section"
	KindBaseline Kind = "baseline"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{KindProject, KindSection, KindBaseline}

// The fields every requirement has, which a document lists before its attributes.
const (
	FieldType     = "type"
	FieldStatus   = "status"
	FieldOwner    = "owner"
	FieldPriority = "priority"
	FieldLabels   = "labels"
)

// Fields lists the fields in the order a document shows them, for the API document.
var Fields = []string{FieldType, FieldStatus, FieldOwner, FieldPriority, FieldLabels}

// ErrNoSection is a document of a section the project does not have.
var ErrNoSection = errors.New("no such requirement to export")

// Document is a specification as it is signed and sent.
type Document struct {
	Kind        Kind   `json:"kind"`
	Title       string `json:"title"`
	ProjectKey  string `json:"projectKey"`
	ProjectName string `json:"projectName"`
	// SectionKey is the key of the section a section's document starts at, empty for the others.
	SectionKey string        `json:"sectionKey"`
	Baseline   *BaselineInfo `json:"baseline"`
	ExportedAt time.Time     `json:"exportedAt"`
	ExportedBy string        `json:"exportedBy"`
	Entries    []Entry       `json:"entries"`
	// Total counts the requirements there are; past MaxEntries the document holds the first of them.
	Total int `json:"total"`
	// Glossary is the terms the document defines (#210): a project's, and its libraries' the reader may read, out
	// of deprecation; a section's, those its requirements use; a baseline's, none yet.
	Glossary []GlossaryTerm `json:"glossary"`
}

// GlossaryTerm is a term as a document defines it, by name.
type GlossaryTerm struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Definition  json.RawMessage `json:"definition"`
	Synonyms    []string        `json:"synonyms"`
	Avoid       []string        `json:"avoid"`
	Status      string          `json:"status"`
	ProjectKey  string          `json:"projectKey"`
	ProjectName string          `json:"projectName"`
}

// BaselineInfo is what a baseline's document says of the baseline on its cover.
type BaselineInfo struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	TakenAt     time.Time  `json:"takenAt"`
	TakenBy     string     `json:"takenBy"`
	LockedAt    *time.Time `json:"lockedAt"`
	// Signatures counts those given and SignaturesRequired those that release it (#205), 0 while nobody signs it.
	Signatures         int  `json:"signatures"`
	SignaturesRequired int  `json:"signaturesRequired"`
	Signed             bool `json:"signed"`
}

// Entry is one requirement or section of a document, at the revision the document holds.
type Entry struct {
	ID        uuid.UUID        `json:"id"`
	Key       string           `json:"key"`
	Kind      requirement.Kind `json:"kind"`
	Outline   string           `json:"outline"`
	Depth     int              `json:"depth"`
	Title     string           `json:"title"`
	Revision  int              `json:"revision"`
	Statement json.RawMessage  `json:"statement"`
	// Rationale is null when it has none.
	Rationale  json.RawMessage `json:"rationale"`
	Properties []Property      `json:"properties"`
	Links      []Link          `json:"links"`
}

// Property is one value of a requirement's table: a field, or an attribute by its name.
type Property struct {
	// Field is one of Fields, empty for an attribute.
	Field string `json:"field"`
	// Name is the attribute's name, empty for a field.
	Name string `json:"name"`
	// Text is the value as a reader reads it; Document holds a rich text attribute's, Flag a yes or no.
	Text     string          `json:"text"`
	Document json.RawMessage `json:"document"`
	Flag     *bool           `json:"flag"`
}

// Link is one trace link of a requirement, read from it: the phrase, then the requirement at its other end.
type Link struct {
	Phrase string `json:"phrase"`
	Key    string `json:"key"`
	Title  string `json:"title"`
}

// Truncated says the document holds fewer requirements than there are.
func (d *Document) Truncated() bool { return len(d.Entries) < d.Total }

// signingSentence says where the baseline's signing stands, in a document's head (#205); empty while nobody signed it.
func (b *BaselineInfo) signingSentence() string {
	switch {
	case b.Signatures == 0:
		return ""
	case b.Signed:
		return fmt.Sprintf(" Signed, with %d of %d signatures.", b.Signatures, b.SignaturesRequired)
	}
	return fmt.Sprintf(" Being signed: %d of %d signatures so far.", b.Signatures, b.SignaturesRequired)
}

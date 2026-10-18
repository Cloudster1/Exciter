// Package exporttemplate keeps the Word templates a specification is exported through (#214): the file whose styles,
// cover, headers and footers the export takes, and which fields each requirement shows.
package exporttemplate

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Agreed limits of templates (#242).
const (
	MaxNameLength        = 80
	MaxDescriptionLength = 500
	// MaxFileMB bounds the upload, and MaxUnpackedMB what it unpacks into.
	MaxFileMB     = 20
	MaxUnpackedMB = 64
	// MaxTemplates bounds the templates one project keeps.
	MaxTemplates = 50
	MaxFields    = 60
)

// Placeholder is a word in double braces a template's text, headers and footers carry, filled in on export.
type Placeholder string

const (
	PlaceProject    Placeholder = "project"
	PlaceTitle      Placeholder = "title"
	PlaceSection    Placeholder = "section"
	PlaceBaseline   Placeholder = "baseline"
	PlaceDate       Placeholder = "date"
	PlaceExportedBy Placeholder = "exportedBy"
	PlaceSigners    Placeholder = "signers"
	// PlaceContent stands alone in a paragraph of the body, where the contents and the requirements go.
	PlaceContent Placeholder = "content"
)

// Placeholders lists every Placeholder, for the API document.
var Placeholders = []Placeholder{PlaceProject, PlaceTitle, PlaceSection, PlaceBaseline, PlaceDate, PlaceExportedBy, PlaceSigners, PlaceContent}

// Field is one of a requirement's fields a template may show.
type Field string

const (
	FieldType      Field = "type"
	FieldStatus    Field = "status"
	FieldOwner     Field = "owner"
	FieldPriority  Field = "priority"
	FieldLabels    Field = "labels"
	FieldRevision  Field = "revision"
	FieldRationale Field = "rationale"
	// FieldAttribute is an attribute, named by Attribute, since a shared template serves projects whose ids differ.
	FieldAttribute Field = "attribute"
)

// Fields lists every Field, for the API document.
var Fields = []Field{FieldType, FieldStatus, FieldOwner, FieldPriority, FieldLabels, FieldRevision, FieldRationale, FieldAttribute}

// FieldRef is one field a requirement shows, in the template's order.
type FieldRef struct {
	Field     Field  `json:"field"`
	Attribute string `json:"attribute,omitempty"`
}

// Include says what a requirement brings besides its fields.
type Include struct {
	Links    bool `json:"links"`
	Comments bool `json:"comments"`
	// ChangeMarks marks what changed since a baseline the export names, as tracked changes.
	ChangeMarks bool `json:"changeMarks"`
}

// Can says what the reader may do with a template.
type Can struct {
	Edit bool `json:"edit"`
}

// Template is a Word template, its fields and what it includes.
type Template struct {
	ID          uuid.UUID `json:"id"`
	ProjectKey  string    `json:"projectKey"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	// Shared offers it in every project, not only its own.
	Shared   bool   `json:"shared"`
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
	// Fields empty shows a requirement as an export without a template does.
	Fields  []FieldRef `json:"fields"`
	Include Include    `json:"include"`
	// Placeholders are the ones its file uses.
	Placeholders []Placeholder         `json:"placeholders"`
	CreatedBy    requirement.PersonRef `json:"createdBy"`
	CreatedAt    time.Time             `json:"createdAt"`
	UpdatedAt    time.Time             `json:"updatedAt"`
	Can          Can                   `json:"can"`
}

// Form is the upload's fields beside the file.
type Form struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Patch changes a template; nil leaves a field alone. The file is changed by uploading a new template.
type Patch struct {
	Name        *string     `json:"name,omitempty"`
	Description *string     `json:"description,omitempty"`
	Shared      *bool       `json:"shared,omitempty"`
	Fields      *[]FieldRef `json:"fields,omitempty"`
	Include     *Include    `json:"include,omitempty"`
}

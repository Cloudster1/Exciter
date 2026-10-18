// Package project keeps the projects requirements live in (#11) and the
// templates a project starts from (#24); docs/api-contract-m1.md says how.
package project

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/perm"
)

// Limits on what a person types, matching the web client's.
const (
	MaxNameLength        = 100
	MaxDescriptionLength = 1000
	MaxKeyLength         = 10
)

// Project is one project as the list and its pages see it.
type Project struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Settings    Settings  `json:"settings"`
	// Template is the key of the template the project began from, empty for a blank one.
	Template string `json:"template"`
	// FormerKeys are keys the project had, which still resolve, the latest first.
	FormerKeys []string `json:"formerKeys"`
	// Requirements counts the requirements and sections out of the trash.
	Requirements int       `json:"requirements"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	// ArchivedAt is when the project was archived, null while it is not; an
	// archived project stays readable and nothing in it changes.
	ArchivedAt     *time.Time `json:"archivedAt"`
	ArchivedByName string     `json:"archivedByName"`
	// Can says what the reader may do here, so the interface offers only that.
	Can perm.ProjectCan `json:"can"`
	// Roles are the reader's project roles, which a field's permits name (#206).
	Roles []string `json:"roles"`
	// Watching says the reader follows every requirement of the project (#34).
	Watching bool `json:"watching"`
}

// Settings are the project's choices that are not a list of their own.
type Settings struct {
	// DefaultTypeID is the requirement type a new requirement takes when none is given.
	DefaultTypeID *uuid.UUID `json:"defaultTypeId"`
	// WorkflowID is the workflow every requirement type without its own follows.
	WorkflowID *uuid.UUID `json:"workflowId"`
	// RequireRevisionComment refuses a save that writes a revision without a comment.
	RequireRevisionComment bool `json:"requireRevisionComment"`
	// SignedApprovals makes deciding on a revision an electronic signature, given with the reviewer's credentials (#46).
	SignedApprovals bool `json:"signedApprovals"`
	// Library offers the project's requirements for reuse in other projects (#49).
	Library bool `json:"library"`
	// Language is what the project's requirements are written in, en or de, which picks the writing rules' words
	// (#207); empty reads as en.
	Language string `json:"language,omitempty"`
	// HomeDashboardID is the shared dashboard the project's home shows (#212), null for none.
	HomeDashboardID *uuid.UUID `json:"homeDashboardId"`
}

// Languages a project's requirements may be written in; DefaultLanguage until an administrator chooses.
var Languages = []string{"en", "de"}

const DefaultLanguage = "en"

// TopicQualityRecheck asks the worker to check a project's requirements again, as a changed language needs (#207).
const TopicQualityRecheck = "quality.recheck"

// LanguageOf is the language a project's requirements are written in.
func (s Settings) LanguageOf() string {
	if s.Language == "" {
		return DefaultLanguage
	}
	return s.Language
}

// SettingsInput changes settings; nil leaves one alone. The workflow is
// changed with PUT /projects/{projectKey}/workflow, which maps statuses.
type SettingsInput struct {
	DefaultTypeID          *uuid.UUID `json:"defaultTypeId,omitempty"`
	RequireRevisionComment *bool      `json:"requireRevisionComment,omitempty"`
	SignedApprovals        *bool      `json:"signedApprovals,omitempty"`
	Library                *bool      `json:"library,omitempty"`
	Language               *string    `json:"language,omitempty"`
	// HomeDashboardID names a shared dashboard of the project for its home; null clears it.
	HomeDashboardID json.RawMessage `json:"homeDashboardId,omitempty"`
}

// CreateInput is a new project as the form sends it.
type CreateInput struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Template names a template from GET /project-templates; empty makes a
	// project with one type, the default workflow and no sections.
	Template string `json:"template,omitempty"`
}

// UpdateInput changes a project's details; nil leaves a field alone. The key
// changes through PUT /projects/{projectKey}/key.
type UpdateInput struct {
	Name        *string        `json:"name,omitempty"`
	Description *string        `json:"description,omitempty"`
	Settings    *SettingsInput `json:"settings,omitempty"`
}

// KeyInput gives a project a new key.
type KeyInput struct {
	Key string `json:"key"`
}

// KeyCheck says whether a key may be used, or suggests one for a name.
type KeyCheck struct {
	// Key is the key asked about, normalized; empty when a name was asked about.
	Key string `json:"key"`
	// Valid says the key has the right shape; Available that no project has
	// it now or had it before.
	Valid     bool `json:"valid"`
	Available bool `json:"available"`
	// Suggestion is a free key derived from the name, empty without one.
	Suggestion string `json:"suggestion"`
}

// Template is a structure a new project can start with.
type Template struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Types, Attributes, Statuses, LinkTypes and Sections name what the template
	// makes, for the chooser to show; the definitions live in the code.
	Types      []string `json:"types"`
	Attributes []string `json:"attributes"`
	Statuses   []string `json:"statuses"`
	LinkTypes  []string `json:"linkTypes"`
	Sections   []string `json:"sections"`
}

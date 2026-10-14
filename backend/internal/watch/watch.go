// Package watch keeps who follows a requirement or a project (#34); docs/api-contract-m3.md says how, Stator's
// watching over requirements.
package watch

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Agreed limits.
const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// Kind says what a watch follows.
type Kind string

const (
	KindRequirement Kind = "requirement"
	KindProject     Kind = "project"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{KindRequirement, KindProject}

// Watching is how the caller follows one requirement: by itself, through its project, or not.
type Watching struct {
	Requirement bool `json:"requirement"`
	Project     bool `json:"project"`
}

// Watch is one thing the caller follows.
type Watch struct {
	ID          uuid.UUID        `json:"id"`
	Kind        Kind             `json:"kind"`
	Requirement *requirement.Ref `json:"requirement"`
	ProjectKey  string           `json:"projectKey"`
	CreatedAt   time.Time        `json:"createdAt"`
}

// Watcher is somebody who follows a requirement, and how.
type Watcher struct {
	UserID uuid.UUID `json:"userId"`
	Name   string    `json:"name"`
	Email  string    `json:"email"`
	Via    Kind      `json:"via"`
}

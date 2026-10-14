// Package baseline keeps named, frozen snapshots of a project or a section and compares them (#37);
// docs/api-contract-m3.md says how. Content is never copied: an item names a revision.
package baseline

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Agreed limits.
const (
	MaxNameLength        = 100
	MaxDescriptionLength = 2000
	DefaultLimit         = 50
	MaxLimit             = 500
)

// Baseline is one snapshot of a project, or of the subtree of one section.
type Baseline struct {
	ID          uuid.UUID        `json:"id"`
	ProjectKey  string           `json:"projectKey"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Root        *requirement.Ref `json:"root"`
	Items       int              `json:"items"`
	// Locked baselines keep their name and are never deleted; no baseline's items ever change.
	Locked        bool       `json:"locked"`
	LockedAt      *time.Time `json:"lockedAt"`
	CreatedByName string     `json:"createdByName"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// Input takes a baseline of the project, or of the section RootKey names and everything below it.
type Input struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	RootKey     string `json:"rootKey,omitempty"`
}

// UpdateInput renames a baseline or locks it; a locked one is never unlocked.
type UpdateInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Locked      *bool   `json:"locked,omitempty"`
}

// Entry is one requirement as the baseline holds it, read as a specification.
type Entry struct {
	Requirement requirement.Ref        `json:"requirement"`
	Outline     string                 `json:"outline"`
	Depth       int                    `json:"depth"`
	Statement   any                    `json:"statement"`
	Rationale   any                    `json:"rationale"`
	Status      *requirement.StatusRef `json:"status"`
}

// Side is one side of a comparison: a baseline, or now when Baseline is null.
type Side struct {
	Baseline *Baseline `json:"baseline"`
	At       time.Time `json:"at"`
}

// Change says how a requirement differs between the two sides.
type Change string

const (
	Added   Change = "added"
	Removed Change = "removed"
	Changed Change = "changed"
)

// Changes lists every Change, for the API document.
var Changes = []Change{Added, Removed, Changed}

// ItemSide is a requirement as one side holds it.
type ItemSide struct {
	Revision int                    `json:"revision"`
	Title    string                 `json:"title"`
	Outline  string                 `json:"outline"`
	Status   *requirement.StatusRef `json:"status"`
}

// ItemChange is a requirement that differs; Revised, Moved and StatusChanged say how a changed one does, and the
// revisions name the diff of #16 to show.
type ItemChange struct {
	Key           string    `json:"key"`
	Change        Change    `json:"change"`
	Revised       bool      `json:"revised"`
	Moved         bool      `json:"moved"`
	StatusChanged bool      `json:"statusChanged"`
	From          *ItemSide `json:"from"`
	To            *ItemSide `json:"to"`
}

// LinkChange is a trace link one side has and the other has not.
type LinkChange struct {
	Change    Change `json:"change"`
	TypeName  string `json:"typeName"`
	SourceKey string `json:"sourceKey"`
	TargetKey string `json:"targetKey"`
}

// ExternalChange is an Armature link one side has and the other has not.
type ExternalChange struct {
	Change         Change `json:"change"`
	RequirementKey string `json:"requirementKey"`
	IssueKey       string `json:"issueKey"`
	Relation       string `json:"relation"`
}

// Comparison is everything that differs between two sides, requirements in the later side's outline order.
type Comparison struct {
	From          Side             `json:"from"`
	To            Side             `json:"to"`
	Items         []ItemChange     `json:"items"`
	Links         []LinkChange     `json:"links"`
	ExternalLinks []ExternalChange `json:"externalLinks"`
}

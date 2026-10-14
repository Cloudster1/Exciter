// Package trace keeps typed, directed links between requirements and the
// types they come in (#18); docs/api-contract-m1.md says how.
package trace

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Limits on link types, matching the web client's.
const (
	MaxKeyLength    = 40
	MaxNameLength   = 40
	MaxPhraseLength = 60
	// MaxLinkTypes bounds the installation's types, and each project's own.
	MaxLinkTypes = 50
	// MaxRules bounds the pairs of requirement types one link type joins in a project.
	MaxRules = 50
	// MaxLinksPerRequirement bounds the links one requirement holds in either direction.
	MaxLinksPerRequirement = 500
	// MaxCycleDepth bounds how far the cycle guard walks an acyclic type.
	MaxCycleDepth = 64
)

// SuspectEnd says which end's new revision makes a link of a type suspect (#36).
type SuspectEnd string

const (
	SuspectTarget SuspectEnd = "target"
	SuspectSource SuspectEnd = "source"
	SuspectBoth   SuspectEnd = "both"
	SuspectNone   SuspectEnd = "none"
)

// SuspectEnds lists every SuspectEnd, for the API document.
var SuspectEnds = []SuspectEnd{SuspectTarget, SuspectSource, SuspectBoth, SuspectNone}

// Counts says what a link of a type counts for in coverage (#38).
type Counts string

const (
	// CountsDecomposition is a source taking part of its target's content further down.
	CountsDecomposition Counts = "decomposition"
	// CountsVerification is a source showing its target is met.
	CountsVerification Counts = "verification"
	CountsNothing      Counts = "nothing"
)

// CountsValues lists every Counts, for the API document.
var CountsValues = []Counts{CountsDecomposition, CountsVerification, CountsNothing}

// Scope says whether a link type is the installation's or one project's.
type Scope string

const (
	ScopeOrg     Scope = "org"
	ScopeProject Scope = "project"
)

// Scopes lists every Scope, for the API document.
var Scopes = []Scope{ScopeOrg, ScopeProject}

// TypeRule allows links of a type from requirements of one type to another; a
// null end is any type.
type TypeRule struct {
	SourceTypeID *uuid.UUID `json:"sourceTypeId"`
	TargetTypeID *uuid.UUID `json:"targetTypeId"`
}

// LinkType is a kind of link, read from its source: the source derives from the target.
type LinkType struct {
	ID uuid.UUID `json:"id"`
	// Key is the stable name scripts and imports use, such as derives.
	Key  string `json:"key"`
	Name string `json:"name"`
	// Forward is the phrase from the source, Backward from the target.
	Forward  string `json:"forward"`
	Backward string `json:"backward"`
	// Acyclic types refuse a link that would come back around to its source.
	Acyclic    bool       `json:"acyclic"`
	SuspectEnd SuspectEnd `json:"suspectEnd"`
	Counts     Counts     `json:"counts"`
	Scope      Scope      `json:"scope"`
	// ProjectKey is the project of a project's own type, empty for the installation's.
	ProjectKey string `json:"projectKey"`
	// Enabled and Rules are the project's when listed for one; elsewhere every
	// type is enabled and joins any types.
	Enabled bool       `json:"enabled"`
	Rules   []TypeRule `json:"rules"`
	Links   int        `json:"links"`
	// Retired types keep their links and take no new ones.
	Retired   bool      `json:"retired"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// LinkTypeInput makes a link type, or changes one where nil leaves a field
// alone; the key is set once.
type LinkTypeInput struct {
	Key        string      `json:"key,omitempty"`
	Name       *string     `json:"name,omitempty"`
	Forward    *string     `json:"forward,omitempty"`
	Backward   *string     `json:"backward,omitempty"`
	Acyclic    *bool       `json:"acyclic,omitempty"`
	SuspectEnd *SuspectEnd `json:"suspectEnd,omitempty"`
	Counts     *Counts     `json:"counts,omitempty"`
	Retired    *bool       `json:"retired,omitempty"`
}

// ProjectLinkTypeInput is how one project uses a link type.
type ProjectLinkTypeInput struct {
	Enabled bool       `json:"enabled"`
	Rules   []TypeRule `json:"rules"`
}

// Direction is which end of a link the requirement asked about is.
type Direction string

const (
	// Outgoing links have the requirement as their source.
	Outgoing Direction = "outgoing"
	Incoming Direction = "incoming"
)

// Directions lists every Direction, for the API document.
var Directions = []Direction{Outgoing, Incoming}

// Link is one link as seen from one of its ends.
type Link struct {
	ID        uuid.UUID `json:"id"`
	Type      LinkType  `json:"type"`
	Direction Direction `json:"direction"`
	// Phrase is the type's phrase read from this end, such as "is verified by".
	Phrase string `json:"phrase"`
	// Other is the requirement at the far end.
	Other requirement.Ref `json:"other"`
	// SourceRevision and TargetRevision are the revisions of the ends the link
	// was made, or last confirmed (#36), against.
	SourceRevision int `json:"sourceRevision"`
	TargetRevision int `json:"targetRevision"`
	// Suspect is the ends that moved on since, empty when the link holds.
	Suspect       []MovedEnd `json:"suspect"`
	CreatedAt     time.Time  `json:"createdAt"`
	CreatedByName string     `json:"createdByName"`
}

// LinkInput links the requirement in the path to another.
type LinkInput struct {
	// TypeID or TypeKey names the type; the key is matched in the project's enabled types.
	TypeID  *uuid.UUID `json:"typeId,omitempty"`
	TypeKey string     `json:"typeKey,omitempty"`
	// Direction says whether the requirement in the path is the source, the default, or the target.
	Direction Direction `json:"direction,omitempty"`
	OtherKey  string    `json:"otherKey"`
}

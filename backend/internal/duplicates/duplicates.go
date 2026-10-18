// Package duplicates finds requirements that say what another already says (#208), by the similarity of their words,
// and keeps what people decided about each pair.
package duplicates

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Agreed limits of duplicate finding (#233).
const (
	// MinThreshold and MaxThreshold bound the similarity a project's administrators set, DefaultThreshold until they
	// do; StoredThreshold is what a pair needs to be kept at all.
	MinThreshold     = 0.30
	MaxThreshold     = 0.95
	DefaultThreshold = 0.60
	StoredThreshold  = MinThreshold
	// MaxSimilar bounds the similar requirements a draft or a requirement lists.
	MaxSimilar = 10
	// Neighbours is how many most similar requirements each one keeps pairs with.
	Neighbours = 20
	// MaxTextChars is how much of a requirement's title and statement is compared.
	MaxTextChars   = 2000
	MaxReasonChars = 500
	DefaultLimit   = 50
	MaxLimit       = 200
)

// Kind is what was decided about a pair.
type Kind string

const (
	// Dismissed says the two do not say the same thing, until either is revised.
	Dismissed Kind = "dismissed"
	// Linked says they are related by a link, which holds while the link does.
	Linked Kind = "linked"
	// Merged says one went to the trash and its links to the other.
	Merged Kind = "merged"
)

// Kinds lists every Kind, for the API document.
var Kinds = []Kind{Dismissed, Linked, Merged}

// Similar is a requirement whose words are like another's or a draft's.
type Similar struct {
	Requirement requirement.Ref `json:"requirement"`
	Score       float64         `json:"score"`
	// Library says it is in a library project, which a project may reuse rather than say again.
	Library bool `json:"library"`
}

// DraftInput is a draft to compare as it is written.
type DraftInput struct {
	Title     string          `json:"title"`
	Statement json.RawMessage `json:"statement"`
	// ExcludeKey names the requirement being revised, which is not its own duplicate.
	ExcludeKey string `json:"excludeKey,omitempty"`
}

// Pair is two requirements of a project that may say the same thing.
type Pair struct {
	A     requirement.Ref `json:"a"`
	B     requirement.Ref `json:"b"`
	Score float64         `json:"score"`
}

// Side is one requirement of a pair at the revision a decision was made against.
type Side struct {
	Key      string `json:"key"`
	Revision int    `json:"revision"`
}

// DismissInput says a pair does not say the same thing.
type DismissInput struct {
	A      Side   `json:"a"`
	B      Side   `json:"b"`
	Reason string `json:"reason"`
}

// MergeInput trashes one side of a pair and keeps its links on the other.
type MergeInput struct {
	Keep    Side   `json:"keep"`
	Trash   Side   `json:"trash"`
	Comment string `json:"comment,omitempty"`
}

// LinkInput links the two sides of a pair with a link type of the project.
type LinkInput struct {
	Source      Side   `json:"source"`
	Target      Side   `json:"target"`
	LinkTypeKey string `json:"linkTypeKey"`
}

// Decision is what somebody decided about a pair.
type Decision struct {
	ID        uuid.UUID             `json:"id"`
	Kind      Kind                  `json:"kind"`
	A         Side                  `json:"a"`
	B         Side                  `json:"b"`
	Reason    string                `json:"reason"`
	LinkID    *uuid.UUID            `json:"linkId"`
	KeptKey   string                `json:"keptKey,omitempty"`
	DecidedBy requirement.PersonRef `json:"decidedBy"`
	DecidedAt time.Time             `json:"decidedAt"`
}

// SkipReason says why a link of the trashed side was not made again on the side kept.
type SkipReason string

const (
	// SkipSelf would link the side kept to itself.
	SkipSelf SkipReason = "self"
	// SkipDuplicate is a link the side kept has already.
	SkipDuplicate SkipReason = "duplicate"
	// SkipRetired is a link of a retired type.
	SkipRetired SkipReason = "retired"
	// SkipRefused is a link the type's rules refuse on the side kept.
	SkipRefused SkipReason = "refused"
)

// SkipReasons lists every SkipReason, for the API document.
var SkipReasons = []SkipReason{SkipSelf, SkipDuplicate, SkipRetired, SkipRefused}

// MovedLink is a link of the trashed side that the kept side now holds, or why it does not.
type MovedLink struct {
	LinkTypeKey string `json:"linkTypeKey"`
	OtherKey    string `json:"otherKey"`
	// Outgoing says the kept side is the link's source.
	Outgoing bool `json:"outgoing"`
	// Skipped says why the link was not moved, empty when it was.
	Skipped SkipReason `json:"skipped,omitempty"`
}

// Merge is what merging a pair did.
type Merge struct {
	Decision Decision    `json:"decision"`
	Links    []MovedLink `json:"links"`
}

// Settings are how similar two requirements must be to be listed.
type Settings struct {
	Threshold float64    `json:"threshold"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// SettingsInput sets the threshold.
type SettingsInput struct {
	Threshold float64 `json:"threshold"`
}

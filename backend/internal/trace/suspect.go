package trace

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Suspect links (#36), agreed in docs/api-contract-m3.md: a link is suspect while an end its type watches has a
// revision newer than the one it was last confirmed against. It is computed, never stored.

// Agreed limits.
const (
	SuspectDefaultLimit = 20
	SuspectMaxLimit     = 100
	// MaxClearCommentLength bounds what a clearance says, in characters.
	MaxClearCommentLength = 500
)

// End is one end of a link.
type End string

const (
	EndSource End = "source"
	EndTarget End = "target"
)

// Ends lists every End, for the API document.
var Ends = []End{EndSource, EndTarget}

// MovedEnd is an end that moved on since the link was confirmed against it.
type MovedEnd struct {
	End      End `json:"end"`
	Reviewed int `json:"reviewed"`
	Current  int `json:"current"`
}

// Watches says whether a link of a type with this setting turns suspect when the end moves on.
func (s SuspectEnd) Watches(e End) bool { return s == SuspectBoth || string(s) == string(e) }

// Revisions are a link's ends as it was confirmed against them and as they are now.
type Revisions struct {
	SourceReviewed, SourceCurrent int
	TargetReviewed, TargetCurrent int
	// Trashed is either end in the trash, where the link waits for it to come back.
	Trashed bool
}

// Moved is the ends that make a link suspect, the rule the view trace_link_state applies in the database: those
// the type watches with a newer revision than confirmed, none while an end is in the trash.
func Moved(watch SuspectEnd, r Revisions) []MovedEnd {
	out := []MovedEnd{}
	if r.Trashed {
		return out
	}
	if watch.Watches(EndSource) && r.SourceCurrent > r.SourceReviewed {
		out = append(out, MovedEnd{End: EndSource, Reviewed: r.SourceReviewed, Current: r.SourceCurrent})
	}
	if watch.Watches(EndTarget) && r.TargetCurrent > r.TargetReviewed {
		out = append(out, MovedEnd{End: EndTarget, Reviewed: r.TargetReviewed, Current: r.TargetCurrent})
	}
	return out
}

// SuspectLink is a suspect link as a project's list shows it, both ends at once.
type SuspectLink struct {
	ID     uuid.UUID       `json:"id"`
	Type   LinkType        `json:"type"`
	Source requirement.Ref `json:"source"`
	Target requirement.Ref `json:"target"`
	Ends   []MovedEnd      `json:"ends"`
	// Since is when the first revision past the confirmed one was saved.
	Since time.Time `json:"since"`
}

// ClearInput confirms a link against the ends as the caller read them; revisions newer than those are refused.
type ClearInput struct {
	SourceRevision int    `json:"sourceRevision"`
	TargetRevision int    `json:"targetRevision"`
	Comment        string `json:"comment"`
}

// Clearance is one confirmation of a link: who, against which revisions, and why.
type Clearance struct {
	ID             uuid.UUID `json:"id"`
	SourceRevision int       `json:"sourceRevision"`
	TargetRevision int       `json:"targetRevision"`
	Comment        string    `json:"comment"`
	ClearedByName  string    `json:"clearedByName"`
	ClearedAt      time.Time `json:"clearedAt"`
}

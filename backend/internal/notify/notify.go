// Package notify tells people what changed on what they follow (#34), in the app and by mail; docs/api-contract-m3.md
// says how, Stator's notifications over requirements.
package notify

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Agreed limits and timings, Stator's where Stator has them.
const (
	DefaultLimit = 20
	MaxLimit     = 100
	// MaxExcerptLength bounds the words a notification quotes, in characters.
	MaxExcerptLength = 200
	// DailyDigestHour is when, in UTC, a daily digest goes out.
	DailyDigestHour = 8
)

// Kind is what a notification is about. A person gets one notification per event: the first kind in Kinds that
// names them.
type Kind string

const (
	KindMentioned       Kind = "mentioned"
	KindReviewRequested Kind = "review_requested"
	KindReviewed        Kind = "reviewed"
	KindSuspect         Kind = "suspect"
	KindReplied         Kind = "replied"
	KindCommented       Kind = "commented"
	KindResolved        Kind = "resolved"
	KindRevised         Kind = "revised"
	KindTransitioned    Kind = "transitioned"
	// KindSourceRevised is a reused requirement's library source revised (#49), told about the reuse.
	KindSourceRevised Kind = "source_revised"
	// KindSessionInvited, KindSessionRound, KindSessionReminder and KindSessionClosed are about a review session one
	// takes part in (#204), told about the session rather than a requirement.
	KindSessionInvited  Kind = "session_invited"
	KindSessionRound    Kind = "session_round"
	KindSessionReminder Kind = "session_reminder"
	KindSessionClosed   Kind = "session_closed"
	// KindCodeOutdated and KindCodeOrphaned are a code scan's tags naming an older revision of requirements one owns or
	// watches, or naming one in the trash or none (#201), told about the scan.
	KindCodeOutdated Kind = "code_outdated"
	KindCodeOrphaned Kind = "code_orphaned"
	// KindTermChanged is the definition of a glossary term changed that a requirement one owns uses (#210).
	KindTermChanged Kind = "term_changed"
)

// Kinds lists every Kind, the order deciding which one a person named twice by one event gets.
var Kinds = []Kind{KindMentioned, KindReviewRequested, KindReviewed, KindSuspect, KindSourceRevised, KindReplied, KindCommented,
	KindResolved, KindRevised, KindTransitioned, KindSessionInvited, KindSessionRound, KindSessionReminder, KindSessionClosed,
	KindCodeOrphaned, KindCodeOutdated, KindTermChanged}

// Digest is how mail is sent: each notification at once, or gathered every hour or day.
type Digest string

const (
	DigestOff    Digest = "off"
	DigestHourly Digest = "hourly"
	DigestDaily  Digest = "daily"
)

// Digests lists every Digest, for the API document.
var Digests = []Digest{DigestOff, DigestHourly, DigestDaily}

// Notification is one thing the caller was told.
type Notification struct {
	ID        uuid.UUID `json:"id"`
	Kind      Kind      `json:"kind"`
	ActorName string    `json:"actorName"`
	// Requirement is what it is about, null for one about a review session, which Session names then (#204).
	Requirement *requirement.Ref `json:"requirement"`
	Session     *SessionLink     `json:"session"`
	// CodeScan is the code scan it is about instead of a requirement (#201).
	CodeScan *CodeScanLink `json:"codeScan"`
	// Revision is the requirement's revision the notification is about.
	Revision  int        `json:"revision"`
	ThreadID  *uuid.UUID `json:"threadId"`
	CommentID *uuid.UUID `json:"commentId"`
	ReviewID  *uuid.UUID `json:"reviewId"`
	LinkID    *uuid.UUID `json:"linkId"`
	Excerpt   string     `json:"excerpt"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt"`
}

// SessionLink is the review session a notification is about.
type SessionLink struct {
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	ProjectKey string    `json:"projectKey"`
}

// CodeScanLink is the code scan a notification is about: its repository and branch, in a project.
type CodeScanLink struct {
	ID         uuid.UUID `json:"id"`
	Repository string    `json:"repository"`
	Branch     string    `json:"branch"`
	ProjectKey string    `json:"projectKey"`
}

// ReadInput marks notifications read: the ids given, or all of the caller's.
type ReadInput struct {
	IDs []uuid.UUID `json:"ids,omitempty"`
	All bool        `json:"all,omitempty"`
}

// Preferences are how the caller wants to be told; a kind left out of a switch map is on.
type Preferences struct {
	InApp     map[Kind]bool `json:"inApp"`
	Email     map[Kind]bool `json:"email"`
	Digest    Digest        `json:"digest"`
	AutoWatch bool          `json:"autoWatch"`
}

package review

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Agreed limits of review sessions (#204).
const (
	MaxSessionItems        = 500
	MaxSessionParticipants = 50
	// MaxSessionApprovers bounds the people a session's approvers reach, groups counted by their members.
	MaxSessionApprovers   = 50
	MaxSessionRounds      = 20
	MaxSessionTitleLength = 100
	// DefaultQuorum is the share of approvers each requirement needs, in percent, when the session names none.
	DefaultQuorum = 100
	// MaxDeadline is how far ahead a deadline may be; MinDeadline how far at least, when it is set.
	MaxDeadline = 365 * 24 * time.Hour
	MinDeadline = time.Hour
	// ReminderLead is how long before the deadline participants are reminded, once.
	ReminderLead = 24 * time.Hour
)

// SessionState is whether a session still takes decisions.
type SessionState string

const (
	SessionOpen   SessionState = "open"
	SessionClosed SessionState = "closed"
)

// SessionStates lists every SessionState, for the API document.
var SessionStates = []SessionState{SessionOpen, SessionClosed}

// ScopeKind says what a session reviews.
type ScopeKind string

const (
	ScopeSection  ScopeKind = "section"
	ScopeBaseline ScopeKind = "baseline"
	ScopeQuery    ScopeKind = "query"
)

// ScopeKinds lists every ScopeKind, for the API document.
var ScopeKinds = []ScopeKind{ScopeSection, ScopeBaseline, ScopeQuery}

// Scope is a section and everything below it, the requirements a baseline holds, or a query's.
type Scope struct {
	Kind       ScopeKind  `json:"kind"`
	RootKey    string     `json:"rootKey,omitempty"`
	BaselineID *uuid.UUID `json:"baselineId,omitempty"`
	Query      string     `json:"query,omitempty"`
}

// Since keeps only what changed since each requirement's last approval, or since a baseline.
type Since struct {
	Base       string     `json:"base"`
	BaselineID *uuid.UUID `json:"baselineId,omitempty"`
}

// ParticipantRole says whether a participant decides or reads and comments.
type ParticipantRole string

const (
	RoleReviewer ParticipantRole = "reviewer"
	RoleApprover ParticipantRole = "approver"
)

// ParticipantRoles lists every ParticipantRole, for the API document.
var ParticipantRoles = []ParticipantRole{RoleReviewer, RoleApprover}

// Participant is a person or a group a session names, and what they do in it.
type Participant struct {
	Subject
	Role ParticipantRole `json:"role"`
}

// SessionInput opens a session; every requirement it holds is pinned and asked for review at once, or none is.
type SessionInput struct {
	Title        string        `json:"title"`
	Description  string        `json:"description,omitempty"`
	Scope        Scope         `json:"scope"`
	Since        *Since        `json:"since,omitempty"`
	Participants []Participant `json:"participants"`
	Deadline     *time.Time    `json:"deadline,omitempty"`
	// Quorum is the share of approvers each requirement needs, in percent; DefaultQuorum when absent.
	Quorum int `json:"quorum,omitempty"`
}

// SessionUpdate changes what a session says and when it ends; participants and scope are fixed once it opens.
type SessionUpdate struct {
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	Deadline    *time.Time `json:"deadline,omitempty"`
}

// RoundInput starts the next round, pinning anew what was revised since.
type RoundInput struct {
	Comment string `json:"comment,omitempty"`
}

// CloseBaseline names the baseline a closing session takes of what it approved.
type CloseBaseline struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CloseInput closes a session, withdrawing what is still pending, and takes a baseline when it names one.
type CloseInput struct {
	Baseline *CloseBaseline `json:"baseline,omitempty"`
}

// SessionRef is the session a review belongs to.
type SessionRef struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Round int       `json:"round"`
}

// SessionItem is one requirement of a session: the revision pinned now, the round that pinned it, and its review.
type SessionItem struct {
	Requirement requirement.Ref `json:"requirement"`
	Outline     string          `json:"outline"`
	Revision    int             `json:"revision"`
	Round       int             `json:"round"`
	Review      Review          `json:"review"`
	OpenThreads int             `json:"openThreads"`
}

// Excluded is a requirement the scope held that the session left out, and why.
type Excluded struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// Progress is how far one person got: their decisions as an approver, their comments as a reviewer.
type Progress struct {
	Person    requirement.PersonRef `json:"person"`
	Role      ParticipantRole       `json:"role"`
	Decided   int                   `json:"decided"`
	Commented int                   `json:"commented"`
	Of        int                   `json:"of"`
}

// SessionCounts counts a session's requirements by where their reviews stand.
type SessionCounts struct {
	Approved int `json:"approved"`
	Rejected int `json:"rejected"`
	Pending  int `json:"pending"`
	Total    int `json:"total"`
}

// SessionCan is what the caller may do to a session.
type SessionCan struct {
	Edit  bool `json:"edit"`
	Round bool `json:"round"`
	Close bool `json:"close"`
}

// Session is one review of many requirements, in rounds, with its progress.
type Session struct {
	ID            uuid.UUID     `json:"id"`
	ProjectKey    string        `json:"projectKey"`
	Title         string        `json:"title"`
	Description   string        `json:"description"`
	Scope         Scope         `json:"scope"`
	Since         *Since        `json:"since"`
	State         SessionState  `json:"state"`
	Round         int           `json:"round"`
	Quorum        int           `json:"quorum"`
	Deadline      *time.Time    `json:"deadline"`
	ModeratorName string        `json:"moderatorName"`
	Participants  []Participant `json:"participants"`
	Items         []SessionItem `json:"items"`
	Excluded      []Excluded    `json:"excluded"`
	Progress      []Progress    `json:"progress"`
	Counts        SessionCounts `json:"counts"`
	// BaselineID is the baseline closing took, null without one.
	BaselineID *uuid.UUID `json:"baselineId"`
	OpenedAt   time.Time  `json:"openedAt"`
	ClosedAt   *time.Time `json:"closedAt"`
	Can        SessionCan `json:"can"`
}

// SessionSummary is a session as a list shows it; Waiting counts what is still the caller's to do.
type SessionSummary struct {
	ID            uuid.UUID     `json:"id"`
	ProjectKey    string        `json:"projectKey"`
	Title         string        `json:"title"`
	State         SessionState  `json:"state"`
	Round         int           `json:"round"`
	Deadline      *time.Time    `json:"deadline"`
	ModeratorName string        `json:"moderatorName"`
	Counts        SessionCounts `json:"counts"`
	Waiting       int           `json:"waiting"`
	OpenedAt      time.Time     `json:"openedAt"`
	ClosedAt      *time.Time    `json:"closedAt"`
}

// RoundEntry is one round of a session.
type RoundEntry struct {
	Number    int       `json:"number"`
	Comment   string    `json:"comment"`
	StartedAt time.Time `json:"startedAt"`
}

// ReportRound is one requirement's review in one round.
type ReportRound struct {
	Round     int             `json:"round"`
	Revision  int             `json:"revision"`
	State     State           `json:"state"`
	Decisions []DecisionEntry `json:"decisions"`
}

// ReportItem is one requirement as the report shows it: its review in every round it was pinned in.
type ReportItem struct {
	Requirement requirement.Ref `json:"requirement"`
	Outline     string          `json:"outline"`
	Rounds      []ReportRound   `json:"rounds"`
	OpenThreads int             `json:"openThreads"`
}

// Report is a session as closed, or as it stands while open.
type Report struct {
	Session      Session      `json:"session"`
	Rounds       []RoundEntry `json:"rounds"`
	Items        []ReportItem `json:"items"`
	BaselineName string       `json:"baselineName"`
}

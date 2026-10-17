// Package review lets named approvers sign off one revision of a requirement (#35); docs/api-contract-m3.md says how,
// shaped after Stator's page reviews.
package review

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/via"
)

// Agreed limits, Stator's where Stator has them.
const (
	// MinRequired and MaxRequired bound how many approvals a rule asks for.
	MinRequired = 1
	MaxRequired = 10
	// MaxCommentLength bounds a request's or a decision's comment, in characters.
	MaxCommentLength = 500
	DefaultLimit     = 20
	MaxLimit         = 100
)

// SubjectType says whether an approver is a person or a group.
type SubjectType string

const (
	SubjectUser  SubjectType = "user"
	SubjectGroup SubjectType = "group"
)

// SubjectTypes lists every SubjectType, for the API document.
var SubjectTypes = []SubjectType{SubjectUser, SubjectGroup}

// Subject is one approver of a rule or a review: a person, or every member of a group.
type Subject struct {
	Type    SubjectType `json:"subjectType"`
	UserID  *uuid.UUID  `json:"userId,omitempty"`
	GroupID *uuid.UUID  `json:"groupId,omitempty"`
	// Name is the person's or the group's, answered and never read.
	Name string `json:"name,omitempty"`
}

// Rule is who approves the requirements of one type in a project, or of every type without a rule of its own.
type Rule struct {
	ID       uuid.UUID  `json:"id"`
	TypeID   *uuid.UUID `json:"typeId"`
	TypeName string     `json:"typeName"`
	Required int        `json:"required"`
	// AuthorMayApprove lets the author of the revision under review approve it too.
	AuthorMayApprove bool      `json:"authorMayApprove"`
	Approvers        []Subject `json:"approvers"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// RuleInput sets the rule of a type, or the project's rule for every other type when TypeID is absent.
type RuleInput struct {
	TypeID           *uuid.UUID `json:"typeId,omitempty"`
	Required         int        `json:"required"`
	AuthorMayApprove bool       `json:"authorMayApprove"`
	Approvers        []Subject  `json:"approvers"`
}

// State is where a review stands.
type State string

const (
	StatePending    State = "pending"
	StateApproved   State = "approved"
	StateRejected   State = "rejected"
	StateWithdrawn  State = "withdrawn"
	StateSuperseded State = "superseded"
)

// States lists every State, for the API document.
var States = []State{StatePending, StateApproved, StateRejected, StateWithdrawn, StateSuperseded}

// Decision is an approver's answer.
type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
)

// Decisions lists every Decision, for the API document.
var Decisions = []Decision{DecisionApprove, DecisionReject}

// Review asks the approvers frozen at its request to sign off one revision of a requirement.
type Review struct {
	ID          uuid.UUID       `json:"id"`
	Requirement requirement.Ref `json:"requirement"`
	// Revision is the one under review; a decision names it, and a later revision supersedes the review.
	Revision    int             `json:"revision"`
	State       State           `json:"state"`
	Required    int             `json:"required"`
	Approvals   int             `json:"approvals"`
	Approvers   []Subject       `json:"approvers"`
	Decisions   []DecisionEntry `json:"decisions"`
	Comment     string          `json:"comment"`
	RequestedBy string          `json:"requestedByName"`
	// Via is the token or connected app the review was asked for through, null for a session (#190).
	Via         *via.Via   `json:"via"`
	RequestedAt time.Time  `json:"requestedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	AuthorName  string     `json:"authorName"`
	Can         Can        `json:"can"`
	// Signed says a decision on this review is an electronic signature, as its project asks (#46).
	Signed bool `json:"signed"`
	// Meanings are what the caller signs with each decision, in their language, while they may decide a signed review;
	// empty otherwise.
	Meanings map[Decision]string `json:"meanings"`
}

// DecisionEntry is one approver's decision, the latest standing.
type DecisionEntry struct {
	ReviewerID   uuid.UUID `json:"reviewerId"`
	ReviewerName string    `json:"reviewerName"`
	Decision     Decision  `json:"decision"`
	Comment      string    `json:"comment"`
	DecidedAt    time.Time `json:"decidedAt"`
	// Signature is what the decision was signed with, null for one given without.
	Signature *Signature `json:"signature"`
}

// Signature is an electronic signature as it was given: what it meant, what it signed, and how the signer proved
// who they are, which never changes (#46).
type Signature struct {
	Meaning string `json:"meaning"`
	// Method is password or oidc, the identity provider.
	Method          string    `json:"method"`
	AuthenticatedAt time.Time `json:"authenticatedAt"`
	SignedAt        time.Time `json:"signedAt"`
	Revision        int       `json:"revision"`
	// ContentHash is the SHA-256 of the revision's normative content, which the signature signs.
	ContentHash string `json:"contentHash"`
}

// Can is what the caller may do to a review.
type Can struct {
	Decide   bool `json:"decide"`
	Withdraw bool `json:"withdraw"`
}

// RequestInput asks for a review of the requirement's current revision, which the caller names as they saw it.
type RequestInput struct {
	Revision int    `json:"revision"`
	Comment  string `json:"comment,omitempty"`
}

// DecisionInput decides a review; Revision is the one the approver read, refused when it is not the one under review.
// A signed review takes the approver's password, or the re-authentication a fresh sign-in at the provider gave.
type DecisionInput struct {
	Decision         Decision   `json:"decision"`
	Revision         int        `json:"revision"`
	Comment          string     `json:"comment,omitempty"`
	Password         string     `json:"password,omitempty"`
	Reauthentication *uuid.UUID `json:"reauthentication,omitempty"`
}

// Approval is a requirement's standing for the workflow and the page; the requirement carries it too, so it is the
// requirement's type.
type Approval = requirement.Approval

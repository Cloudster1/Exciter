package baseline

import (
	"time"

	"github.com/google/uuid"
)

// MaxSigners bounds the people and groups one signing rule names (#205).
const MaxSigners = 20

// SignerType says whether a signer is a person or every member of a group.
type SignerType string

const (
	SignerUser  SignerType = "user"
	SignerGroup SignerType = "group"
)

// SignerTypes lists every SignerType, for the API document.
var SignerTypes = []SignerType{SignerUser, SignerGroup}

// Signer is somebody who signs a project's baselines, shaped as a review's approver.
type Signer struct {
	Type    SignerType `json:"subjectType"`
	UserID  *uuid.UUID `json:"userId,omitempty"`
	GroupID *uuid.UUID `json:"groupId,omitempty"`
	// Name is the person's or the group's, answered and never read.
	Name string `json:"name,omitempty"`
}

// SigningRule is who signs a project's baselines and how many signatures release one.
type SigningRule struct {
	Required  int       `json:"required"`
	Signers   []Signer  `json:"signers"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SigningRuleInput sets the project's signing rule; baselines already being signed keep the one frozen on them.
type SigningRuleInput struct {
	Required int      `json:"required"`
	Signers  []Signer `json:"signers"`
}

// Signing is where a baseline's signing stands, under the rule its first signature froze on it.
type Signing struct {
	Required   int      `json:"required"`
	Signers    []Signer `json:"signers"`
	Signatures int      `json:"signatures"`
	Signed     bool     `json:"signed"`
}

// Signature is one signer's release of a baseline as it was given, which never changes.
type Signature struct {
	SignerName string `json:"signerName"`
	Meaning    string `json:"meaning"`
	// Method is password or oidc, the identity provider.
	Method          string    `json:"method"`
	AuthenticatedAt time.Time `json:"authenticatedAt"`
	SignedAt        time.Time `json:"signedAt"`
	// ContentHash is the SHA-256 of the baseline's content in the form docs/api-contract-m8.md gives.
	ContentHash string `json:"contentHash"`
}

// SignInput signs a baseline with the meaning the dialog showed, proven by the password or a fresh sign-in.
type SignInput struct {
	Meaning          string     `json:"meaning"`
	Password         string     `json:"password,omitempty"`
	Reauthentication *uuid.UUID `json:"reauthentication,omitempty"`
}

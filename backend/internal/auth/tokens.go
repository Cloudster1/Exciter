package auth

import (
	"time"

	"github.com/google/uuid"
)

// Personal access tokens (#22), as Stator's; docs/api-contract-m1.md says how.
const (
	APITokenPrefix     = "exciter_pat_"
	MaxTokenNameLength = 100
	MaxTokenProjects   = 50
	// ScopeRead limits a token to reading; no scope is every right its owner has.
	ScopeRead = "read"
)

// APIToken is one personal access token, without its secret but on the answer that made it.
type APIToken struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Scopes []string  `json:"scopes"`
	// Projects are the keys the token is limited to; AllProjects says it is not limited.
	Projects    []string   `json:"projects"`
	AllProjects bool       `json:"allProjects"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	Secret      string     `json:"secret,omitempty"`
}

// OrgAPIToken is a token in the organization's list, with whose it is.
type OrgAPIToken struct {
	APIToken
	Owner User `json:"owner"`
}

// NewAPIToken is a token to make; no scopes is full, and no projects is every project.
type NewAPIToken struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes,omitempty"`
	Projects  []string   `json:"projects,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

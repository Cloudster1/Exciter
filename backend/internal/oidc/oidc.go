// Package oidc signs people in through their organization's identity provider
// and keeps their groups in step with what its verified id token says.
package oidc

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Defaults a provider gets for what its form leaves blank.
const (
	DefaultGroupsClaim = "groups"
	DefaultScopes      = "openid profile email"
)

// Provider is one organization's identity provider.
type Provider struct {
	OrgID    uuid.UUID `json:"-"`
	Issuer   string    `json:"issuer"`
	ClientID string    `json:"clientId"`
	// ClientSecret is never sent to a client. Whether one is stored is, so a
	// settings page can say so without revealing it.
	ClientSecret string `json:"-"`
	HasSecret    bool   `json:"hasSecret"`
	// GroupsClaim names the claim listing somebody's groups; a dotted path
	// reaches into a nested claim.
	GroupsClaim string `json:"groupsClaim"`
	Scopes      string `json:"scopes"`
	// CreateGroups makes a group the provider names appear on first sight.
	CreateGroups bool      `json:"createGroups"`
	Enabled      bool      `json:"enabled"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ScopeList splits the configured scopes, always including openid, without
// which the provider has no reason to return an id token at all.
func (p Provider) ScopeList() []string {
	seen := map[string]bool{"openid": true}
	out := []string{"openid"}
	for _, scope := range strings.Fields(p.Scopes) {
		if seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out
}

// Identity is what a verified token said about somebody.
type Identity struct {
	// Issuer and Subject together are the provider's stable name for a
	// person, which survives an email change.
	Issuer  string
	Subject string
	Email   string
	Name    string
	// Groups are the values of the configured claim, trimmed and without
	// repeats. They are matched against a group's external reference.
	Groups []string
}

// ValidationError is a provider setting that cannot be saved as given.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

var (
	// ErrNotConfigured is returned when an organization has no provider, or
	// has turned the one it has off.
	ErrNotConfigured = errors.New("this organization does not sign in through an identity provider")
	// ErrUnknownLogin is returned for a callback whose state matches no login
	// this server started, which is what a forged or replayed callback is.
	ErrUnknownLogin = errors.New("that sign-in did not start here, or it has expired")
	// ErrNoEmail is returned when the token carries no email. Without one the
	// person cannot be shown to anybody or matched to a bootstrap account.
	ErrNoEmail = errors.New("the identity provider did not send an email address")
	// ErrEmailUnverified is returned when the provider says it has not checked
	// the address it sent.
	ErrEmailUnverified = errors.New("the identity provider has not verified that email address")
	// ErrUnreachable is returned when the provider cannot be asked who it is,
	// which is the provider's address or the network, not the person.
	ErrUnreachable = errors.New("the identity provider could not be reached")
	// ErrNotAMember is returned when somebody authenticates correctly but has
	// not been let in. Signing in is not the same as being let in.
	ErrNotAMember = errors.New("you are not a member of that organization")
)

// howLongALoginMayTake is long enough to type a password and answer a second
// factor, short enough that a captured link is useless later.
const howLongALoginMayTake = 15 * time.Minute

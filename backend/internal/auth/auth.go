// Package auth handles identity: argon2 passwords, opaque session tokens, and
// the organization a request is acting within.
package auth

import (
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// ErrInvalidToken is an unknown, expired or revoked credential. The caller is
// then anonymous, which is not an error in itself.
var ErrInvalidToken = errors.New("invalid or expired credential")

// Principal is the caller a credential resolved to.
type Principal struct {
	UserID uuid.UUID
	// Org is the installation's organization, which every caller the service resolves belongs to.
	Org *tenant.Org

	Email     string
	Name      string
	AvatarURL string
	// Locale is the language the person chose for the interface; empty means the browser's.
	Locale Locale
	// OrgName is Org's display name, the installation's.
	OrgName string
	// Role is the caller's standing in the installation.
	Role OrgRole
	// SessionID names the sign-in this request rides on, and keys its
	// read-your-writes position; nil for a caller without a session.
	SessionID *uuid.UUID
	// Proof is how that session was opened.
	Proof Proof
	// Token is the personal access token this request rides on; nil for a session.
	Token *CallingToken
	// TokenProjects are the ids of the projects Token is limited to, when it is.
	TokenProjects []uuid.UUID
}

// ReadOnly reports whether the caller came with a token made to read.
func (p *Principal) ReadOnly() bool {
	return p != nil && p.Token != nil && slices.Contains(p.Token.Scopes, ScopeRead)
}

// InProjectsOnly reports whether the caller came with a token limited to some projects.
func (p *Principal) InProjectsOnly() bool { return p != nil && p.Token != nil && !p.Token.AllProjects }

// Guest reports whether the caller is a guest in their organization.
func (p *Principal) Guest() bool { return p != nil && p.Role == RoleGuest }

// CanAdminister reports whether the caller may change the installation's settings.
func (p *Principal) CanAdminister() bool { return p != nil && p.Role.CanAdminister() }

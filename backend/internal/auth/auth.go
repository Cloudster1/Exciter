// Package auth handles identity: argon2 passwords, opaque session tokens, and
// the organization a request is acting within.
package auth

import (
	"errors"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// ErrInvalidToken is an unknown, expired or revoked credential. The caller is
// then anonymous, which is not an error in itself.
var ErrInvalidToken = errors.New("invalid or expired credential")

// Principal is the caller a credential resolved to.
type Principal struct {
	UserID uuid.UUID
	// Org is the organization the caller is acting in; nil until one is chosen.
	Org *tenant.Org

	Email     string
	Name      string
	AvatarURL string
	// Locale is the language the person chose for the interface; empty means the browser's.
	Locale Locale
	// OrgName is Org's display name, empty when Org is nil.
	OrgName string
	// Role is the caller's standing in Org, empty when Org is nil.
	Role OrgRole
	// SessionID names the sign-in this request rides on, and keys its
	// read-your-writes position; nil for a caller without a session.
	SessionID *uuid.UUID
	// Proof is how that session was opened.
	Proof Proof
}

// Guest reports whether the caller is a guest in their organization.
func (p *Principal) Guest() bool { return p != nil && p.Role == RoleGuest }

// InOrg reports whether the caller has chosen an organization to act in.
func (p *Principal) InOrg() bool { return p != nil && p.Org != nil && p.Org.ID != uuid.Nil }

// CanAdminister reports whether the caller may change the settings of the
// organization they are acting in.
func (p *Principal) CanAdminister() bool { return p.InOrg() && p.Role.CanAdminister() }

// Package auth handles identity: argon2 passwords, opaque session tokens, and the standing a request acts with.
package auth

import (
	"errors"
	"slices"

	"github.com/google/uuid"
)

// ErrInvalidToken is an unknown, expired or revoked credential. The caller is
// then anonymous, which is not an error in itself.
var ErrInvalidToken = errors.New("invalid or expired credential")

// Principal is the caller a credential resolved to.
type Principal struct {
	UserID uuid.UUID

	Email     string
	Name      string
	AvatarURL string
	// Locale is the language the person chose for the interface; empty means the browser's.
	Locale Locale
	// InstallationName is what the installation is called, which the shell shows.
	InstallationName string
	// Role is the caller's standing in the installation; empty for somebody who no longer belongs to it.
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
	// PartnerApp says Token is a partner app's from a token exchange (#273), good for the whole API as a personal
	// token is rather than for MCP alone.
	PartnerApp bool
}

// ReadOnly reports whether the caller came with a token made to read.
func (p *Principal) ReadOnly() bool {
	return p != nil && p.Token != nil && slices.Contains(p.Token.Scopes, ScopeRead)
}

// Evidence reports whether the caller came with a token that only uploads evidence and runs the trace check.
func (p *Principal) Evidence() bool {
	return p != nil && p.Token != nil && slices.Contains(p.Token.Scopes, ScopeEvidence)
}

// InProjectsOnly reports whether the caller came with a token limited to some projects.
func (p *Principal) InProjectsOnly() bool { return p != nil && p.Token != nil && !p.Token.AllProjects }

// Guest reports whether the caller is a guest of the installation.
func (p *Principal) Guest() bool { return p != nil && p.Role == RoleGuest }

// CanAdminister reports whether the caller may change the installation's settings.
func (p *Principal) CanAdminister() bool { return p != nil && p.Role.CanAdminister() }

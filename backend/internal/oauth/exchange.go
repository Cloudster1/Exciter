package oauth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/oidc"
)

// The token exchange (RFC 8693) a partner app trades a person's token from the shared identity provider with (#273).
const (
	GrantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	TokenTypeAccess    = "urn:ietf:params:oauth:token-type:access_token"
)

// partnerGrantTypes are a partner's grants, the exchange and refreshing what it gave, as the column holds them.
var partnerGrantTypes = []string{GrantTokenExchange, GrantRefreshToken}

// People verifies a person's access token from the installation's identity provider and lets them in as a sign-in
// would, in the exchange's own transaction; *oidc.Service is one.
type People interface {
	VerifyAccessToken(ctx context.Context, raw, authorizedParty string) (*oidc.Identity, error)
	Admit(ctx context.Context, tx db.DBTX, identity *oidc.Identity) (*oidc.Admission, error)
}

// WithPeople lets partner apps exchange the identity provider's tokens; without it every exchange is refused.
func (s *Service) WithPeople(people People) *Service {
	s.people = people
	return s
}

// partnerScopeAsked reads a partner's scope against what the app may hold: read among them, nothing beyond, and
// all it holds when none is named. It answers whether the grant reads only.
func partnerScopeAsked(asked string, allowed []string) (readOnly bool, ok bool) {
	fields := strings.Fields(asked)
	if len(fields) == 0 {
		return !slices.Contains(allowed, ScopeWrite), true
	}
	if !slices.Contains(fields, ScopeRead) {
		return false, false
	}
	for _, f := range fields {
		if !slices.Contains(allowed, f) {
			return false, false
		}
	}
	return !slices.Contains(fields, ScopeWrite), true
}

// exchange trades a person's access token from the identity provider for a grant of the partner, with no consent
// page: the administrator who named the app's Keycloak client consented for everybody.
func (s *Service) exchange(ctx context.Context, c *client, in TokenRequest) (*TokenResponse, error) {
	if in.SubjectToken == "" || in.SubjectTokenType == "" {
		return nil, refuse(ErrInvalidRequest, "Send subject_token and subject_token_type.")
	}
	if in.SubjectTokenType != TokenTypeAccess {
		return nil, refuse(ErrInvalidRequest, "Exciter exchanges access tokens only: subject_token_type is "+TokenTypeAccess+".")
	}
	if in.RequestedTokenType != "" && in.RequestedTokenType != TokenTypeAccess {
		return nil, refuse(ErrInvalidRequest, "Exciter issues access tokens only: requested_token_type is "+TokenTypeAccess+".")
	}
	if c.KeycloakClientID == "" || s.people == nil {
		return nil, refuse(ErrUnauthorizedClient, fmt.Sprintf(
			"%s may not exchange identity provider tokens. An Exciter administrator names its Keycloak client under Settings, Partner apps.", c.Name))
	}
	readOnly, ok := partnerScopeAsked(in.Scope, c.Scopes)
	if !ok {
		return nil, refuse(ErrInvalidScope, fmt.Sprintf("Ask for read and at most %s.", strings.Join(c.Scopes, " ")))
	}
	identity, err := s.people.VerifyAccessToken(ctx, in.SubjectToken, c.KeycloakClientID)
	var refused *oidc.TokenError
	switch {
	case errors.Is(err, oidc.ErrNotConfigured):
		return nil, refuse(ErrUnauthorizedClient, "Single sign-on is not set up in this Exciter, so there is no identity provider token to exchange.")
	case errors.Is(err, oidc.ErrUnreachable):
		return nil, refuse(ErrTemporarilyUnavailable, "Exciter could not reach the identity provider to check the token. Try again shortly.")
	case errors.As(err, &refused), errors.Is(err, oidc.ErrNoEmail), errors.Is(err, oidc.ErrEmailUnverified):
		return nil, refuse(ErrInvalidGrant, "The identity provider's token was refused: "+err.Error()+".")
	case err != nil:
		return nil, err
	}
	sc := scope{readOnly: readOnly, partner: true}
	var out *TokenResponse
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		admitted, err := s.people.Admit(ctx, tx, identity)
		switch {
		case errors.Is(err, oidc.ErrNotAMember):
			return refuse(ErrInvalidGrant, fmt.Sprintf(
				"%s has no Exciter account yet. Sign in to Exciter once, or ask an administrator to let them in.", identity.Name))
		case errors.Is(err, auth.ErrSwitchedOff):
			return refuse(ErrInvalidGrant, fmt.Sprintf("%s is switched off in Exciter. Ask an Exciter administrator.", identity.Name))
		case err != nil:
			return err
		}
		grant, err := partnerGrant(ctx, tx, c, admitted.UserID, readOnly)
		if err != nil {
			return err
		}
		if out, err = s.issue(ctx, tx, grant, sc); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthGranted, TargetType: "oauth_grant", TargetID: &grant, Actor: admitted.UserID,
			Data: map[string]any{"client": c.Name, "clientId": c.ID, "readOnly": readOnly, "evidence": false, "projects": []string{}, "by": "token_exchange"}})
	})
	if err != nil {
		return nil, err
	}
	out.IssuedTokenType = TokenTypeAccess
	return out, nil
}

// partnerGrant is the person's one grant of the partner, its earlier tokens ended: the same grant when it reads as
// asked, else a new one in its place, since a grant stays what it was made.
func partnerGrant(ctx context.Context, tx db.DBTX, c *client, person uuid.UUID, readOnly bool) (uuid.UUID, error) {
	var (
		id   uuid.UUID
		held bool
	)
	err := tx.QueryRow(ctx, `SELECT id, read_only FROM oauth_grant WHERE client_id = $1 AND user_id = $2 FOR UPDATE`, c.ID, person).Scan(&id, &held)
	switch {
	case err == nil && held == readOnly:
		_, err = tx.Exec(ctx, `DELETE FROM oauth_token WHERE grant_id = $1`, id)
		return id, err
	case err == nil:
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_grant WHERE id = $1`, id); err != nil {
			return uuid.Nil, err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO oauth_grant (user_id, client_id, redirect_uri, read_only) VALUES ($1, $2, '', $3) RETURNING id`,
		person, c.ID, readOnly).Scan(&id)
	return id, err
}

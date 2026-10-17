package oauth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// ClientAuth is how a client said who it is: in the form, or with HTTP Basic.
type ClientAuth struct {
	ID, Secret string
	Basic      bool
}

// clientFor authenticates the client of a token or revocation request.
func (s *Service) clientFor(ctx context.Context, who ClientAuth) (*client, error) {
	if who.ID == "" {
		return nil, refuse(ErrInvalidClient, "Say which client asks, with client_id or HTTP Basic.")
	}
	c, err := s.clientOf(ctx, who.ID)
	if err != nil {
		return nil, err
	}
	if r := c.authenticate(who.Secret, who.Basic); r != nil {
		return nil, r
	}
	return c, nil
}

var errGrant = refuse(ErrInvalidGrant, "The code or refresh token is not one this client may use: it expired, was used, or was ended. Connect the client again.")

// Token trades a code and its verifier, or a refresh token, for a new access and refresh token.
func (s *Service) Token(ctx context.Context, in TokenRequest, who ClientAuth) (*TokenResponse, error) {
	if on, err := s.Enabled(ctx); err != nil || !on {
		return nil, firstErr(err, refuse(ErrTemporarilyUnavailable, "OAuth connections are switched off on this server. Ask its administrators."))
	}
	if in.GrantType != GrantAuthorizationCode && in.GrantType != GrantRefreshToken {
		return nil, refuse(ErrUnsupportedGrantType, "The grants offered are authorization_code and refresh_token.")
	}
	c, err := s.clientFor(ctx, who)
	if err != nil {
		return nil, err
	}
	if !c.grants(in.GrantType) {
		return nil, refuse(ErrUnauthorizedClient, "The client did not register for "+in.GrantType+".")
	}
	if in.Resource != "" && !s.isResource(in.Resource) {
		return nil, refuse(ErrInvalidTarget, "The one resource is the MCP endpoint, "+s.Resource()+".")
	}
	var (
		out     *TokenResponse
		refusal *Refusal
	)
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		redeem := redeemCode
		if in.GrantType == GrantRefreshToken {
			redeem = turnRefresh
		}
		grant, readOnly, err := redeem(ctx, tx, c.ID, in)
		// A refusal still commits: a code or refresh token used twice has just ended its grant.
		if errors.As(err, &refusal) {
			return nil
		}
		if err != nil {
			return err
		}
		out, err = s.issue(ctx, tx, grant, readOnly)
		return err
	})
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, refusal
	}
	return out, nil
}

// endGrant ends a grant whose code or refresh token came back once used, as a stolen one would, and refuses.
func endGrant(ctx context.Context, tx db.DBTX, grant uuid.UUID, r *Refusal) error {
	if err := deleteGrant(ctx, tx, grant, EndedByReusedToken); err != nil {
		return err
	}
	return r
}

// deleteGrant ends a grant nobody signed in ended, and records who did.
func deleteGrant(ctx context.Context, tx db.DBTX, grant uuid.UUID, by string) error {
	var owner uuid.UUID
	var client string
	err := tx.QueryRow(ctx, `DELETE FROM oauth_grant g USING oauth_client c WHERE g.id = $1 AND c.id = g.client_id
		RETURNING g.user_id, c.name`, grant).Scan(&owner, &client)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return audit.Write(ctx, tx, endedEntry(grant, uuid.Nil, owner, client, by, ""))
}

func redeemCode(ctx context.Context, tx db.DBTX, clientID string, in TokenRequest) (uuid.UUID, bool, error) {
	var (
		grant                          uuid.UUID
		owner, redirect, challenge, rs string
		readOnly, expired              bool
		used                           *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT g.id, g.client_id, g.read_only, c.redirect_uri, c.code_challenge, c.resource, c.used_at, c.expires_at <= now()
		FROM oauth_code c JOIN oauth_grant g ON g.id = c.grant_id
		WHERE c.code_hash = $1 FOR UPDATE OF c`, auth.HashToken(in.Code)).Scan(&grant, &owner, &readOnly, &redirect, &challenge, &rs, &used, &expired)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return grant, false, errGrant
	case err != nil:
		return grant, false, err
	case owner != clientID:
		return grant, false, errGrant
	case used != nil:
		return grant, false, endGrant(ctx, tx, grant, refuse(ErrInvalidGrant, "The code was used already, so the grant it made is ended. Connect the client again."))
	case expired:
		return grant, false, errGrant
	case in.RedirectURI != redirect:
		return grant, false, refuse(ErrInvalidGrant, "Send the redirect_uri the code was asked for with.")
	case !VerifyPKCE(challenge, in.CodeVerifier):
		return grant, false, refuse(ErrInvalidGrant, "The code_verifier is not the one the code_challenge was made from.")
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_code SET used_at = now() WHERE code_hash = $1`, auth.HashToken(in.Code)); err != nil {
		return grant, false, err
	}
	return grant, readOnly, nil
}

func turnRefresh(ctx context.Context, tx db.DBTX, clientID string, in TokenRequest) (uuid.UUID, bool, error) {
	var (
		grant             uuid.UUID
		owner             string
		readOnly, expired bool
		replaced          *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT g.id, g.client_id, t.read_only, t.replaced_at, t.expires_at <= now()
		FROM oauth_token t JOIN oauth_grant g ON g.id = t.grant_id
		WHERE t.token_hash = $1 AND t.kind = 'refresh' FOR UPDATE OF t`, auth.HashToken(in.RefreshToken)).Scan(&grant, &owner, &readOnly, &replaced, &expired)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return grant, false, errGrant
	case err != nil:
		return grant, false, err
	case owner != clientID:
		return grant, false, errGrant
	case replaced != nil:
		return grant, false, endGrant(ctx, tx, grant,
			refuse(ErrInvalidGrant, "The refresh token was used already, so the grant is ended, as a stolen one would be. Connect the client again."))
	case expired:
		return grant, false, errGrant
	}
	asked, ok := scopeReadOnly(in.Scope)
	if !ok || (readOnly && in.Scope != "" && !asked) {
		return grant, false, refuse(ErrInvalidScope, "A refresh may only narrow the scope, to mcp:read.")
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_token SET replaced_at = now() WHERE token_hash = $1`, auth.HashToken(in.RefreshToken)); err != nil {
		return grant, false, err
	}
	return grant, readOnly || (in.Scope != "" && asked), nil
}

// issue hands a grant a new access and refresh token; only their digests are kept.
func (s *Service) issue(ctx context.Context, tx db.DBTX, grant uuid.UUID, readOnly bool) (*TokenResponse, error) {
	access, accessDigest, err := secret(AccessTokenPrefix)
	if err != nil {
		return nil, err
	}
	refresh, refreshDigest, err := secret(RefreshTokenPrefix)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO oauth_token (token_hash, grant_id, kind, read_only, expires_at) VALUES
			($1, $3, 'access', $4, now() + make_interval(secs => $5)),
			($2, $3, 'refresh', $4, now() + make_interval(secs => $6))`,
		accessDigest, refreshDigest, grant, readOnly, AccessTTL.Seconds(), RefreshTTL.Seconds()); err != nil {
		return nil, err
	}
	return &TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: expiresIn(AccessTTL), RefreshToken: refresh, Scope: scopeOf(readOnly)}, nil
}

// Revoke ends an access token, or a refresh token and with it the grant (RFC 7009); a token unknown, ended
// already or another client's is no refusal, so the answer says nothing of it.
func (s *Service) Revoke(ctx context.Context, in RevokeRequest, who ClientAuth) error {
	c, err := s.clientFor(ctx, who)
	if err != nil {
		return err
	}
	if in.Token == "" {
		return refuse(ErrInvalidRequest, "Send the token to end.")
	}
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			kind  string
			grant uuid.UUID
		)
		err := tx.QueryRow(ctx, `SELECT t.kind, t.grant_id FROM oauth_token t JOIN oauth_grant g ON g.id = t.grant_id
			WHERE t.token_hash = $1 AND g.client_id = $2`, auth.HashToken(in.Token), c.ID).Scan(&kind, &grant)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return err
		case kind == "refresh":
			return deleteGrant(ctx, tx, grant, EndedByClient)
		}
		_, err = tx.Exec(ctx, `DELETE FROM oauth_token WHERE token_hash = $1`, auth.HashToken(in.Token))
		return err
	})
	return err
}

// lastUsedEvery is how often a grant's use is noted, as a personal token's is.
const lastUsedEvery = time.Minute

// Authenticate reads the person an access token acts for, with the grant's limits on them, as a personal token's.
func (s *Service) Authenticate(ctx context.Context, secret string) (*auth.Principal, error) {
	if auth.ParsePrefixed(secret, AccessTokenPrefix) != nil {
		return nil, auth.ErrInvalidToken
	}
	var (
		p        auth.Principal
		tok      auth.CallingToken
		lastUsed *time.Time
		only, on bool
		active   bool
		readOnly bool
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT g.id, c.name, g.read_only OR t.read_only, g.last_used_at, t.expires_at, g.projects_only,
			       ARRAY(SELECT gp.project_id FROM oauth_grant_project gp WHERE gp.grant_id = g.id),
			       ARRAY(SELECT p.key FROM oauth_grant_project gp JOIN project p ON p.id = gp.project_id
			             WHERE gp.grant_id = g.id ORDER BY p.key),
			       u.id, u.email::text, u.name, COALESCE(u.avatar_url, ''), COALESCE(u.locale, ''), u.is_active,
			       i.name, u.role, i.oauth_connections
			FROM oauth_token t
			JOIN oauth_grant g ON g.id = t.grant_id
			JOIN oauth_client c ON c.id = g.client_id
			JOIN app_user u ON u.id = g.user_id AND u.role IS NOT NULL
			CROSS JOIN installation i
			WHERE t.token_hash = $1 AND t.kind = 'access' AND t.expires_at > now()`, auth.HashToken(secret)).Scan(
			&tok.ID, &tok.Name, &readOnly, &lastUsed, &tok.ExpiresAt, &only, &p.TokenProjects, &tok.Projects,
			&p.UserID, &p.Email, &p.Name, &p.AvatarURL, &p.Locale, &active, &p.InstallationName, &p.Role, &on)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, auth.ErrInvalidToken
	case err != nil:
		return nil, err
	case !on:
		return nil, ErrSwitchedOff
	case !active:
		return nil, auth.ErrSwitchedOff
	}
	tok.Kind, tok.AllProjects, tok.Scopes = auth.TokenOAuth, !only, []string{}
	if readOnly {
		tok.Scopes = []string{auth.ScopeRead}
	}
	if tok.AllProjects {
		p.TokenProjects = nil
	}
	p.Token = &tok
	if lastUsed == nil || s.now().Sub(*lastUsed) > lastUsedEvery {
		_, _ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE oauth_grant SET last_used_at = now() WHERE id = $1`, tok.ID)
			return err
		})
	}
	return &p, nil
}

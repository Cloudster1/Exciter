package oauth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// The narrower token of #235: a client trades an access token of its own grant for a shorter one that may do less,
// for one run, and the grant keeps what its person allowed. Adapted from Armature's narrow (see NOTICE).
const (
	// ParamExpiresIn asks the narrower token to end sooner than its subject, in seconds.
	ParamExpiresIn = "exciter_expires_in"
	// ParamProjects limits the narrower token to some of the grant's projects, by key, space-separated.
	ParamProjects = "exciter_projects"
)

var errSubject = refuse(ErrInvalidGrant, "The subject_token is not a live access token of this client's own grant. "+
	"Refresh the grant's token and exchange the new one.")

// subject is the access token an exchange narrows, with what its grant allows.
type subject struct {
	grant        uuid.UUID
	client       string
	user         uuid.UUID
	held         scope
	exchanged    bool
	projectsOnly bool
	projects     []uuid.UUID
	remaining    time.Duration
}

// narrowScope reads the scope an exchange asks for against the one held: none keeps it, and nothing widens it.
func narrowScope(asked string, held scope) (scope, bool) {
	if strings.TrimSpace(asked) == "" {
		return held, true
	}
	sc, ok := scopeAsked(asked)
	if !ok || sc.evidence != held.evidence || (held.readOnly && !sc.readOnly) {
		return scope{}, false
	}
	return sc, true
}

// narrowLifetime is how long the narrower token lasts: what is asked, from a second to AccessTTL, and never longer
// than its subject has left, which is also what none asked gets.
func narrowLifetime(asked string, remaining time.Duration) (time.Duration, bool) {
	if asked == "" {
		return remaining, true
	}
	n, err := strconv.Atoi(asked)
	if err != nil || n < 1 || n > expiresIn(AccessTTL) {
		return 0, false
	}
	return min(time.Duration(n)*time.Second, remaining), true
}

// narrow trades an access token of the client's own grant for a narrower, shorter one; the grant is untouched and
// no refresh token is issued, so the new token ends at the latest with its subject's hour.
func (s *Service) narrow(ctx context.Context, c *client, in TokenRequest) (*TokenResponse, error) {
	if c.Kind == ClientPartner {
		return nil, refuse(ErrUnauthorizedClient, fmt.Sprintf(
			"%s exchanges identity provider tokens; it narrows its own by refreshing to read.", c.Name))
	}
	if in.SubjectTokenType != TokenTypeAccess {
		return nil, refuse(ErrInvalidRequest, "Exciter exchanges access tokens only: subject_token_type is "+TokenTypeAccess+".")
	}
	if in.RequestedTokenType != "" && in.RequestedTokenType != TokenTypeAccess {
		return nil, refuse(ErrInvalidRequest, "Exciter issues access tokens only: requested_token_type is "+TokenTypeAccess+".")
	}
	if in.Resource != "" && !s.isResource(in.Resource) {
		return nil, refuse(ErrInvalidTarget, "The one resource is the MCP endpoint, "+s.Resource()+".")
	}
	keys, err := projectKeys(strings.Fields(in.Projects))
	if err != nil {
		return nil, refuse(ErrInvalidScope, fmt.Sprintf("Name at most %d projects in %s.", auth.MaxTokenProjects, ParamProjects))
	}
	sub, err := s.subjectOf(ctx, in.SubjectToken)
	if err != nil {
		return nil, err
	}
	if sub.client != c.ID || sub.exchanged {
		return nil, errSubject
	}
	sc, ok := narrowScope(in.Scope, sub.held)
	if !ok {
		return nil, refuse(ErrInvalidScope, "An exchange may only narrow the grant's scope: to mcp:read, and an evidence grant stays evidence.")
	}
	ttl, ok := narrowLifetime(in.ExpiresIn, sub.remaining)
	if !ok {
		return nil, refuse(ErrInvalidRequest, fmt.Sprintf("%s is a whole number of seconds from 1 to %d.", ParamExpiresIn, expiresIn(AccessTTL)))
	}
	projects, err := s.reachable(ctx, sub, keys)
	if err != nil {
		return nil, err
	}
	var out *TokenResponse
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// The subject is held while the narrower token is made, so revoking it at once cannot be outrun.
		var live bool
		err := tx.QueryRow(ctx, `SELECT expires_at > now() FROM oauth_token WHERE token_hash = $1 FOR UPDATE`,
			auth.HashToken(in.SubjectToken)).Scan(&live)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !live) {
			return errSubject
		}
		if err != nil {
			return err
		}
		out, err = issueAccess(ctx, tx, sub.grant, sc, accessToken{ttl: ttl, exchanged: true, projectsOnly: len(keys) > 0, projects: projects})
		return err
	})
	if err != nil {
		return nil, err
	}
	out.IssuedTokenType = TokenTypeAccess
	return out, nil
}

// subjectOf reads the live access token an exchange names, with its grant.
func (s *Service) subjectOf(ctx context.Context, token string) (subject, error) {
	if auth.ParsePrefixed(token, AccessTokenPrefix) != nil {
		return subject{}, errSubject
	}
	var (
		sub  subject
		left float64
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT g.id, g.client_id, g.user_id, g.read_only OR t.read_only, g.evidence, t.exchanged, g.projects_only,
			       ARRAY(SELECT gp.project_id FROM oauth_grant_project gp WHERE gp.grant_id = g.id),
			       EXTRACT(EPOCH FROM t.expires_at - now())::float8
			FROM oauth_token t JOIN oauth_grant g ON g.id = t.grant_id
			WHERE t.token_hash = $1 AND t.kind = 'access' AND t.expires_at > now()`, auth.HashToken(token)).Scan(
			&sub.grant, &sub.client, &sub.user, &sub.held.readOnly, &sub.held.evidence, &sub.exchanged, &sub.projectsOnly, &sub.projects, &left)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return subject{}, errSubject
	}
	// Whole seconds, as expires_in counts them, so the answer never promises more than the subject has.
	sub.remaining = time.Duration(math.Floor(left)) * time.Second
	if err == nil && sub.remaining < time.Second {
		return subject{}, errSubject
	}
	return sub, err
}

// reachable resolves the keys a narrower token is limited to, read as its person within the grant's projects, so a
// key they cannot see, or one the grant does not reach, refuses alike.
func (s *Service) reachable(ctx context.Context, sub subject, keys []string) ([]uuid.UUID, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	as := db.WithUser(ctx, sub.user)
	if sub.projectsOnly {
		as = db.WithUserInProjects(ctx, sub.user, sub.projects)
	}
	var ids []uuid.UUID
	err := s.db.ReadPrimary(as, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT ARRAY(SELECT id FROM project WHERE key = ANY ($1))`, keys).Scan(&ids)
	})
	if err != nil {
		return nil, err
	}
	if len(ids) != len(keys) {
		return nil, refuse(ErrInvalidScope, "Name in "+ParamProjects+" only projects the grant reaches and its person may see.")
	}
	return ids, nil
}

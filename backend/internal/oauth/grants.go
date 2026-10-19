package oauth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// Who ended a grant, as the audit log names it (#189).
const (
	EndedByPerson        = "person"
	EndedByAdministrator = "administrator"
	EndedByClient        = "client"
	EndedByReusedToken   = "reused_token"
)

// ErrNoSuchGrant is a grant that is not the caller's to end, or is gone already.
var ErrNoSuchGrant = errors.New("that connection was not found")

// grantColumns name a grant's projects through the reader's own rights, as a token's list does, so a list never
// spells the key of a project its reader may not see.
const grantColumns = `g.id, c.id, c.name, c.uri, c.kind, g.redirect_uri,
	g.read_only OR (c.kind = 'partner' AND NOT 'write' = ANY (c.scopes)), g.evidence, NOT g.projects_only,
	ARRAY(SELECT p.key FROM oauth_grant_project gp JOIN project p ON p.id = gp.project_id WHERE gp.grant_id = g.id ORDER BY p.key),
	g.created_at, g.last_used_at`

// The lists are by last use, newest first, then those never used by when they were made.
const grantOrder = ` ORDER BY g.last_used_at DESC NULLS LAST, g.created_at DESC, g.id`

func scanGrant(row pgx.Row, extra ...any) (Grant, error) {
	var (
		g        Grant
		redirect string
		readOnly bool
		evidence bool
	)
	err := row.Scan(append([]any{&g.ID, &g.Client.ID, &g.Client.Name, &g.Client.URI, &g.Client.Kind, &redirect, &readOnly, &evidence, &g.AllProjects,
		&g.Projects, &g.CreatedAt, &g.LastUsedAt}, extra...)...)
	g.RedirectHost, g.Scopes = RedirectHost(redirect), []string{}
	switch {
	case evidence:
		g.Scopes = []string{auth.ScopeEvidence}
	case readOnly:
		g.Scopes = []string{auth.ScopeRead}
	}
	return g, err
}

// Grants are the clients a person connected, read under row level security like their tokens.
func (s *Service) Grants(ctx context.Context, person uuid.UUID) ([]Grant, error) {
	out := []Grant{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+grantColumns+` FROM oauth_grant g JOIN oauth_client c ON c.id = g.client_id
			WHERE g.user_id = $1`+grantOrder, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			g, err := scanGrant(rows)
			if err != nil {
				return err
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	return out, err
}

// EveryonesGrants are everybody's connected clients with whose each is; row level security shows them to
// administrators alone.
func (s *Service) EveryonesGrants(ctx context.Context) ([]OrgGrant, error) {
	out := []OrgGrant{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+grantColumns+`, u.id, u.email::text, u.name, COALESCE(u.avatar_url, ''), COALESCE(u.locale, '')
			FROM oauth_grant g JOIN oauth_client c ON c.id = g.client_id JOIN app_user u ON u.id = g.user_id`+grantOrder)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var owner auth.User
			g, err := scanGrant(rows, &owner.ID, &owner.Email, &owner.Name, &owner.AvatarURL, &owner.Locale)
			if err != nil {
				return err
			}
			out = append(out, OrgGrant{Grant: g, Owner: owner})
		}
		return rows.Err()
	})
	return out, err
}

// EndGrant ends one of the person's own grants and every token it holds, at once.
func (s *Service) EndGrant(ctx context.Context, person, id uuid.UUID, ip string) (db.LSN, error) {
	return s.end(ctx, &person, person, id, ip)
}

// EndAnybodysGrant ends anybody's grant, for an administrator.
func (s *Service) EndAnybodysGrant(ctx context.Context, actor, id uuid.UUID, ip string) (db.LSN, error) {
	return s.end(ctx, nil, actor, id, ip)
}

func (s *Service) end(ctx context.Context, owner *uuid.UUID, actor, id uuid.UUID, ip string) (db.LSN, error) {
	by := EndedByAdministrator
	if owner != nil {
		by = EndedByPerson
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var holder uuid.UUID
		var client string
		// The tokens go with the grant by its key, so nothing it handed out works after this commits.
		err := tx.QueryRow(ctx, `
			DELETE FROM oauth_grant g USING oauth_client c
			WHERE g.id = $1 AND c.id = g.client_id AND ($2::uuid IS NULL OR g.user_id = $2)
			RETURNING g.user_id, c.name`, id, owner).Scan(&holder, &client)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchGrant
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, endedEntry(id, actor, holder, client, by, ip))
	})
}

// endedEntry is the record of a grant ended; actor is nobody when the client or a reused token ended it.
func endedEntry(grant, actor, owner uuid.UUID, client, by, ip string) audit.Entry {
	return audit.Entry{Action: audit.ActionOAuthRevoked, TargetType: "oauth_grant", TargetID: &grant, Actor: actor, IP: ip,
		Data: map[string]any{"owner": owner, "client": client, "by": by}}
}

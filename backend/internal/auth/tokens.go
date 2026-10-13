package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Personal access tokens (#22), as Stator's: a token belongs to one person in one organization and acts as them
// there, never more. docs/api-contract-m1.md says how.
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
	// Projects are the keys the token is limited to, as far as the reader may see them; AllProjects says it is
	// not limited, and a limited token whose projects are all gone reaches nothing.
	Projects    []string `json:"projects"`
	AllProjects bool     `json:"allProjects"`
	// LastUsedAt moves at most once a minute; null until first use.
	LastUsedAt *time.Time `json:"lastUsedAt"`
	// ExpiresAt is null for a token that lasts until it is revoked.
	ExpiresAt *time.Time `json:"expiresAt"`
	CreatedAt time.Time  `json:"createdAt"`
	Secret    string     `json:"secret,omitempty"`
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

// CallingToken is the token a request rides on, as GET /auth/me reports it.
type CallingToken struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Scopes []string  `json:"scopes"`
	// Projects are the keys of the projects it is limited to; AllProjects says it is not limited.
	Projects    []string   `json:"projects"`
	AllProjects bool       `json:"allProjects"`
	ExpiresAt   *time.Time `json:"expiresAt"`
}

var (
	// ErrTokenName is a token without a name, or with a long one.
	ErrTokenName = errors.New("give the token a name of at most 100 characters")
	// ErrTokenScope is a scope nobody enforces, so a token never carries a promise the API does not keep.
	ErrTokenScope = errors.New("that is not a token scope; use read, or leave the scopes empty")
	// ErrTokenExpiry is an expiry that has already passed.
	ErrTokenExpiry = errors.New("the expiry has to be in the future")
	// ErrNoSuchToken is a token that is not the caller's to revoke.
	ErrNoSuchToken = errors.New("that token was not found")
	// ErrTokenProjects is more projects than a token names.
	ErrTokenProjects = errors.New("a token names at most 50 projects")
	// ErrNoSuchProject is a project the maker cannot see: naming it would not widen the token, but it would
	// read as if it had.
	ErrNoSuchProject = errors.New("a token can only name projects you can already see")
)

// ValidateScopes refuses any scope but read.
func ValidateScopes(scopes []string) error {
	for _, s := range scopes {
		if s != ScopeRead {
			return ErrTokenScope
		}
	}
	return nil
}

func (s *Service) validateNewToken(in *NewAPIToken) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > MaxTokenNameLength {
		return ErrTokenName
	}
	if err := ValidateScopes(in.Scopes); err != nil {
		return err
	}
	// Never nil: the column is not null, and the answer lists no scopes as [].
	in.Scopes = append([]string{}, slices.Compact(slices.Sorted(slices.Values(in.Scopes)))...)
	keys := make([]string, 0, len(in.Projects))
	for _, k := range in.Projects {
		if k = strings.ToUpper(strings.TrimSpace(k)); k != "" {
			keys = append(keys, k)
		}
	}
	in.Projects = append([]string{}, slices.Compact(slices.Sorted(slices.Values(keys)))...)
	if len(in.Projects) > MaxTokenProjects {
		return ErrTokenProjects
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()) {
		return ErrTokenExpiry
	}
	return nil
}

// CreateAPIToken makes a token for the caller in the organization the context names, under row level security
// like any other tenant data.
func (s *Service) CreateAPIToken(ctx context.Context, orgID, userID uuid.UUID, in NewAPIToken, ip string) (*APIToken, db.LSN, error) {
	if err := s.validateNewToken(&in); err != nil {
		return nil, 0, err
	}
	secret, digest, err := GenerateAPIToken()
	if err != nil {
		return nil, 0, err
	}
	tok := APIToken{Name: in.Name, Scopes: in.Scopes, Projects: in.Projects, AllProjects: len(in.Projects) == 0, ExpiresAt: in.ExpiresAt, Secret: secret}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO api_token (org_id, user_id, name, token_hash, scopes, expires_at, projects_only)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6)
			RETURNING id, created_at`,
			userID, tok.Name, digest, tok.Scopes, tok.ExpiresAt, !tok.AllProjects,
		).Scan(&tok.ID, &tok.CreatedAt); err != nil {
			return fmt.Errorf("create api token: %w", err)
		}
		if !tok.AllProjects {
			// The projects are read under the maker's own rights, so a key they cannot see inserts nothing and the
			// count refuses it.
			tag, err := tx.Exec(ctx, `
				INSERT INTO api_token_project (org_id, token_id, project_id)
				SELECT current_org_id(), $1, p.id FROM project p WHERE p.org_id = current_org_id() AND p.key = ANY ($2)`, tok.ID, tok.Projects)
			if err != nil {
				return fmt.Errorf("limit the token to its projects: %w", err)
			}
			if int(tag.RowsAffected()) != len(tok.Projects) {
				return ErrNoSuchProject
			}
		}
		return audit.Write(ctx, tx, orgID, audit.Entry{Action: audit.ActionTokenCreated, TargetType: "api_token", TargetID: &tok.ID, Actor: userID, IP: ip,
			Data: map[string]any{"name": tok.Name, "scopes": tok.Scopes, "projects": tok.Projects}})
	})
	if err != nil {
		return nil, 0, err
	}
	return &tok, lsn, nil
}

// tokenColumns name a token's projects through the reader's own rights, so a list never spells the key of a
// project its reader may not see.
const tokenColumns = `t.id, t.name, t.scopes, t.last_used_at, t.expires_at, t.created_at, NOT t.projects_only,
	ARRAY(SELECT p.key FROM api_token_project tp JOIN project p ON p.org_id = tp.org_id AND p.id = tp.project_id
	      WHERE tp.token_id = t.id ORDER BY p.key)`

func scanToken(row pgx.Row, extra ...any) (APIToken, error) {
	var t APIToken
	err := row.Scan(append([]any{&t.ID, &t.Name, &t.Scopes, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt, &t.AllProjects, &t.Projects}, extra...)...)
	return t, err
}

// ListAPITokens answers the caller's tokens in the organization, newest first and without their secrets.
func (s *Service) ListAPITokens(ctx context.Context, userID uuid.UUID) ([]APIToken, error) {
	out := []APIToken{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+tokenColumns+` FROM api_token t WHERE t.user_id = $1 ORDER BY t.created_at DESC, t.id`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanToken(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// ListOrgAPITokens answers every token in the organization, for an administrator.
func (s *Service) ListOrgAPITokens(ctx context.Context) ([]OrgAPIToken, error) {
	out := []OrgAPIToken{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT `+tokenColumns+`, u.id, u.email::text, u.name, COALESCE(u.avatar_url, ''), COALESCE(u.locale, '')
			FROM api_token t JOIN app_user u ON u.id = t.user_id
			ORDER BY t.created_at DESC, t.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var owner User
			t, err := scanToken(rows, &owner.ID, &owner.Email, &owner.Name, &owner.AvatarURL, &owner.Locale)
			if err != nil {
				return err
			}
			out = append(out, OrgAPIToken{APIToken: t, Owner: owner})
		}
		return rows.Err()
	})
	return out, err
}

// RevokeAPIToken deletes one of the caller's own tokens.
func (s *Service) RevokeAPIToken(ctx context.Context, orgID, userID, tokenID uuid.UUID, ip string) (db.LSN, error) {
	return s.revoke(ctx, orgID, &userID, userID, tokenID, ip)
}

// RevokeOrgAPIToken deletes anybody's token in the organization, for an administrator.
func (s *Service) RevokeOrgAPIToken(ctx context.Context, orgID, actor, tokenID uuid.UUID, ip string) (db.LSN, error) {
	return s.revoke(ctx, orgID, nil, actor, tokenID, ip)
}

func (s *Service) revoke(ctx context.Context, orgID uuid.UUID, owner *uuid.UUID, actor, tokenID uuid.UUID, ip string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var holder uuid.UUID
		err := tx.QueryRow(ctx, `
			DELETE FROM api_token WHERE id = $1 AND ($2::uuid IS NULL OR user_id = $2)
			RETURNING user_id`, tokenID, owner).Scan(&holder)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchToken
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, orgID, audit.Entry{Action: audit.ActionTokenRevoked, TargetType: "api_token", TargetID: &tokenID, Actor: actor, IP: ip,
			Data: map[string]any{"owner": holder}})
	})
}

// apiTokenPrincipalSQL resolves a token to its owner in its organization. The membership is joined, not left
// joined: a token outlives no membership.
const apiTokenPrincipalSQL = `
SELECT t.id, t.name, t.scopes, t.last_used_at, t.expires_at, t.projects_only,
       ARRAY(SELECT tp.project_id FROM api_token_project tp WHERE tp.token_id = t.id ORDER BY tp.project_id),
       ARRAY(SELECT p.key FROM api_token_project tp JOIN project p ON p.org_id = tp.org_id AND p.id = tp.project_id
             WHERE tp.token_id = t.id ORDER BY p.key),
       u.id, u.email::text, u.name, COALESCE(u.avatar_url, ''), COALESCE(u.locale, ''), u.is_active,
       o.id, o.slug, o.name, m.org_role
FROM api_token t
JOIN app_user u ON u.id = t.user_id
JOIN org o ON o.id = t.org_id AND o.archived_at IS NULL
JOIN org_member m ON m.org_id = t.org_id AND m.user_id = t.user_id
WHERE t.token_hash = $1 AND (t.expires_at IS NULL OR t.expires_at > now())`

func (s *Service) authenticateAPIToken(ctx context.Context, secret string) (*Principal, error) {
	if err := ParseAPIToken(secret); err != nil {
		return nil, err
	}
	var (
		p        Principal
		tok      CallingToken
		lastUsed *time.Time
		only     bool
		active   bool
		orgID    uuid.UUID
		orgSlug  string
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, apiTokenPrincipalSQL, HashToken(secret)).Scan(
			&tok.ID, &tok.Name, &tok.Scopes, &lastUsed, &tok.ExpiresAt, &only, &p.TokenProjects, &tok.Projects,
			&p.UserID, &p.Email, &p.Name, &p.AvatarURL, &p.Locale, &active,
			&orgID, &orgSlug, &p.OrgName, &p.Role,
		)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrUserInactive
	}
	tok.AllProjects = !only
	p.Token = &tok
	p.Org = &tenant.Org{ID: orgID, Slug: orgSlug}
	if lastUsed == nil || s.now().Sub(*lastUsed) > lastSeenEvery {
		_, _ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE api_token SET last_used_at = now() WHERE id = $1`, tok.ID)
			return err
		})
	}
	return &p, nil
}

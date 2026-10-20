package oidc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// Session is what a completed sign-in hands back to the HTTP layer.
type Session struct {
	UserID    uuid.UUID
	Secret    string
	ExpiresAt time.Time
	// Joined and Left are the provider groups the sign-in changed.
	Joined []string
	Left   []string
	// Role is the standing the sign-in left the person with.
	Role auth.OrgRole
	// SessionID and LSN let the caller's first reads see the sign-in's writes.
	SessionID uuid.UUID
	LSN       db.LSN
}

// SignIn turns a verified identity into a session. Somebody who is not a member
// is refused with ErrNotAMember, and their request to join is noted.
func (s *Service) SignIn(ctx context.Context, identity *Identity, ttl time.Duration, userAgent, ip string) (*Session, error) {
	// Authenticating is not being let in, or membership would be only a sign-in page; the refusal is
	// remembered, so an administrator lets them in with a click instead of an invitation.
	session, err := s.signIn(ctx, identity, ttl, userAgent, ip)
	if errors.Is(err, ErrNotAMember) {
		if noted := s.noteJoinRequest(ctx, identity); noted != nil {
			return nil, noted
		}
	}
	return session, err
}

// noteJoinRequest keeps the account and the request in their own transaction,
// because the refusal that led here rolled the sign-in's back.
func (s *Service) noteJoinRequest(ctx context.Context, identity *Identity) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		userID, err := upsertUser(ctx, tx, identity)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO join_request (user_id) VALUES ($1)
			ON CONFLICT DO NOTHING`, userID); err != nil {
			return fmt.Errorf("note the request to join: %w", err)
		}
		return nil
	})
	return err
}

func (s *Service) signIn(ctx context.Context, identity *Identity, ttl time.Duration, userAgent, ip string) (*Session, error) {
	out := Session{ExpiresAt: time.Now().Add(ttl)}
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		admitted, err := admit(ctx, tx, identity)
		if err != nil {
			return err
		}
		out.UserID, out.Role, out.Joined, out.Left = admitted.UserID, admitted.Role, admitted.Joined, admitted.Left
		id, secret, err := auth.OpenSession(ctx, tx, out.UserID, auth.ProofOIDC, out.ExpiresAt, userAgent, ip)
		out.SessionID, out.Secret = id, secret
		return err
	})
	if err != nil {
		return nil, err
	}
	out.LSN = lsn
	return &out, nil
}

// Admission is who a verified identity was let in as, and the provider groups that changed on the way.
type Admission struct {
	UserID       uuid.UUID
	Role         auth.OrgRole
	Joined, Left []string
}

// Admit lets a verified identity in as a sign-in would, without opening a session: a partner trading the person's
// token from the shared realm (#273) is them signing in there. Somebody not let in is ErrNotAMember, and noted
// nowhere, since they asked the partner rather than Exciter.
func (s *Service) Admit(ctx context.Context, tx db.DBTX, identity *Identity) (*Admission, error) {
	return admit(ctx, tx, identity)
}

func admit(ctx context.Context, tx db.DBTX, identity *Identity) (*Admission, error) {
	userID, err := upsertUser(ctx, tx, identity)
	if err != nil {
		return nil, err
	}
	out := Admission{UserID: userID}

	// The provider vouches for the person; whether the account is switched
	// on is still Exciter's to say, as it is at every other door.
	var active bool
	if err := tx.QueryRow(ctx, `SELECT is_active FROM app_user WHERE id = $1`, userID).Scan(&active); err != nil {
		return nil, err
	}
	if !active {
		return nil, auth.ErrSwitchedOff
	}

	var create bool
	err = tx.QueryRow(ctx, `SELECT create_groups FROM oidc_provider WHERE enabled`).Scan(&create)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	// Before the groups, whose rows need a membership to hang off: a mapped
	// group is an administrator's yes given in advance.
	if out.Role, err = followMapping(ctx, tx, userID, identity.Groups); err != nil {
		return nil, err
	}
	// A guest is in no group, whatever the provider says: a group would
	// carry them into every project it is granted.
	if out.Role != auth.RoleGuest {
		out.Joined, out.Left, err = syncGroups(ctx, tx, userID, identity.Groups, create)
		if err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// upsertUser finds the account an identity belongs to by issuer and subject,
// or makes one.
func upsertUser(ctx context.Context, tx db.DBTX, identity *Identity) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		UPDATE user_identity SET last_login_at = now()
		WHERE issuer = $1 AND subject = $2
		RETURNING user_id`, identity.Issuer, identity.Subject).Scan(&id)
	switch {
	case err == nil:
		// The provider is the source of truth for the name, and for the
		// address unless another account here already uses the new one.
		_, err = tx.Exec(ctx, `
			UPDATE app_user SET name = $2,
			    email = CASE WHEN EXISTS (SELECT 1 FROM app_user o WHERE o.email = $3 AND o.id <> $1)
			                 THEN email ELSE $3 END
			WHERE id = $1 AND (name <> $2 OR email <> $3)`, id, identity.Name, identity.Email)
		if err != nil {
			return uuid.Nil, fmt.Errorf("update the account: %w", err)
		}
		return id, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, fmt.Errorf("find the identity: %w", err)
	}

	// A first sign-in whose verified address already has an account, such as one named ahead of time, is
	// tied to it and named by the provider from then on; session_reaches still bounds where it may go.
	err = tx.QueryRow(ctx, `
		INSERT INTO app_user (email, name) VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, identity.Email, identity.Name).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("find or create the account: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_identity (issuer, subject, user_id) VALUES ($1, $2, $3)`,
		identity.Issuer, identity.Subject, id); err != nil {
		return uuid.Nil, fmt.Errorf("remember the identity: %w", err)
	}
	return id, nil
}

// syncGroups makes somebody's provider groups exactly what the token said.
// Groups kept by hand are left alone: the provider has no opinion about them.
func syncGroups(ctx context.Context, tx db.DBTX, userID uuid.UUID, claimed []string, create bool) (joined, left []string, err error) {
	if create {
		for _, ref := range claimed {
			// No conflict target: a local group already holding the name is
			// left as it is, and the claim simply matches nothing.
			if _, err := tx.Exec(ctx, `
				INSERT INTO groups (name, source, external_ref) VALUES ($1, 'oidc', $1)
				ON CONFLICT DO NOTHING`, ref); err != nil {
				return nil, nil, fmt.Errorf("create the group %q the provider named: %w", ref, err)
			}
		}
	}

	known, err := refsToIDs(ctx, tx, `
		SELECT external_ref, id FROM groups
		WHERE source = 'oidc' AND external_ref = ANY($1)`, claimed)
	if err != nil {
		return nil, nil, err
	}
	current, err := refsToIDs(ctx, tx, `
		SELECT g.external_ref, g.id FROM group_member m JOIN groups g ON g.id = m.group_id
		WHERE m.user_id = $1 AND g.source = 'oidc'`, userID)
	if err != nil {
		return nil, nil, err
	}

	var available []string
	for _, ref := range claimed {
		if _, ok := known[ref]; ok {
			available = append(available, ref)
		}
	}
	join, leave := diffGroups(keys(current), available)
	for _, ref := range leave {
		if _, err := tx.Exec(ctx, `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, current[ref], userID); err != nil {
			return nil, nil, fmt.Errorf("leave the group %q: %w", ref, err)
		}
	}
	for _, ref := range join {
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, known[ref], userID); err != nil {
			return nil, nil, fmt.Errorf("join the group %q: %w", ref, err)
		}
	}
	return join, leave, nil
}

func refsToIDs(ctx context.Context, tx db.DBTX, sql string, args ...any) (map[string]uuid.UUID, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("read groups: %w", err)
	}
	defer rows.Close()
	out := map[string]uuid.UUID{}
	for rows.Next() {
		var ref string
		var id uuid.UUID
		if err := rows.Scan(&ref, &id); err != nil {
			return nil, err
		}
		out[ref] = id
	}
	return out, rows.Err()
}

func keys(m map[string]uuid.UUID) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

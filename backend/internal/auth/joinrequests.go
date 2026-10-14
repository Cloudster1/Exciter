package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// JoinRequest is somebody the identity provider vouched for who is waiting
// to be let in.
type JoinRequest struct {
	UserID      uuid.UUID `json:"userId"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	RequestedAt time.Time `json:"requestedAt"`
}

var (
	// ErrNoSuchRequest is returned when the person named is not waiting here.
	ErrNoSuchRequest = errors.New("nobody by that name is waiting to be let in")
	// ErrBadJoinRole is returned for a standing a waiting person cannot be given.
	ErrBadJoinRole = errors.New("a person is let in as a member or an administrator")
)

// JoinRequests lists who is waiting to be let in, oldest first.
func (s *Service) JoinRequests(ctx context.Context) ([]JoinRequest, error) {
	out := []JoinRequest{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.email::text, u.name, r.created_at
			FROM org_join_request r JOIN app_user u ON u.id = r.user_id
			ORDER BY r.created_at, u.email`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r JoinRequest
			if err := rows.Scan(&r.UserID, &r.Email, &r.Name, &r.RequestedAt); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// AdmitJoinRequest lets a waiting person in with the standing given. Their
// next sign-in through the provider then succeeds.
func (s *Service) AdmitJoinRequest(ctx context.Context, userID uuid.UUID, role OrgRole, actor uuid.UUID, ip string) (*Membership, db.LSN, error) {
	if role != RoleAdmin && role != RoleMember {
		return nil, 0, ErrBadJoinRole
	}
	made := Membership{UserID: userID, Role: role}
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM org_join_request WHERE user_id = $1`, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSuchRequest
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO org_member (user_id, org_role) VALUES ($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET org_role = EXCLUDED.org_role, role_source = 'manual'`,
			userID, string(role)); err != nil {
			return fmt.Errorf("create membership: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberAdmitted, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip,
			Data: map[string]any{"role": role}})
	})
	if err != nil {
		return nil, 0, err
	}
	return &made, lsn, nil
}

// DeclineJoinRequest forgets the request. The person may sign in again and
// ask once more; nothing about the request is kept but the record of the refusal.
func (s *Service) DeclineJoinRequest(ctx context.Context, userID, actor uuid.UUID, ip string) (db.LSN, error) {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM org_join_request WHERE user_id = $1`, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSuchRequest
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberDeclined, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip})
	})
}

// EnsureMember lets an address into the installation ahead of its first sign-in,
// making the account if need be; a membership already there is left alone.
func (s *Service) EnsureMember(ctx context.Context, email string, role OrgRole) (bool, error) {
	email = NormalizeEmail(email)
	if !strings.Contains(email, "@") {
		return false, fmt.Errorf("%q is not an email address", email)
	}
	if role != RoleOwner && role != RoleAdmin && role != RoleMember {
		return false, fmt.Errorf("%q is not a role; use owner, admin or member", role)
	}
	name, _, _ := strings.Cut(email, "@")
	var added bool
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var userID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO app_user (email, name) VALUES ($1, $2)
			ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
			RETURNING id`, email, name).Scan(&userID); err != nil {
			return fmt.Errorf("find or create %s: %w", email, err)
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO org_member (user_id, org_role) VALUES ($1, $2)
			ON CONFLICT (user_id) DO NOTHING`, userID, string(role))
		added = tag.RowsAffected() == 1
		return err
	})
	return added, err
}

package auth

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Members lists the people of the organization on ctx, owners first, then by
// name. It reads through row level security like any other tenant data.
func (s *Service) Members(ctx context.Context) ([]Member, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, err
	}
	out := []Member{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.email::text, u.name, m.org_role, m.role_source, m.created_at
			FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = $1
			ORDER BY CASE m.org_role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'member' THEN 2 ELSE 3 END, lower(u.name), u.email`, org.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Member
			if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.RoleSource, &m.JoinedAt); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// memberRole locks a membership for the change about to be made and says its role.
func memberRole(ctx context.Context, tx db.DBTX, org, userID uuid.UUID) (OrgRole, error) {
	var role OrgRole
	err := tx.QueryRow(ctx, `SELECT org_role FROM org_member WHERE org_id = $1 AND user_id = $2 FOR UPDATE`, org, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSuchMember
	}
	return role, err
}

// SetRole gives a member of the organization on ctx another standing, which the provider then leaves alone until a
// mapped group applies. The owner and the caller keep theirs, so nobody locks themselves or everybody else out.
func (s *Service) SetRole(ctx context.Context, userID uuid.UUID, role OrgRole, actor uuid.UUID, ip string) (*Member, db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	if !slices.Contains(AssignableRoles, role) {
		return nil, 0, ErrBadRole
	}
	if userID == actor {
		return nil, 0, ErrChangeOwnRole
	}
	var out Member
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		was, err := memberRole(ctx, tx, org.ID, userID)
		if err != nil {
			return err
		}
		if was == RoleOwner {
			return ErrOwnerStays
		}
		if err := tx.QueryRow(ctx, `
			UPDATE org_member m SET org_role = $3, role_source = 'manual', updated_at = now()
			FROM app_user u WHERE u.id = m.user_id AND m.org_id = $1 AND m.user_id = $2
			RETURNING u.id, u.email::text, u.name, m.org_role, m.role_source, m.created_at`, org.ID, userID, string(role),
		).Scan(&out.UserID, &out.Email, &out.Name, &out.Role, &out.RoleSource, &out.JoinedAt); err != nil {
			return err
		}
		if was == role {
			return nil
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: audit.ActionMemberRoleChanged, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip,
			Data: map[string]any{"from": was, "to": role}})
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}

// RemoveMember takes somebody out of the organization on ctx; their open
// sessions stop reaching it at once, as every request checks.
func (s *Service) RemoveMember(ctx context.Context, userID, actor uuid.UUID, ip string) (db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return 0, err
	}
	if userID == actor {
		return 0, ErrRemoveSelf
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		role, err := memberRole(ctx, tx, org.ID, userID)
		if err != nil {
			return err
		}
		if role == RoleOwner {
			return ErrOwnerStays
		}
		// The group rows would outlive the membership otherwise: their guard checks membership only when a row is written.
		if _, err := tx.Exec(ctx, `DELETE FROM group_member WHERE org_id = $1 AND user_id = $2`, org.ID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM org_member WHERE org_id = $1 AND user_id = $2`, org.ID, userID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: audit.ActionMemberRemoved, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip,
			Data: map[string]any{"role": role}})
	})
}

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

// memberSQL reads a member as Member holds one, in memberFields' order.
const memberSQL = `
	SELECT u.id, u.email::text, u.name, m.org_role, m.role_source, m.created_at, u.is_active
	FROM org_member m JOIN app_user u ON u.id = m.user_id`

func memberFields(m *Member) []any {
	return []any{&m.UserID, &m.Email, &m.Name, &m.Role, &m.RoleSource, &m.JoinedAt, &m.Active}
}

// Members lists the installation's people, those switched on first, owners first among them, then by name. It
// reads through row level security like any other tenant data.
func (s *Service) Members(ctx context.Context) ([]Member, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, err
	}
	out := []Member{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, memberSQL+`
			WHERE m.org_id = $1
			ORDER BY NOT u.is_active, CASE m.org_role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'member' THEN 2 ELSE 3 END,
				lower(u.name), u.email`, org.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Member
			if err := rows.Scan(memberFields(&m)...); err != nil {
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

// SetRole gives a member of the installation another standing, which the provider then leaves alone until a
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
			return ErrOwnerStanding
		}
		if _, err := tx.Exec(ctx, `
			UPDATE org_member SET org_role = $3, role_source = 'manual', updated_at = now()
			WHERE org_id = $1 AND user_id = $2`, org.ID, userID, string(role)); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, memberSQL+` WHERE m.org_id = $1 AND m.user_id = $2`, org.ID, userID).Scan(memberFields(&out)...); err != nil {
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

// SetActive switches a member's account off or on. Off ends their sessions and tokens at once and keeps their
// account, roles and work, so switching them on brings them back as they were.
func (s *Service) SetActive(ctx context.Context, userID uuid.UUID, active bool, actor uuid.UUID, ip string) (*Member, db.LSN, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	if userID == actor && !active {
		return nil, 0, ErrSwitchSelfOff
	}
	var out Member
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		role, err := memberRole(ctx, tx, org.ID, userID)
		if err != nil {
			return err
		}
		if !active {
			if err := mayBeSwitchedOff(ctx, tx, org.ID, userID, role, actor); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE app_user SET is_active = $2 WHERE id = $1 AND is_active <> $2`, userID, active)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, memberSQL+` WHERE m.org_id = $1 AND m.user_id = $2`, org.ID, userID).Scan(memberFields(&out)...); err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		action := audit.ActionMemberSwitchedOn
		if !active {
			action = audit.ActionMemberSwitchedOff
			if _, err := tx.Exec(ctx, `DELETE FROM api_token WHERE user_id = $1`, userID); err != nil {
				return err
			}
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: action, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip,
			Data: map[string]any{"role": role}})
	})
	if err != nil {
		return nil, lsn, err
	}
	if !active {
		// Sessions are the admin role's alone; the request after this one would refuse them anyway, as it reads the account.
		if lsn, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `DELETE FROM user_session WHERE user_id = $1`, userID)
			return err
		}); err != nil {
			return nil, lsn, err
		}
	}
	return &out, lsn, nil
}

// mayBeSwitchedOff refuses switching off an owner for anybody but an owner, and the installation's last active
// administrator for anybody at all.
func mayBeSwitchedOff(ctx context.Context, tx db.DBTX, org, userID uuid.UUID, role OrgRole, actor uuid.UUID) error {
	if role == RoleOwner {
		var actorRole OrgRole
		if err := tx.QueryRow(ctx, `SELECT org_role FROM org_member WHERE org_id = $1 AND user_id = $2`, org, actor).Scan(&actorRole); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if actorRole != RoleOwner {
			return ErrOwnerOnly
		}
	}
	if !role.CanAdminister() {
		return nil
	}
	var others bool
	if err := tx.QueryRow(ctx, `SELECT administered_without($1)`, userID).Scan(&others); err != nil {
		return err
	}
	if !others {
		return ErrLastAdministrator
	}
	return nil
}

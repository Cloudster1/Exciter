package auth

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// memberSQL reads the members as Member holds one, in memberFields' order; a caller narrows it with AND.
const memberSQL = `
	SELECT u.id, u.email::text, u.name, u.role, u.role_source, u.joined_at, u.is_active
	FROM app_user u WHERE u.role IS NOT NULL`

func memberFields(m *Member) []any {
	return []any{&m.UserID, &m.Email, &m.Name, &m.Role, &m.RoleSource, &m.JoinedAt, &m.Active}
}

// Members lists the installation's people, those switched on first, owners first among them, then by name. It
// reads through row level security like any other data.
func (s *Service) Members(ctx context.Context) ([]Member, error) {
	out := []Member{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, memberSQL+`
			ORDER BY NOT u.is_active, CASE u.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'member' THEN 2 ELSE 3 END,
				lower(u.name), u.email`)
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
func memberRole(ctx context.Context, tx db.DBTX, userID uuid.UUID) (OrgRole, error) {
	var role OrgRole
	err := tx.QueryRow(ctx, `SELECT role FROM app_user WHERE id = $1 AND role IS NOT NULL FOR UPDATE`, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSuchMember
	}
	return role, err
}

// SetRole gives a member of the installation another standing, which the provider then leaves alone until a
// mapped group applies. The owner and the caller keep theirs, so nobody locks themselves or everybody else out.
func (s *Service) SetRole(ctx context.Context, userID uuid.UUID, role OrgRole, actor uuid.UUID, ip string) (*Member, db.LSN, error) {
	if !slices.Contains(AssignableRoles, role) {
		return nil, 0, ErrBadRole
	}
	if userID == actor {
		return nil, 0, ErrChangeOwnRole
	}
	var out Member
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		was, err := memberRole(ctx, tx, userID)
		if err != nil {
			return err
		}
		if was == RoleOwner {
			return ErrOwnerStanding
		}
		if _, err := tx.Exec(ctx, `
			UPDATE app_user SET role = $2, role_source = 'manual' WHERE id = $1`, userID, string(role)); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, memberSQL+` AND u.id = $1`, userID).Scan(memberFields(&out)...); err != nil {
			return err
		}
		if was == role {
			return nil
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberRoleChanged, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip,
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
	if userID == actor && !active {
		return nil, 0, ErrSwitchSelfOff
	}
	var out Member
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		role, err := memberRole(ctx, tx, userID)
		if err != nil {
			return err
		}
		if !active {
			if err := mayBeSwitchedOff(ctx, tx, userID, role, actor); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE app_user SET is_active = $2 WHERE id = $1 AND is_active <> $2`, userID, active)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, memberSQL+` AND u.id = $1`, userID).Scan(memberFields(&out)...); err != nil {
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
		return audit.Write(ctx, tx, audit.Entry{Action: action, TargetType: "user", TargetID: &userID, Actor: actor, IP: ip,
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
func mayBeSwitchedOff(ctx context.Context, tx db.DBTX, userID uuid.UUID, role OrgRole, actor uuid.UUID) error {
	if role == RoleOwner {
		var actorRole OrgRole
		if err := tx.QueryRow(ctx, `SELECT COALESCE(role, '') FROM app_user WHERE id = $1`, actor).Scan(&actorRole); err != nil && !errors.Is(err, pgx.ErrNoRows) {
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

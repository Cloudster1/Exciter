package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// memberSQL reads the members as Member holds one, in memberFields' order; a caller narrows it with AND.
const memberSQL = `
	SELECT u.id, u.email::text, u.name, u.role, u.joined_at, u.is_active, perm_is_administrator(u.id),
	       COALESCE((SELECT jsonb_agg(jsonb_build_object('role', r.key, 'name', r.name, 'groupId', g.id,
	                                                     'groupName', g.name, 'groupBuiltin', g.builtin)
	                                  ORDER BY r.sort_order, lower(r.name), g.builtin NULLS LAST, lower(g.name) NULLS FIRST)
	                 FROM role_assignment a
	                 JOIN role r ON r.key = a.role
	                 LEFT JOIN groups g ON g.id = a.group_id
	                 WHERE a.project_id IS NULL AND u.role = 'member'
	                   AND (a.user_id = u.id OR EXISTS (
	                       SELECT 1 FROM group_member gm WHERE gm.group_id = a.group_id AND gm.user_id = u.id))), '[]')
	FROM app_user u WHERE u.role IS NOT NULL`

func memberFields(m *Member) []any {
	return []any{&m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt, &m.Active, &m.Administers, &m.Roles}
}

// Members lists the installation's people, those switched on first, members before guests, then by name, each
// with the roles they hold across it. It reads through row level security like any other data.
func (s *Service) Members(ctx context.Context) ([]Member, error) {
	out := []Member{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, memberSQL+`
			ORDER BY NOT u.is_active, u.role = 'guest', lower(u.name), u.email`)
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

// memberRole locks a membership for the change about to be made and says its standing.
func memberRole(ctx context.Context, tx db.DBTX, userID uuid.UUID) (OrgRole, error) {
	var role OrgRole
	err := tx.QueryRow(ctx, `SELECT role FROM app_user WHERE id = $1 AND role IS NOT NULL FOR UPDATE`, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSuchMember
	}
	return role, err
}

// SetRole makes a member a guest or a guest a member. The caller keeps theirs, so nobody locks themselves out, and
// the last active administrator stays a member, which the database holds to as well.
func (s *Service) SetRole(ctx context.Context, userID uuid.UUID, role OrgRole, actor uuid.UUID, ip string) (*Member, db.LSN, error) {
	if !slices.Contains(OrgRoles, role) {
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
		if was == role {
			return tx.QueryRow(ctx, memberSQL+` AND u.id = $1`, userID).Scan(memberFields(&out)...)
		}
		if role == RoleGuest {
			if err := keepsAnAdministrator(ctx, tx, userID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE app_user SET role = $2 WHERE id = $1`, userID, string(role)); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, memberSQL+` AND u.id = $1`, userID).Scan(memberFields(&out)...); err != nil {
			return err
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
			if err := keepsAnAdministrator(ctx, tx, userID); err != nil {
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

// keepsAnAdministrator refuses taking the installation's last active administrator out of it.
func keepsAnAdministrator(ctx context.Context, tx db.DBTX, userID uuid.UUID) error {
	var administers, others bool
	if err := tx.QueryRow(ctx, `SELECT perm_is_administrator($1), administered_without($1)`, userID).Scan(&administers, &others); err != nil {
		return err
	}
	if administers && !others {
		return ErrLastAdministrator
	}
	return nil
}

// GrantAdministrator grants somebody the built-in administrator role across the installation, in the caller's
// transaction; a grant there already is kept.
func GrantAdministrator(ctx context.Context, tx db.DBTX, userID uuid.UUID, by *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO role_assignment (role, user_id, created_by) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, AdministratorRole, userID, by); err != nil {
		return fmt.Errorf("make the person an administrator: %w", err)
	}
	return nil
}

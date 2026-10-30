package oidc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// GroupRole grants a role to everybody the provider names in a group.
type GroupRole struct {
	ID uuid.UUID `json:"id"`
	// Group is a value of the groups claim, matched exactly as the provider sends it.
	Group     string       `json:"group"`
	Role      auth.OrgRole `json:"role"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

// ErrNoSuchGroupRole is returned for a mapping that is not there.
var ErrNoSuchGroupRole = errors.New("that group is not mapped to a role")

// roleRank orders the roles a mapping may grant; a role it may not grant has none.
var roleRank = map[auth.OrgRole]int{auth.RoleMember: 1, auth.RoleAdmin: 2}

// grantedRole is the highest role the mapping grants for the groups claimed,
// with the groups that grant it; an empty role when none of them is mapped.
func grantedRole(mapping map[string]auth.OrgRole, claimed []string) (auth.OrgRole, []string) {
	var best auth.OrgRole
	from := []string{}
	for _, group := range claimed {
		role, ok := mapping[group]
		if !ok || roleRank[role] == 0 {
			continue
		}
		switch {
		case roleRank[role] > roleRank[best]:
			best, from = role, []string{group}
		case role == best:
			from = append(from, group)
		}
	}
	slices.Sort(from)
	return best, from
}

// standing is somebody's role in the installation and who decided it.
type standing struct {
	Role   auth.OrgRole
	Source auth.RoleSource
}

// nextStanding is what a sign-in makes of a membership, given the role the
// mapping grants and whether a mapped group lets somebody new in; nil means
// the person is not let in.
func nextStanding(current *standing, granted auth.OrgRole, admit bool) *standing {
	switch {
	case current == nil && (granted == "" || !admit):
		// Nobody here said yes, and no mapped group says it in advance.
		return nil
	case current == nil:
		return &standing{Role: granted, Source: auth.RoleSourceProvider}
	case current.Role == auth.RoleOwner, current.Role == auth.RoleGuest:
		// A guest's standing is their invitation's, as an owner's is their own.
		return current
	case granted != "":
		return &standing{Role: granted, Source: auth.RoleSourceProvider}
	case current.Source == auth.RoleSourceProvider:
		// The group that granted the role has gone; membership itself stays
		// an administrator's to take away, so the floor is member.
		return &standing{Role: auth.RoleMember, Source: auth.RoleSourceManual}
	default:
		return current
	}
}

// GroupRoles lists the mapping, by group.
func (s *Service) GroupRoles(ctx context.Context) ([]GroupRole, error) {
	out := []GroupRole{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id, group_ref, role, updated_at FROM oidc_group_role ORDER BY group_ref`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g GroupRole
			if err := rows.Scan(&g.ID, &g.Group, &g.Role, &g.UpdatedAt); err != nil {
				return err
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read the group roles: %w", err)
	}
	return out, nil
}

// SetGroupRole maps a group to a role, or changes the role it maps to. It
// takes effect at each member's next sign-in, when their groups are known.
func (s *Service) SetGroupRole(ctx context.Context, group string, role auth.OrgRole, actor uuid.UUID, ip string) (*GroupRole, db.LSN, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return nil, 0, &ValidationError{Field: "group", Message: "Enter the group's name as the provider sends it in the groups claim."}
	}
	if roleRank[role] == 0 {
		return nil, 0, &ValidationError{Field: "role", Message: "Map the group to member or admin."}
	}
	var saved GroupRole
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var configured bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM oidc_provider)`).Scan(&configured); err != nil {
			return err
		}
		if !configured {
			return &ValidationError{Field: "group", Message: "Set up single sign-on first, then map its groups to roles."}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO oidc_group_role (group_ref, role) VALUES ($1, $2)
			ON CONFLICT (group_ref) DO UPDATE SET role = EXCLUDED.role
			RETURNING id, group_ref, role, updated_at`, group, string(role),
		).Scan(&saved.ID, &saved.Group, &saved.Role, &saved.UpdatedAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionGroupRoleSet, TargetType: "group_role", TargetID: &saved.ID, Actor: actor, IP: ip,
			Data: map[string]any{"group": group, "role": role}})
	})
	if err != nil {
		var invalid *ValidationError
		if errors.As(err, &invalid) {
			return nil, lsn, err
		}
		return nil, lsn, fmt.Errorf("map the group: %w", err)
	}
	return &saved, lsn, nil
}

// RemoveGroupRole unmaps a group. Roles it granted fall back to member at
// each person's next sign-in, unless another mapped group grants one.
func (s *Service) RemoveGroupRole(ctx context.Context, id, actor uuid.UUID, ip string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var group, role string
		err := tx.QueryRow(ctx, `DELETE FROM oidc_group_role WHERE id = $1 RETURNING group_ref, role`, id).Scan(&group, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchGroupRole
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionGroupRoleRemoved, TargetType: "group_role", TargetID: &id, Actor: actor, IP: ip,
			Data: map[string]any{"group": group, "role": role}})
	})
}

// Where somebody was let in by their group, as the audit log names it.
const (
	viaSignIn        = "sign_in"
	viaTokenExchange = "token_exchange"
)

// followMapping settles a sign-in's membership by the mapping, inside the
// sign-in's transaction, and records every role it changes. Somebody new is
// let in by a mapped group only when admit is on; via says where.
func followMapping(ctx context.Context, tx db.DBTX, userID uuid.UUID, claimed []string, admit bool, via string) (auth.OrgRole, error) {
	mapping := map[string]auth.OrgRole{}
	rows, err := tx.Query(ctx, `SELECT group_ref, role FROM oidc_group_role`)
	if err != nil {
		return "", fmt.Errorf("read the group roles: %w", err)
	}
	for rows.Next() {
		var group string
		var role auth.OrgRole
		if err := rows.Scan(&group, &role); err != nil {
			rows.Close()
			return "", err
		}
		mapping[group] = role
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}

	var current *standing
	var found standing
	err = tx.QueryRow(ctx, `
		SELECT role, role_source FROM app_user WHERE id = $1 AND role IS NOT NULL FOR UPDATE`,
		userID).Scan(&found.Role, &found.Source)
	switch {
	case err == nil:
		current = &found
	case !errors.Is(err, pgx.ErrNoRows):
		return "", err
	}

	granted, groups := grantedRole(mapping, claimed)
	next := nextStanding(current, granted, admit)
	if next == nil {
		return "", ErrNotAMember
	}
	if current != nil && *next == *current {
		return next.Role, nil
	}

	if current == nil {
		if _, err := tx.Exec(ctx, `
			UPDATE app_user SET role = $2, role_source = $3 WHERE id = $1`,
			userID, string(next.Role), string(next.Source)); err != nil {
			return "", fmt.Errorf("let the person in by their group: %w", err)
		}
		// A request noted before the group was mapped has been answered now.
		if _, err := tx.Exec(ctx, `DELETE FROM join_request WHERE user_id = $1`, userID); err != nil {
			return "", err
		}
		return next.Role, audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberJoined, TargetType: "user", TargetID: &userID,
			Data: map[string]any{"role": next.Role, "group": groups[0], "groups": groups, "via": via}})
	}

	if _, err := tx.Exec(ctx, `
		UPDATE app_user SET role = $2, role_source = $3 WHERE id = $1`,
		userID, string(next.Role), string(next.Source)); err != nil {
		return "", fmt.Errorf("follow the group roles: %w", err)
	}
	// A hand-set role a mapped group now manages changes who decides it, not
	// the role, and only a changed role is an event worth recording.
	if next.Role == current.Role {
		return next.Role, nil
	}
	return next.Role, audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberRoleChanged, TargetType: "user", TargetID: &userID,
		Data: map[string]any{"from": current.Role, "to": next.Role, "groups": groups}})
}

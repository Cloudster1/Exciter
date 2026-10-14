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
	"github.com/cloudster1/exciter/backend/internal/tenant"
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

// standing is somebody's role in an organization and who decided it.
type standing struct {
	Role   auth.OrgRole
	Source auth.RoleSource
}

// nextStanding is what a sign-in makes of a membership, given the role the
// mapping grants; nil means the person is not let in.
func nextStanding(current *standing, granted auth.OrgRole) *standing {
	switch {
	case current == nil && granted == "":
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

// GroupRoles lists the mapping of the organization on ctx, by group.
func (s *Service) GroupRoles(ctx context.Context) ([]GroupRole, error) {
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, err
	}
	out := []GroupRole{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id, group_ref, org_role, updated_at FROM oidc_group_role
			WHERE org_id = $1 ORDER BY group_ref`, org.ID)
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
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
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
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM oidc_provider WHERE org_id = $1)`, org.ID).Scan(&configured); err != nil {
			return err
		}
		if !configured {
			return &ValidationError{Field: "group", Message: "Set up single sign-on first, then map its groups to roles."}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, $2, $3)
			ON CONFLICT (group_ref) DO UPDATE SET org_role = EXCLUDED.org_role
			RETURNING id, group_ref, org_role, updated_at`, org.ID, group, string(role),
		).Scan(&saved.ID, &saved.Group, &saved.Role, &saved.UpdatedAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: audit.ActionGroupRoleSet, TargetType: "group_role", TargetID: &saved.ID, Actor: actor, IP: ip,
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
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var group, role string
		err := tx.QueryRow(ctx, `DELETE FROM oidc_group_role WHERE org_id = $1 AND id = $2 RETURNING group_ref, org_role`, org.ID, id).Scan(&group, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchGroupRole
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{Action: audit.ActionGroupRoleRemoved, TargetType: "group_role", TargetID: &id, Actor: actor, IP: ip,
			Data: map[string]any{"group": group, "role": role}})
	})
}

// followMapping settles a sign-in's membership by the mapping, inside the
// sign-in's transaction, and records every role it changes.
func followMapping(ctx context.Context, tx db.DBTX, orgID, userID uuid.UUID, claimed []string) (auth.OrgRole, error) {
	mapping := map[string]auth.OrgRole{}
	rows, err := tx.Query(ctx, `SELECT group_ref, org_role FROM oidc_group_role WHERE org_id = $1`, orgID)
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
		SELECT org_role, role_source FROM org_member WHERE org_id = $1 AND user_id = $2 FOR UPDATE`,
		orgID, userID).Scan(&found.Role, &found.Source)
	switch {
	case err == nil:
		current = &found
	case !errors.Is(err, pgx.ErrNoRows):
		return "", err
	}

	granted, groups := grantedRole(mapping, claimed)
	next := nextStanding(current, granted)
	if next == nil {
		return "", ErrNotAMember
	}
	if current != nil && *next == *current {
		return next.Role, nil
	}

	if current == nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO org_member (org_id, user_id, org_role, role_source) VALUES ($1, $2, $3, $4)`,
			orgID, userID, string(next.Role), string(next.Source)); err != nil {
			return "", fmt.Errorf("let the person in by their group: %w", err)
		}
		// A request noted before the group was mapped has been answered now.
		if _, err := tx.Exec(ctx, `DELETE FROM org_join_request WHERE org_id = $1 AND user_id = $2`, orgID, userID); err != nil {
			return "", err
		}
		return next.Role, audit.Write(ctx, tx, orgID, audit.Entry{Action: audit.ActionMemberJoined, TargetType: "user", TargetID: &userID,
			Data: map[string]any{"role": next.Role, "groups": groups}})
	}

	if _, err := tx.Exec(ctx, `
		UPDATE org_member SET org_role = $3, role_source = $4 WHERE org_id = $1 AND user_id = $2`,
		orgID, userID, string(next.Role), string(next.Source)); err != nil {
		return "", fmt.Errorf("follow the group roles: %w", err)
	}
	// A hand-set role a mapped group now manages changes who decides it, not
	// the role, and only a changed role is an event worth recording.
	if next.Role == current.Role {
		return next.Role, nil
	}
	return next.Role, audit.Write(ctx, tx, orgID, audit.Entry{Action: audit.ActionMemberRoleChanged, TargetType: "user", TargetID: &userID,
		Data: map[string]any{"from": current.Role, "to": next.Role, "groups": groups}})
}

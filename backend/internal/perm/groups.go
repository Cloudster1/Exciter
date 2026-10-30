package perm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// Limits of a group's name and the claim that fills it.
const (
	MaxGroupNameLength = 120
	MaxGroupRefLength  = 255
)

var (
	// ErrNoSuchGroup is returned for a group that is not there.
	ErrNoSuchGroup = errors.New("there is no such group")
	// ErrBuiltinGroup is returned for changing Everyone, whose members are every member of the installation.
	ErrBuiltinGroup = errors.New("everyone holds every member and is not changed by hand")
	// ErrProviderGroup is returned for changing by hand who is in a group the identity provider fills, which the
	// next sign-in would undo.
	ErrProviderGroup = errors.New("the identity provider fills this group at every sign-in")
)

// GroupInput makes a group or changes one; nil leaves a field alone on a change.
type GroupInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	// ExternalRef is the name in the provider's groups claim that fills the group at every sign-in; empty keeps
	// its members by hand.
	ExternalRef *string `json:"externalRef,omitempty"`
}

// GroupMemberInput names somebody to put in a group.
type GroupMemberInput struct {
	UserID uuid.UUID `json:"userId"`
}

func readGroup(ctx context.Context, tx db.DBTX, id uuid.UUID, lock bool) (*Group, error) {
	sql := selectGroup + ` WHERE g.id = $1`
	if lock {
		sql += ` FOR UPDATE OF g`
	}
	rows, err := tx.Query(ctx, sql, id)
	if err != nil {
		return nil, err
	}
	g, err := pgx.CollectExactlyOneRow(rows, scanGroupRow)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSuchGroup
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Group reads one group with its members, for the installation's administrators.
func (s *Service) Group(ctx context.Context, id uuid.UUID) (*Group, error) {
	var out *Group
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		var err error
		out, err = withMembers(ctx, tx, id)
		return err
	})
	return out, err
}

func withMembers(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Group, error) {
	g, err := readGroup(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT u.id, u.name, u.email::text FROM group_member gm JOIN app_user u ON u.id = gm.user_id
		WHERE gm.group_id = $1 ORDER BY lower(u.name), u.email`, id)
	if err != nil {
		return nil, err
	}
	members, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Person])
	if err != nil {
		return nil, err
	}
	g.Members = nonNil(members)
	return g, nil
}

func groupFields(in GroupInput, current *Group) (name, description string, ref *string, err error) {
	if current != nil {
		name, description = current.Name, current.Description
		if current.ExternalRef != "" {
			r := current.ExternalRef
			ref = &r
		}
	}
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
	}
	if in.ExternalRef != nil {
		ref = nil
		if r := strings.TrimSpace(*in.ExternalRef); r != "" {
			ref = &r
		}
	}
	switch {
	case name == "":
		return "", "", nil, &FieldError{Field: "name", Message: "Give the group a name."}
	case utf8.RuneCountInString(name) > MaxGroupNameLength:
		return "", "", nil, &FieldError{Field: "name", Message: fmt.Sprintf("Keep the name to %d characters.", MaxGroupNameLength)}
	case ref != nil && utf8.RuneCountInString(*ref) > MaxGroupRefLength:
		return "", "", nil, &FieldError{Field: "externalRef", Message: fmt.Sprintf("Keep the provider's group name to %d characters.", MaxGroupRefLength)}
	}
	return name, description, ref, nil
}

// groupRefusal reads the database's refusal of a group as a field error.
func groupRefusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if strings.Contains(pgErr.ConstraintName, "ref") {
			return &FieldError{Field: "externalRef", Message: "Another group carries that provider claim already. Use that group, or name another claim."}
		}
		return &FieldError{Field: "name", Message: "There is already a group with that name. Choose another name."}
	}
	return err
}

func sourceOf(ref *string) string {
	if ref != nil {
		return "oidc"
	}
	return "local"
}

// CreateGroup makes a group, kept by hand or filled by the provider's claim.
func (s *Service) CreateGroup(ctx context.Context, actor uuid.UUID, in GroupInput) (*Group, db.LSN, error) {
	name, description, ref, err := groupFields(in, nil)
	if err != nil {
		return nil, 0, err
	}
	var out *Group
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO groups (name, description, source, external_ref) VALUES ($1, $2, $3, $4) RETURNING id`,
			name, description, sourceOf(ref), ref).Scan(&id); err != nil {
			return groupRefusal(err)
		}
		var err error
		if out, err = withMembers(ctx, tx, id); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionGroupCreated, "group", &id, map[string]any{"name": name, "externalRef": ref})
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// UpdateGroup renames a group, describes it, or changes the claim that fills it. Giving a group kept by hand a
// claim hands its members to the provider.
func (s *Service) UpdateGroup(ctx context.Context, actor, id uuid.UUID, in GroupInput) (*Group, db.LSN, error) {
	var out *Group
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		current, err := readGroup(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if current.Builtin != "" {
			return ErrBuiltinGroup
		}
		name, description, ref, err := groupFields(in, current)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE groups SET name = $2, description = $3, source = $4, external_ref = $5 WHERE id = $1`,
			id, name, description, sourceOf(ref), ref); err != nil {
			return groupRefusal(err)
		}
		if out, err = withMembers(ctx, tx, id); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionGroupUpdated, "group", &id, map[string]any{"name": name, "from": current.Name, "externalRef": ref})
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// DeleteGroup removes a group and every grant to it.
func (s *Service) DeleteGroup(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		current, err := readGroup(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if current.Builtin != "" {
			return ErrBuiltinGroup
		}
		if _, err := tx.Exec(ctx, `DELETE FROM groups WHERE id = $1`, id); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionGroupDeleted, "group", &id, map[string]any{"name": current.Name, "grants": current.RoleCount})
	})
	return lsn, Refusal(err)
}

// AddGroupMember puts a member in a group kept by hand.
func (s *Service) AddGroupMember(ctx context.Context, actor, id, userID uuid.UUID) (*Group, db.LSN, error) {
	var out *Group
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		current, err := editableGroup(ctx, tx, id)
		if err != nil {
			return err
		}
		var member bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1 AND role IS NOT NULL)`, userID).Scan(&member); err != nil {
			return err
		}
		if !member {
			return &FieldError{Field: "userId", Message: "That person is not a member here. Pick somebody from the list."}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, userID); err != nil {
			return err
		}
		if out, err = withMembers(ctx, tx, id); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionGroupMemberAdded, "group", &id, map[string]any{"name": current.Name, "userId": userID})
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// RemoveGroupMember takes somebody out of a group kept by hand.
func (s *Service) RemoveGroupMember(ctx context.Context, actor, id, userID uuid.UUID) (*Group, db.LSN, error) {
	var out *Group
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		current, err := editableGroup(ctx, tx, id)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, id, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return &FieldError{Field: "userId", Message: "That person is not in the group. Reload the group; somebody may have taken them out already."}
		}
		if out, err = withMembers(ctx, tx, id); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionGroupMemberRemoved, "group", &id, map[string]any{"name": current.Name, "userId": userID})
	})
	if err != nil {
		return nil, lsn, Refusal(err)
	}
	return out, lsn, nil
}

func editableGroup(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Group, error) {
	g, err := readGroup(ctx, tx, id, true)
	switch {
	case err != nil:
		return nil, err
	case g.Builtin != "":
		return nil, ErrBuiltinGroup
	case g.FromProvider:
		return nil, ErrProviderGroup
	}
	return g, nil
}

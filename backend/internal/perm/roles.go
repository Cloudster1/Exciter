package perm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// Permission is one thing a role may hold: a project permission, which counts in the project a grant names or,
// granted across the installation, in every project, or a permission of the installation as a whole, which counts
// only granted across it.
type Permission string

const (
	PermCreateProjects         Permission = auth.PermissionCreateProjects
	PermAdministerInstallation Permission = auth.PermissionAdministerInstallation
)

// AllPermissions is every permission Exciter checks, in the order the matrix lists them: what a project allows,
// then what the installation does.
var AllPermissions = []Permission{
	Permission(RightView), Permission(RightComment), Permission(RightEdit), Permission(RightApprove), Permission(RightAdminister),
	PermCreateProjects, PermAdministerInstallation,
}

// PermissionWords says what each permission lets somebody do, for people who read the matrix rather than parse
// it; the web app says it in their language.
var PermissionWords = map[Permission]string{
	Permission(RightView):       "Read the project's requirements, their history and links",
	Permission(RightComment):    "Comment on requirements",
	Permission(RightEdit):       "Write and change requirements",
	Permission(RightApprove):    "Approve revisions",
	Permission(RightAdminister): "Administer the project: its settings, grants, trash and archive",
	PermCreateProjects:          "Create projects",
	PermAdministerInstallation:  "Administer Exciter",
}

// PermissionScope says where a permission counts.
type PermissionScope string

const (
	// ScopeProject is a permission that counts in a project.
	ScopeProject PermissionScope = "project"
	// ScopeInstallation is a permission that counts across the installation only.
	ScopeInstallation PermissionScope = "installation"
)

// PermissionScopes lists every PermissionScope, for the API document.
var PermissionScopes = []PermissionScope{ScopeProject, ScopeInstallation}

// PermissionView is a permission as the matrix lists it.
type PermissionView struct {
	Permission Permission      `json:"permission"`
	Words      string          `json:"words"`
	Scope      PermissionScope `json:"scope"`
}

// Permissions lists every permission with what it means.
func Permissions() []PermissionView {
	out := make([]PermissionView, 0, len(AllPermissions))
	for _, p := range AllPermissions {
		scope := ScopeProject
		if p == PermCreateProjects || p == PermAdministerInstallation {
			scope = ScopeInstallation
		}
		out = append(out, PermissionView{Permission: p, Words: PermissionWords[p], Scope: scope})
	}
	return out
}

// The built-in roles' keys, which nobody renames: the administrator of the installation, and in a project the
// project administrator, editor, reviewer and viewer, whose keys the workflow conditions and field rules name.
const (
	RoleAdministrator        = auth.AdministratorRole
	RoleProjectAdministrator = "admin"
	RoleEditor               = "editor"
	RoleReviewer             = "reviewer"
	RoleViewer               = "viewer"
)

// Definition is one of the installation's roles: what it is called and what it holds. The built-in ones are rows
// like any other, editable but not deletable; a role an administrator added is wholly theirs.
type Definition struct {
	Key         string `json:"role"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// OrgWideOnly is a role that answers for the whole installation and is never granted in one project.
	OrgWideOnly bool         `json:"orgWideOnly"`
	Builtin     bool         `json:"builtin"`
	Permissions []Permission `json:"permissions"`
	// InUse counts the grants that name it, so a listing can say what deleting it would take away.
	InUse int `json:"inUse"`
}

// RoleInput is a role as the matrix sends it; nil leaves a field alone on an edit.
type RoleInput struct {
	Key         string       `json:"key,omitempty"`
	Name        *string      `json:"name,omitempty"`
	Description *string      `json:"description,omitempty"`
	OrgWideOnly *bool        `json:"orgWideOnly,omitempty"`
	Permissions []Permission `json:"permissions,omitempty"`
}

var (
	// ErrNoSuchRole is returned for a role nobody defined.
	ErrNoSuchRole = errors.New("there is no such role")
	// ErrBuiltinRole is returned when a built-in role is deleted.
	ErrBuiltinRole = errors.New("a built-in role cannot be deleted; take its grants away instead")
)

var roleKey = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// KeyFromName makes a key out of a name: lowercase words joined by underscores, which is what the built-in keys
// look like.
func KeyFromName(name string) string {
	var b strings.Builder
	last := '_'
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			last = r
		case last != '_':
			b.WriteRune('_')
			last = '_'
		}
	}
	key := strings.Trim(b.String(), "_")
	if key != "" && (key[0] < 'a' || key[0] > 'z') {
		key = "r_" + key
	}
	if len(key) > 40 {
		key = strings.TrimRight(key[:40], "_")
	}
	return key
}

const selectRoles = `
SELECT r.key, r.name, r.description, r.installation_wide_only, r.builtin, r.permissions,
       (SELECT count(*)::int FROM role_assignment a WHERE a.role = r.key)
FROM role r`

func scanDefinition(row pgx.Row) (*Definition, error) {
	var (
		d     Definition
		perms []string
	)
	err := row.Scan(&d.Key, &d.Name, &d.Description, &d.OrgWideOnly, &d.Builtin, &perms, &d.InUse)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSuchRole
	}
	if err != nil {
		return nil, err
	}
	d.Permissions = make([]Permission, 0, len(perms))
	for _, p := range AllPermissions {
		if slices.Contains(perms, string(p)) {
			d.Permissions = append(d.Permissions, p)
		}
	}
	return &d, nil
}

// Roles lists the installation's roles, the built-in ones first as they were installed, then the others by name.
func (s *Service) Roles(ctx context.Context) ([]Definition, error) {
	out := []Definition{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRoles+` ORDER BY r.sort_order, lower(r.name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDefinition(rows)
			if err != nil {
				return err
			}
			out = append(out, *d)
		}
		return rows.Err()
	})
	return out, err
}

func cleanPermissions(in []Permission) ([]string, error) {
	out := []string{}
	for _, p := range in {
		if !slices.Contains(AllPermissions, p) {
			return nil, &FieldError{Field: "permissions", Message: fmt.Sprintf("There is no permission %q. Pick permissions from the matrix.", p)}
		}
	}
	for _, p := range AllPermissions {
		if slices.Contains(in, p) {
			out = append(out, string(p))
		}
	}
	return out, nil
}

// roleRefusal reads the database's refusal of a role change as a field error or a conflict, each a sentence.
func roleRefusal(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == "23505":
		return &FieldError{Field: "name", Message: "There is already a role with that name. Choose another name."}
	case pgErr.ConstraintName == "role_key_shape":
		return &FieldError{Field: "key", Message: "A role's key is 2 to 40 lowercase letters, digits and underscores, starting with a letter."}
	case pgErr.ConstraintName == "role_name_not_blank":
		return &FieldError{Field: "name", Message: "Give the role a name."}
	case pgErr.ConstraintName == "role_assignment_guest_not_administrator", pgErr.ConstraintName == "role_assignment_everyone_administers":
		return &FieldError{Field: "permissions", Message: pgErr.Message}
	}
	return refusal(err, "permissions")
}

// CreateRole adds a role of the installation's own.
func (s *Service) CreateRole(ctx context.Context, actor uuid.UUID, in RoleInput) (*Definition, db.LSN, error) {
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		return nil, 0, &FieldError{Field: "name", Message: "Give the role a name."}
	}
	key := strings.TrimSpace(in.Key)
	if key == "" {
		key = KeyFromName(name)
	}
	if !roleKey.MatchString(key) {
		return nil, 0, &FieldError{Field: "key", Message: "A role's key is 2 to 40 lowercase letters, digits and underscores, starting with a letter."}
	}
	perms, err := cleanPermissions(in.Permissions)
	if err != nil {
		return nil, 0, err
	}
	description := ""
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
	}
	whole := in.OrgWideOnly != nil && *in.OrgWideOnly

	var out *Definition
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM role WHERE key = $1)`, key).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return &FieldError{Field: "key", Message: fmt.Sprintf("There is already a role with the key %s. Choose another name or key.", key)}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO role (key, name, description, installation_wide_only, permissions, sort_order)
			VALUES ($1, $2, $3, $4, $5, 100)`, key, name, description, whole, perms); err != nil {
			return roleRefusal(err)
		}
		var err error
		if out, err = scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1`, key)); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionRoleCreated, "role", nil, map[string]any{"role": key, "name": name, "permissions": perms})
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// UpdateRole renames a role, describes it, or changes what it holds; every grant of it follows from the next
// request.
func (s *Service) UpdateRole(ctx context.Context, actor uuid.UUID, key string, in RoleInput) (*Definition, db.LSN, error) {
	var perms []string
	if in.Permissions != nil {
		cleaned, err := cleanPermissions(in.Permissions)
		if err != nil {
			return nil, 0, err
		}
		perms = cleaned
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, 0, &FieldError{Field: "name", Message: "Give the role a name."}
	}
	var out *Definition
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		current, err := scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1 FOR UPDATE OF r`, key))
		if err != nil {
			return err
		}
		name, description, whole := current.Name, current.Description, current.OrgWideOnly
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		if in.OrgWideOnly != nil && !current.Builtin {
			whole = *in.OrgWideOnly
		}
		before := make([]string, 0, len(current.Permissions))
		for _, p := range current.Permissions {
			before = append(before, string(p))
		}
		if perms == nil {
			perms = before
		}
		if whole && !current.OrgWideOnly {
			var inProjects bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM role_assignment WHERE role = $1 AND project_id IS NOT NULL)`, key).Scan(&inProjects); err != nil {
				return err
			}
			if inProjects {
				return &FieldError{Field: "orgWideOnly", Message: "This role is granted in projects. Take those grants away before it answers for the whole installation only."}
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE role SET name = $2, description = $3, installation_wide_only = $4, permissions = $5
			WHERE key = $1`, key, name, description, whole, perms); err != nil {
			return roleRefusal(err)
		}
		if out, err = scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1`, key)); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionRoleUpdated, "role", nil, map[string]any{
			"role": key, "name": name, "from": current.Name, "permissions": perms, "before": before})
	})
	if err != nil {
		return nil, lsn, Refusal(err)
	}
	return out, lsn, nil
}

// DeleteRole removes a role that is not built in, and every grant of it.
func (s *Service) DeleteRole(ctx context.Context, actor uuid.UUID, key string) (db.LSN, error) {
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx); err != nil {
			return err
		}
		current, err := scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1 FOR UPDATE OF r`, key))
		if err != nil {
			return err
		}
		if current.Builtin {
			return ErrBuiltinRole
		}
		if _, err := tx.Exec(ctx, `DELETE FROM role WHERE key = $1`, key); err != nil {
			return err
		}
		return Record(ctx, tx, actor, audit.ActionRoleDeleted, "role", nil, map[string]any{"role": key, "name": current.Name, "grants": current.InUse})
	})
	return lsn, Refusal(err)
}

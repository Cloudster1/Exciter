// Package installation is the one way to the installation's name and settings, which live on the context tenant's
// org row until the schema drops it (#91). Adapted from Armature's.
package installation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// Slug is the address of the organization a first start makes; nothing outside reads it once sign-in stops asking
// for one (#85).
const Slug = "exciter"

// MaxNameLength keeps a name to what the sidebar head shows on one line or two.
const MaxNameLength = 80

// Installation is what everybody signed in may read about it.
type Installation struct {
	Name string `json:"name"`
}

var (
	// ErrNameRequired is a rename to nothing.
	ErrNameRequired = errors.New("the installation needs a name")
	// ErrNameTooLong is a name past MaxNameLength.
	ErrNameTooLong = fmt.Errorf("the installation's name is longer than %d characters", MaxNameLength)
	// ErrNotFound is a tenant in the context with no row behind it, or a rename an administrator may not make.
	ErrNotFound = errors.New("the installation was not found")
)

// Service serves the installation's own page.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

// Get reads the installation.
func (s *Service) Get(ctx context.Context) (*Installation, error) {
	var out Installation
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out.Name, err = Name(ctx, tx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Rename gives the installation a new name and records who did; the database lets only an administrator.
func (s *Service) Rename(ctx context.Context, actor uuid.UUID, name string) (*Installation, db.LSN, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return nil, 0, ErrNameRequired
	case utf8.RuneCountInString(name) > MaxNameLength:
		return nil, 0, ErrNameTooLong
	}
	org, err := tenant.MustFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := Name(ctx, tx)
		if err != nil {
			return err
		}
		if err := set(ctx, tx, "name", name); err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{
			Action: audit.ActionInstallationRenamed, TargetType: "installation", TargetID: &org.ID, Actor: actor,
			Data: map[string]any{"from": before, "to": name},
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return &Installation{Name: name}, lsn, nil
}

// Name is what the installation is called.
func Name(ctx context.Context, tx db.DBTX) (string, error) {
	var name string
	return name, get(ctx, tx, "name", &name)
}

// DefaultTheme is the shared theme shown to whoever has not chosen one, or nil for the built-in one.
func DefaultTheme(ctx context.Context, tx db.DBTX) (*uuid.UUID, error) {
	var id *uuid.UUID
	return id, get(ctx, tx, "default_theme_id", &id)
}

// SetDefaultTheme names the default theme; the database refuses one not shared, and anybody but an administrator.
func SetDefaultTheme(ctx context.Context, tx db.DBTX, id *uuid.UUID) error {
	return set(ctx, tx, "default_theme_id", id)
}

// get and set take a column from the fixed list above, never from a caller.
func get(ctx context.Context, tx db.DBTX, column string, into any) error {
	err := tx.QueryRow(ctx, `SELECT `+column+` FROM org WHERE id = current_org_id()`).Scan(into)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read the installation's %s: %w", column, err)
	}
	return nil
}

func set(ctx context.Context, tx db.DBTX, column string, value any) error {
	tag, err := tx.Exec(ctx, `UPDATE org SET `+column+` = $1 WHERE id = current_org_id()`, value)
	if err != nil {
		return fmt.Errorf("change the installation's %s: %w", column, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

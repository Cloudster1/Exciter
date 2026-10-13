// Package testorg makes and removes throwaway organizations, so each browser spec runs in an organization of its
// own. The api serves it only when EXCITER_TEST_ENDPOINTS is on, which no production deployment sets.
package testorg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/oidc"
	"github.com/cloudster1/exciter/backend/internal/seed"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

// ErrNotFound is returned for a slug that names no throwaway organization,
// which includes every organization made any other way.
var ErrNotFound = errors.New("no throwaway organization has that slug")

// Slug limits: the org table allows 40 characters, and the random suffix
// keeps two runs of one spec apart.
const (
	maxLabelLength = 24
	suffixBytes    = 4
	defaultLabel   = "e2e"
)

// mark is what org.settings carries for an organization made here. Delete
// removes nothing without it, so a mistyped slug cannot take a real one.
const mark = `{"throwaway": true}`

// Org is a throwaway organization as the endpoint returns it.
type Org struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
}

// Service makes organizations with the seed's owner and people in them.
type Service struct {
	db       *db.Cluster
	accounts *auth.Service
	sso      *oidc.Service
	store    objectstore.Store
	// owner is the bootstrap administrator, who owns each organization, so
	// the provider is configured in their name as the seed configures demo's.
	owner  string
	people seed.People
}

func NewService(cluster *db.Cluster, accounts *auth.Service, sso *oidc.Service, store objectstore.Store, owner string, people seed.People) *Service {
	return &Service{db: cluster, accounts: accounts, sso: sso, store: store, owner: owner, people: people}
}

// Create makes an organization whose slug starts with the label, owned by the bootstrap administrator, with the
// seed's people let in and the seed's provider configured.
func (s *Service) Create(ctx context.Context, label string) (*Org, error) {
	suffix := make([]byte, suffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		return nil, err
	}
	label = slugPart(label)
	org := Org{Slug: label + "-" + hex.EncodeToString(suffix)}
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `INSERT INTO org (slug, name, settings) VALUES ($1, $2, $3) RETURNING id`,
			org.Slug, "Test "+label, mark).Scan(&org.ID)
	})
	if err != nil {
		return nil, fmt.Errorf("make organization %s: %w", org.Slug, err)
	}
	if err := s.populate(ctx, org); err != nil {
		_ = s.Delete(context.WithoutCancel(ctx), org.Slug)
		return nil, fmt.Errorf("populate organization %s: %w", org.Slug, err)
	}
	return &org, nil
}

func (s *Service) populate(ctx context.Context, org Org) error {
	if _, err := s.accounts.EnsureMember(tenant.WithOrg(ctx, tenant.Org{ID: org.ID, Slug: org.Slug}), s.owner, auth.RoleOwner); err != nil {
		return err
	}
	owner, err := s.accounts.AccountID(ctx, s.owner)
	if err != nil {
		return err
	}
	_, err = seed.Populate(db.WithUser(ctx, owner), s.accounts, s.sso, tenant.Org{ID: org.ID, Slug: org.Slug}, s.people)
	return err
}

// Delete removes a throwaway organization with every row that names it and every file under its prefix, and the
// sessions proven there, which reach nothing once it is gone.
func (s *Service) Delete(ctx context.Context, slug string) error {
	var id uuid.UUID
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT id FROM org WHERE slug = $1 AND settings @> $2 FOR UPDATE`,
			strings.ToLower(strings.TrimSpace(slug)), mark).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_session WHERE current_org_id = $1`, id); err != nil {
			return err
		}
		// Cleared first: the cascade to theme would otherwise set it null on
		// the very row this statement is deleting.
		if _, err := tx.Exec(ctx, `UPDATE org SET default_theme_id = NULL WHERE id = $1`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM org WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return err
	}
	if objectstore.IsUnavailable(s.store) {
		return nil
	}
	if err := objectstore.DeletePrefix(ctx, s.store, objectstore.OrgPrefix(id)); err != nil {
		return fmt.Errorf("the organization is gone, but not all of its files: %w", err)
	}
	return nil
}

// slugPart keeps what the org table accepts in a slug, so any label works.
func slugPart(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
		if b.Len() >= maxLabelLength {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return defaultLabel
	}
	return out
}

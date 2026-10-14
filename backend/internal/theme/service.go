package theme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
)

var (
	ErrNotFound      = errors.New("theme not found")
	ErrDuplicateName = errors.New("you already have a theme with that name")
	ErrNotYours      = errors.New("only the owner or an administrator can change a theme")
	ErrAssetNotFound = errors.New("that file is not in the theme")
	// ErrNotAdmin is returned when somebody who does not administer the
	// installation names its default theme.
	ErrNotAdmin = errors.New("only an administrator can name the default theme for everyone")
	// ErrDefaultNotShared is returned when a theme nobody else can see is made
	// the installation's default.
	ErrDefaultNotShared = errors.New("the default for everyone has to be a shared theme")
)

// Theme is one saved theme as the page and the loader see it.
type Theme struct {
	ID        uuid.UUID `json:"id"`
	OwnerID   uuid.UUID `json:"ownerId"`
	OwnerName string    `json:"ownerName"`
	Name      string    `json:"name"`
	Shared    bool      `json:"shared"`
	Spec      Spec      `json:"spec"`
	Assets    []Asset   `json:"assets"`
	// InUse counts the people who chose it; Active says the reader is one;
	// Default says the installation shows it to whoever has not chosen.
	InUse     int       `json:"inUse"`
	Active    bool      `json:"active"`
	Default   bool      `json:"default"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Asset is one uploaded file of a theme.
type Asset struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Input is a theme as the page sends it; nil leaves a field alone on an edit.
type Input struct {
	Name   *string `json:"name,omitempty"`
	Shared *bool   `json:"shared,omitempty"`
	Spec   *Spec   `json:"spec,omitempty"`
}

// Service keeps themes and their files, the rows in Postgres and the bytes in
// the object store.
type Service struct {
	db    *db.Cluster
	store objectstore.Store

	// installed is the installation's id, which never changes; files keep its prefix from before #107.
	mu        sync.Mutex
	installed uuid.UUID
}

func NewService(cluster *db.Cluster, store objectstore.Store) *Service {
	return &Service{db: cluster, store: store}
}

// selectThemes reads themes with the reader's marks; $1 is the reader. The
// count comes from theme_in_use, since a person sees only their own choice.
const selectThemes = `
SELECT t.id, t.owner_id, COALESCE(u.name, ''), t.name, t.shared, t.spec, t.created_at, t.updated_at,
       theme_in_use(t.id),
       EXISTS (SELECT 1 FROM user_theme ut WHERE ut.theme_id = t.id AND ut.user_id = $1),
       EXISTS (SELECT 1 FROM org o WHERE o.default_theme_id = t.id)
FROM theme t
LEFT JOIN app_user u ON u.id = t.owner_id`

// visible is what a reader may see: their own themes and the shared ones. Row
// level security says the same; the service says it too, for the admin role.
const visible = ` (t.owner_id = $1 OR t.shared)`

func scan(row pgx.Row) (*Theme, error) {
	var (
		t   Theme
		raw []byte
	)
	err := row.Scan(&t.ID, &t.OwnerID, &t.OwnerName, &t.Name, &t.Shared, &raw, &t.CreatedAt, &t.UpdatedAt, &t.InUse, &t.Active, &t.Default)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &t.Spec); err != nil {
		return nil, fmt.Errorf("read theme %s: %w", t.ID, err)
	}
	t.Spec.normalise()
	t.Assets = []Asset{}
	return &t, nil
}

// withAssets fills in the files of every theme listed.
func withAssets(ctx context.Context, tx db.DBTX, themes []*Theme) error {
	if len(themes) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(themes))
	byID := map[uuid.UUID]*Theme{}
	for _, t := range themes {
		ids = append(ids, t.ID)
		byID[t.ID] = t
	}
	rows, err := tx.Query(ctx, `
		SELECT theme_id, id, name, content_type, size, created_at FROM theme_asset
		WHERE theme_id = ANY($1) ORDER BY created_at`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			themeID uuid.UUID
			a       Asset
		)
		if err := rows.Scan(&themeID, &a.ID, &a.Name, &a.ContentType, &a.Size, &a.CreatedAt); err != nil {
			return err
		}
		byID[themeID].Assets = append(byID[themeID].Assets, a)
	}
	return rows.Err()
}

func assetSet(t *Theme) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, a := range t.Assets {
		out[a.ID] = true
	}
	return out
}

// List is every theme the reader may see: theirs first, then the shared ones.
func (s *Service) List(ctx context.Context, reader uuid.UUID) ([]Theme, error) {
	out := []Theme{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectThemes+` WHERE`+visible+` ORDER BY (t.owner_id <> $1), lower(t.name)`, reader)
		if err != nil {
			return err
		}
		defer rows.Close()
		var themes []*Theme
		for rows.Next() {
			t, err := scan(rows)
			if err != nil {
				return err
			}
			themes = append(themes, t)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := withAssets(ctx, tx, themes); err != nil {
			return err
		}
		for _, t := range themes {
			out = append(out, *t)
		}
		return nil
	})
	return out, err
}

func readOne(ctx context.Context, tx db.DBTX, reader, id uuid.UUID, where string) (*Theme, error) {
	t, err := scan(tx.QueryRow(ctx, selectThemes+where, reader, id))
	if err != nil {
		return nil, err
	}
	return t, withAssets(ctx, tx, []*Theme{t})
}

// Get is one theme the reader may see.
func (s *Service) Get(ctx context.Context, id, reader uuid.UUID) (*Theme, error) {
	var out *Theme
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = readOne(ctx, tx, reader, id, ` WHERE t.id = $2 AND`+visible)
		return err
	})
	return out, err
}

// Source says where the theme somebody sees came from.
type Source string

const (
	// SourceChosen is a theme the person picked for themselves.
	SourceChosen Source = "chosen"
	// SourceEveryone is the default for everyone, shown to whoever has not chosen a theme.
	SourceEveryone Source = "everyone"
	// SourceBuiltIn is the stylesheet's own look: nothing chosen and no
	// default, or the built-in theme chosen over the default.
	SourceBuiltIn Source = ""
)

// Sources are the answers Active gives, for a client to name.
var Sources = []Source{SourceChosen, SourceEveryone, SourceBuiltIn}

// Active is the reader's chosen theme, else the installation's default, else
// nil for the built-in one.
func (s *Service) Active(ctx context.Context, reader uuid.UUID) (*Theme, Source, error) {
	var (
		out    *Theme
		source Source
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var chosen *uuid.UUID
		err := tx.QueryRow(ctx, `SELECT theme_id FROM user_theme WHERE user_id = $1`, reader).Scan(&chosen)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			fallback, err := installation.DefaultTheme(ctx, tx)
			if err != nil {
				return err
			}
			if fallback == nil {
				return nil
			}
			chosen, source = fallback, SourceEveryone
		case err != nil:
			return err
		case chosen == nil:
			return nil
		default:
			source = SourceChosen
		}
		out, err = readOne(ctx, tx, reader, *chosen, ` WHERE t.id = $2 AND`+visible)
		if errors.Is(err, ErrNotFound) {
			// Unshared since it was chosen: the choice lapses quietly.
			out, source = nil, SourceBuiltIn
			return nil
		}
		return err
	})
	return out, source, err
}

// Default is the installation's default theme, nil for the built-in one,
// whatever the reader chose for themselves: what the installation looks like.
func (s *Service) Default(ctx context.Context, reader uuid.UUID) (*Theme, error) {
	var out *Theme
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, err := installation.DefaultTheme(ctx, tx)
		if err != nil || id == nil {
			return err
		}
		out, err = readOne(ctx, tx, reader, *id, ` WHERE t.id = $2 AND`+visible)
		if errors.Is(err, ErrNotFound) {
			out = nil
			return nil
		}
		return err
	})
	return out, err
}

// Choose makes a theme the reader's own. Nil returns them to the
// installation's; nil with builtIn keeps the built-in one.
func (s *Service) Choose(ctx context.Context, reader uuid.UUID, id *uuid.UUID, builtIn bool) (*Theme, db.LSN, error) {
	var out *Theme
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if id == nil && !builtIn {
			_, err := tx.Exec(ctx, `DELETE FROM user_theme WHERE user_id = $1`, reader)
			return err
		}
		if id != nil {
			t, err := readOne(ctx, tx, reader, *id, ` WHERE t.id = $2 AND`+visible)
			if err != nil {
				return err
			}
			t.Active = true
			out = t
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO user_theme (user_id, theme_id) VALUES ($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET theme_id = EXCLUDED.theme_id`, reader, id)
		return err
	})
	return out, lsn, err
}

// SetDefault names the shared theme the installation shows to whoever has not
// chosen, or nil for the built-in one; the database refuses a private one.
func (s *Service) SetDefault(ctx context.Context, reader uuid.UUID, id *uuid.UUID) (*Theme, db.LSN, error) {
	var out *Theme
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		switch err := installation.SetDefaultTheme(ctx, tx, id); {
		case errors.Is(err, installation.ErrNotFound):
			return ErrNotAdmin
		case isCheck(err):
			return ErrDefaultNotShared
		case err != nil:
			return err
		}
		entry := audit.Entry{Action: audit.ActionThemeDefaultSet, TargetType: "theme", TargetID: id, Actor: reader,
			Data: map[string]any{"builtIn": true}}
		if id != nil {
			t, err := readOne(ctx, tx, reader, *id, ` WHERE t.id = $2 AND`+visible)
			if err != nil {
				return err
			}
			out = t
			entry.Data = map[string]any{"name": t.Name}
		}
		return audit.Write(ctx, tx, entry)
	})
	return out, lsn, err
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheck(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

package theme

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Following the Armature theme (#30), Stator's: the theme Armature shows a person is kept as their mirror, a
// private theme of theirs that is applied, never chosen, edited or shared.

// Following says whether the reader follows their Armature theme.
func (s *Service) Following(ctx context.Context, reader uuid.UUID) (bool, error) {
	var follows bool
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT follow_armature FROM user_theme WHERE user_id = $1`, reader).Scan(&follows)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return follows, err
}

// Follow makes the theme Armature shows the reader their choice, in place of any theme they chose; the mirror
// arrives with SaveMirror.
func (s *Service) Follow(ctx context.Context, reader uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO user_theme (user_id, theme_id, follow_armature) VALUES ($1, NULL, true)
			ON CONFLICT (user_id) DO UPDATE SET theme_id = NULL, follow_armature = true`, reader)
		return err
	})
}

// Unfollow returns a follower to the installation's default and forgets their mirror; somebody who does not follow
// keeps their choice.
func (s *Service) Unfollow(ctx context.Context, reader uuid.UUID) (db.LSN, error) {
	var keys []string
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if keys, err = s.dropMirrors(ctx, tx, reader, nil); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM user_theme WHERE user_id = $1 AND follow_armature`, reader)
		return err
	})
	if err == nil {
		s.dropObjects(ctx, keys)
	}
	return lsn, err
}

// Mirror is the reader's copy of their Armature theme, or nil.
func (s *Service) Mirror(ctx context.Context, reader uuid.UUID) (*Theme, error) {
	var out *Theme
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		t, err := scan(tx.QueryRow(ctx, selectThemes+`
			WHERE t.owner_id = $1 AND t.armature_theme_id IS NOT NULL AND t.armature_updated_at <> $2
			ORDER BY t.created_at DESC LIMIT 1`, reader, MirrorPending))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		out = t
		return withAssets(ctx, tx, []*Theme{t})
	})
	return out, err
}

// SaveMirror keeps pkg as the reader's copy of the Armature theme of, checked as an import is, and forgets the copy
// it replaces.
func (s *Service) SaveMirror(ctx context.Context, reader uuid.UUID, of MirrorOf, pkg *Package) (*Theme, db.LSN, error) {
	// A follower who reloads hangs up mid-copy; the copy, or its undo, is finished anyway rather than left half made.
	ctx = context.WithoutCancel(ctx)
	made, lsn, err := s.importPackage(withMirroring(ctx), reader, pkg, &MirrorOf{ID: of.ID, UpdatedAt: MirrorPending})
	if err != nil {
		return nil, lsn, err
	}
	var keys []string
	lsn, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		done, err := tx.Exec(ctx, `UPDATE theme SET armature_updated_at = $2 WHERE id = $1`, made.ID, of.UpdatedAt)
		if err != nil {
			return err
		}
		if done.RowsAffected() == 0 {
			return ErrNotFound
		}
		keys, err = s.dropMirrors(ctx, tx, reader, &made.ID)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	s.dropObjects(ctx, keys)
	made.Mirror = &of
	return made, lsn, nil
}

// MirrorPending marks a mirror still being copied, which takes several writes: no reader takes it for the theme, and
// no other copy removes it.
var MirrorPending = time.Unix(0, 0).UTC()

// dropMirrors removes the owner's mirrors but keep and those still being copied, or every one without a keep, and
// answers their files' object keys.
func (s *Service) dropMirrors(ctx context.Context, tx db.DBTX, owner uuid.UUID, keep *uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `
		DELETE FROM theme_asset a USING theme t
		WHERE a.theme_id = t.id AND t.owner_id = $1 AND t.armature_theme_id IS NOT NULL AND t.id IS DISTINCT FROM $2
		  AND ($2::uuid IS NULL OR t.armature_updated_at <> $3)
		RETURNING a.theme_id, a.id`, owner, keep, MirrorPending)
	if err != nil {
		return nil, err
	}
	type file struct{ theme, asset uuid.UUID }
	files, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (file, error) {
		var f file
		return f, row.Scan(&f.theme, &f.asset)
	})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(files))
	for _, f := range files {
		key, err := s.objectKey(ctx, f.theme, f.asset)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	_, err = tx.Exec(ctx, `
		DELETE FROM theme WHERE owner_id = $1 AND armature_theme_id IS NOT NULL AND id IS DISTINCT FROM $2
		  AND ($2::uuid IS NULL OR armature_updated_at <> $3)`, owner, keep, MirrorPending)
	return keys, err
}

type mirroringKey struct{}

// withMirroring lets the import write a mirror, which no request may edit.
func withMirroring(ctx context.Context) context.Context {
	return context.WithValue(ctx, mirroringKey{}, true)
}

func mirroring(ctx context.Context) bool {
	v, _ := ctx.Value(mirroringKey{}).(bool)
	return v
}

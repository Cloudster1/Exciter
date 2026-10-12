package theme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/tenant"
)

func cleanName(name *string) (string, error) {
	if name == nil {
		return "", errors.New("a theme needs a name")
	}
	trimmed := strings.TrimSpace(*name)
	if trimmed == "" {
		return "", errors.New("a theme needs a name")
	}
	return trimmed, nil
}

// Create saves a theme for the owner. Files come later, so the spec cannot
// name any yet.
func (s *Service) Create(ctx context.Context, owner uuid.UUID, in Input) (*Theme, db.LSN, error) {
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	spec := Spec{}
	if in.Spec != nil {
		spec = *in.Spec
	}
	if err := Validate(&spec, uuid.Nil, nil); err != nil {
		return nil, 0, err
	}
	shared := in.Shared != nil && *in.Shared
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, 0, err
	}
	var out *Theme
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO theme (org_id, owner_id, name, shared, spec)
			VALUES (current_org_id(), $1, $2, $3, $4) RETURNING id`, owner, name, shared, encoded).Scan(&id)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if err != nil {
			return fmt.Errorf("save theme: %w", err)
		}
		out, err = readOne(ctx, tx, owner, id, ` WHERE t.id = $2`)
		return err
	})
	return out, lsn, err
}

// lockOwned reads a theme for writing and refuses anybody but its owner, or
// an administrator, who may tidy what anybody shared.
func lockOwned(ctx context.Context, tx db.DBTX, id, actor uuid.UUID, administers bool) (*Theme, error) {
	current, err := readOne(ctx, tx, actor, id, ` WHERE t.id = $2 FOR UPDATE OF t`)
	if errors.Is(err, ErrNotFound) {
		// Row level security locks only what the actor may change, so a shared
		// theme they may merely see is found by a plain read.
		if _, seen := readOne(ctx, tx, actor, id, ` WHERE t.id = $2 AND`+visible); seen == nil {
			return nil, ErrNotYours
		}
	}
	if err != nil {
		return nil, err
	}
	switch {
	case current.OwnerID == actor:
		return current, nil
	case !current.Shared:
		return nil, ErrNotFound
	case administers:
		return current, nil
	}
	return nil, ErrNotYours
}

// Update changes a theme; the owner's to do, or an administrator's when shared.
func (s *Service) Update(ctx context.Context, id, actor uuid.UUID, administers bool, in Input) (*Theme, db.LSN, error) {
	var out *Theme
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := lockOwned(ctx, tx, id, actor, administers)
		if err != nil {
			return err
		}
		name, shared, spec := current.Name, current.Shared, current.Spec
		if in.Name != nil {
			if name, err = cleanName(in.Name); err != nil {
				return err
			}
		}
		if in.Shared != nil {
			shared = *in.Shared
		}
		if in.Spec != nil {
			spec = *in.Spec
		}
		if err := Validate(&spec, id, assetSet(current)); err != nil {
			return err
		}
		encoded, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE theme SET name = $2, shared = $3, spec = $4 WHERE id = $1`, id, name, shared, encoded)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if err != nil {
			return err
		}
		out, err = readOne(ctx, tx, actor, id, ` WHERE t.id = $2`)
		if errors.Is(err, ErrNotFound) {
			// An administrator who takes somebody's theme private no longer sees it.
			current.Name, current.Shared, current.Spec, current.Default = name, shared, spec, false
			out, err = current, nil
		}
		return err
	})
	return out, lsn, err
}

// Delete removes a theme and its files. Everybody who chose it returns to the
// built-in theme, by the cascade.
func (s *Service) Delete(ctx context.Context, id, actor uuid.UUID, administers bool) (db.LSN, error) {
	var keys []string
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := lockOwned(ctx, tx, id, actor, administers)
		if err != nil {
			return err
		}
		for _, a := range current.Assets {
			keys = append(keys, objectKey(ctx, id, a.ID))
		}
		_, err = tx.Exec(ctx, `DELETE FROM theme WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.dropObjects(ctx, keys)
	return lsn, nil
}

// dropObjects removes files after their rows are gone. An object left behind
// is unreachable and cheap; a row without its object would be a broken link.
func (s *Service) dropObjects(ctx context.Context, keys []string) {
	if s.store == nil {
		return
	}
	for _, key := range keys {
		_ = s.store.Delete(ctx, key)
	}
}

// objectKey is where a file's bytes live, under its organization's prefix so
// removing an organization finds every file it has.
func objectKey(ctx context.Context, themeID, assetID uuid.UUID) string {
	org, _ := tenant.FromContext(ctx)
	return ObjectKey(org.ID, themeID, assetID)
}

// ObjectKey is the key of one theme file in the object store.
func ObjectKey(orgID, themeID, assetID uuid.UUID) string {
	return objectstore.OrgPrefix(orgID) + "theme/" + themeID.String() + "/" + assetID.String()
}

// UploadAsset puts a file on a theme. The type is what the bytes say.
func (s *Service) UploadAsset(ctx context.Context, id, actor uuid.UUID, administers bool, name string, body io.Reader) (*Asset, db.LSN, error) {
	if objectstore.IsUnavailable(s.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxAssetBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read upload: %w", err)
	}
	contentType, err := sniff(name, data)
	if err != nil {
		return nil, 0, err
	}
	name = objectstore.CleanName(name)
	var out Asset
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := lockOwned(ctx, tx, id, actor, administers)
		if err != nil {
			return err
		}
		if len(current.Assets) >= MaxAssetsPerSpec {
			return ErrTooManyAssets
		}
		assetID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		out = Asset{ID: assetID, Name: name, ContentType: contentType, Size: int64(len(data))}
		if err := tx.QueryRow(ctx, `
			INSERT INTO theme_asset (id, org_id, theme_id, name, content_type, size)
			VALUES ($1, current_org_id(), $2, $3, $4, $5) RETURNING created_at`,
			assetID, id, name, contentType, len(data)).Scan(&out.CreatedAt); err != nil {
			return fmt.Errorf("record the file: %w", err)
		}
		return s.store.Put(ctx, objectKey(ctx, id, assetID), bytes.NewReader(data), int64(len(data)), contentType)
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, lsn, nil
}

// OpenAsset reads a file of a theme the reader may see. The caller closes it.
func (s *Service) OpenAsset(ctx context.Context, id, assetID, reader uuid.UUID) (io.ReadCloser, *Asset, error) {
	if s.store == nil {
		return nil, nil, ErrAssetNotFound
	}
	var found *Asset
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		t, err := readOne(ctx, tx, reader, id, ` WHERE t.id = $2 AND`+visible)
		if err != nil {
			return err
		}
		for _, a := range t.Assets {
			if a.ID == assetID {
				found = &a
				return nil
			}
		}
		return ErrAssetNotFound
	})
	if err != nil {
		return nil, nil, err
	}
	body, err := s.store.Get(ctx, objectKey(ctx, id, assetID))
	if err != nil {
		return nil, nil, ErrAssetNotFound
	}
	return body, found, nil
}

// DeleteAsset takes a file off a theme, unless the theme still names it.
func (s *Service) DeleteAsset(ctx context.Context, id, assetID, actor uuid.UUID, administers bool) (db.LSN, error) {
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := lockOwned(ctx, tx, id, actor, administers)
		if err != nil {
			return err
		}
		if !assetSet(current)[assetID] {
			return ErrAssetNotFound
		}
		for _, used := range current.Spec.AssetIDs() {
			if used == assetID {
				return ErrAssetInUse
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM theme_asset WHERE id = $1 AND theme_id = $2`, assetID, id)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.dropObjects(ctx, []string{objectKey(ctx, id, assetID)})
	return lsn, nil
}

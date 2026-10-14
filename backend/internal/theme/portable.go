package theme

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// PackageFormat names the shape of an exported theme, so a file from a later
// version can be told apart from one this version reads.
const PackageFormat = "armature-theme/1"

// MaxPackageBytes bounds an import. A theme rarely carries more than a
// handful of files; one that would not fit is split by taking files out.
const MaxPackageBytes = 64 * 1024 * 1024

// Package is a theme as one file: its name, its spec and every file it draws
// from, so it can be sent to another installation or kept outside.
type Package struct {
	Format string          `json:"format"`
	Name   string          `json:"name"`
	Spec   Spec            `json:"spec"`
	Assets []PackagedAsset `json:"assets"`
}

// PackagedAsset is one file of a theme, its bytes carried inline.
type PackagedAsset struct {
	// ID is the id the spec and the CSS refer to it by; the import gives it a new one.
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Data        []byte    `json:"data"`
}

// ErrNotAThemeFile is returned for a file that is not an exported theme.
var ErrNotAThemeFile = errors.New("that is not a theme file: export one from the Themes page to see the shape")

// Export packs a theme the reader may see, files included.
func (s *Service) Export(ctx context.Context, id, reader uuid.UUID) (*Package, error) {
	t, err := s.Get(ctx, id, reader)
	if err != nil {
		return nil, err
	}
	out := &Package{Format: PackageFormat, Name: t.Name, Spec: t.Spec, Assets: []PackagedAsset{}}
	for _, a := range t.Assets {
		key, err := s.objectKey(ctx, id, a.ID)
		if err != nil {
			return nil, err
		}
		body, err := s.store.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", a.Name, ErrAssetNotFound)
		}
		data, err := io.ReadAll(body)
		_ = body.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", a.Name, err)
		}
		out.Assets = append(out.Assets, PackagedAsset{ID: a.ID, Name: a.Name, ContentType: a.ContentType, Data: data})
	}
	return out, nil
}

// maxNameTries is how far an import numbers a taken name, "Name (2)" on,
// before it gives up and says the name is taken.
const maxNameTries = 50

var ownAssetURL = regexp.MustCompile(`/api/v1/themes/([0-9a-f-]{36})/assets/([0-9a-f-]{36})`)

// Import makes the package a theme of the owner's own, its files under new ids
// that the spec and the CSS are rewritten to name; a taken name gets a number.
func (s *Service) Import(ctx context.Context, owner uuid.UUID, pkg *Package) (*Theme, db.LSN, error) {
	if pkg == nil || pkg.Format != PackageFormat {
		return nil, 0, ErrNotAThemeFile
	}
	name := strings.TrimSpace(pkg.Name)
	if name == "" {
		return nil, 0, ErrNotAThemeFile
	}
	if len(pkg.Assets) > MaxAssetsPerSpec {
		return nil, 0, ErrTooManyAssets
	}

	// The theme exists first, with nothing in it, so its files have
	// somewhere to go; the spec that names them comes last.
	var (
		made *Theme
		lsn  db.LSN
		err  error
	)
	for n := 1; n <= maxNameTries; n++ {
		attempt := name
		if n > 1 {
			attempt = fmt.Sprintf("%s (%d)", name, n)
		}
		made, lsn, err = s.Create(ctx, owner, Input{Name: &attempt})
		if !errors.Is(err, ErrDuplicateName) {
			break
		}
	}
	if err != nil {
		return nil, 0, err
	}
	// A refused import has still written, twice, so it hands back where the
	// undo landed: the caller's next read must not find the theme it removed.
	undo := func(cause error) (*Theme, db.LSN, error) {
		if gone, err := s.Delete(ctx, made.ID, owner, false); err == nil {
			lsn = gone
		}
		return nil, lsn, cause
	}

	renamed := map[uuid.UUID]uuid.UUID{}
	for _, a := range pkg.Assets {
		asset, _, err := s.UploadAsset(ctx, made.ID, owner, false, a.Name, bytes.NewReader(a.Data))
		if err != nil {
			return undo(fmt.Errorf("%s: %w", a.Name, err))
		}
		renamed[a.ID] = asset.ID
	}

	spec := pkg.Spec
	spec.normalise()
	swap := func(id *uuid.UUID) error {
		if id == nil {
			return nil
		}
		next, ok := renamed[*id]
		if !ok {
			return fmt.Errorf("%w: the theme names a file it does not carry", ErrNotAThemeFile)
		}
		*id = next
		return nil
	}
	for _, font := range []*Font{spec.Fonts.Sans, spec.Fonts.Mono} {
		if font != nil {
			if err := swap(font.AssetID); err != nil {
				return undo(err)
			}
		}
	}
	for kind, cursor := range spec.Cursors {
		if err := swap(&cursor.AssetID); err != nil {
			return undo(err)
		}
		spec.Cursors[kind] = cursor
	}
	for iconName, icon := range spec.Icons {
		if err := swap(icon.AssetID); err != nil {
			return undo(err)
		}
		spec.Icons[iconName] = icon
	}
	if spec.Backdrop != nil {
		if err := swap(&spec.Backdrop.AssetID); err != nil {
			return undo(err)
		}
	}
	spec.CSS = ownAssetURL.ReplaceAllStringFunc(spec.CSS, func(match string) string {
		parts := ownAssetURL.FindStringSubmatch(match)
		old, err := uuid.Parse(parts[2])
		if err != nil {
			return match
		}
		next, ok := renamed[old]
		if !ok {
			return match
		}
		return "/api/v1/themes/" + made.ID.String() + "/assets/" + next.String()
	})

	saved, updated, err := s.Update(ctx, made.ID, owner, false, Input{Spec: &spec})
	if err != nil {
		return undo(err)
	}
	return saved, updated, nil
}

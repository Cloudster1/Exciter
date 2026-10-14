package armature

import (
	"context"
	"errors"

	"github.com/cloudster1/exciter/backend/internal/theme"
)

// The Armature theme a person follows (#30), Stator's: which theme Armature shows them, and its file.

// ShownTheme is the theme Armature shows a person, as the cache keeps it.
type ShownTheme struct {
	// Theme is nil for Armature's built-in theme, which Exciter's matches.
	Theme *theme.MirrorOf `json:"theme"`
	// Invalid is why Exciter could not use it, kept so a theme that failed the checks is not downloaded again on
	// every page.
	Invalid string `json:"invalid,omitempty"`
}

// ShownTheme asks which theme Armature shows the viewer: from the cache unless fresh, else from Armature, bounded
// by ctx.
func (s *Service) ShownTheme(ctx context.Context, v *Viewer, fresh bool) (*ShownTheme, error) {
	var out ShownTheme
	if !fresh && s.cache.Theme(ctx, v.TokenID, &out) {
		return &out, nil
	}
	var answer struct {
		Theme *theme.MirrorOf `json:"theme"`
	}
	if err := v.Caller.Get(ctx, "/themes/active", nil, &answer); err != nil {
		s.noteThemeFailure(ctx, v, err)
		return nil, err
	}
	out = ShownTheme{Theme: answer.Theme}
	if cached := (ShownTheme{}); s.cache.Theme(ctx, v.TokenID, &cached) && cached.Invalid != "" && answer.Theme.Same(cached.Theme) {
		out.Invalid = cached.Invalid
	}
	s.cache.PutTheme(ctx, v.TokenID, out)
	return &out, nil
}

// RememberShownTheme keeps what was learned of the viewer's theme, such as that it does not pass the checks.
func (s *Service) RememberShownTheme(ctx context.Context, v *Viewer, shown *ShownTheme) {
	s.cache.PutTheme(ctx, v.TokenID, shown)
}

// ExportTheme downloads an Armature theme as its armature-theme/1 file.
func (s *Service) ExportTheme(ctx context.Context, v *Viewer, of theme.MirrorOf) (*theme.Package, error) {
	var pkg theme.Package
	if err := v.Caller.GetWithin(ctx, "/themes/"+of.ID.String()+"/export", theme.MaxPackageBytes, &pkg); err != nil {
		s.noteThemeFailure(ctx, v, err)
		return nil, err
	}
	return &pkg, nil
}

func (s *Service) noteThemeFailure(ctx context.Context, v *Viewer, err error) {
	if errors.Is(err, ErrRejected) {
		if noteErr := s.NoteRejected(context.WithoutCancel(ctx), v); noteErr != nil {
			s.opts.Log.Warn("a rejected Armature token could not be marked", "token_id", v.TokenID, "error", noteErr)
		}
		return
	}
	s.opts.Log.Info("Armature's theme could not be fetched; the person sees their fallback", "error", err)
}

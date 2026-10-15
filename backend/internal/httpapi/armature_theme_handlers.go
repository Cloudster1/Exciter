package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/armature"
	"github.com/cloudster1/exciter/backend/internal/theme"
)

// Following the Armature theme (#30), Stator's: the theme Armature shows a person is kept as their mirror and
// applied in place of an Exciter one.

// themeInvalid says why a theme Armature shows cannot be followed, and what to do.
func themeInvalid(why string) string {
	return "Armature's theme cannot be used in Exciter: " + why + " Choose another theme in Armature, then follow it again."
}

// followedTheme is the theme a follower sees, nil for Armature's built-in one, and how asking went; a status other
// than ok or an error falls back.
func (s *Server) followedTheme(ctx context.Context, reader uuid.UUID, fresh bool) (*theme.Theme, armature.Follow, error) {
	follow := armature.Follow{Following: true, Status: armature.StatusOK}
	v, status, err := s.Armature.Viewer(ctx)
	if err != nil {
		return nil, follow, err
	}
	if status != armature.StatusOK {
		follow.Status = status
		return nil, follow, nil
	}
	if lacks := v.Offered.Lacks(armature.CapabilityThemes); lacks != nil {
		follow.Status, follow.Error = armature.StatusOutdated, lacks.Message
		return nil, follow, nil
	}
	// One budget for every call to Armature, so a slow one never holds the page longer than ThemeTimeout; Exciter's
	// own writes are not bounded by it.
	call, cancel := context.WithTimeout(ctx, armature.ThemeTimeout)
	defer cancel()
	shown, err := s.Armature.ShownTheme(call, v, fresh)
	if err != nil {
		follow.Status = armature.StatusOf(err)
		return nil, follow, nil
	}
	if shown.Theme == nil {
		return nil, follow, nil
	}
	if shown.Invalid != "" {
		follow.Error = shown.Invalid
		return nil, follow, nil
	}
	mirror, err := s.Themes.Mirror(ctx, reader)
	if err != nil {
		return nil, follow, err
	}
	if mirror != nil && mirror.Mirror.Same(shown.Theme) {
		return mirror, follow, nil
	}
	pkg, err := s.Armature.ExportTheme(call, v, *shown.Theme)
	if err != nil {
		follow.Status = armature.StatusOf(err)
		return nil, follow, nil
	}
	made, lsn, err := s.Themes.SaveMirror(ctx, reader, *shown.Theme, pkg)
	noteWrite(ctx, lsn)
	if err != nil {
		var refused *APIError
		if errors.As(asValidationError(err), &refused) && refused.Status < http.StatusInternalServerError {
			shown.Invalid = themeInvalid(refused.Message)
			s.Armature.RememberShownTheme(ctx, v, shown)
			follow.Error = shown.Invalid
			return nil, follow, nil
		}
		return nil, follow, err
	}
	return made, follow, nil
}

func (s *Server) handleGetArmatureTheme(w http.ResponseWriter, r *http.Request) {
	following, err := s.Themes.Following(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	var follow armature.Follow
	if following {
		// The settings screen asks Armature now, so a theme changed there shows up without waiting for the cache.
		if _, follow, err = s.followedTheme(r.Context(), userFrom(r), true); err != nil {
			respondError(w, r, err)
			return
		}
	} else {
		_, status, err := s.Armature.Viewer(r.Context())
		if err != nil {
			respondError(w, r, err)
			return
		}
		follow = armature.Follow{Status: status}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"follow": follow})
}

func (s *Server) handleFollowArmatureTheme(w http.ResponseWriter, r *http.Request) {
	v, status, err := s.Armature.Viewer(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	switch status {
	case armature.StatusNotConfigured:
		respondError(w, r, armature.ErrNotConfigured)
		return
	case armature.StatusNotConnected:
		respondError(w, r, armature.ErrNotConnected)
		return
	case armature.StatusRejected:
		respondError(w, r, armature.ErrRejected)
		return
	}
	if lacks := v.Offered.Lacks(armature.CapabilityThemes); lacks != nil {
		respondError(w, r, lacks)
		return
	}
	_, follow, err := s.followedTheme(r.Context(), userFrom(r), true)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if follow.Error != "" {
		respondError(w, r, &APIError{Status: http.StatusUnprocessableEntity, Code: "armature_theme_invalid", Message: follow.Error})
		return
	}
	lsn, err := s.Themes.Follow(r.Context(), userFrom(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"follow": follow})
}

func (s *Server) handleUnfollowArmatureTheme(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Themes.Unfollow(r.Context(), userFrom(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// armatureActiveTheme answers GET /themes/active for a follower whose Armature answered in time; false falls back to
// what they would see anyway.
func (s *Server) armatureActiveTheme(w http.ResponseWriter, r *http.Request) bool {
	if s.Armature == nil {
		return false
	}
	reader := userFrom(r)
	following, err := s.Themes.Following(r.Context(), reader)
	if err != nil || !following {
		return false
	}
	seen, follow, err := s.followedTheme(r.Context(), reader, false)
	if err != nil {
		loggerFrom(r.Context()).Warn("the followed Armature theme could not be applied", "error", err)
		return false
	}
	if follow.Status != armature.StatusOK || follow.Error != "" {
		return false
	}
	if seen != nil {
		seen.Active = true
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"theme": seen, "source": string(theme.SourceArmature)})
	return true
}

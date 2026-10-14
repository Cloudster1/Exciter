package httpapi

import (
	"errors"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/notify"
	"github.com/cloudster1/exciter/backend/internal/watch"
)

// Watching and notifications (#34), Stator's over requirements. See docs/api-contract-m3.md.

var errNotificationsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "notifications_unavailable",
	Message: "This server is not set up for watching and notifications. Ask the operator to check its configuration."}

func (s *Server) notificationsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Watches == nil || s.Notifications == nil {
			respondError(w, r, errNotificationsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// notifyError maps the refusals of watching and notifications; nil when err is none of theirs.
func notifyError(err error) *APIError {
	var field *notify.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, watch.ErrSignedOut), errors.Is(err, notify.ErrSignedOut):
		return ErrUnauthorized("Sign in to follow requirements and read what you were told.")
	}
	return nil
}

func (s *Server) handleWatchRequirement(w http.ResponseWriter, r *http.Request) {
	watching, lsn, err := s.Watches.WatchRequirement(r.Context(), reqKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"watching": watching})
}

func (s *Server) handleUnwatchRequirement(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Watches.UnwatchRequirement(r.Context(), reqKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListWatchers(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, watch.DefaultLimit, watch.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	watchers, total, err := s.Watches.Watchers(r.Context(), reqKeyParam(r), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"watchers": watchers, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleWatchProject(w http.ResponseWriter, r *http.Request) {
	made, lsn, err := s.Watches.WatchProject(r.Context(), projectKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"watch": made})
}

func (s *Server) handleUnwatchProject(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Watches.UnwatchProject(r.Context(), projectKeyParam(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListWatches(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, watch.DefaultLimit, watch.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	watches, total, err := s.Watches.List(r.Context(), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"watches": watches, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, notify.DefaultLimit, notify.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var unread bool
	switch r.URL.Query().Get("unread") {
	case "", "false":
	case "true":
		unread = true
	default:
		respondError(w, r, ErrValidation(map[string]string{"unread": "Send true for the unread notifications alone, or leave it out for all."}))
		return
	}
	found, total, err := s.Notifications.List(r.Context(), unread, limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"notifications": found, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleCountUnreadNotifications(w http.ResponseWriter, r *http.Request) {
	n, err := s.Notifications.Unread(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"unread": n})
}

func (s *Server) handleMarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var req notify.ReadInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	lsn, err := s.Notifications.MarkRead(r.Context(), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.Notifications.Preferences(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"preferences": prefs})
}

func (s *Server) handleSaveNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	var req notify.Preferences
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	prefs, lsn, err := s.Notifications.SavePreferences(r.Context(), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"preferences": prefs})
}

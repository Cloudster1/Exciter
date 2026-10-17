package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/comment"
)

// Comments and mentions (#33): the discussion on a requirement, Stator's. See docs/api-contract-m3.md.

var errCommentsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "comments_unavailable",
	Message: "This server is not set up for comments. Ask the operator to check its configuration."}

func (s *Server) commentsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Comments == nil {
			respondError(w, r, errCommentsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// commentError maps the comments' refusals; nil when err is none of theirs.
func commentError(err error) *APIError {
	var field *comment.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, comment.ErrNotFound):
		return ErrNotFound("That comment was not found. Reload the requirement; it may have been deleted.")
	case errors.Is(err, comment.ErrNotYours):
		return ErrForbidden("Only its author changes a comment, and only its author or the project's administrators delete it.")
	case errors.Is(err, comment.ErrMayNotComment):
		return ErrForbidden("You may read this requirement but not comment on it. Ask one of the project's administrators to make you a reviewer or an editor.")
	}
	return nil
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	asked, apiErr := markdownAsked(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	threads, err := s.Comments.List(r.Context(), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	if asked {
		for i := range threads {
			threads[i].WithMarkdown()
		}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"threads": threads})
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	var req comment.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	thread, lsn, err := s.Comments.Start(r.Context(), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"thread": thread})
}

func (s *Server) handleListMentionable(w http.ResponseWriter, r *http.Request) {
	limit := comment.MentionableDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{"limit": "Ask for 1 to " + strconv.Itoa(comment.MentionableMaxLimit) + " people."}))
			return
		}
		limit = n
	}
	people, err := s.Comments.Mentionable(r.Context(), reqKeyParam(r), r.URL.Query().Get("q"), limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"people": people})
}

func (s *Server) handleGetCommentThread(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "commentID", "comment")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	asked, apiErr := markdownAsked(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	thread, err := s.Comments.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if asked {
		thread.WithMarkdown()
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"thread": thread})
}

func (s *Server) handleReplyToComment(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "commentID", "comment")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req comment.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, thread, lsn, err := s.Comments.Reply(r.Context(), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"comment": made, "thread": thread})
}

func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "commentID", "comment")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req comment.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	edited, lsn, err := s.Comments.Edit(r.Context(), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"comment": edited})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "commentID", "comment")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Comments.Delete(r.Context(), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) resolveThread(w http.ResponseWriter, r *http.Request, resolved bool) {
	id, apiErr := pathUUID(r, "threadID", "thread")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	thread, lsn, err := s.Comments.Resolve(r.Context(), id, resolved)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"thread": thread})
}

func (s *Server) handleResolveThread(w http.ResponseWriter, r *http.Request) {
	s.resolveThread(w, r, true)
}

func (s *Server) handleReopenThread(w http.ResponseWriter, r *http.Request) {
	s.resolveThread(w, r, false)
}

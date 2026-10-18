package httpapi

import (
	"cmp"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/review"
)

// Review sessions (#204). See docs/api-contract-m8.md.

// sessionError maps the review sessions' refusals; nil when err is none of theirs.
func sessionError(err error) *APIError {
	switch {
	case errors.Is(err, review.ErrSessionNotFound):
		return ErrNotFound("That review session was not found. Reload the project's sessions; it may be of a project you cannot read.")
	case errors.Is(err, review.ErrNotEditor):
		return ErrForbidden("Only the project's editors open review sessions. Ask one of its administrators to make you an editor.")
	case errors.Is(err, review.ErrNotModerator):
		return ErrForbidden("Only the session's moderator, or the project's administrators, change it.")
	case errors.Is(err, review.ErrSessionClosed):
		return &APIError{Status: http.StatusConflict, Code: "session_closed", Message: "This review session is closed and changes no more. Open a new one."}
	case errors.Is(err, review.ErrNothingRevised):
		return &APIError{Status: http.StatusConflict, Code: "nothing_revised",
			Message: "Nothing in the session was revised since its last round. Revise a requirement, then start the next round."}
	case errors.Is(err, review.ErrTooManyRounds):
		return &APIError{Status: http.StatusConflict, Code: "too_many_rounds", Message: "The session has had every round it takes. Close it, and open a new one."}
	}
	return nil
}

func sessionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, apiErr := pathUUID(r, "sessionID", "review session")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return id, false
	}
	return id, true
}

func (s *Server) handleListReviewSessions(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, review.DefaultLimit, review.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	all, total, err := s.Reviews.Sessions(r.Context(), projectKeyParam(r), review.SessionState(r.URL.Query().Get("state")), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"sessions": all, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleListMyReviewSessions(w http.ResponseWriter, r *http.Request) {
	limit, offset, apiErr := offsetWindow(r, review.DefaultLimit, review.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	all, total, err := s.Reviews.MySessions(r.Context(), limit, offset)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"sessions": all, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleOpenReviewSession(w http.ResponseWriter, r *http.Request) {
	var req review.SessionInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Reviews.OpenSession(r.Context(), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"session": got})
}

func (s *Server) handleGetReviewSession(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	got, err := s.Reviews.Session(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"session": got})
}

func (s *Server) handleUpdateReviewSession(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	var req review.SessionUpdate
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Reviews.UpdateSession(r.Context(), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"session": got})
}

func (s *Server) handleStartReviewRound(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	var req review.RoundInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Reviews.StartRound(r.Context(), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"session": got})
}

func (s *Server) handleCloseReviewSession(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	var req review.CloseInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Reviews.CloseSession(r.Context(), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"session": got})
}

func (s *Server) handleGetReviewSessionReport(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	rep, err := s.Reviews.Report(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"report": rep})
}

// noteReport records a session's report about to leave, as an exported document is.
func (s *Server) noteReport(w http.ResponseWriter, r *http.Request, rep *review.Report, format string) bool {
	id := rep.Session.ID
	return s.noteExport(w, r, audit.Entry{Action: audit.ActionSpecificationExported, TargetType: "review_session", TargetID: &id,
		Data: map[string]any{"project": rep.Session.ProjectKey, "kind": "session", "format": format, "title": rep.Session.Title}})
}

func (s *Server) handleReviewSessionReportDocx(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	rep, err := s.Reviews.Report(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	at := time.Now().UTC()
	f, err := s.framingOf(r, rep.Session.ProjectKey)
	if err != nil {
		respondError(w, r, err)
		return
	}
	var frame *docx.Template
	fill := docx.Fill{"project": rep.Session.ProjectKey, "title": rep.Session.Title, "date": at.Format(time.DateOnly), "exportedBy": s.nameOf(r)}
	if f != nil {
		frame = f.frame
		// A template names the project as an export of its specification does.
		if p, err := s.Projects.Get(r.Context(), userFrom(r), rep.Session.ProjectKey); err == nil {
			fill["project"] = p.Name + " (" + p.Key + ")"
		}
	}
	body, err := docx.ReportBytesWith(r.Context(), rep, s.nameOf(r), at, language(r), frame, fill)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !s.noteReport(w, r, rep, "docx") {
		return
	}
	sendFile(w, docx.ContentType, docx.ReportFileName(rep, at, "docx"), body)
}

func (s *Server) handleReviewSessionReportPDF(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	rep, err := s.Reviews.Report(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	pdf, ok := s.printed(w, r, "/print/sessions/"+id.String())
	if !ok || !s.noteReport(w, r, rep, "pdf") {
		return
	}
	sendFile(w, "application/pdf", docx.ReportFileName(rep, time.Now().UTC(), "pdf"), pdf)
}

// nameOf is who a file is exported by, as its cover says it.
func (s *Server) nameOf(r *http.Request) string {
	if p := PrincipalFrom(r.Context()); p != nil {
		return cmp.Or(p.Name, p.Email)
	}
	return ""
}

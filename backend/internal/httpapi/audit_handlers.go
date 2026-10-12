package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
)

// auditDay is how a day is written in the log's date filters.
const auditDay = "2006-01-02"

// auditExportName is what the browser saves an export of the log as.
const auditExportName = "audit-log.csv"

// errAuditOff answers for a server built without the audit log, which only a
// test does.
var errAuditOff = &APIError{Status: http.StatusServiceUnavailable, Code: "audit_unavailable",
	Message: "The audit log is not set up on this server. Ask its operator to configure it."}

var errAuditExportTooLarge = &APIError{Status: http.StatusUnprocessableEntity, Code: "export_too_large",
	Message: "That filter matches more than " + strconv.Itoa(audit.MaxExport) + " entries. Narrow the dates or choose an action, then export again."}

// auditFilter reads the log's filters. A day is the whole of that day in UTC;
// a time with its zone is that instant, so a client can send its own midnight.
func auditFilter(r *http.Request) (audit.Filter, *APIError) {
	q := r.URL.Query()
	var f audit.Filter
	problems := map[string]string{}
	if action := q.Get("action"); action != "" {
		if !slices.Contains(audit.Actions, action) {
			problems["action"] = "Choose one of the actions the log records."
		}
		f.Action = action
	}
	f.TargetType = q.Get("targetType")
	ids := map[string]**uuid.UUID{"actor": &f.ActorID, "target": &f.TargetID}
	for name, into := range ids {
		raw := q.Get(name)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			problems[name] = "Send an id, as the log's entries carry them."
			continue
		}
		*into = &id
	}
	moment := func(name string, endOfDay bool) *time.Time {
		raw := q.Get(name)
		if raw == "" {
			return nil
		}
		if day, err := time.Parse(auditDay, raw); err == nil {
			if endOfDay {
				day = day.AddDate(0, 0, 1)
			}
			return &day
		}
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			problems[name] = "Write a day as YYYY-MM-DD, or a time with its zone such as 2026-10-01T00:00:00+02:00."
			return nil
		}
		return &at
	}
	f.From = moment("from", false)
	f.To = moment("to", true)
	if f.From != nil && f.To != nil && !f.From.Before(*f.To) {
		problems["to"] = "The end of the range has to come after its start."
	}
	if len(problems) > 0 {
		return f, ErrValidation(problems)
	}
	return f, nil
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	if s.Audit == nil {
		respondError(w, r, errAuditOff)
		return
	}
	f, apiErr := auditFilter(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	limit, after, apiErr := keysetWindow(r, audit.DefaultLimit, audit.MaxLimit)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	entries, next, err := s.Audit.List(r.Context(), f, after, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"entries": entries, "next": next})
}

func (s *Server) handleAuditFacets(w http.ResponseWriter, r *http.Request) {
	if s.Audit == nil {
		respondError(w, r, errAuditOff)
		return
	}
	facets, err := s.Audit.Facets(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	facets.RetentionDays = int(s.AuditRetention.Hours() / 24)
	respondJSON(w, r, http.StatusOK, map[string]any{"facets": facets})
}

func (s *Server) handleExportAudit(w http.ResponseWriter, r *http.Request) {
	if s.Audit == nil {
		respondError(w, r, errAuditOff)
		return
	}
	f, apiErr := auditFilter(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	body, n, err := s.Audit.CSV(r.Context(), f)
	if errors.Is(err, audit.ErrExportTooLarge) {
		respondError(w, r, errAuditExportTooLarge)
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	data := f.Describe()
	data["entries"] = n
	if !s.noteExport(w, r, audit.Entry{Action: audit.ActionAuditExported, TargetType: "audit_log", Data: data}) {
		return
	}
	download(w, "text/csv; charset=utf-8", auditExportName)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// noteExport records a file about to leave and says whether it may: an export
// the log cannot record is refused, so none ever leaves unrecorded.
func (s *Server) noteExport(w http.ResponseWriter, r *http.Request, e audit.Entry) bool {
	if s.Audit == nil {
		respondError(w, r, errAuditOff)
		return false
	}
	lsn, err := s.Audit.Note(r.Context(), e)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return false
	}
	return true
}

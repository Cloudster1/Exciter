package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/dashboard"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Dashboards (#212). See docs/api-contract-m11.md.

var errDashboardsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "dashboards_unavailable",
	Message: "Dashboards are not set up on this server. Ask an administrator to start Exciter with its database."}

func (s *Server) dashboardsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Dashboards == nil {
			respondError(w, r, errDashboardsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// dashboardError maps the dashboards' refusals; nil when err is none of theirs.
func dashboardError(err error) *APIError {
	var field *dashboard.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, dashboard.ErrNotFound):
		return ErrNotFound("That dashboard or widget was not found. It may have been deleted, or not shared with you.")
	case errors.Is(err, dashboard.ErrForbidden):
		return ErrForbidden("Only a dashboard's owner changes it, or once it is shared the project's administrators; sharing it needs edit on the project.")
	case errors.Is(err, dashboard.ErrTooManyDashboards):
		return &APIError{Status: http.StatusConflict, Code: "too_many_dashboards",
			Message: "You have as many dashboards in this project as one person may keep. Delete one first."}
	case errors.Is(err, dashboard.ErrTooManyWidgets):
		return &APIError{Status: http.StatusConflict, Code: "too_many_widgets",
			Message: "This dashboard holds as many widgets as one may. Take one off first."}
	}
	return nil
}

// idParam reads a path value as an id, answering 404 for one that is not, as for one that does not exist.
func idParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		respondError(w, r, dashboard.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleListDashboards(w http.ResponseWriter, r *http.Request) {
	list, err := s.Dashboards.Dashboards(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"dashboards": list})
}

func (s *Server) handleCreateDashboard(w http.ResponseWriter, r *http.Request) {
	var req dashboard.Input
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Dashboards.Create(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"dashboard": made})
}

func (s *Server) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "dashboardID")
	if !ok {
		return
	}
	d, err := s.Dashboards.Dashboard(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"dashboard": d})
}

func (s *Server) handleUpdateDashboard(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "dashboardID")
	if !ok {
		return
	}
	var req dashboard.Patch
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	d, lsn, err := s.Dashboards.Update(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"dashboard": d})
}

func (s *Server) handleDeleteDashboard(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "dashboardID")
	if !ok {
		return
	}
	lsn, err := s.Dashboards.Delete(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleAddWidget(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "dashboardID")
	if !ok {
		return
	}
	var req dashboard.WidgetInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Dashboards.AddWidget(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"widget": made})
}

func (s *Server) handleOrderWidgets(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "dashboardID")
	if !ok {
		return
	}
	var req dashboard.OrderInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	d, lsn, err := s.Dashboards.Order(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"dashboard": d})
}

func (s *Server) handleUpdateWidget(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "widgetID")
	if !ok {
		return
	}
	var req dashboard.WidgetPatch
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	widget, lsn, err := s.Dashboards.UpdateWidget(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"widget": widget})
}

func (s *Server) handleDeleteWidget(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "widgetID")
	if !ok {
		return
	}
	lsn, err := s.Dashboards.DeleteWidget(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleWidgetData(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "widgetID")
	if !ok {
		return
	}
	data, err := s.Dashboards.Data(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) handlePreviewWidget(w http.ResponseWriter, r *http.Request) {
	var req dashboard.PreviewInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	data, err := s.Dashboards.Preview(r.Context(), userFrom(r), projectKeyParam(r), req)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) handleListDashboardTemplates(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"templates": dashboard.Templates(), "kinds": dashboard.KindInfos()})
}

// handleDashboardPDF prints a dashboard as the reader, as the specification is printed.
func (s *Server) handleDashboardPDF(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "dashboardID")
	if !ok {
		return
	}
	d, err := s.Dashboards.Dashboard(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	pdf, ok := s.printed(w, r, "/print/dashboards/"+d.ID.String())
	if !ok {
		return
	}
	sendFile(w, "application/pdf", requirement.NormalizedProjectKey(d.ProjectKey)+"-"+fileSafe(d.Name)+".pdf", pdf)
}

var unsafeInName = regexp.MustCompile(`[^a-z0-9]+`)

// fileSafe is a name as a downloaded file's name takes it: lower case words joined by hyphens.
func fileSafe(name string) string {
	slug := strings.Trim(unsafeInName.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		return "dashboard"
	}
	return slug
}

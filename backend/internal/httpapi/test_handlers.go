package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/testexec"
)

// Test plans and executions (#251). See docs/api-contract-m12.md.

var errTestsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "tests_unavailable",
	Message: "Test management is not set up on this server. Ask its operator to look at its configuration."}

func (s *Server) testsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Tests == nil {
			respondError(w, r, errTestsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// testError maps the refusals of plans and executions; nil when err is none of theirs.
func testError(err error) *APIError {
	var field *testexec.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, testexec.ErrNotFound):
		return ErrNotFound("That test plan, execution, case or file was not found. It may have been deleted.")
	case errors.Is(err, testexec.ErrForbidden):
		return ErrForbidden("This is not yours to do here: plans are their project's editors', an execution its manager's, and results its testers' who may comment.")
	case errors.Is(err, testexec.ErrClosed):
		return &APIError{Status: http.StatusConflict, Code: "execution_closed", Message: "The test execution is closed and never changes. Start another to run the cases again."}
	case errors.Is(err, testexec.ErrNotFailed):
		return ErrValidation(map[string]string{"iteration": "Record the failure first; an issue is filed for a case iteration that failed."})
	case errors.Is(err, testexec.ErrNoIssues):
		return &APIError{Status: http.StatusConflict, Code: "armature_not_configured", Message: "No Armature is connected, so no issue can be filed. Ask an administrator to connect one."}
	case errors.Is(err, testexec.ErrTooLarge):
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: fmt.Sprintf("A file is at most %d MB. Make it smaller, or leave it out.", testexec.MaxFileMB)}
	case errors.Is(err, testexec.ErrEmpty):
		return ErrValidation(map[string]string{"file": "The file is empty. Pick the file again."})
	}
	return nil
}

func testIDParam(w http.ResponseWriter, r *http.Request, name, what string) (uuid.UUID, bool) {
	id, apiErr := pathUUID(r, name, what)
	if apiErr != nil {
		respondError(w, r, testexec.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleListTestPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.Tests.Plans(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"plans": plans})
}

func (s *Server) handleCreateTestPlan(w http.ResponseWriter, r *http.Request) {
	var in testexec.PlanInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	plan, lsn, err := s.Tests.CreatePlan(r.Context(), userFrom(r), projectKeyParam(r), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"plan": plan})
}

func (s *Server) handleGetTestPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "planID", "test plan")
	if !ok {
		return
	}
	plan, err := s.Tests.Plan(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"plan": plan})
}

func (s *Server) handleUpdateTestPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "planID", "test plan")
	if !ok {
		return
	}
	var in testexec.PlanPatch
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	plan, lsn, err := s.Tests.UpdatePlan(r.Context(), userFrom(r), id, in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"plan": plan})
}

func (s *Server) handleSetTestPlanCases(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "planID", "test plan")
	if !ok {
		return
	}
	var in testexec.CasesInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	plan, lsn, err := s.Tests.SetPlanCases(r.Context(), userFrom(r), id, in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"plan": plan})
}

func (s *Server) handleDeleteTestPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "planID", "test plan")
	if !ok {
		return
	}
	lsn, err := s.Tests.DeletePlan(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListTestExecutions(w http.ResponseWriter, r *http.Request) {
	list, err := s.Tests.Executions(r.Context(), userFrom(r), projectKeyParam(r), testexec.State(r.URL.Query().Get("state")))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"executions": list})
}

func (s *Server) handleListMyTestExecutions(w http.ResponseWriter, r *http.Request) {
	list, err := s.Tests.Mine(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"executions": list})
}

func (s *Server) handleStartTestExecution(w http.ResponseWriter, r *http.Request) {
	var in testexec.ExecutionInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	e, lsn, err := s.Tests.Start(r.Context(), userFrom(r), projectKeyParam(r), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"execution": e})
}

func (s *Server) handleGetTestExecution(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	e, err := s.Tests.Execution(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"execution": e})
}

func (s *Server) handleUpdateTestExecution(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	var in testexec.ExecutionPatch
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	e, lsn, err := s.Tests.Update(r.Context(), userFrom(r), id, in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"execution": e})
}

func (s *Server) handleCloseTestExecution(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	var in testexec.CloseInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	e, lsn, err := s.Tests.Close(r.Context(), userFrom(r), id, in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"execution": e})
}

// iterationOf is the iteration a query names, 1 when it names none; one that is no number is refused.
func iterationOf(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("iteration")
	if raw == "" {
		return 1, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		respondError(w, r, ErrValidation(map[string]string{"iteration": "Name the iteration by its number, from 1."}))
		return 0, false
	}
	return n, true
}

func (s *Server) handleGetTestExecutionCase(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	iteration, ok := iterationOf(w, r)
	if !ok {
		return
	}
	c, err := s.Tests.Case(r.Context(), userFrom(r), id, chi.URLParam(r, "reqKey"), iteration)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"case": c})
}

func (s *Server) handleRecordTestResult(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	var in testexec.RecordInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	c, lsn, err := s.Tests.Record(r.Context(), userFrom(r), id, chi.URLParam(r, "reqKey"), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"case": c})
}

func (s *Server) handleUploadTestExecutionFile(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	iteration, ok := iterationOf(w, r)
	if !ok {
		return
	}
	var step *int
	if raw := r.URL.Query().Get("step"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			respondError(w, r, ErrValidation(map[string]string{"step": "Name the step by its number, from 1."}))
			return
		}
		step = &n
	}
	r.Body = http.MaxBytesReader(w, r.Body, testexec.MaxFileBytes+uploadSlack)
	file, header, err := r.FormFile("file")
	if err != nil {
		if tooBig(err) {
			respondError(w, r, testexec.ErrTooLarge)
			return
		}
		respondError(w, r, ErrBadRequest("Send the file as multipart form data, in a part named file."))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, testexec.MaxFileBytes+1))
	if err != nil {
		respondError(w, r, ErrBadRequest("The upload could not be read. Try sending the file again."))
		return
	}
	made, lsn, err := s.Tests.Upload(r.Context(), userFrom(r), id, chi.URLParam(r, "reqKey"), iteration, step, header.Filename,
		header.Header.Get("Content-Type"), data)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"file": made})
}

func (s *Server) handleDownloadTestExecutionFile(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "fileID", "file")
	if !ok {
		return
	}
	f, data, err := s.Tests.FileBytes(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	sendFile(w, f.ContentType, objectstore.CleanName(f.FileName), data)
}

func (s *Server) handleDeleteTestExecutionFile(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "fileID", "file")
	if !ok {
		return
	}
	lsn, err := s.Tests.DeleteFile(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleFileTestFailureIssue(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	var in testexec.FailureIssueInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	issue, lsn, err := s.Tests.FileIssue(r.Context(), userFrom(r), id, chi.URLParam(r, "reqKey"), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"issue": issue})
}

func (s *Server) handleListTestCaseResults(w http.ResponseWriter, r *http.Request) {
	results, err := s.Tests.CaseResults(r.Context(), userFrom(r), chi.URLParam(r, "reqKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleGetTestExecutionReport(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	rep, err := s.Tests.Report(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"report": rep})
}

// noteExecutionReport records an execution's report about to leave, as an exported document is.
func (s *Server) noteExecutionReport(w http.ResponseWriter, r *http.Request, rep *testexec.Report, format string) bool {
	id := rep.Execution.ID
	return s.noteExport(w, r, audit.Entry{Action: audit.ActionSpecificationExported, TargetType: "test_execution", TargetID: &id,
		Data: map[string]any{"project": rep.Execution.ProjectKey, "kind": "execution", "format": format, "title": rep.Execution.Title}})
}

func (s *Server) handleTestExecutionReportDocx(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	rep, err := s.Tests.Report(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	at := time.Now().UTC()
	f, err := s.framingOf(r, rep.Execution.ProjectKey)
	if err != nil {
		respondError(w, r, err)
		return
	}
	var frame *docx.Template
	fill := docx.Fill{"project": rep.Execution.ProjectKey, "title": rep.Execution.Title, "date": at.Format(time.DateOnly), "exportedBy": s.nameOf(r)}
	if f != nil {
		frame = f.frame
		if p, err := s.Projects.Get(r.Context(), userFrom(r), rep.Execution.ProjectKey); err == nil {
			fill["project"] = p.Name + " (" + p.Key + ")"
		}
	}
	body, err := docx.ExecutionReportBytesWith(r.Context(), rep, s.nameOf(r), at, language(r), frame, fill)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !s.noteExecutionReport(w, r, rep, "docx") {
		return
	}
	sendFile(w, docx.ContentType, docx.ExecutionReportFileName(rep, at, "docx"), body)
}

func (s *Server) handleTestExecutionReportPDF(w http.ResponseWriter, r *http.Request) {
	id, ok := testIDParam(w, r, "executionID", "test execution")
	if !ok {
		return
	}
	rep, err := s.Tests.Report(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	pdf, ok := s.printed(w, r, "/print/executions/"+id.String())
	if !ok || !s.noteExecutionReport(w, r, rep, "pdf") {
		return
	}
	sendFile(w, "application/pdf", docx.ExecutionReportFileName(rep, time.Now().UTC(), "pdf"), pdf)
}

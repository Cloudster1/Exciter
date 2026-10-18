package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/reqifio"
)

// ReqIF import and export (#44): a file from another requirements tool into a project, its types, attributes and
// link types mapped by the person who uploaded it and run by the worker, and a project or a section out again.

// reqifOn refuses every ReqIF route of a server built without the service.
func (s *Server) reqifOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.ReqIF == nil {
			respondError(w, r, errImportsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// reqifError maps the ReqIF imports' refusals; nil when err is none of theirs.
func reqifError(err error) *APIError {
	var (
		file  *reqifio.FileError
		field *reqifio.FieldError
	)
	switch {
	case errors.As(err, &file):
		return ErrValidation(map[string]string{"file": file.Message})
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, reqifio.ErrNotFound):
		return ErrNotFound("That import was not found. It may have been discarded; upload the file again.")
	case errors.Is(err, reqifio.ErrForbidden):
		return ErrForbidden("Only the project's administrators import into it, and only the person who uploaded a file maps and starts it. Ask them.")
	case errors.Is(err, reqifio.ErrStarted):
		return ErrConflict("This import has started, so its choices no longer change. Upload the file again to import it another way.")
	case errors.Is(err, reqifio.ErrProblems):
		return &APIError{Status: http.StatusConflict, Code: "import_problems",
			Message: "Some of the file does not fit the project yet. Map or leave out what the problems name, or change the file, before importing."}
	case errors.Is(err, reqifio.ErrNoSection):
		return ErrNotFound("The project has no requirement or section with that key to export. Check the key, or export the whole project.")
	}
	return nil
}

func (s *Server) handleUploadReqIF(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, reqifio.MaxImportBytes+uploadSlack)
	file, err := uploadedFile(r)
	if err != nil {
		respondError(w, r, reqifTooLarge(err))
		return
	}
	data, err := io.ReadAll(io.LimitReader(file.Body, reqifio.MaxImportBytes+1))
	if err != nil || len(data) > reqifio.MaxImportBytes {
		respondError(w, r, reqifTooLarge(err))
		return
	}
	made, lsn, err := s.ReqIF.Upload(r.Context(), userFrom(r), projectKeyParam(r), objectstore.CleanName(file.FileName), data)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"import": made})
}

// reqifTooLarge is the refusal of a file over the limit, whether the body cap or the read tripped it.
func reqifTooLarge(err error) error {
	if err == nil || tooBig(err) {
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: fmt.Sprintf("That file is too large: an import takes files of up to %d MB. Export the specifications in several files, and import them one after the other.", reqifio.MaxImportMB)}
	}
	return err
}

func (s *Server) handleGetReqIF(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.ReqIF.Get(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"import": found})
}

func (s *Server) handleChooseReqIF(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req reqifio.ChoicesInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	chosen, lsn, err := s.ReqIF.SetChoices(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"import": chosen})
}

func (s *Server) handleStartReqIF(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	started, lsn, err := s.ReqIF.Start(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"import": started})
}

func (s *Server) handleCancelReqIF(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.ReqIF.Cancel(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleExportReqIF(w http.ResponseWriter, r *http.Request) {
	s.exportReqIF(w, r, false)
}
func (s *Server) handleExportReqIFZ(w http.ResponseWriter, r *http.Request) {
	s.exportReqIF(w, r, true)
}

// exportReqIF writes the file whole before answering, so a refusal is still the error envelope.
func (s *Server) exportReqIF(w http.ResponseWriter, r *http.Request, archive bool) {
	var buf bytes.Buffer
	done, err := s.ReqIF.Export(r.Context(), &buf, userFrom(r), projectKeyParam(r), r.URL.Query().Get("section"), archive)
	if err != nil {
		respondError(w, r, err)
		return
	}
	contentType := "application/xml; charset=utf-8"
	if archive {
		contentType = "application/zip"
	}
	download(w, contentType, done.Name)
	if done.Truncated() {
		w.Header().Set("X-Export-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

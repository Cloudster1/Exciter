package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/csvio"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// CSV import and export (#23): a file into a project through a mapping the person confirms, run by the worker,
// and a project's grid out as a file.

var errImportsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "imports_unavailable",
	Message: "Importing and exporting are not set up on this server. Ask its operator to configure it."}

// importsOn refuses every import route of a server built without the service.
func (s *Server) importsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Imports == nil || s.Search == nil {
			respondError(w, r, errImportsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// importError maps the imports' refusals; nil when err is none of theirs.
func importError(err error) *APIError {
	var (
		file   *csvio.FileError
		column *csvio.ColumnError
	)
	switch {
	case errors.As(err, &file):
		return ErrValidation(map[string]string{"file": file.Message})
	case errors.As(err, &column):
		return ErrValidation(map[string]string{"column": column.Message})
	case errors.Is(err, csvio.ErrNotFound):
		return ErrNotFound("That import was not found. It may have been discarded; upload the file again.")
	case errors.Is(err, csvio.ErrForbidden):
		return ErrForbidden("Only the project's administrators import into it, and only the person who uploaded a file maps and starts it. Ask them.")
	case errors.Is(err, csvio.ErrStarted):
		return ErrConflict("This import has started, so its mapping no longer changes. Upload the file again to import it another way.")
	case errors.Is(err, csvio.ErrProblems):
		return &APIError{Status: http.StatusConflict, Code: "import_problems",
			Message: "Some values in the file match nothing in the project. Map them, or change the file, before importing."}
	}
	return nil
}

func (s *Server) handleUploadImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, csvio.MaxImportBytes+uploadSlack)
	file, err := uploadedFile(r)
	if err != nil {
		respondError(w, r, importTooLarge(err))
		return
	}
	data, err := io.ReadAll(io.LimitReader(file.Body, csvio.MaxImportBytes+1))
	if err != nil || len(data) > csvio.MaxImportBytes {
		respondError(w, r, importTooLarge(err))
		return
	}
	made, lsn, err := s.Imports.Upload(r.Context(), userFrom(r), projectKeyParam(r), objectstore.CleanName(file.FileName), data)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"import": made})
}

// importTooLarge is the refusal of a file over the limit, whether the body cap or the read tripped it.
func importTooLarge(err error) error {
	if err == nil || tooBig(err) {
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: fmt.Sprintf("That file is too large: an import takes files of up to %d MB. Split it, and import the parts one after the other.", csvio.MaxImportMB)}
	}
	return err
}

func (s *Server) handleGetImport(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.Imports.Get(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"import": found})
}

func (s *Server) handleMapImport(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req csvio.MappingInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	mapped, lsn, err := s.Imports.SetMapping(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"import": mapped})
}

func (s *Server) handleStartImport(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	started, lsn, err := s.Imports.Start(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"import": started})
}

func (s *Server) handleCancelImport(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "importID", "import")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Imports.Cancel(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// handleExportCSV writes the file whole before answering, so a refusal is still the error envelope.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	filter, sort, err := search.ParseGrid(r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	key := projectKeyParam(r)
	var buf bytes.Buffer
	// The mark tells a spreadsheet the file is UTF-8, so names with accents open as they were written.
	buf.Write(utf8Mark)
	done, err := s.Imports.Export(r.Context(), &buf, userFrom(r), key, filter, sort, r.URL.Query()["column"])
	if err != nil {
		respondError(w, r, err)
		return
	}
	download(w, "text/csv; charset=utf-8", requirement.NormalizedProjectKey(key)+".csv")
	if done.Truncated() {
		w.Header().Set("X-Export-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// utf8Mark is the byte order mark of UTF-8, which a spreadsheet reads as the file's encoding.
var utf8Mark = []byte{0xEF, 0xBB, 0xBF}

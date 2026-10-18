package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/exporttemplate"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

// Word templates an export goes through (#214). See docs/api-contract-m11.md.

var errTemplatesOff = &APIError{Status: http.StatusServiceUnavailable, Code: "templates_unavailable",
	Message: "Word templates are not set up on this server. Ask its operator to configure the object store."}

func (s *Server) templatesOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.ExportTemplates == nil {
			respondError(w, r, errTemplatesOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// templateError maps the templates' refusals; nil when err is none of theirs.
func templateError(err error) *APIError {
	var field *exporttemplate.FieldError
	switch {
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.Is(err, exporttemplate.ErrNotFound):
		return ErrNotFound("That template was not found. It may have been deleted, or not shared with your project.")
	case errors.Is(err, exporttemplate.ErrForbidden):
		return ErrForbidden("Only the project's administrators upload and change its Word templates. Ask one of them.")
	case errors.Is(err, exporttemplate.ErrTooMany):
		return &APIError{Status: http.StatusConflict, Code: "too_many_templates",
			Message: fmt.Sprintf("A project keeps at most %d templates. Delete one first.", exporttemplate.MaxTemplates)}
	case errors.Is(err, exporttemplate.ErrTooLarge):
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: fmt.Sprintf("A template is at most %d MB, and unpacks into at most %d MB. Make it smaller, for instance by leaving out large pictures.",
				exporttemplate.MaxFileMB, exporttemplate.MaxUnpackedMB)}
	case errors.Is(err, exporttemplate.ErrNotUsable):
		return ErrValidation(map[string]string{"template": "This project may not use that template. Pick one of its own, or one another project shares."})
	case errors.Is(err, specdoc.ErrSinceNotBaseline):
		return ErrValidation(map[string]string{"since": "Mark changes since a baseline of this project. Pick one of its baselines."})
	}
	return nil
}

// templateUpload is a template's upload: the file and its name and description.
type templateUpload struct {
	exporttemplate.Form
	fileName string
	data     []byte
	sent     bool
}

func uploadedTemplate(r *http.Request) (templateUpload, error) {
	var up templateUpload
	reader, err := r.MultipartReader()
	if err != nil {
		return up, ErrBadRequest("Send the template as multipart form data, in a part named file, with its name in a part named name.")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if tooBig(err) {
				return up, exporttemplate.ErrTooLarge
			}
			return up, ErrBadRequest("The upload could not be read. Try sending the template again.")
		}
		switch part.FormName() {
		case "file":
			data, err := io.ReadAll(io.LimitReader(part, exporttemplate.MaxFileBytes+1))
			if err != nil || len(data) > exporttemplate.MaxFileBytes {
				return up, exporttemplate.ErrTooLarge
			}
			up.fileName, up.data, up.sent = part.FileName(), data, true
		case "name", "description":
			value, err := io.ReadAll(io.LimitReader(part, maxFormField+1))
			if err != nil {
				return up, err
			}
			if part.FormName() == "name" {
				up.Name = string(value)
			} else {
				up.Description = string(value)
			}
		}
	}
	if !up.sent {
		return up, ErrBadRequest("The upload has no part named file. Send the template in a part named file.")
	}
	return up, nil
}

func (s *Server) handleListExportTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.ExportTemplates.Templates(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"templates": list})
}

func (s *Server) handleUploadExportTemplate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, exporttemplate.MaxFileBytes+uploadSlack)
	up, err := uploadedTemplate(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.ExportTemplates.Upload(r.Context(), userFrom(r), projectKeyParam(r), up.Form, objectstore.CleanName(up.fileName), up.data)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"template": made})
}

func (s *Server) handleGetExportTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := templateIDParam(w, r)
	if !ok {
		return
	}
	t, err := s.ExportTemplates.Get(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"template": t})
}

func (s *Server) handleUpdateExportTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := templateIDParam(w, r)
	if !ok {
		return
	}
	var req exporttemplate.Patch
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	t, lsn, err := s.ExportTemplates.Update(r.Context(), userFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"template": t})
}

func (s *Server) handleDeleteExportTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := templateIDParam(w, r)
	if !ok {
		return
	}
	lsn, err := s.ExportTemplates.Delete(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleDownloadExportTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := templateIDParam(w, r)
	if !ok {
		return
	}
	t, data, err := s.ExportTemplates.File(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	sendFile(w, docx.ContentType, t.FileName, data)
}

func templateIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, apiErr := pathUUID(r, "templateID", "template")
	if apiErr != nil {
		respondError(w, r, exporttemplate.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

// framing is what a template asks of one export: the template itself, its frame for Word, and what it shows.
type framing struct {
	template *exporttemplate.Template
	frame    *docx.Template
	since    *uuid.UUID
}

// framingOf reads the template an export names, for the project it is of, nil when it names none.
func (s *Server) framingOf(r *http.Request, projectKey string) (*framing, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("template"))
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || s.ExportTemplates == nil {
		return nil, exporttemplate.ErrNotUsable
	}
	t, frame, err := s.ExportTemplates.Usable(r.Context(), userFrom(r), projectKey, id)
	if errors.Is(err, exporttemplate.ErrNotFound) {
		return nil, exporttemplate.ErrNotUsable
	}
	if err != nil {
		return nil, err
	}
	f := &framing{template: t, frame: frame}
	if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" && t.Include.ChangeMarks {
		id, err := uuid.Parse(since)
		if err != nil {
			return nil, specdoc.ErrSinceNotBaseline
		}
		f.since = &id
	}
	return f, nil
}

// shape applies a template's fields and inclusions to a document.
func (s *Server) shape(r *http.Request, doc *specdoc.Document, f *framing) error {
	if f == nil {
		return nil
	}
	o := specdoc.Options{Links: f.template.Include.Links, Comments: f.template.Include.Comments, Since: f.since}
	for _, c := range f.template.Fields {
		o.Fields = append(o.Fields, specdoc.Choice{Field: string(c.Field), Attribute: c.Attribute})
	}
	return s.Documents.Shape(r.Context(), userFrom(r), doc, doc.ProjectKey, o)
}

// shapedDocument reads the document a route names, shaped by the template it asks for.
func (s *Server) shapedDocument(r *http.Request) (*specdoc.Document, *framing, error) {
	doc, err := s.documentOf(r)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.framingOf(r, doc.ProjectKey)
	if err != nil {
		return nil, nil, err
	}
	return doc, f, s.shape(r, doc, f)
}

// fillOf is what a template's placeholders say for a document.
func (s *Server) fillOf(r *http.Request, doc *specdoc.Document) docx.Fill {
	fill := docx.Fill{"project": doc.ProjectName + " (" + doc.ProjectKey + ")", "title": doc.Title, "section": doc.SectionKey,
		"date": doc.ExportedAt.Format(time.DateOnly), "exportedBy": doc.ExportedBy}
	if b := doc.Baseline; b != nil {
		fill["baseline"] = b.Name
		if names, err := s.Documents.Signers(r.Context(), userFrom(r), b.ID); err == nil {
			fill["signers"] = strings.Join(names, ", ")
		}
	}
	return fill
}

// printQuery carries a template and the baseline its change marks count from to the print view.
func printQuery(path string, f *framing) string {
	if f == nil {
		return path
	}
	q := url.Values{"template": {f.template.ID.String()}}
	if f.since != nil {
		q.Set("since", f.since.String())
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + q.Encode()
}

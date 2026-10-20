package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/render"
	"github.com/cloudster1/exciter/backend/internal/sitezip"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

// Export of a specification or a baseline as a document to sign and send (#45): read as JSON for the print view,
// written as Word here, and printed as PDF by the render service from that print view, as Stator prints a page.

var errDocumentsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "documents_unavailable",
	Message: "Exporting documents is not set up on this server. Ask its operator to configure it."}

// documentsOn refuses every document route of a server built without the service.
func (s *Server) documentsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Documents == nil {
			respondError(w, r, errDocumentsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// documentError maps the documents' refusals and the render service's; nil when err is none of theirs.
func documentError(err error) *APIError {
	switch {
	case errors.Is(err, specdoc.ErrNoSection):
		return ErrNotFound("The project has no requirement or section with that key to export. Check the key, or export the whole project.")
	case errors.Is(err, docx.ErrTooLarge):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "docx_too_large",
			Message: "The pictures in this specification make the Word file larger than this server hands out. Export a smaller section, or the PDF."}
	case errors.Is(err, render.ErrUnavailable):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "render_unavailable",
			Message: "PDF export is not set up on this server. Export the document as Word instead, or ask whoever runs Exciter to start the render service."}
	case errors.Is(err, render.ErrBusy):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "render_busy",
			Message: "Too many PDFs are being made right now. Try again in a minute."}
	case errors.Is(err, render.ErrTimeout):
		return &APIError{Status: http.StatusGatewayTimeout, Code: "render_timeout",
			Message: "The document took too long to print. Export a smaller section, or the document as Word."}
	case errors.Is(err, render.ErrTooLarge):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "render_too_large",
			Message: "The PDF is larger than this server hands out. Export a smaller section, or the document as Word."}
	case errors.Is(err, render.ErrFailed):
		return &APIError{Status: http.StatusBadGateway, Code: "render_failed",
			Message: "The document could not be printed. Try again; if it fails again, ask whoever runs Exciter to look at the render service."}
	}
	return nil
}

// errProjectToken refuses a print to a token held to some projects, since the print's own token reads them all.
var errProjectToken = &APIError{Status: http.StatusForbidden, Code: "forbidden",
	Message: "A token limited to some projects cannot print a PDF. Sign in, or use a token for every project, or export the document as Word."}

// documentOf reads the document a route names: a project's, a section's or a baseline's.
func (s *Server) documentOf(r *http.Request) (*specdoc.Document, error) {
	if chi.URLParam(r, "baselineID") != "" {
		id, apiErr := pathUUID(r, "baselineID", "baseline")
		if apiErr != nil {
			return nil, apiErr
		}
		return s.Documents.Baseline(r.Context(), userFrom(r), id)
	}
	return s.Documents.Project(r.Context(), userFrom(r), projectKeyParam(r), r.URL.Query().Get("section"))
}

func (s *Server) handleGetProjectDocument(w http.ResponseWriter, r *http.Request) {
	s.sendDocument(w, r)
}
func (s *Server) handleGetBaselineDocument(w http.ResponseWriter, r *http.Request) {
	s.sendDocument(w, r)
}

func (s *Server) sendDocument(w http.ResponseWriter, r *http.Request) {
	doc, _, err := s.shapedDocument(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"document": doc})
}

// language is the reader's language for the words a document adds, English unless they chose German.
func language(r *http.Request) string {
	if p := PrincipalFrom(r.Context()); p != nil && p.Locale != "" {
		return string(p.Locale)
	}
	return "en"
}

func (s *Server) handleProjectDocx(w http.ResponseWriter, r *http.Request)  { s.sendDocx(w, r) }
func (s *Server) handleBaselineDocx(w http.ResponseWriter, r *http.Request) { s.sendDocx(w, r) }

// sendDocx writes the document as Word, with the pictures the reader may open, before anything is sent.
func (s *Server) sendDocx(w http.ResponseWriter, r *http.Request) {
	doc, f, err := s.shapedDocument(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	reader := docx.Reader{BaseURL: s.AppBaseURL, Picture: s.pictureFor(userFrom(r)), Diagram: s.diagramPNG}
	var frame *docx.Template
	if f != nil {
		frame = f.frame
	}
	body, err := docx.BytesWith(r.Context(), doc, language(r), reader, frame, s.fillOf(r, doc))
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !s.noteDocument(w, r, doc, "docx", f) {
		return
	}
	sendFile(w, docx.ContentType, docx.FileName(doc, "docx"), body)
}

// pictureFor opens a file as the reader may; one they may not open, or a server without files, is left out.
func (s *Server) pictureFor(reader uuid.UUID) func(context.Context, uuid.UUID) (io.ReadCloser, error) {
	if s.Attachments == nil {
		return nil
	}
	return func(ctx context.Context, id uuid.UUID) (io.ReadCloser, error) {
		located, err := s.Attachments.Locate(ctx, reader, id)
		if err != nil {
			return nil, nil
		}
		body, err := located.Bytes(ctx)
		if err != nil {
			return nil, nil
		}
		return body, nil
	}
}

func (s *Server) handleProjectMarkdown(w http.ResponseWriter, r *http.Request)  { s.sendMarkdown(w, r) }
func (s *Server) handleBaselineMarkdown(w http.ResponseWriter, r *http.Request) { s.sendMarkdown(w, r) }

// sendMarkdown writes the document as Markdown (#187), recorded as an export as Word and PDF are.
func (s *Server) sendMarkdown(w http.ResponseWriter, r *http.Request) {
	doc, err := s.documentOf(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !s.noteDocument(w, r, doc, "md", nil) {
		return
	}
	sendFile(w, specdoc.MarkdownContentType, docx.FileName(doc, "md"), []byte(doc.Markdown().String()))
}

func (s *Server) handleProjectPDF(w http.ResponseWriter, r *http.Request)  { s.sendPDF(w, r) }
func (s *Server) handleBaselinePDF(w http.ResponseWriter, r *http.Request) { s.sendPDF(w, r) }

// sendPDF has the render service print the document's print view as the reader, with a token made for that print
// alone and deleted once the PDF is back.
func (s *Server) sendPDF(w http.ResponseWriter, r *http.Request) {
	doc, f, err := s.shapedDocument(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	pdf, ok := s.printed(w, r, printQuery(printPath(doc), f))
	if !ok || !s.noteDocument(w, r, doc, "pdf", f) {
		return
	}
	sendFile(w, "application/pdf", docx.FileName(doc, "pdf"), pdf)
}

func (s *Server) handleProjectSite(w http.ResponseWriter, r *http.Request)  { s.sendSite(w, r) }
func (s *Server) handleBaselineSite(w http.ResponseWriter, r *http.Request) { s.sendSite(w, r) }

// sendSite has the render service hand over the print view's HTML as the reader, and cuts it into a site that
// opens offline (#214).
func (s *Server) sendSite(w http.ResponseWriter, r *http.Request) {
	doc, f, err := s.shapedDocument(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	page, ok := s.rendered(w, r, printQuery(printPath(doc), f), true)
	if !ok {
		return
	}
	site, err := sitezip.Build(r.Context(), page, doc.Title, siteLabels(language(r)), s.pictureFor(userFrom(r)))
	if err != nil {
		respondError(w, r, err)
		return
	}
	if !s.noteDocument(w, r, doc, "html", f) {
		return
	}
	sendFile(w, "application/zip", docx.FileName(doc, "html.zip"), site)
}

// siteLabels are the words a site adds, in the reader's language.
func siteLabels(lang string) sitezip.Labels {
	if lang == "de" {
		return sitezip.Labels{Contents: "Inhalt", Search: "Suchen", NoMatch: "Nichts gefunden.", Previous: "Zurück", Next: "Weiter", Glossary: "Glossar"}
	}
	return sitezip.Labels{Contents: "Contents", Search: "Search", NoMatch: "Nothing found.", Previous: "Back", Next: "On", Glossary: "Glossary"}
}

// printed has the render service print a page of the web client as the reader, with a token only the print holds;
// false when it answered the refusal itself.
func (s *Server) printed(w http.ResponseWriter, r *http.Request, path string) ([]byte, bool) {
	return s.rendered(w, r, path, false)
}

// rendered has the render service print a page, or hand its HTML over when asHTML says so.
func (s *Server) rendered(w http.ResponseWriter, r *http.Request, path string, asHTML bool) ([]byte, bool) {
	renderer := s.Renderer
	if renderer == nil {
		renderer = render.Unavailable{}
	}
	pages, hasHTML := renderer.(render.HTMLRenderer)
	if _, off := renderer.(render.Unavailable); off || (asHTML && !hasHTML) {
		respondError(w, r, render.ErrUnavailable)
		return nil, false
	}
	p := PrincipalFrom(r.Context())
	if p == nil || len(p.TokenProjects) > 0 || (p.Token != nil && !p.Token.AllProjects) {
		respondError(w, r, errProjectToken)
		return nil, false
	}
	token, tokenID, lsn, err := s.Accounts.MintRenderToken(r.Context(), p.UserID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return nil, false
	}
	// The print's own requests read no older than the token, so what the reader wrote just before is printed.
	if s.Fresh != nil {
		s.Fresh.Note(r.Context(), tokenFreshnessKey(tokenID), lsn)
	}
	req := render.Request{Path: path, Token: token, Language: language(r)}
	var pdf []byte
	if asHTML {
		pdf, err = pages.HTML(r.Context(), req)
	} else {
		pdf, err = renderer.PDF(r.Context(), req)
	}
	if revokeErr := s.Accounts.RevokeRenderToken(context.WithoutCancel(r.Context()), tokenID); revokeErr != nil && s.Log != nil {
		s.Log.Warn("a print's token could not be deleted; it expires by itself", "error", revokeErr)
	}
	if err != nil {
		respondError(w, r, err)
		return nil, false
	}
	return pdf, true
}

// printPath is the print view of a document in the web client.
func printPath(doc *specdoc.Document) string {
	if doc.Baseline != nil {
		return "/print/baselines/" + doc.Baseline.ID.String()
	}
	path := "/print/p/" + url.PathEscape(doc.ProjectKey) + "/document"
	if doc.SectionKey != "" {
		path += "?section=" + url.QueryEscape(doc.SectionKey)
	}
	return path
}

// noteDocument records a document about to leave; an export the log cannot record is refused.
func (s *Server) noteDocument(w http.ResponseWriter, r *http.Request, doc *specdoc.Document, format string, f *framing) bool {
	data := map[string]any{"project": doc.ProjectKey, "kind": string(doc.Kind), "format": format, "title": doc.Title}
	if f != nil {
		data["template"] = f.template.Name
	}
	entry := audit.Entry{Action: audit.ActionSpecificationExported, TargetType: "project", Data: data}
	if doc.Baseline != nil {
		id := doc.Baseline.ID
		entry.TargetType, entry.TargetID = "baseline", &id
	}
	return s.noteExport(w, r, entry)
}

// sendFile answers a file whole, kept out of every cache since what it holds is the reader's.
func sendFile(w http.ResponseWriter, contentType, name string, body []byte) {
	download(w, contentType, name)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/exciter/backend/internal/attachment"
	"github.com/cloudster1/exciter/backend/internal/objectstore"
)

// Attachments (#21): files on a requirement, uploaded as multipart form data and streamed back through the API,
// as Stator does, so that the session and not a bucket policy decides who may read them.

// attachmentsOn answers for a server built without the file service, such as a test's, as for one without storage.
func (s *Server) attachmentsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Attachments == nil || s.Requirements == nil {
			respondError(w, r, objectstore.ErrUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// attachmentError maps the files' refusals; nil when err is none of theirs.
func attachmentError(err error) *APIError {
	var tooLarge *attachment.TooLargeError
	switch {
	case errors.As(err, &tooLarge):
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: tooLarge.Error()}
	case errors.Is(err, attachment.ErrEmpty):
		return ErrValidation(map[string]string{"file": "That file is empty. Choose a file with something in it."})
	case errors.Is(err, attachment.ErrNotFound):
		return ErrNotFound("That file was not found. Reload the requirement; somebody may have deleted it.")
	case errors.Is(err, attachment.ErrForbidden):
		return ErrForbidden("Only the project's editors change a requirement's files. Ask a project administrator for the editor role.")
	case errors.Is(err, objectstore.ErrNoObject):
		return &APIError{Status: http.StatusGone, Code: "file_missing", cause: err,
			Message: "The bytes of this file are missing from storage. Upload it again, or ask an administrator to restore the storage."}
	}
	return nil
}

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	found, err := s.Attachments.List(r.Context(), userFrom(r), chi.URLParam(r, "reqKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"attachments": found})
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	limit := s.Attachments.MaxSize
	r.Body = http.MaxBytesReader(w, r.Body, limit+uploadSlack)
	in, err := uploadedFile(r)
	if err != nil {
		respondError(w, r, uploadError(err, limit))
		return
	}
	created, lsn, err := s.Attachments.Upload(r.Context(), userFrom(r), chi.URLParam(r, "reqKey"), in)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, uploadError(err, limit))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"attachment": created})
}

// uploadedFile finds the part named file and hands its stream on.
func uploadedFile(r *http.Request) (attachment.UploadInput, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return attachment.UploadInput{}, ErrBadRequest("Send the file as multipart form data in a part named file.")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return attachment.UploadInput{}, ErrBadRequest("The upload has no part named file. Send the file in a part named file.")
		}
		if err != nil {
			if tooBig(err) {
				return attachment.UploadInput{}, err
			}
			return attachment.UploadInput{}, ErrBadRequest("The upload could not be read. Try sending the file again.")
		}
		if part.FormName() == "file" {
			return attachment.UploadInput{FileName: part.FileName(), ContentType: part.Header.Get("Content-Type"), Body: part}, nil
		}
	}
}

func tooBig(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// uploadError turns the body cap tripping mid-stream into the size refusal.
func uploadError(err error, limit int64) error {
	if tooBig(err) {
		return &attachment.TooLargeError{Limit: limit}
	}
	return err
}

// handleDownloadAttachment streams the bytes with the stored type, as a download unless inline asks for one of
// the types a browser shows without running anything.
func (s *Server) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "attachmentID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	inline := false
	if raw := r.URL.Query().Get("inline"); raw != "" {
		var err error
		if inline, err = strconv.ParseBool(raw); err != nil {
			respondError(w, r, ErrValidation(map[string]string{"inline": "Say true to show the file in place, or leave it out to download it."}))
			return
		}
	}
	found, err := s.Attachments.Locate(r.Context(), userFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	body, err := found.Bytes(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer body.Close()
	disposition := "attachment"
	if inline && attachment.ShowsInPlace(found.ContentType) {
		disposition = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", found.ContentType)
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": found.FileName}))
	h.Set("Content-Length", strconv.FormatInt(found.Size, 10))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=0")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "attachmentID", "file")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Attachments.Delete(r.Context(), userFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

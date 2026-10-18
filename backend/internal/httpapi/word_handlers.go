package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/reqifio"
)

// Word import (#209): a .docx into a project or below one of its sections, which then goes on as a ReqIF import.

// wordUpload is a Word document's upload: the file, its name, and the section to import below.
type wordUpload struct {
	reqifio.WordForm
	fileName string
	data     []byte
	sent     bool
}

// uploadedWord reads the parts of a Word upload, the file within MaxImportBytes, whichever order they come in.
func uploadedWord(r *http.Request) (wordUpload, error) {
	var up wordUpload
	reader, err := r.MultipartReader()
	if err != nil {
		return up, ErrBadRequest("Send the document as multipart form data, in a part named file, and the section to import below in a part named parentKey.")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if tooBig(err) {
				return up, err
			}
			return up, ErrBadRequest("The upload could not be read. Try sending the document again.")
		}
		switch part.FormName() {
		case "file":
			data, err := io.ReadAll(io.LimitReader(part, reqifio.MaxImportBytes+1))
			if err != nil || len(data) > reqifio.MaxImportBytes {
				return up, reqifTooLarge(err)
			}
			up.fileName, up.data, up.sent = part.FileName(), data, true
		case "parentKey":
			value, err := io.ReadAll(io.LimitReader(part, maxFormField+1))
			if err != nil {
				return up, err
			}
			if len(value) > maxFormField {
				return up, ErrValidation(map[string]string{"parentKey": "Name the section by its key, such as BRK-R4."})
			}
			up.ParentKey = strings.TrimSpace(string(value))
		}
	}
	if !up.sent {
		return up, ErrBadRequest("The upload has no part named file. Send the document in a part named file.")
	}
	return up, nil
}

func (s *Server) handleUploadWord(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, reqifio.MaxImportBytes+uploadSlack)
	up, err := uploadedWord(r)
	if err != nil {
		respondError(w, r, reqifTooLarge(err))
		return
	}
	made, lsn, err := s.ReqIF.UploadWord(r.Context(), userFrom(r), projectKeyParam(r), up.ParentKey, objectstore.CleanName(up.fileName), up.data)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"import": made})
}

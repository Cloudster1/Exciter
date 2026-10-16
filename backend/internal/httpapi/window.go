package httpapi

import (
	"mime"
	"net/http"
	"strconv"

	"github.com/cloudster1/exciter/backend/internal/keyset"
)

// keysetWindow reads a list's window: how many, up to maxLimit, and the cursor where the window before ended.
func keysetWindow(r *http.Request, defaultLimit, maxLimit int) (int, *keyset.Cursor, *APIError) {
	limit, apiErr := queryLimit(r, defaultLimit, maxLimit)
	if apiErr != nil {
		return 0, nil, apiErr
	}
	after, err := keyset.Decode(r.URL.Query().Get("cursor"))
	if err != nil {
		return 0, nil, ErrValidation(map[string]string{"cursor": "That cursor is not one this list gave out. Start again from the first window."})
	}
	return limit, after, nil
}

// queryLimit reads how many a list answers: defaultLimit when absent, and refused outside 1 to maxLimit.
func queryLimit(r *http.Request, defaultLimit, maxLimit int) (int, *APIError) {
	return namedLimit(r, "limit", defaultLimit, maxLimit)
}

func namedLimit(r *http.Request, name string, defaultLimit, maxLimit int) (int, *APIError) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxLimit {
		return 0, ErrValidation(map[string]string{name: "Ask for 1 to " + strconv.Itoa(maxLimit) + " at a time."})
	}
	return n, nil
}

// offsetWindow reads a list's window by count: how many, up to maxLimit, and how many to pass over first.
func offsetWindow(r *http.Request, defaultLimit, maxLimit int) (int, int, *APIError) {
	return namedWindow(r, "limit", "offset", defaultLimit, maxLimit)
}

// namedWindow is offsetWindow under other names, for a second window in one request such as a matrix's columns.
func namedWindow(r *http.Request, limitName, offsetName string, defaultLimit, maxLimit int) (int, int, *APIError) {
	limit, apiErr := namedLimit(r, limitName, defaultLimit, maxLimit)
	if apiErr != nil {
		return 0, 0, apiErr
	}
	offset := 0
	if raw := r.URL.Query().Get(offsetName); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, ErrValidation(map[string]string{offsetName: "Start at 0 or later."})
		}
		offset = n
	}
	return limit, offset, nil
}

// download says the answer is a file to save, never to run or show in place.
func download(w http.ResponseWriter, contentType, name string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=0")
}

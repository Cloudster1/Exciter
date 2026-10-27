package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/docx"
	"github.com/cloudster1/exciter/backend/internal/render"
	"github.com/cloudster1/exciter/backend/internal/specdoc"
)

// What an export cannot hand out is answered in its own status and code with a sentence saying what to do, wrapped
// or not; anything else is left to the general mapping (#310).
func TestAnExportThatCannotBeMadeSaysWhy(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{render.ErrUnavailable, http.StatusServiceUnavailable, "render_unavailable"},
		{render.ErrBusy, http.StatusServiceUnavailable, "render_busy"},
		{render.ErrTimeout, http.StatusGatewayTimeout, "render_timeout"},
		{render.ErrTooLarge, http.StatusUnprocessableEntity, "render_too_large"},
		{render.ErrFailed, http.StatusBadGateway, "render_failed"},
		{docx.ErrTooLarge, http.StatusUnprocessableEntity, "docx_too_large"},
		{specdoc.ErrNoSection, http.StatusNotFound, "not_found"},
	} {
		for _, err := range []error{c.err, fmt.Errorf("export the document: %w", c.err)} {
			got := documentError(err)
			if got == nil {
				t.Errorf("%v is not answered", err)
				continue
			}
			if got.Status != c.status || got.Code != c.code || !strings.HasSuffix(got.Message, ".") {
				t.Errorf("%v is answered %d %s %q, want %d %s in a sentence", err, got.Status, got.Code, got.Message, c.status, c.code)
			}
		}
	}
	if got := documentError(errors.New("the database went away")); got != nil {
		t.Errorf("an error of no export is answered %+v", got)
	}
}

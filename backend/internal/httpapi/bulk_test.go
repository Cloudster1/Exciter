package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/requirement"
)

func TestABulkRefusalSaysWhatTheSingleOperationWould(t *testing.T) {
	r := httptestRequest(http.MethodPost, "/api/v1/projects/CHK/transitions", `{}`)
	for name, tc := range map[string]struct {
		err  error
		want requirement.BulkRefusal
	}{
		"not found": {requirement.ErrNotFound, requirement.BulkRefusal{Code: "not_found", Message: toAPIError(requirement.ErrNotFound).Message}},
		"moved on": {&requirement.TransitionConflict{Key: "CHK-R1", Status: "In review"},
			requirement.BulkRefusal{Code: "transition_unavailable", Message: "Somebody moved CHK-R1 to In review meanwhile. Pick a transition from there."}},
		"a field": {ErrValidation(map[string]string{"comment": "Say why it goes back.", "attributes.x": "Fill in Risk."}),
			requirement.BulkRefusal{Code: "validation_failed", Message: "Fill in Risk. Say why it goes back."}},
		"a failure": {errors.New("the database went away"), requirement.BulkRefusal{Code: "internal_error", Message: toAPIError(errors.New("x")).Message}},
	} {
		if got := refusalOf(r, tc.err); *got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, *got, tc.want)
		}
	}
}

//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// Writes no right guards, so nothing answers them 403, with the reason (#334).
var noForbidden = map[string]string{
	"POST /auth/logout":                          "anybody signed in may sign out",
	"DELETE /imports/{importID}":                 "whoever sees an import may stop it, and who may not see it is answered 404 (decided in #390)",
	"DELETE /reqif-imports/{importID}":           "whoever sees an import may stop it, and who may not see it is answered 404 (decided in #390)",
	"POST /projects/{projectKey}/reviews":        "answers 200 and refuses each requirement it may not ask for in its result, with the code forbidden",
	"POST /projects/{projectKey}/transitions":    "answers 200 and refuses each requirement it may not move in its result, with the code forbidden",
	"POST /projects/{projectKey}/widget-preview": "writes nothing: whoever reads the project may preview, and who may not is answered 404",
}

// Operations taking a body that has nothing to refuse in it, with the reason (#334).
var noFieldRefusal = map[string]string{
	"POST /armature/webhook":              "vouched for by its signature: a body that does not parse is 400, one not signed 401",
	"POST /stator/webhook":                "vouched for by its signature: a body that does not parse is 400, one not signed 401",
	"POST /mcp":                           "JSON-RPC answers its errors inside the envelope, and each tool's refusal is counted as the tool's",
	"POST /oauth/register":                "answers OAuth's own errors, 400 invalid_client_metadata and the like, as RFC 7591 says",
	"POST /oauth/revoke":                  "answers OAuth's own errors, 400 invalid_request, as RFC 7009 says",
	"POST /oauth/token":                   "answers OAuth's own errors, 400 invalid_grant and the like, as RFC 6749 says",
	"PUT /imports/{importID}/mapping":     "a mapping is never refused: what it leaves unread is answered as problems in the preview",
	"PUT /installation/oauth-connections": "one switch: a body that is no boolean is 400 bad_request",
	"PUT /themes/active":                  "a theme nobody has, or one the person may not use, is answered 404",
}

// Tools that cannot answer the way named, with the reason (#334); those never refused are the sweep's.
var (
	toolsNeverOK      = map[string]string{}
	toolsNeverRefused = toolsWithNothingToRefuse
)

// Every write refused 403 with a code, every body 422 on a field, every tool answered and refused, or said why not
// here: before, a refusal of any kind counted, so one refused only for a missing session passed (#334).
func TestEveryOperationAndToolWasRefusedForItsReasons(t *testing.T) {
	if runFiltered() {
		t.Skip("a filtered run cannot cover the whole surface")
	}
	writes := func(r httpapi.Route) bool {
		return !r.Public && !r.Pending && !r.Redirect && r.Method != http.MethodGet && noForbidden[r.Method+" "+r.Path] == ""
	}
	bodies := func(r httpapi.Route) bool {
		return r.Body && !r.Pending && noFieldRefusal[r.Method+" "+r.Path] == ""
	}
	anyCode := func(code string) bool { return code != "" }
	onAField := func(code string) bool { return code == "validation_failed" }
	if missed := apiContract.unrefusedFor(writes, http.StatusForbidden, anyCode); len(missed) > 0 {
		t.Errorf("%d writes were never refused 403 with a code; refuse each in a test, or say in noForbidden why it cannot be:\n  %s",
			len(missed), strings.Join(missed, "\n  "))
	}
	if missed := apiContract.unrefusedFor(bodies, http.StatusUnprocessableEntity, onAField); len(missed) > 0 {
		t.Errorf("%d operations taking a body were never refused 422 validation_failed; refuse a field of each, or say in noFieldRefusal why it has none:\n  %s",
			len(missed), strings.Join(missed, "\n  "))
	}
	var tools []string
	for _, r := range httpapi.Catalog() {
		if r.Tool != "" && !r.Pending {
			tools = append(tools, r.Tool)
		}
	}
	for _, c := range []struct {
		isError bool
		exempt  map[string]string
		what    string
	}{{false, toolsNeverOK, "answered with a result"}, {true, toolsNeverRefused, "refused with isError"}} {
		var missed []string
		for _, name := range apiContract.toolsUncalled(tools, c.isError) {
			if c.exempt[name] == "" {
				missed = append(missed, name)
			}
		}
		if len(missed) > 0 {
			t.Errorf("%d MCP tools were never %s over /api/v1/mcp:\n  %s", len(missed), c.what, strings.Join(missed, "\n  "))
		}
	}
	for name, exempt := range map[string]map[string]string{"noForbidden": noForbidden, "noFieldRefusal": noFieldRefusal, "toolsNeverOK": toolsNeverOK, "toolsNeverRefused": toolsNeverRefused} {
		for key, why := range exempt {
			if strings.TrimSpace(why) == "" {
				t.Errorf("%s exempts %s without saying why", name, key)
			}
		}
	}
}

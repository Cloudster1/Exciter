//go:build integration

package test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cloudster1/exciter/backend/internal/armature"
)

// armatureOffers sets what the stub's /meta lists, for every tenant at once; keepArmature puts Armature's own back.
func armatureOffers(t *testing.T, capabilities ...string) {
	t.Helper()
	stubControl(t, http.MethodPut, "capabilities", map[string]any{"capabilities": append([]string{}, capabilities...)})
}

// missingOf is the capabilities an answer's missing names, in its order.
func missingOf(t *testing.T, answer map[string]any) []string {
	t.Helper()
	out := []string{}
	for _, each := range answer["missing"].([]any) {
		m := each.(map[string]any)
		if !strings.Contains(m["message"].(string), "Update Armature") {
			t.Errorf("%s is missing without saying what to do: %q", m["capability"], m["message"])
		}
		out = append(out, m["capability"].(string))
	}
	return out
}

// What Armature offers is read from its /meta whenever the address is saved or a token checked (#100); what Exciter
// needs and Armature lacks is said on the settings, on the account and by each feature that needs it.
func TestWhatArmatureLacksIsSaidWhereItIsNeeded(t *testing.T) {
	capKey := projectKey(t, "CAP")
	h := newHarness(t)
	h.keepArmature(t)
	api := newAPIServer(t, h)
	home := h.owner(t, "arm-offers")
	slug := stubTenant(t)
	stubControl(t, http.MethodDelete, slug, nil)
	owner := api.as(t, home.user)
	base := armatureURL(t)

	conn := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base}), http.StatusOK, "connect Armature"), "connection")
	if got := missingOf(t, conn); len(got) != 0 || !slices.Contains(conn["capabilities"].([]any), any(armature.CapabilityIssues)) {
		t.Errorf("an Armature offering everything reads as %v", conn)
	}
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "connect a token")
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": capKey, "name": "Capabilities"}), http.StatusCreated, "make a project")
	want(t, owner.post(t, "/api/v1/projects/"+capKey+"/requirements", map[string]any{"title": "Brakes"}), http.StatusCreated, "write a requirement")

	t.Run("raw SQL claims no capability", func(t *testing.T) {
		admin := h.appConn(t, home.user)
		refusedOutright(t, admin, "an administrator claiming a capability", `UPDATE armature_connection SET capabilities = '{issue-embed@1}'`)
	})

	t.Run("a check finds an older Armature, and the settings and the account say what goes", func(t *testing.T) {
		armatureOffers(t, armature.CapabilityWebhooks)
		account := obj(t, want(t, owner.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "check the token"), "account")
		lacking := []string{armature.CapabilityIssues, armature.CapabilityRemoteLinks, armature.CapabilityThemes}
		if got := missingOf(t, account); !slices.Equal(got, lacking) {
			t.Errorf("the account lacks %v, want %v", got, lacking)
		}
		h.settle(t)
		conn := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the settings"), "connection")
		if got := missingOf(t, conn); !slices.Equal(got, lacking) || len(conn["capabilities"].([]any)) != 1 {
			t.Errorf("the settings lack %v of %v, want %v", got, conn["capabilities"], lacking)
		}
	})

	t.Run("each feature says so instead of calling", func(t *testing.T) {
		if got := obj(t, want(t, owner.get(t, "/api/v1/armature/issues?key=CP-1"), http.StatusOK, "look up a key")); got["status"] != "outdated" {
			t.Errorf("a lookup reads %v", got)
		}
		if got := obj(t, want(t, owner.get(t, "/api/v1/armature/search?q=brake"), http.StatusOK, "search")); got["status"] != "outdated" {
			t.Errorf("a search reads %v", got)
		}
		refuse(t, "linking an issue", owner.post(t, "/api/v1/requirements/"+capKey+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "implements"}),
			http.StatusConflict, "armature_outdated")
		refuse(t, "following the theme", owner.put(t, "/api/v1/armature/theme", nil), http.StatusConflict, "armature_outdated")
	})

	t.Run("a link stays unsynced while Armature takes no remote links", func(t *testing.T) {
		armatureOffers(t, armature.CapabilityIssues, armature.CapabilityWebhooks, armature.CapabilityThemes, "remote-links@2")
		want(t, owner.post(t, "/api/v1/armature/account/check", nil), http.StatusOK, "check again")
		h.settle(t)
		want(t, owner.post(t, "/api/v1/requirements/"+capKey+"-R1/armature-links", map[string]any{"issueKey": "CP-1", "relation": "implements"}),
			http.StatusCreated, "link an issue")
		h.drained(t)
		link := linkStates(t, owner, capKey+"-R1")["CP-1 implements"]
		if link["sync"] != "failed" || !strings.Contains(link["error"].(string), armature.CapabilityRemoteLinks) {
			t.Errorf("the link reads %v", link)
		}
	})

	t.Run("saving the address again reads it again", func(t *testing.T) {
		armatureOffers(t, armature.Needs()...)
		conn := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base}), http.StatusOK, "save again"), "connection")
		if got := missingOf(t, conn); len(got) != 0 || conn["connected"] != float64(1) {
			t.Errorf("after saving again: %v", conn)
		}
	})
}

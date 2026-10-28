//go:build integration

package test

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/httpapi"
)

// pathParam is one {name} of a route's path.
var pathParam = regexp.MustCompile(`\{([A-Za-z]+)\}`)

// nothingThere is an argument for a path parameter that names nothing anybody has.
func nothingThere(param string) any {
	switch {
	case param == "reqKey":
		return "ZZNONE-R1"
	case param == "projectKey":
		return "ZZNONE"
	case strings.HasSuffix(param, "ID"):
		return uuid.NewString()
	case strings.HasSuffix(param, "Number"):
		return 1
	default:
		return "nothing-" + uuid.NewString()[:8]
	}
}

// Tools that take nothing that could be missing, so a token that may use them cannot be refused them (#334).
var toolsWithNothingToRefuse = map[string]string{
	"whoami":                  "answers who the caller is, whoever that is",
	"server_info":             "describes the server to anybody signed in",
	"list_projects":           "lists the projects the caller may read, none at all included",
	"list_people":             "lists the people the caller may name, a token held to projects included",
	"list_labels":             "lists the labels on what the caller may read, which may be none",
	"list_my_test_executions": "lists the caller's own open executions, which may be none",
	"list_waiting_reviews":    "lists the reviews waiting on the caller, which may be none",
	"list_armature_projects":  "answers what Armature lets the caller see, or that it is not connected, and refuses nothing itself",
}

// Arguments a tool without a path to miss is refused for instead.
var refusedArguments = map[string]map[string]any{
	"search_requirements": {"updatedAfter": "not a day"},
}

// Every MCP tool refuses with isError what it cannot reach: a key or an id nobody has, and a tool for the whole
// installation called by a token held to one project. Its answer is read as the tool's caller reads it (#334).
func TestEveryToolRefusesWhatItCannotReach(t *testing.T) {
	key := projectKey(t, "MRS")
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.owner(t, "mcp-refusals").user)
	want(t, owner.post(t, "/api/v1/projects", map[string]any{"key": key, "name": key}), http.StatusCreated, "make "+key)
	_, full := makeToken(t, owner, map[string]any{"name": "Sweep"})
	_, narrow := makeToken(t, owner, map[string]any{"name": "Sweep, one project", "projects": []string{key}})
	h.settle(t)
	everywhere, held := api.withToken(owner.user, full), api.withToken(owner.user, narrow)

	var answered []string
	for _, r := range httpapi.Catalog() {
		if r.Tool == "" || r.Pending {
			continue
		}
		args := map[string]any{}
		for _, m := range pathParam.FindAllStringSubmatch(r.Path, -1) {
			args[m[1]] = nothingThere(m[1])
		}
		for name, value := range refusedArguments[r.Tool] {
			args[name] = value
		}
		caller := everywhere
		if r.OrgWide {
			caller = held
		}
		result := rpc(t, caller, "tools/call", map[string]any{"name": r.Tool, "arguments": args})
		if result["isError"] == true {
			if toolsWithNothingToRefuse[r.Tool] != "" {
				t.Errorf("%s is listed as having nothing to refuse, but refused %v", r.Tool, args)
			}
			continue
		}
		if toolsWithNothingToRefuse[r.Tool] == "" {
			answered = append(answered, r.Tool)
		}
	}
	sort.Strings(answered)
	if len(answered) > 0 {
		t.Errorf("%d tools answered what they could not reach; make each refuse it, or say in toolsWithNothingToRefuse why it has nothing to:\n  %s",
			len(answered), strings.Join(answered, "\n  "))
	}
}

package httpapi

import (
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/armature"
)

// Issues made from requirements (#28): what the caller may file in Armature, and one issue per requirement, each
// traced to its requirement.

func (s *Server) handleListArmatureProjects(w http.ResponseWriter, r *http.Request) {
	status, projects, err := s.Armature.Projects(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	if projects == nil {
		projects = []armature.Project{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "projects": projects})
}

func (s *Server) handleListArmatureIssueTypes(w http.ResponseWriter, r *http.Request) {
	status, types, err := s.Armature.IssueTypes(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	if types == nil {
		types = []armature.IssueType{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": status, "issueTypes": types})
}

func (s *Server) handleCreateArmatureIssues(w http.ResponseWriter, r *http.Request) {
	var req armature.CreateInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Armature.CreateIssues(r.Context(), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	var item *armature.ItemError
	if err != nil && (!errors.As(err, &item) || (len(made) == 0 && item.IssueKey == "")) {
		// Nothing stands in Armature, so the refusal is the answer.
		if item != nil {
			err = item.Err
		}
		respondError(w, r, err)
		return
	}
	var failed *armature.CreateFailure
	if item != nil {
		failed = createFailure(item)
		loggerFrom(r.Context()).Info("an Armature issue of a round was not made", "requirement", item.RequirementKey, "code", failed.Code, "error", err)
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"created": made, "failed": failed})
}

// createFailure says which requirement a round stopped at, and why, in words for its author.
func createFailure(item *armature.ItemError) *armature.CreateFailure {
	if item.IssueKey != "" {
		return &armature.CreateFailure{RequirementKey: item.RequirementKey, Code: "armature_unlinked",
			Message: "Armature made " + item.IssueKey + ", but Exciter could not link it to " + item.RequirementKey +
				". Link it under Armature issues on the requirement, or remove it in Armature."}
	}
	apiErr := toAPIError(item.Err)
	return &armature.CreateFailure{RequirementKey: item.RequirementKey, Code: apiErr.Code, Message: refusalSentence(apiErr)}
}

// refusalSentence is what to tell the author about one requirement: Armature's sentences on its fields say more
// than its general "Some fields need attention".
func refusalSentence(refusal *APIError) string {
	if len(refusal.Fields) == 0 {
		return refusal.Message
	}
	sentences := make([]string, 0, len(refusal.Fields))
	for _, field := range slices.Sorted(maps.Keys(refusal.Fields)) {
		sentences = append(sentences, sentence(refusal.Fields[field]))
	}
	return strings.Join(sentences, " ")
}

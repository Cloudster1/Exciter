package httpapi

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// Statuses and workflows (#17): the states requirements move through, and the transitions between them.

var errWorkflowsOff = &APIError{Status: http.StatusServiceUnavailable, Code: "workflows_unavailable",
	Message: "Workflows are not set up on this server. Ask its operator to configure it."}

// workflowsOn refuses every workflow route of a server built without the services.
func (s *Server) workflowsOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Workflows == nil || s.Projects == nil || s.Requirements == nil {
			respondError(w, r, errWorkflowsOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleListStatuses(w http.ResponseWriter, r *http.Request) {
	statuses, err := s.Workflows.Statuses(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"statuses": statuses})
}

func (s *Server) handleCreateStatus(w http.ResponseWriter, r *http.Request) {
	var req workflow.StatusInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Workflows.CreateStatus(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"status": made})
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "statusID", "status")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req workflow.StatusInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Workflows.UpdateStatus(r.Context(), userFrom(r), projectKeyParam(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"status": got})
}

func (s *Server) handleDeleteStatus(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "statusID", "status")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Workflows.DeleteStatus(r.Context(), userFrom(r), projectKeyParam(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	flows, err := s.Workflows.Workflows(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"workflows": flows})
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req workflow.GraphInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Workflows.CreateWorkflow(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"workflow": made})
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "workflowID", "workflow")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, err := s.Workflows.Workflow(r.Context(), userFrom(r), projectKeyParam(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"workflow": got})
}

func (s *Server) handleSaveWorkflow(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "workflowID", "workflow")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req workflow.GraphInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Workflows.SaveWorkflow(r.Context(), userFrom(r), projectKeyParam(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"workflow": got})
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "workflowID", "workflow")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Workflows.DeleteWorkflow(r.Context(), userFrom(r), projectKeyParam(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleAssignWorkflow(w http.ResponseWriter, r *http.Request) {
	var req workflow.AssignInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Workflows.Assign(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"workflow": got})
}

func (s *Server) handleListRuleTypes(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"ruleTypes": workflow.RuleTypes()})
}

func (s *Server) handleListTransitions(w http.ResponseWriter, r *http.Request) {
	offered, err := s.Requirements.Transitions(r.Context(), userFrom(r), reqKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"transitions": offered})
}

func (s *Server) handleTransitionRequirement(w http.ResponseWriter, r *http.Request) {
	var req workflow.TransitionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Requirements.Transition(r.Context(), userFrom(r), reqKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirement": got})
}

func (s *Server) handleListBulkTransitions(w http.ResponseWriter, r *http.Request) {
	offered, err := s.Requirements.TransitionsOf(r.Context(), userFrom(r), projectKeyParam(r), r.URL.Query()["key"])
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requirements": offered})
}

func (s *Server) handleBulkTransition(w http.ResponseWriter, r *http.Request) {
	var req requirement.BulkTransitionInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	outcomes, lsn, err := s.Requirements.BulkTransition(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	results := make([]requirement.BulkTransitionResult, len(outcomes))
	for i, o := range outcomes {
		results[i] = requirement.BulkTransitionResult{Key: o.Key, Outcome: requirement.BulkTaken, Requirement: o.Requirement}
		if o.Err != nil {
			results[i] = requirement.BulkTransitionResult{Key: o.Key, Outcome: requirement.BulkRefused, Error: refusalOf(r, o.Err)}
		}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"results": results})
}

// refusalOf is what the single operation would have answered for one requirement of a bulk one: its code, and its
// sentence, or the fields' sentences when it refused a field.
func refusalOf(r *http.Request, err error) *requirement.BulkRefusal {
	apiErr := toAPIError(err)
	if apiErr.Status >= http.StatusInternalServerError {
		loggerFrom(r.Context()).Error("one requirement of a bulk request failed", "code", apiErr.Code, "error", err)
	}
	message := apiErr.Message
	if len(apiErr.Fields) > 0 {
		sentences := make([]string, 0, len(apiErr.Fields))
		for _, field := range slices.Sorted(maps.Keys(apiErr.Fields)) {
			sentences = append(sentences, apiErr.Fields[field])
		}
		message = strings.Join(sentences, " ")
	}
	return &requirement.BulkRefusal{Code: apiErr.Code, Message: message}
}

// workflowError maps the workflows' refusals; nil when err is none of theirs.
func workflowError(err error) *APIError {
	var (
		invalid  *workflow.InvalidError
		field    *workflow.FieldError
		inUse    *workflow.InUseError
		dropped  *workflow.ErrStatusInUse
		conflict *requirement.TransitionConflict
	)
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(map[string]string{invalid.Field: invalid.Message})
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.As(err, &inUse):
		return &APIError{Status: http.StatusConflict, Code: "in_use", Message: fmt.Sprintf(
			"This %s is still used %d times. Assign another in its place first.", inUse.What, inUse.Count)}
	case errors.As(err, &dropped):
		return &APIError{Status: http.StatusConflict, Code: "status_in_use", Message: fmt.Sprintf(
			"%d requirements under this workflow are in %s. Move them first, or assign another workflow with a mapping.", dropped.Count, dropped.Status)}
	case errors.As(err, &conflict):
		return &APIError{Status: http.StatusConflict, Code: "transition_unavailable", Message: fmt.Sprintf(
			"Somebody moved %s to %s meanwhile. Pick a transition from there.", conflict.Key, conflict.Status)}
	case errors.Is(err, workflow.ErrNotFound):
		return ErrNotFound("That status or workflow was not found. Reload the list; somebody may have deleted it.")
	case errors.Is(err, workflow.ErrForbidden):
		return ErrForbidden("Only the project's administrators change its statuses and workflows. Ask one of them.")
	}
	return nil
}

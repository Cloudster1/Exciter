package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/attribute/formula"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// Types and attributes (#14): what kinds of requirement a project tells apart, and what it records about them.

var errAttributesOff = &APIError{Status: http.StatusServiceUnavailable, Code: "attributes_unavailable",
	Message: "Types and attributes are not set up on this server. Ask its operator to configure it."}

// attributesOn refuses every type and attribute route of a server built without the services.
func (s *Server) attributesOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Attributes == nil || s.Projects == nil {
			respondError(w, r, errAttributesOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleListAttributeKinds(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"kinds": attribute.KindInfos()})
}

func (s *Server) handleListRequirementTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.Attributes.Types(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"types": types})
}

func (s *Server) handleCreateRequirementType(w http.ResponseWriter, r *http.Request) {
	var req attribute.TypeInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Attributes.CreateType(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"type": made})
}

func (s *Server) handleUpdateRequirementType(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "typeID", "type")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req attribute.TypeInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Attributes.UpdateType(r.Context(), userFrom(r), projectKeyParam(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"type": got})
}

func (s *Server) handleDeleteRequirementType(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "typeID", "type")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Attributes.DeleteType(r.Context(), userFrom(r), projectKeyParam(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListAttributes(w http.ResponseWriter, r *http.Request) {
	defs, err := s.Attributes.Attributes(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	progress, err := s.Attributes.Progress(r.Context(), userFrom(r), projectKeyParam(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"attributes": defs, "recompute": progress})
}

// handleFormulaCheck checks a computed attribute's formula as it is written (#213).
func (s *Server) handleFormulaCheck(w http.ResponseWriter, r *http.Request) {
	var req formula.CheckInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	check, err := s.Attributes.CheckFormula(r.Context(), userFrom(r), projectKeyParam(r), req)
	if errors.Is(err, attribute.ErrNoSuchRequirement) {
		respondError(w, r, ErrValidation(map[string]string{"requirementKey": "There is no such requirement in this project. Name one of its keys, such as " +
			requirement.NormalizedProjectKey(projectKeyParam(r)) + "-R1."}))
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"check": check})
}

func (s *Server) handleCreateAttribute(w http.ResponseWriter, r *http.Request) {
	var req attribute.DefInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Attributes.CreateAttribute(r.Context(), userFrom(r), projectKeyParam(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"attribute": made})
}

func (s *Server) handleUpdateAttribute(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "attributeID", "attribute")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req attribute.DefInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	got, lsn, err := s.Attributes.UpdateAttribute(r.Context(), userFrom(r), projectKeyParam(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"attribute": got})
}

func (s *Server) handleDeleteAttribute(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "attributeID", "attribute")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Attributes.DeleteAttribute(r.Context(), userFrom(r), projectKeyParam(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

// attributeError maps the types' and attributes' refusals; nil when err is none of theirs.
func attributeError(err error) *APIError {
	var (
		field     *attribute.FieldError
		inUse     *attribute.InUseError
		inFormula *attribute.InFormulaError
	)
	switch {
	case errors.As(err, &inFormula):
		return &APIError{Status: http.StatusConflict, Code: "attribute_in_formula", Message: fmt.Sprintf(
			"The formula of %s reads this attribute. Take it out of that formula first.", inFormula.Name)}
	case errors.As(err, &field):
		return ErrValidation(map[string]string{field.Field: field.Message})
	case errors.As(err, &inUse):
		return &APIError{Status: http.StatusConflict, Code: "in_use", Message: fmt.Sprintf(
			"This %s is used by %d requirements and revisions. Retire it instead, so their history keeps it.", inUse.What, inUse.Count)}
	case errors.Is(err, attribute.ErrNotFound):
		return ErrNotFound("That type or attribute was not found. Reload the list; somebody may have deleted it.")
	case errors.Is(err, attribute.ErrForbidden):
		return ErrForbidden("Only the project's administrators change its types and attributes. Ask one of them.")
	case errors.Is(err, attribute.ErrDefaultType):
		return &APIError{Status: http.StatusConflict, Code: "default_type",
			Message: "This is the project's default type. Make another type the default under the project's settings first."}
	}
	return nil
}

package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/version"
)

// Product is what /meta calls this tool, as Stator and Armature name themselves
// on theirs.
const Product = "exciter"

// APIVersion is info.version of the OpenAPI document. It rises only for a
// change that breaks clients.
const APIVersion = "1"

// metaResponse is the answer Exciter, Armature and Stator share, so a partner,
// an operator or Dynamo can tell which build of which tool answers.
type metaResponse struct {
	Product    string       `json:"product"`
	Build      version.Info `json:"build"`
	APIVersion string       `json:"apiVersion"`
	// Capabilities are the integration features this build offers, as name@version.
	Capabilities []string `json:"capabilities"`
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, metaResponse{
		Product:      Product,
		Build:        version.Current(),
		APIVersion:   APIVersion,
		Capabilities: capabilities(),
	})
}

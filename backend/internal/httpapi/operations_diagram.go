package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/diagram"
)

// Diagrams (#284), which docs/api-contract-backlog.md agrees.
func init() { operations = append(operations, diagramOperations...) }

var diagramOperations = []operation{
	{method: "POST", path: "/diagrams", handler: "handleDrawDiagram", orgWide: true, tag: "diagrams",
		summary: "Draw a diagram from its source as SVG, for anybody signed in: the picture a requirement's diagram shows. " +
			"422 is source the renderer could not read, in its words; 503 is a server that draws no diagrams, or is drawing too many.",
		request: diagram.Request{},
		responses: map[int]any{http.StatusOK: diagram.Picture{}, http.StatusUnprocessableEntity: errorEnvelope{},
			http.StatusServiceUnavailable: errorEnvelope{}, http.StatusGatewayTimeout: errorEnvelope{}}},
}

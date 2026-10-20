package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/diagram"
	"github.com/cloudster1/exciter/backend/internal/document"
)

func (s *Server) handleDrawDiagram(w http.ResponseWriter, r *http.Request) {
	var in diagram.Request
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	drawer := s.Diagrams
	if drawer == nil {
		drawer = diagram.Unavailable{}
	}
	picture, err := drawer.Draw(r.Context(), in.Language, diagram.SVG, in.Source)
	if err != nil {
		respondError(w, r, diagramError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, diagram.Picture{SVG: string(picture)})
}

// diagramPNG draws a diagram for a Word export, which embeds PNG.
func (s *Server) diagramPNG(ctx context.Context, language, source string) ([]byte, error) {
	if s.Diagrams == nil {
		return nil, diagram.ErrUnavailable
	}
	return s.Diagrams.Draw(ctx, language, diagram.PNG, source)
}

// diagramError says what a drawing that failed means to the writer, and what to do about it.
func diagramError(err error) error {
	var source *diagram.SourceError
	switch {
	case errors.As(err, &source):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "diagram_source",
			Message: "The diagram could not be drawn: " + source.Message + ". Check its source."}
	case errors.Is(err, diagram.ErrInvalid):
		return ErrValidation(map[string]string{"source": fmt.Sprintf("Write the diagram in PlantUML, in at most %d characters.", document.MaxDiagramLength)})
	case errors.Is(err, diagram.ErrUnavailable):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "diagrams_unavailable",
			Message: "Diagrams are not drawn on this server, so they show as their source. Ask whoever runs Exciter to start the diagram renderer."}
	case errors.Is(err, diagram.ErrBusy):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "diagrams_busy",
			Message: "Too many diagrams are being drawn right now. Try again in a moment."}
	case errors.Is(err, diagram.ErrTimeout):
		return &APIError{Status: http.StatusGatewayTimeout, Code: "diagram_timeout",
			Message: "The diagram took too long to draw. Make it smaller, or split it in two."}
	case errors.Is(err, diagram.ErrTooLarge):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "diagram_too_large",
			Message: "The drawn diagram is larger than this server hands out. Make it smaller, or split it in two."}
	case errors.Is(err, diagram.ErrFailed):
		return &APIError{Status: http.StatusBadGateway, Code: "diagram_failed",
			Message: "The diagram could not be drawn. Try again; if it fails again, ask whoever runs Exciter to look at the diagram renderer."}
	}
	return err
}

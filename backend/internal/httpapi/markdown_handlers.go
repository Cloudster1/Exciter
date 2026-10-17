package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/openapi"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// formatQuery asks a read for its documents as Markdown too (#186).
var formatQuery = param{name: "format", schema: &openapi.Schema{Type: "string", Enum: enumStrings(requirement.Formats)},
	description: "markdown answers each document as Markdown as well, left out where Markdown cannot hold it; document, the default, does not."}

// markdownAsked says whether a read was asked for Markdown.
func markdownAsked(r *http.Request) (bool, *APIError) {
	switch requirement.Format(r.URL.Query().Get("format")) {
	case "", requirement.FormatDocument:
		return false, nil
	case requirement.FormatMarkdown:
		return true, nil
	}
	return false, ErrValidation(map[string]string{"format": "Ask for format=markdown, or leave format out for the document alone."})
}

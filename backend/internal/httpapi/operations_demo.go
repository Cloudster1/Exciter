package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/demo"
)

// The suite demo (#270), which docs/api-contract-backlog.md agrees.
func init() { operations = append(operations, demoOperations...) }

var demoOperations = []operation{
	{method: "GET", path: "/installation/suite-demo", handler: "handleGetSuiteDemo", orgWide: true, tag: "installation",
		summary:   "The suite demo: how far it got, what it made and why links wait; null while nobody added it. For administrators.",
		responses: ok(env{"demo": &demo.Demo{}})},
	{method: "POST", path: "/installation/suite-demo", handler: "handleAddSuiteDemo", orgWide: true, tag: "installation",
		summary: "Add the suite demo once: the requirements of Armature, Exciter and Stator from their own issues, built by the worker as the caller and linked to their issues and pages. For administrators.",
		responses: map[int]any{http.StatusAccepted: env{"demo": demo.Demo{}}, http.StatusConflict: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/installation/suite-demo/links", handler: "handleLinkSuiteDemo", orgWide: true, tag: "installation",
		summary: "Link the suite demo again, adding the links to Armature and Stator it is missing, as the caller. For administrators.",
		responses: map[int]any{http.StatusAccepted: env{"demo": demo.Demo{}}, http.StatusNotFound: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}}},
}

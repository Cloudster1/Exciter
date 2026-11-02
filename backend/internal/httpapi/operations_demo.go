package httpapi

import (
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/demo"
)

// The suite demo (#270, #488), which docs/api-contract-backlog.md agrees.
func init() { operations = append(operations, demoOperations...) }

var demoOperations = []operation{
	{method: "GET", path: "/installation/suite-demo", handler: "handleGetSuiteDemo", orgWide: true, tag: "installation",
		summary:   "The suite demo: how far it got, what it made and why links wait; null while nobody added it. For administrators.",
		responses: ok(env{"demo": &demo.Demo{}})},
	{method: "POST", path: "/installation/suite-demo", handler: "handleAddSuiteDemo", orgWide: true, tag: "installation",
		summary: "Add the suite demo once: a written specification of Armature, Exciter, Stator and Rotor each, with test cases and a test run, built by the worker as the caller and linked to their issues and guide pages. For administrators.",
		responses: map[int]any{http.StatusAccepted: env{"demo": demo.Demo{}}, http.StatusConflict: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/installation/suite-demo/links", handler: "handleLinkSuiteDemo", orgWide: true, tag: "installation",
		summary: "Link the suite demo again, adding the links to Armature and Stator it is missing and finishing a test run that stopped, as the caller. For administrators.",
		responses: map[int]any{http.StatusAccepted: env{"demo": demo.Demo{}}, http.StatusNotFound: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}}},
}

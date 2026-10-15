package httpapi

import "slices"

// capability is one integration feature a partner checks for by name at /meta, with the operations it promises;
// docs/integration.md lists them.
type capability struct {
	name, promise string
	// operations are "METHOD /path" as the operations table spells them.
	operations []string
}

// integrationCapabilities are named as Armature names its own, until Dynamo's shared list (Dynamo#4) settles them.
var integrationCapabilities = []capability{
	{
		name:    "requirement-embed@1",
		promise: "A requirement read by its key, and requirements found by their words or the first letters of their titles, with a session or a token.",
		operations: []string{
			"GET /requirements/{reqKey}",
			"GET /search",
			"GET /search/quick",
		},
	},
	{
		name:    "trace-links@1",
		promise: "The trace links between requirements: listed from either end, made with a link type, and taken off.",
		operations: []string{
			"GET /link-types",
			"GET /requirements/{reqKey}/links",
			"POST /requirements/{reqKey}/links",
			"DELETE /links/{linkID}",
		},
	},
	{
		name:    "specifications@1",
		promise: "The projects a caller may read, and each one's requirements as the outline of its specification.",
		operations: []string{
			"GET /projects",
			"GET /projects/{projectKey}",
			"GET /projects/{projectKey}/tree",
		},
	},
	{
		name:    "theme-export@1",
		promise: "A theme exported as one armature-theme/1 file with its pictures and fonts, and such a file imported as a theme of the caller's own.",
		operations: []string{
			"GET /themes/{themeID}/export",
			"POST /themes/import",
		},
	},
}

// capabilities is what this build announces beyond its API version, sorted; a client checks for a name here
// rather than comparing versions.
func capabilities() []string {
	names := make([]string, 0, len(integrationCapabilities))
	for _, c := range integrationCapabilities {
		names = append(names, c.name)
	}
	slices.Sort(names)
	return names
}

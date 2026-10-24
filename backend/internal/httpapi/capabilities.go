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
		name:    "requirement-list@1",
		promise: "A project's requirements narrowed and ordered by a query in the query language, a window at a time, and what may come next at the caret of such a query.",
		operations: []string{
			"GET /projects/{projectKey}/requirements",
			"GET /projects/{projectKey}/requirements/suggest",
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
		// Stator's name and operations for the same feature, which Exciter's webhooks were adapted from (#48).
		name:    "webhooks@1",
		promise: "Endpoints subscribed to topics, their deliveries, a redelivery, a test delivery and a new secret, each delivery a signed envelope.",
		operations: []string{
			"GET /webhooks",
			"POST /webhooks",
			"PATCH /webhooks/{webhookID}",
			"DELETE /webhooks/{webhookID}",
			"GET /webhooks/{webhookID}/deliveries",
			"POST /webhooks/{webhookID}/deliveries/{deliveryID}/redeliver",
			"POST /webhooks/{webhookID}/test",
			"POST /webhooks/{webhookID}/rotate-secret",
		},
	},
	{
		// The same operations, an endpoint free to choose the CloudEvents envelope (#133); webhooks@1 stays beside it.
		name:    "webhooks@2",
		promise: "What webhooks@1 promises, and an endpoint registered for envelope version 2 is posted a CloudEvent 1.0 in structured mode, its data version 1's payload.",
		operations: []string{
			"GET /webhooks",
			"POST /webhooks",
			"PATCH /webhooks/{webhookID}",
			"DELETE /webhooks/{webhookID}",
			"GET /webhooks/{webhookID}/deliveries",
			"POST /webhooks/{webhookID}/deliveries/{deliveryID}/redeliver",
			"POST /webhooks/{webhookID}/test",
			"POST /webhooks/{webhookID}/rotate-secret",
		},
	},
	{
		// The tools, resources and prompts are discovered from their lists, so only the endpoint is promised (#183).
		name:    "mcp@1",
		promise: "An assistant works with requirements over MCP 2025-11-25 or 2025-06-18 as the person whose token it holds, with the tools, resources and prompts its lists name.",
		operations: []string{
			"POST /mcp",
		},
	},
	{
		// Armature's name and promise for the same grant (Cloudster1/Armature#302), so a partner checks all three alike.
		name: "oauth.token-exchange@1",
		promise: "POST /oauth/token with grant_type urn:ietf:params:oauth:grant-type:token-exchange: subject_token an access token of the installation's identity provider " +
			"whose azp is the partner app's keycloakClientId and whose aud holds Exciter's client, the app authenticating with its own id and secret; " +
			"the answer an access and a refresh token within the person's rights, with no consent page. " +
			"Or subject_token an access token of the calling client's own grant: the answer an access token alone, no wider than the grant in scope " +
			"and projects (exciter_projects), ending at most when the subject does (exciter_expires_in), the grant untouched.",
		operations: []string{
			"POST /oauth/token",
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

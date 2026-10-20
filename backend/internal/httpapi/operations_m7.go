package httpapi

import (
	"maps"
	"net/http"

	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/oauth"
)

// The operations the MCP integration adds (#181), kept beside the table they join; docs/api-contract-m7.md agrees
// them. The tools it adds are rows of the table already and are marked by their issues.
func init() { operations = append(operations, assistantOperations...) }

// oauthRefusals are the answers of the protocol's endpoints, which refuse in the RFCs' shape rather than Exciter's.
var oauthRefusals = map[int]any{http.StatusBadRequest: oauth.Error{}, http.StatusUnauthorized: oauth.Error{}}

var assistantOperations = []operation{
	// Discovery (#188): where a client that met a 401 at /mcp learns how to be let in. Outside the API, where the
	// RFCs put them.
	{method: "GET", path: "/.well-known/oauth-protected-resource", handler: "handleProtectedResource", id: "getProtectedResource",
		tag: "oauth", public: true, root: true,
		summary:   "The MCP endpoint as a resource that OAuth protects, and its authorization server (RFC 9728); 404 while OAuth connections are off.",
		responses: map[int]any{http.StatusOK: oauth.ProtectedResource{}, http.StatusNotFound: errorEnvelope{}}},
	{method: "GET", path: "/.well-known/oauth-protected-resource/api/v1/mcp", handler: "handleProtectedResource", id: "getMCPProtectedResource",
		tag: "oauth", public: true, root: true,
		summary:   "The same, at the address RFC 9728 derives from the MCP endpoint's.",
		responses: map[int]any{http.StatusOK: oauth.ProtectedResource{}, http.StatusNotFound: errorEnvelope{}}},
	{method: "GET", path: "/.well-known/oauth-authorization-server", handler: "handleAuthorizationServer",
		tag: "oauth", public: true, root: true,
		summary:   "Exciter as an authorization server: its endpoints, scopes, grants and S256 (RFC 8414); 404 while OAuth connections are off.",
		responses: map[int]any{http.StatusOK: oauth.ServerMetadata{}, http.StatusNotFound: errorEnvelope{}}},

	// The protocol (#188).
	{method: "POST", path: "/oauth/register", handler: "handleOAuthRegister", tag: "oauth", public: true,
		summary: "A client registers itself (RFC 7591): https or loopback redirects, limited in rate by address; a registration never used expires.",
		request: oauth.ClientMetadataInput{}, responses: map[int]any{http.StatusCreated: oauth.RegisteredClient{},
			http.StatusBadRequest: oauth.Error{}, http.StatusTooManyRequests: oauth.Error{}}},
	{method: "GET", path: "/oauth/authorize", handler: "handleOAuthAuthorize", tag: "oauth", public: true, redirect: true,
		summary: "Begin connecting a client: the browser goes on to the consent page, or to the page that says why the client or its redirect is refused.",
		query: []param{
			{name: "response_type", description: "code, the one response type."},
			{name: "client_id", description: "An id from registering, or the https address of the client's metadata document."},
			{name: "redirect_uri", description: "One of the client's redirects, exactly."},
			{name: "code_challenge", description: "The PKCE challenge."},
			{name: "code_challenge_method", description: "S256, the one method."},
			{name: "state", description: "Handed back with the code."},
			{name: "resource", description: "The MCP endpoint's address (RFC 8707)."},
			{name: "scope", description: "mcp, or mcp:read to only read; mcp when absent."},
		},
		responses: map[int]any{}},
	{method: "GET", path: "/oauth/requests/{requestID}", handler: "handleGetOAuthRequest", tag: "oauth", orgWide: true,
		summary:   "A connection waiting for the caller's consent, as the consent page shows it. Needs a session.",
		responses: map[int]any{http.StatusOK: env{"request": oauth.AuthorizationRequest{}}, http.StatusNotFound: errorEnvelope{}}},
	{method: "POST", path: "/oauth/requests/{requestID}", handler: "handleDecideOAuthRequest", tag: "oauth", orgWide: true,
		summary: "Allow or deny a connection, narrowed to reading or to projects; answers where the browser goes back to. Needs a session.",
		request: oauth.ConsentInput{}, responses: map[int]any{http.StatusOK: env{"redirectTo": ""}, http.StatusNotFound: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/oauth/token", handler: "handleOAuthToken", tag: "oauth", public: true, form: true,
		summary: "Trade a code and its verifier, a refresh token, or for a partner app a person's access token from the identity provider (RFC 8693), for an hour's access token and the next refresh token.",
		request: oauth.TokenRequest{}, responses: merge(ok(oauth.TokenResponse{}), oauthRefusals)},
	{method: "POST", path: "/oauth/revoke", handler: "handleOAuthRevoke", tag: "oauth", public: true, form: true,
		summary: "End an access or refresh token (RFC 7009); a token unknown or already ended is 200 as well.",
		request: oauth.RevokeRequest{}, responses: merge(ok(nil), oauthRefusals)},

	// Connected apps (#189).
	{method: "GET", path: "/oauth/grants", handler: "handleListOAuthGrants", tag: "oauth", orgWide: true,
		summary:   "The clients the caller connected, what each may do and when it was last used.",
		responses: ok(env{"grants": []oauth.Grant{}})},
	{method: "DELETE", path: "/oauth/grants/{grantID}", handler: "handleRevokeOAuthGrant", tag: "oauth", orgWide: true,
		summary:   "End one of the caller's connections and every token it holds, at once. Needs a session.",
		responses: none()},
	{method: "GET", path: "/oauth/grants/everyone", handler: "handleListEveryonesOAuthGrants", tag: "oauth", orgWide: true,
		summary:   "Everybody's connected clients, with whose each is. For administrators.",
		responses: ok(env{"grants": []oauth.OrgGrant{}})},
	{method: "DELETE", path: "/oauth/grants/everyone/{grantID}", handler: "handleRevokeAnybodysOAuthGrant", tag: "oauth", orgWide: true,
		summary:   "End anybody's connection. For administrators, with a session.",
		responses: none()},

	// Partner apps (#273): the tools beside Exciter that exchange a person's token from the shared identity provider.
	{method: "GET", path: "/oauth/clients", handler: "handleListPartnerApps", tag: "oauth", orgWide: true,
		summary:   "The partner apps, by name, with how many people each holds a grant of. For administrators.",
		responses: ok(env{"clients": []oauth.PartnerApp{}})},
	{method: "POST", path: "/oauth/clients", handler: "handleCreatePartnerApp", tag: "oauth", orgWide: true,
		summary: "Register a partner app with the Keycloak client it signs people in with; its secret is in this answer and never again. For administrators.",
		request: oauth.PartnerAppInput{}, responses: map[int]any{http.StatusCreated: env{"client": oauth.PartnerApp{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PATCH", path: "/oauth/clients/{clientID}", handler: "handleUpdatePartnerApp", tag: "oauth", orgWide: true,
		summary: "Change a partner app; what is left out keeps its value, and taking write away narrows every grant of it at once. For administrators.",
		request: oauth.PartnerAppInput{}, responses: map[int]any{http.StatusOK: env{"client": oauth.PartnerApp{}}, http.StatusNotFound: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/oauth/clients/{clientID}/rotate-secret", handler: "handleRotatePartnerSecret", tag: "oauth", orgWide: true,
		summary:   "Issue a partner app a new secret, in this answer and never again; the old one stops at once. For administrators.",
		responses: map[int]any{http.StatusOK: env{"client": oauth.PartnerApp{}}, http.StatusNotFound: errorEnvelope{}}},
	{method: "DELETE", path: "/oauth/clients/{clientID}", handler: "handleDeletePartnerApp", tag: "oauth", orgWide: true,
		summary:   "Remove a partner app, and with it every grant it holds. For administrators.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: errorEnvelope{}}},
	{method: "PUT", path: "/installation/oauth-connections", handler: "handleSwitchOAuthConnections", orgWide: true, tag: "installation",
		summary: "Let assistants connect with OAuth, or stop them; while off, every grant's tokens are refused and kept. For administrators.",
		request: oauthConnectionsRequest{}, responses: ok(env{"installation": installation.Installation{}})},

	// The specification as Markdown (#187), which the MCP resources read.
	{method: "GET", path: "/projects/{projectKey}/document.md", handler: "handleProjectMarkdown", tag: "documents",
		summary: "The same document as Markdown: each entry a heading with its outline, key and title, its text, its fields and its links.",
		query:   []param{documentSection}, binary: true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}}},
	{method: "GET", path: "/baselines/{baselineID}/document.md", handler: "handleBaselineMarkdown", tag: "documents",
		summary: "A baseline's document as Markdown, as it was taken.",
		binary:  true, responses: map[int]any{http.StatusOK: nil, http.StatusNotFound: errorEnvelope{}}},
}

// oauthConnectionsRequest switches OAuth connections on or off for the installation.
type oauthConnectionsRequest struct {
	Enabled bool `json:"enabled"`
}

// merge joins two maps of answers; the second wins a status both name.
func merge(a, b map[int]any) map[int]any {
	out := maps.Clone(a)
	maps.Copy(out, b)
	return out
}

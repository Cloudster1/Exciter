// Package oauth makes Exciter the authorization server of its own MCP endpoint (#188), as
// docs/api-contract-m7.md agrees: a person consents, and their assistant gets a token for MCP alone.
package oauth

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

const (
	// AccessTokenPrefix and RefreshTokenPrefix tell the two apart from each other and from a personal token's.
	AccessTokenPrefix  = "exciter_oat_"
	RefreshTokenPrefix = "exciter_ort_"
	// AccessTTL is short because an access token is sent on every call; the refresh token renews it.
	AccessTTL = time.Hour
	// RefreshTTL ends a grant nobody used for that long.
	RefreshTTL = 30 * 24 * time.Hour
	// CodeTTL and RequestTTL bound the redirect and the consent page: long enough to sign in, no longer.
	CodeTTL    = time.Minute
	RequestTTL = 10 * time.Minute
	// UnusedClientTTL removes a registration that never led to a grant.
	UnusedClientTTL = 24 * time.Hour
	// MaxRegistrationsPerHour is per client address, as registering needs nobody signed in.
	MaxRegistrationsPerHour = 10
	// MaxClientMetadataBytes bounds a client ID metadata document fetched from its address.
	MaxClientMetadataBytes = 5 << 10
	MaxRedirectURIs        = 10
	MaxClientNameLength    = 100
)

// The scopes a client may ask for; the person may narrow the full one to reading at the consent page.
const (
	ScopeMCP     = "mcp"
	ScopeMCPRead = "mcp:read"
)

// Scopes lists every scope, for the metadata documents.
var Scopes = []string{ScopeMCP, ScopeMCPRead}

// ClientKind is how Exciter came to know a client.
type ClientKind string

const (
	// ClientRegistered registered itself at POST /oauth/register (RFC 7591).
	ClientRegistered ClientKind = "registered"
	// ClientMetadata is known by the address of its metadata document, which is its client_id.
	ClientMetadata ClientKind = "metadata"
)

// ClientKinds lists every ClientKind, for the API document.
var ClientKinds = []ClientKind{ClientRegistered, ClientMetadata}

// ProtectedResource is the MCP endpoint described for a client that met a 401 there (RFC 9728).
type ProtectedResource struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceName           string   `json:"resource_name"`
}

// ServerMetadata is Exciter described as an authorization server (RFC 8414).
type ServerMetadata struct {
	Issuer                                 string   `json:"issuer"`
	AuthorizationEndpoint                  string   `json:"authorization_endpoint"`
	TokenEndpoint                          string   `json:"token_endpoint"`
	RegistrationEndpoint                   string   `json:"registration_endpoint"`
	RevocationEndpoint                     string   `json:"revocation_endpoint"`
	ScopesSupported                        []string `json:"scopes_supported"`
	ResponseTypesSupported                 []string `json:"response_types_supported"`
	GrantTypesSupported                    []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
	ClientIDMetadataDocumentSupported      bool     `json:"client_id_metadata_document_supported"`
	// AuthorizationResponseIssParameterSupported says the redirect names the issuer (RFC 9207).
	AuthorizationResponseIssParameterSupported bool `json:"authorization_response_iss_parameter_supported"`
}

// ClientMetadataInput is what a client registers itself with (RFC 7591), and what its metadata document holds.
type ClientMetadataInput struct {
	RedirectURIs []string `json:"redirect_uris"`
	ClientName   string   `json:"client_name,omitempty"`
	ClientURI    string   `json:"client_uri,omitempty"`
	// TokenEndpointAuthMethod is none for a client that keeps no secret, the default.
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	SoftwareID              string   `json:"software_id,omitempty"`
	SoftwareVersion         string   `json:"software_version,omitempty"`
}

// RegisteredClient is the registration's answer: what was registered, with the identity it was given.
type RegisteredClient struct {
	ClientMetadataInput
	ClientID string `json:"client_id"`
	// ClientSecret is answered once, to a client that asked for a secret.
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at"`
}

// TokenRequest is the form a client posts to the token endpoint, for either grant.
type TokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
	CodeVerifier string `json:"code_verifier,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Resource     string `json:"resource,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// TokenResponse hands a client its tokens (RFC 6749).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// RevokeRequest is the form a client ends a token with (RFC 7009).
type RevokeRequest struct {
	Token         string `json:"token"`
	TokenTypeHint string `json:"token_type_hint,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
}

// Error is how the protocol's endpoints refuse, in the shape the RFCs give clients, with a sentence a person reads.
type Error struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ClientRef is a client as a person sees it at the consent page and in the list of connected apps.
type ClientRef struct {
	// ID is the client_id: an id Exciter gave, or the address of the client's metadata document.
	ID   string     `json:"id"`
	Name string     `json:"name"`
	URI  string     `json:"uri"`
	Kind ClientKind `json:"kind"`
}

// AuthorizationRequest is an authorization waiting for its person's consent.
type AuthorizationRequest struct {
	ID     uuid.UUID `json:"id"`
	Client ClientRef `json:"client"`
	// RedirectHost is where the browser goes back to, which the person should recognise.
	RedirectHost string `json:"redirectHost"`
	// ReadOnly says the client asked only to read.
	ReadOnly  bool      `json:"readOnly"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ConsentInput is the person's answer; a client that asked for everything may be granted only reading.
type ConsentInput struct {
	Allow    bool `json:"allow"`
	ReadOnly bool `json:"readOnly"`
	// Projects limits the grant as a personal token's projects limit it; none is every project.
	Projects []string `json:"projects,omitempty"`
}

// Grant is a client a person connected, as their list of connected apps shows it.
type Grant struct {
	ID           uuid.UUID `json:"id"`
	Client       ClientRef `json:"client"`
	RedirectHost string    `json:"redirectHost"`
	// Scopes are a personal token's: read, or none for every right the person has.
	Scopes      []string   `json:"scopes"`
	Projects    []string   `json:"projects"`
	AllProjects bool       `json:"allProjects"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
}

// OrgGrant is a grant in the installation's list, with whose it is.
type OrgGrant struct {
	Grant
	Owner auth.User `json:"owner"`
}

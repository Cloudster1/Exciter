package oauth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// ErrSwitchedOff refuses everything OAuth while the installation's administrators have switched connections off.
var ErrSwitchedOff = errors.New("OAuth connections are switched off on this server")

// MCPPath is the endpoint the tokens are for, under the API's prefix.
const MCPPath = "/mcp"

// Service is the authorization server: it knows clients, keeps what people consented to and hands out tokens.
type Service struct {
	db *db.Cluster
	// issuer is EXCITER_APP_URL, the address everything is named from; api is it with the API's prefix.
	issuer, api string
	fetch       Fetcher
	now         func() time.Time
}

// NewService names the server from the web client's address and the API's prefix under it.
func NewService(cluster *db.Cluster, appURL, apiPrefix string, fetch Fetcher) *Service {
	issuer := strings.TrimRight(appURL, "/")
	return &Service{db: cluster, issuer: issuer, api: issuer + apiPrefix, fetch: fetch, now: time.Now}
}

// Issuer is the server's own name, which a client compares and the redirect carries (RFC 9207).
func (s *Service) Issuer() string { return s.issuer }

// Resource is the MCP endpoint's address, the one resource a token is for (RFC 8707).
func (s *Service) Resource() string { return s.api + MCPPath }

// ResourceMetadata is where a client that met a 401 at the endpoint reads how to be let in (RFC 9728).
func (s *Service) ResourceMetadata() string {
	return s.issuer + "/.well-known/oauth-protected-resource" + strings.TrimPrefix(s.Resource(), s.issuer)
}

// ConsentPage is the web page a person decides at, which also says why a request was refused.
func (s *Service) ConsentPage() string { return s.issuer + "/oauth/consent" }

// Enabled says whether OAuth connections are on for the installation.
func (s *Service) Enabled(ctx context.Context) (bool, error) {
	var on bool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT oauth_connections FROM installation`).Scan(&on)
	})
	return on, err
}

// ProtectedResource describes the MCP endpoint for its clients.
func (s *Service) ProtectedResource() ProtectedResource {
	return ProtectedResource{Resource: s.Resource(), AuthorizationServers: []string{s.issuer}, ScopesSupported: Scopes,
		BearerMethodsSupported: []string{"header"}, ResourceName: "Exciter"}
}

// Metadata describes the server (RFC 8414).
func (s *Service) Metadata() ServerMetadata {
	return ServerMetadata{
		Issuer: s.issuer, AuthorizationEndpoint: s.api + "/oauth/authorize", TokenEndpoint: s.api + "/oauth/token",
		RegistrationEndpoint: s.api + "/oauth/register", RevocationEndpoint: s.api + "/oauth/revoke",
		ScopesSupported: Scopes, ResponseTypesSupported: []string{ResponseCode}, GrantTypesSupported: GrantTypes,
		CodeChallengeMethodsSupported: []string{ChallengeS256}, TokenEndpointAuthMethodsSupported: AuthMethods,
		RevocationEndpointAuthMethodsSupported: AuthMethods, ClientIDMetadataDocumentSupported: true,
		AuthorizationResponseIssParameterSupported: true,
	}
}

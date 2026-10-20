package httpapi

import (
	"reflect"

	"github.com/cloudster1/exciter/backend/internal/oauth"
	"github.com/cloudster1/exciter/backend/internal/openapi"
)

// assistantSchemas names the types of the MCP integration (#182), whose short names would meet others', and spells
// their enums.
func assistantSchemas(b *openapi.Builder) {
	names := map[reflect.Type]string{
		reflect.TypeFor[oauth.ProtectedResource]():    "OAuthProtectedResource",
		reflect.TypeFor[oauth.ServerMetadata]():       "OAuthServerMetadata",
		reflect.TypeFor[oauth.ClientMetadataInput]():  "OAuthClientMetadata",
		reflect.TypeFor[oauth.RegisteredClient]():     "OAuthRegisteredClient",
		reflect.TypeFor[oauth.TokenRequest]():         "OAuthTokenRequest",
		reflect.TypeFor[oauth.TokenResponse]():        "OAuthTokenResponse",
		reflect.TypeFor[oauth.RevokeRequest]():        "OAuthRevokeRequest",
		reflect.TypeFor[oauth.Error]():                "OAuthError",
		reflect.TypeFor[oauth.ClientRef]():            "OAuthClient",
		reflect.TypeFor[oauth.AuthorizationRequest](): "OAuthAuthorizationRequest",
		reflect.TypeFor[oauth.ConsentInput]():         "OAuthConsent",
		reflect.TypeFor[oauth.Grant]():                "OAuthGrant",
		reflect.TypeFor[oauth.OrgGrant]():             "OrgOAuthGrant",
		reflect.TypeFor[oauthConnectionsRequest]():    "OAuthConnectionsRequest",
		reflect.TypeFor[oauth.PartnerApp]():           "OAuthPartnerApp",
		reflect.TypeFor[oauth.PartnerAppInput]():      "OAuthPartnerAppInput",
	}
	for t, name := range names {
		b.Names[t] = name
	}
	b.Enums[reflect.TypeOf(oauth.ClientKind(""))] = enumStrings(oauth.ClientKinds)
}

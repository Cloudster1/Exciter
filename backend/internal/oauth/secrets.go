package oauth

import (
	"strings"

	"github.com/cloudster1/exciter/backend/internal/auth"
)

// secret is a new random value with a prefix, and the digest it is stored as; only the digest outlives the answer.
func secret(prefix string) (value string, digest []byte, err error) {
	s, _, err := auth.GenerateToken()
	if err != nil {
		return "", nil, err
	}
	value = prefix + s
	return value, auth.HashToken(value), nil
}

// The secrets that carry no prefix of their own: a code goes through the browser, a client secret to the client.
const (
	codePrefix         = "exciter_oac_"
	clientSecretPrefix = "exciter_ocs_"
)

// IsAccessToken says whether a credential claims to be an OAuth access token, which is good for MCP alone.
func IsAccessToken(credential string) bool {
	return strings.HasPrefix(credential, AccessTokenPrefix)
}

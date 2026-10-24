package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// The client authentication methods a client registers with; the default keeps no secret.
const (
	AuthNone        = "none"
	AuthSecretBasic = "client_secret_basic"
	AuthSecretPost  = "client_secret_post"
)

// AuthMethods lists every method, for the server's metadata.
var AuthMethods = []string{AuthNone, AuthSecretBasic, AuthSecretPost}

// The grants offered; a client registers with either or both.
const (
	GrantAuthorizationCode = "authorization_code"
	GrantRefreshToken      = "refresh_token"
)

// GrantTypes lists every grant, for the server's metadata.
var GrantTypes = []string{GrantAuthorizationCode, GrantRefreshToken}

// ResponseCode is the one response type, and ChallengeS256 the one PKCE method.
const (
	ResponseCode  = "code"
	ChallengeS256 = "S256"
)

// The errors the RFCs name, as a client reads them in error.
const (
	ErrInvalidRequest          = "invalid_request"
	ErrInvalidClient           = "invalid_client"
	ErrInvalidGrant            = "invalid_grant"
	ErrUnauthorizedClient      = "unauthorized_client"
	ErrUnsupportedGrantType    = "unsupported_grant_type"
	ErrUnsupportedResponseType = "unsupported_response_type"
	ErrInvalidScope            = "invalid_scope"
	ErrInvalidTarget           = "invalid_target"
	ErrAccessDenied            = "access_denied"
	ErrInvalidRedirectURI      = "invalid_redirect_uri"
	ErrInvalidClientMetadata   = "invalid_client_metadata"
	ErrTemporarilyUnavailable  = "temporarily_unavailable"
)

// Refusal is an Error to answer, and an error in Go, so a service can return it; Status is its HTTP status.
type Refusal struct {
	Body   Error
	Status int
}

func (r *Refusal) Error() string { return r.Body.ErrorDescription }

func refuse(code, description string) *Refusal {
	status := http.StatusBadRequest
	if code == ErrInvalidClient {
		status = http.StatusUnauthorized
	}
	return &Refusal{Body: Error{Error: code, ErrorDescription: description}, Status: status}
}

// verifierShape is RFC 7636's code_verifier: 43 to 128 unreserved characters.
var verifierShape = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// challengeShape is an S256 challenge: the verifier's SHA-256, base64url without padding.
var challengeShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// VerifyPKCE says whether a verifier is the one an S256 challenge was made from (RFC 7636).
func VerifyPKCE(challenge, verifier string) bool {
	if !verifierShape.MatchString(verifier) || !challengeShape.MatchString(challenge) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// loopback is how a client on the person's own machine is reached, at whichever port it listens on (RFC 8252).
func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validRedirect is a redirect a client may register: https, or http on a loopback address, without a fragment.
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Fragment != "" || u.User != nil || u.Host == "" || u.Opaque != "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && loopback(u.Hostname()))
}

// RedirectMatches says whether a redirect is one the client registered: the same address exactly, but for the port
// of a loopback one, which a native client picks anew each time it runs.
func RedirectMatches(registered []string, asked string) bool {
	if slices.Contains(registered, asked) {
		return true
	}
	a, err := url.Parse(asked)
	if err != nil || a.Scheme != "http" || !loopback(a.Hostname()) {
		return false
	}
	for _, each := range registered {
		r, err := url.Parse(each)
		if err == nil && r.Scheme == a.Scheme && r.Hostname() == a.Hostname() && r.Path == a.Path && r.RawQuery == a.RawQuery {
			return true
		}
	}
	return false
}

// RedirectHost is the host a person sees they are sent back to.
func RedirectHost(redirect string) string {
	if u, err := url.Parse(redirect); err == nil {
		return u.Host
	}
	return redirect
}

// checkMetadata holds a registration, or a metadata document, to the rules of the contract, and fills its defaults.
func checkMetadata(in *ClientMetadataInput) *Refusal {
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > MaxRedirectURIs {
		return refuse(ErrInvalidRedirectURI, "Register between 1 and 10 redirect addresses.")
	}
	for _, r := range in.RedirectURIs {
		if !validRedirect(r) {
			return refuse(ErrInvalidRedirectURI, "The redirect "+r+" is neither https nor http on a loopback address, or it has a fragment. "+
				"Register https addresses, or http://127.0.0.1 or http://localhost at any port.")
		}
	}
	if in.TokenEndpointAuthMethod == "" {
		in.TokenEndpointAuthMethod = AuthNone
	}
	if !slices.Contains(AuthMethods, in.TokenEndpointAuthMethod) {
		return refuse(ErrInvalidClientMetadata, "The token endpoint takes none, client_secret_basic or client_secret_post.")
	}
	if len(in.GrantTypes) == 0 {
		in.GrantTypes = []string{GrantAuthorizationCode, GrantRefreshToken}
	}
	for _, g := range in.GrantTypes {
		// The exchange narrows a token of the client's own grant (#235), so a client may register for it.
		if !slices.Contains(GrantTypes, g) && g != GrantTokenExchange {
			return refuse(ErrInvalidClientMetadata, "The grants offered are authorization_code, refresh_token and "+GrantTokenExchange+
				"; "+g+" is not one of them.")
		}
	}
	if !slices.Contains(in.GrantTypes, GrantAuthorizationCode) {
		return refuse(ErrInvalidClientMetadata, "A client is connected through authorization_code; register it among its grants.")
	}
	for _, t := range in.ResponseTypes {
		if t != ResponseCode {
			return refuse(ErrInvalidClientMetadata, "The one response type is code.")
		}
	}
	in.ClientName = strings.TrimSpace(in.ClientName)
	if utf8.RuneCountInString(in.ClientName) > MaxClientNameLength {
		return refuse(ErrInvalidClientMetadata, "A client's name is at most 100 characters.")
	}
	if in.ClientName == "" {
		in.ClientName = RedirectHost(in.RedirectURIs[0])
	}
	if in.ClientURI != "" {
		if u, err := url.Parse(in.ClientURI); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return refuse(ErrInvalidClientMetadata, "The client's address is a web address; leave it out otherwise.")
		}
	}
	return nil
}

// IsMetadataClientID says whether a client_id names a metadata document: an https address with a path.
func IsMetadataClientID(id string) bool {
	u, err := url.Parse(id)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Path != "" && u.Path != "/" && u.Fragment == "" && u.User == nil
}

// scope is what a grant or a token may do: everything its person may, reading, or evidence alone; a partner's is
// named in its own words.
type scope struct {
	readOnly, evidence, partner bool
}

// scopeAsked reads a requested scope: mcp or nothing is everything, mcp:read is reading, evidence alone is evidence;
// ok is false otherwise.
func scopeAsked(asked string) (sc scope, ok bool) {
	fields := strings.Fields(asked)
	if len(fields) == 0 {
		return scope{}, true
	}
	if slices.Contains(fields, ScopeEvidence) {
		for _, f := range fields {
			if f != ScopeEvidence {
				return scope{}, false
			}
		}
		return scope{evidence: true}, true
	}
	sc.readOnly = true
	for _, f := range fields {
		switch f {
		case ScopeMCP:
			sc.readOnly = false
		case ScopeMCPRead:
		default:
			return scope{}, false
		}
	}
	return sc, true
}

// name is the scope as a token answer names it.
func (sc scope) name() string {
	switch {
	case sc.partner && sc.readOnly:
		return ScopeRead
	case sc.partner:
		return ScopeRead + " " + ScopeWrite
	case sc.evidence:
		return ScopeEvidence
	case sc.readOnly:
		return ScopeMCPRead
	}
	return ScopeMCP
}

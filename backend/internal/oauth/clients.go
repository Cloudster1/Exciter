package oauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// MaxMetadataAge is the longest a client's metadata document is used without fetching it again.
const MaxMetadataAge = 24 * time.Hour

// FetchTimeout bounds fetching a metadata document, which a person waits for at the consent page.
const FetchTimeout = 5 * time.Second

// Fetcher reads a client's metadata document from its address, with how long it may be kept.
type Fetcher func(ctx context.Context, address string) (body []byte, keep time.Duration, err error)

// HTTPFetcher fetches a document with a client that should refuse private addresses, as netguard's does.
func HTTPFetcher(client *http.Client) Fetcher {
	// A document names itself by its address, so one that sends the client elsewhere is not the client's.
	own := *client
	own.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &own
	return func(ctx context.Context, address string) ([]byte, time.Duration, error) {
		ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("the document answered %s", resp.Status)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, MaxClientMetadataBytes+1))
		if err != nil {
			return nil, 0, err
		}
		if len(body) > MaxClientMetadataBytes {
			return nil, 0, errors.New("the document is larger than 5 KiB")
		}
		return body, keepFor(resp.Header.Get("Cache-Control")), nil
	}
}

var maxAge = regexp.MustCompile(`(?:^|[,\s])max-age=(\d+)`)

// keepFor is how long cache headers let a document be kept, at most a day; no-store and no-cache keep it not at all.
func keepFor(cacheControl string) time.Duration {
	lower := strings.ToLower(cacheControl)
	if strings.Contains(lower, "no-store") || strings.Contains(lower, "no-cache") {
		return 0
	}
	m := maxAge.FindStringSubmatch(lower)
	if m == nil {
		return 0
	}
	seconds, err := strconv.Atoi(m[1])
	if err != nil {
		return MaxMetadataAge
	}
	return min(time.Duration(seconds)*time.Second, MaxMetadataAge)
}

// client is a client as the server knows it.
type client struct {
	ID, Name, URI string
	Kind          ClientKind
	RedirectURIs  []string
	AuthMethod    string
	SecretHash    []byte
	GrantTypes    []string
	// KeycloakClientID and Scopes are a partner's (#273), empty for any other client.
	KeycloakClientID string
	Scopes           []string
}

func (c *client) ref() ClientRef { return ClientRef{ID: c.ID, Name: c.Name, URI: c.URI, Kind: c.Kind} }

// grants says whether the client registered for a grant.
func (c *client) grants(grant string) bool {
	for _, g := range c.GrantTypes {
		if g == grant {
			return true
		}
	}
	return false
}

// authenticate holds what a client sent to the method it registered with; a client that keeps no secret sends none.
func (c *client) authenticate(secret string, basic bool) *Refusal {
	wrong := refuse(ErrInvalidClient, "The client could not be authenticated. Send its secret as it registered, or register it again.")
	switch c.AuthMethod {
	case AuthNone:
		if secret != "" {
			return wrong
		}
		return nil
	case AuthSecretBasic:
		// A partner authenticates either way, as at Armature, since it was never asked how it would.
		if !basic && c.Kind != ClientPartner {
			return wrong
		}
	case AuthSecretPost:
		if basic {
			return wrong
		}
	}
	if secret == "" || subtle.ConstantTimeCompare(auth.HashToken(secret), c.SecretHash) != 1 {
		return wrong
	}
	return nil
}

var errUnknownClient = refuse(ErrInvalidClient, "Exciter does not know this client. Register it, or give the address of its metadata document as its client_id.")

const clientSQL = `SELECT id, name, uri, kind, redirect_uris, auth_method, secret_hash, grant_types, fresh_until, keycloak_client_id, scopes
	FROM oauth_client WHERE id = $1`

// clientOf finds a client, fetching its metadata document again when it is one and the kept copy went stale.
func (s *Service) clientOf(ctx context.Context, id string) (*client, error) {
	var (
		c     client
		fresh *time.Time
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, clientSQL, id).Scan(&c.ID, &c.Name, &c.URI, &c.Kind, &c.RedirectURIs, &c.AuthMethod, &c.SecretHash, &c.GrantTypes, &fresh,
			&c.KeycloakClientID, &c.Scopes)
	})
	switch {
	case err == nil && (c.Kind != ClientMetadata || fresh.After(s.now())):
		return &c, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	case !IsMetadataClientID(id):
		return nil, errUnknownClient
	}
	return s.fetchClient(ctx, id)
}

// fetchClient reads a client's metadata document and keeps it, held to the rules a registration is held to.
func (s *Service) fetchClient(ctx context.Context, id string) (*client, error) {
	if s.fetch == nil {
		return nil, errUnknownClient
	}
	body, keep, err := s.fetch(ctx, id)
	if err != nil {
		return nil, refuse(ErrInvalidClient, "The client's metadata document at "+id+" could not be read: "+err.Error()+".")
	}
	var doc struct {
		ClientMetadataInput
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, refuse(ErrInvalidClient, "The client's metadata document is not JSON.")
	}
	if doc.ClientID != id {
		return nil, refuse(ErrInvalidClient, "The client's metadata document names another client_id than its own address.")
	}
	if doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != AuthNone {
		return nil, refuse(ErrInvalidClient, "A client known by its metadata document keeps no secret; its token_endpoint_auth_method is none.")
	}
	if r := checkMetadata(&doc.ClientMetadataInput); r != nil {
		return nil, refuse(ErrInvalidClient, "The client's metadata document is refused: "+r.Body.ErrorDescription)
	}
	c := client{ID: id, Name: doc.ClientName, URI: doc.ClientURI, Kind: ClientMetadata, RedirectURIs: doc.RedirectURIs,
		AuthMethod: AuthNone, GrantTypes: doc.GrantTypes}
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO oauth_client (id, kind, name, uri, redirect_uris, grant_types, fresh_until)
			VALUES ($1, 'metadata', $2, $3, $4, $5, now() + make_interval(secs => $6))
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, uri = EXCLUDED.uri, redirect_uris = EXCLUDED.redirect_uris,
				grant_types = EXCLUDED.grant_types, fresh_until = EXCLUDED.fresh_until
			WHERE oauth_client.kind = 'metadata'`, c.ID, c.Name, c.URI, c.RedirectURIs, c.GrantTypes, keep.Seconds())
		return err
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Register keeps a client that registered itself (RFC 7591), at most MaxRegistrationsPerHour from one address.
func (s *Service) Register(ctx context.Context, in ClientMetadataInput, from string) (*RegisteredClient, error) {
	if on, err := s.Enabled(ctx); err != nil || !on {
		return nil, firstErr(err, refuse(ErrTemporarilyUnavailable, "OAuth connections are switched off on this server. Ask its administrators."))
	}
	if r := checkMetadata(&in); r != nil {
		return nil, r
	}
	id, _, err := secret("")
	if err != nil {
		return nil, err
	}
	out := RegisteredClient{ClientMetadataInput: in, ClientID: id, ClientIDIssuedAt: s.now().Unix()}
	var digest []byte
	if in.TokenEndpointAuthMethod != AuthNone {
		if out.ClientSecret, digest, err = secret(clientSecretPrefix); err != nil {
			return nil, err
		}
	}
	tooMany := false
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM oauth_client WHERE kind = 'registered' AND registered_from = $1
			AND created_at > now() - interval '1 hour'`, from).Scan(&recent); err != nil {
			return err
		}
		if tooMany = recent >= MaxRegistrationsPerHour; tooMany {
			return nil
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oauth_client (id, kind, name, uri, redirect_uris, auth_method, secret_hash, grant_types, registered_from)
			VALUES ($1, 'registered', $2, $3, $4, $5, $6, $7, $8)`,
			id, in.ClientName, in.ClientURI, in.RedirectURIs, in.TokenEndpointAuthMethod, digest, in.GrantTypes, from)
		return err
	})
	if err != nil {
		return nil, err
	}
	if tooMany {
		r := refuse(ErrTemporarilyUnavailable, "Too many clients registered from this address in the last hour. Try again later.")
		r.Status = http.StatusTooManyRequests
		return nil, r
	}
	return &out, nil
}

// firstErr is err when there is one, otherwise the refusal.
func firstErr(err error, r *Refusal) error {
	if err != nil {
		return err
	}
	return r
}

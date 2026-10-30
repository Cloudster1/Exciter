package oidc

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/auth"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/secret"
)

// Service runs the sign-in flow and keeps the installation's provider, with
// discovery cached per issuer; the library refreshes keys it has not seen.
type Service struct {
	db          *db.Cluster
	box         *secret.Box
	redirectURL string
	// http reaches the provider; nil is the default client.
	http *http.Client

	mu         sync.Mutex
	discovered map[string]*coreoidc.Provider
}

// NewService returns the service. box seals client secrets; a nil box refuses
// to store one.
func NewService(cluster *db.Cluster, box *secret.Box, redirectURL string) *Service {
	return &Service{
		db:          cluster,
		box:         box,
		redirectURL: redirectURL,
		discovered:  map[string]*coreoidc.Provider{},
	}
}

// Provider reads the installation's configuration, through row level security like any other data.
func (s *Service) Provider(ctx context.Context) (*Provider, error) {
	var p Provider
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT issuer, client_id, client_secret IS NOT NULL, groups_claim, scopes,
			       create_groups, admit_by_group, enabled, updated_at
			FROM oidc_provider`,
		).Scan(&p.Issuer, &p.ClientID, &p.HasSecret, &p.GroupsClaim, &p.Scopes,
			&p.CreateGroups, &p.AdmitByGroup, &p.Enabled, &p.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("read the identity provider: %w", err)
	}
	return &p, nil
}

// Save writes the installation's configuration. An empty secret keeps the stored one, so a form that never shows it
// cannot erase it.
func (s *Service) Save(ctx context.Context, in Provider) (*Provider, db.LSN, error) {
	in, err := normalize(in)
	if err != nil {
		return nil, 0, err
	}
	var sealed []byte
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if in.ClientSecret != "" {
			id, err := installation.ID(ctx, tx)
			if err != nil {
				return err
			}
			// Sealed for the installation, as before (#107), so a secret stored earlier still opens.
			if sealed, err = s.box.Seal([]byte(in.ClientSecret), id[:]); errors.Is(err, secret.ErrNoKey) {
				return &ValidationError{Field: "clientSecret", Message: "The server has no EXCITER_SECRET_KEY, so it cannot store a client secret. Set one and try again."}
			} else if err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oidc_provider (issuer, client_id, client_secret, groups_claim, scopes, create_groups, enabled, admit_by_group)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT ((true)) DO UPDATE SET
			    issuer = EXCLUDED.issuer,
			    client_id = EXCLUDED.client_id,
			    client_secret = COALESCE(EXCLUDED.client_secret, oidc_provider.client_secret),
			    groups_claim = EXCLUDED.groups_claim,
			    scopes = EXCLUDED.scopes,
			    create_groups = EXCLUDED.create_groups,
			    enabled = EXCLUDED.enabled,
			    admit_by_group = EXCLUDED.admit_by_group`,
			in.Issuer, in.ClientID, sealed, in.GroupsClaim, in.Scopes, in.CreateGroups, in.Enabled, in.AdmitByGroup)
		if err != nil {
			return err
		}
		secretChange := "kept"
		if sealed != nil {
			secretChange = "set"
		}
		actor, _ := db.UserFrom(ctx)
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionSSOProviderSaved, TargetType: "oidc_provider", Actor: actor,
			Data: map[string]any{
				"issuer": in.Issuer, "clientId": in.ClientID, "clientSecret": secretChange, "groupsClaim": in.GroupsClaim,
				"scopes": in.Scopes, "createGroups": in.CreateGroups, "admitByGroup": in.AdmitByGroup, "enabled": in.Enabled,
			},
		})
	})
	if err != nil {
		return nil, 0, fmt.Errorf("save the identity provider: %w", err)
	}

	// A changed issuer means the cached discovery is about somewhere else.
	s.mu.Lock()
	delete(s.discovered, in.Issuer)
	s.mu.Unlock()

	saved, err := s.Provider(ctx)
	return saved, lsn, err
}

// normalize trims a provider's settings, fills in the defaults and refuses
// what cannot work.
func normalize(in Provider) (Provider, error) {
	in.Issuer = strings.TrimSuffix(strings.TrimSpace(in.Issuer), "/")
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.GroupsClaim = strings.TrimSpace(in.GroupsClaim)
	in.Scopes = strings.Join(strings.Fields(in.Scopes), " ")
	if u, err := url.Parse(in.Issuer); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return in, &ValidationError{Field: "issuer", Message: "Enter the provider's issuer URL, such as https://id.example.com/realms/acme."}
	}
	if in.ClientID == "" {
		return in, &ValidationError{Field: "clientId", Message: "Enter the client ID the provider gave Exciter."}
	}
	if in.GroupsClaim == "" {
		in.GroupsClaim = DefaultGroupsClaim
	}
	if in.Scopes == "" {
		in.Scopes = DefaultScopes
	}
	return in, nil
}

// signInProvider reads the provider for a sign-in, which happens before anybody acts, and opens its secret.
func (s *Service) signInProvider(ctx context.Context) (*Provider, error) {
	var (
		p      Provider
		sealed []byte
		id     uuid.UUID
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT (SELECT id FROM installation), issuer, client_id, client_secret, groups_claim, scopes, create_groups, admit_by_group, enabled, updated_at
			FROM oidc_provider`,
		).Scan(&id, &p.Issuer, &p.ClientID, &sealed, &p.GroupsClaim, &p.Scopes, &p.CreateGroups, &p.AdmitByGroup, &p.Enabled, &p.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("read the identity provider: %w", err)
	}
	if !p.Enabled {
		return nil, ErrNotConfigured
	}
	if sealed != nil {
		plain, err := s.box.Open(sealed, id[:])
		if err != nil {
			return nil, fmt.Errorf("open the client secret: %w", err)
		}
		p.ClientSecret, p.HasSecret = string(plain), true
	}
	return &p, nil
}

// discover returns the provider's endpoints and key set, fetching them once.
func (s *Service) discover(ctx context.Context, issuer string) (*coreoidc.Provider, error) {
	s.mu.Lock()
	cached, ok := s.discovered[issuer]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	found, err := coreoidc.NewProvider(s.providerContext(ctx), issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: ask %s who it is: %w", ErrUnreachable, issuer, err)
	}
	s.mu.Lock()
	s.discovered[issuer] = found
	s.mu.Unlock()
	return found, nil
}

func (s *Service) oauthConfig(p *Provider, discovered *coreoidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  s.redirectURL,
		Scopes:       p.ScopeList(),
	}
}

// Start begins a sign-in and returns the URL to send the browser to.
func (s *Service) Start(ctx context.Context, redirect string) (string, error) {
	return s.start(ctx, redirect, nil)
}

// reauth is who a re-authentication proves again, and in which session.
type reauth struct {
	userID, sessionID uuid.UUID
	email             string
}

// StartReauth begins a sign-in that proves again who a signed-in person is, for a signature (#46): the provider is
// told to ask for their credentials whatever session it holds, and the callback opens no session.
func (s *Service) StartReauth(ctx context.Context, userID, sessionID uuid.UUID, email, redirect string) (string, error) {
	return s.start(ctx, redirect, &reauth{userID: userID, sessionID: sessionID, email: email})
}

func (s *Service) start(ctx context.Context, redirect string, again *reauth) (string, error) {
	provider, err := s.signInProvider(ctx)
	if err != nil {
		return "", err
	}
	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return "", err
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", err
	}
	// The state ties the callback to this attempt, the nonce the id token, and
	// the PKCE verifier proves whoever redeems the code is who asked for it.
	verifier := oauth2.GenerateVerifier()

	purpose, user, session := PurposeSignIn, (*uuid.UUID)(nil), (*uuid.UUID)(nil)
	if again != nil {
		purpose, user, session = PurposeReauth, &again.userID, &again.sessionID
	}
	if _, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Abandoned attempts are dead weight, and the table is small enough
		// that tidying on write is the whole story.
		if _, err := tx.Exec(ctx, `DELETE FROM oidc_login WHERE expires_at < now()`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oidc_login (state, nonce, code_verifier, redirect, expires_at, purpose, user_id, session_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			state, nonce, verifier, redirect, time.Now().Add(howLongALoginMayTake), purpose, user, session)
		return err
	}); err != nil {
		return "", fmt.Errorf("remember this sign-in: %w", err)
	}

	config := s.oauthConfig(provider, discovered)
	if again != nil {
		return reauthURL(config, state, nonce, verifier, again.email), nil
	}
	return authCodeURL(config, state, nonce, verifier), nil
}

// authCodeURL carries the state, the nonce and the S256 challenge; the verifier
// itself stays behind for the code exchange.
func authCodeURL(config *oauth2.Config, state, nonce, verifier string, extra ...oauth2.AuthCodeOption) string {
	options := append([]oauth2.AuthCodeOption{coreoidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}, extra...)
	return config.AuthCodeURL(state, options...)
}

// reauthURL is a sign-in the provider must answer by asking for the person's credentials again: prompt=login and a
// max_age of zero, so an earlier session there does not count, with the person named so they need not type it.
func reauthURL(config *oauth2.Config, state, nonce, verifier, email string) string {
	return authCodeURL(config, state, nonce, verifier,
		oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0"), oauth2.SetAuthURLParam("login_hint", email))
}

// Exchange trades the code for a verified id token and returns its identity, with the sign-in it answers: where the
// browser was headed, and for a re-authentication whose session it proves.
func (s *Service) Exchange(ctx context.Context, state, code string) (*Identity, *Login, error) {
	var (
		nonce    string
		verifier string
		login    Login
	)
	// Consumed as it is read: a state is good for exactly one sign-in, so a
	// replayed callback cannot open a second session.
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			DELETE FROM oidc_login WHERE state = $1 AND expires_at > now()
			RETURNING nonce, code_verifier, redirect, purpose, user_id, session_id, requested_at`, state,
		).Scan(&nonce, &verifier, &login.Redirect, &login.Purpose, &login.UserID, &login.SessionID, &login.RequestedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownLogin
		}
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	provider, err := s.signInProvider(ctx)
	if err != nil {
		return nil, nil, err
	}
	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return nil, nil, err
	}

	ctx = s.providerContext(ctx)
	tokens, err := s.oauthConfig(provider, discovered).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, nil, fmt.Errorf("exchange the code: %w", err)
	}
	raw, ok := tokens.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, nil, errors.New("the identity provider returned no id token")
	}

	// Verified against the issuer's published keys, for this client, unexpired; the library refuses an
	// unsigned token and one signed with a symmetric key, the two ways this check is usually skipped.
	verified, err := discovered.Verifier(&coreoidc.Config{ClientID: provider.ClientID}).Verify(ctx, raw)
	if err != nil {
		return nil, nil, fmt.Errorf("verify the id token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(nonce)) != 1 {
		return nil, nil, errors.New("the id token belongs to a different sign-in")
	}

	var claims map[string]any
	if err := verified.Claims(&claims); err != nil {
		return nil, nil, fmt.Errorf("read the token's claims: %w", err)
	}
	identity, err := identityFromClaims(verified.Issuer, verified.Subject, claims, provider.GroupsClaim)
	if err != nil {
		return nil, nil, err
	}
	identity.AccessToken = tokens.AccessToken
	return identity, &login, nil
}

// VerifyAccessToken checks a person's access token from the installation's provider, issued to authorizedParty for
// Exciter: the provider's issuer and keys, unexpired, azp the partner's client and aud holding Exciter's own, as
// Armature checks one (#273). The identity in it is read as a sign-in reads an id token's.
func (s *Service) VerifyAccessToken(ctx context.Context, raw, authorizedParty string) (*Identity, error) {
	provider, err := s.signInProvider(ctx)
	if err != nil {
		return nil, err
	}
	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return nil, err
	}
	// The audience is checked below, by name, since an access token is for its resources rather than its client.
	verified, err := discovered.Verifier(&coreoidc.Config{SkipClientIDCheck: true}).Verify(s.providerContext(ctx), raw)
	if err != nil {
		return nil, &TokenError{Reason: "it is not a current token signed by the identity provider"}
	}
	var claims map[string]any
	if err := verified.Claims(&claims); err != nil {
		return nil, fmt.Errorf("read the token's claims: %w", err)
	}
	if party := asString(claims["azp"]); party != authorizedParty {
		return nil, &TokenError{Reason: fmt.Sprintf("it was issued to %q, not to the app's Keycloak client", party)}
	}
	if !slices.Contains(verified.Audience, provider.ClientID) {
		return nil, &TokenError{Reason: fmt.Sprintf(
			"its audience does not hold Exciter's client %s; add an audience mapper to the app's Keycloak client", provider.ClientID)}
	}
	return identityFromClaims(verified.Issuer, verified.Subject, claims, provider.GroupsClaim)
}

// TokenError is an access token the provider did not vouch for as asked, with the reason in words.
type TokenError struct{ Reason string }

func (e *TokenError) Error() string { return e.Reason }

// randomTokenBytes is what both the state and the nonce need to be unguessable.
const randomTokenBytes = 32

// randomToken is randomTokenBytes of randomness, URL safe.
func randomToken() (string, error) {
	buf := make([]byte, randomTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// clockSkew is how far the provider's clock may be ahead of ours when it says when somebody signed in.
const clockSkew = time.Minute

// Reauthenticated records what a re-authentication proved: the person signed in again, at the provider, after it
// was asked for. It answers the re-authentication a signature spends.
func (s *Service) Reauthenticated(ctx context.Context, identity *Identity, login *Login) (uuid.UUID, error) {
	if login.Purpose != PurposeReauth || login.UserID == nil || login.SessionID == nil {
		return uuid.Nil, ErrUnknownLogin
	}
	if identity.AuthTime.IsZero() || identity.AuthTime.Before(login.RequestedAt.Add(-clockSkew)) {
		return uuid.Nil, ErrNotFresh
	}
	var id uuid.UUID
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var owner uuid.UUID
		err := tx.QueryRow(ctx, `SELECT user_id FROM user_identity WHERE issuer = $1 AND subject = $2`, identity.Issuer, identity.Subject).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != *login.UserID) {
			return ErrNotTheSigner
		}
		if err != nil {
			return err
		}
		// The provider's clock is not ours: a sign-in it dates a moment ahead counts as now.
		at := identity.AuthTime
		if now := time.Now(); at.After(now) {
			at = now
		}
		return tx.QueryRow(ctx, `
			INSERT INTO reauthentication (user_id, session_id, method, authenticated_at, expires_at)
			VALUES ($1, $2, 'oidc', $3, now() + make_interval(secs => $4)) RETURNING id`,
			owner, *login.SessionID, at, auth.ReauthenticationTTL.Seconds()).Scan(&id)
	})
	return id, err
}

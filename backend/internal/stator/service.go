package stator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/secret"
	"github.com/cloudster1/exciter/backend/internal/tokenexchange"
)

// WebhookPath is where Stator posts the installation's events, under the app's origin.
const WebhookPath = "/api/v1/stator/webhook"

// Audit actions of the connection.
const (
	ActionConnectionSaved   = audit.ActionStatorConnected
	ActionConnectionRemoved = audit.ActionStatorDisconnected
)

var (
	// ErrNotConfigured is an installation without a Stator connection.
	ErrNotConfigured = errors.New("the installation has not connected Stator")
	// ErrNotConnected is a caller who stored no token.
	ErrNotConnected = errors.New("the caller has not connected their Stator account")

	webhookSecretShape = regexp.MustCompile(`^` + WebhookSecretPrefix + `[A-Za-z0-9_-]{43}$`)
)

// FieldError is input that cannot be stored, with the sentence to show by it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Options are the settings of the process the service needs.
type Options struct {
	// AppURL is Exciter's own origin, for the webhook address.
	AppURL string
	// Allow is what EXCITER_OUTBOUND_ALLOW names; such a host may be plain http.
	Allow netguard.Allow
	// Development lets any base URL be plain http.
	Development bool
	// TokenExchange connects people at sign-in by exchanging their identity provider's token at Stator (#274),
	// EXCITER_STATOR_TOKEN_EXCHANGE.
	TokenExchange bool
	Log           *slog.Logger
}

// Service keeps the installation's connection and each member's token, and
// hands later work the caller as Stator knows them.
type Service struct {
	db     *db.Cluster
	box    *secret.Box
	client *Client
	cache  *Cache
	opts   Options
}

// NewService returns the service. A nil box refuses to store a token or a secret; a nil cache keeps nothing.
func NewService(cluster *db.Cluster, box *secret.Box, client *Client, cache *Cache, opts Options) *Service {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Service{db: cluster, box: box, client: client, cache: cache, opts: opts}
}

// Cache is the per-person answer cache, nil without Valkey.
func (s *Service) Cache() *Cache { return s.cache }

// Client is the one outbound client.
func (s *Service) Client() *Client { return s.client }

// endpoint is what any member may know of the connection.
type endpoint struct {
	// Installation is the id stored secrets are sealed with.
	Installation uuid.UUID
	BaseURL      string
	RemoteLinks  bool
}

func (s *Service) endpoint(ctx context.Context, tx db.DBTX) (*endpoint, error) {
	var e endpoint
	err := tx.QueryRow(ctx, `SELECT (SELECT id FROM installation), base_url, remote_links FROM stator_endpoint()`).
		Scan(&e.Installation, &e.BaseURL, &e.RemoteLinks)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Stator connection: %w", err)
	}
	return &e, nil
}

// Connection is the installation's connection for an administrator, or nil.
func (s *Service) Connection(ctx context.Context) (*Connection, error) {
	var out *Connection
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.readConnection(ctx, tx)
		return err
	})
	return out, err
}

func (s *Service) readConnection(ctx context.Context, tx db.DBTX) (*Connection, error) {
	c := Connection{WebhookURL: s.opts.AppURL + WebhookPath, WebhookTopics: WebhookTopics, TokenExchange: s.opts.TokenExchange}
	var connected *int
	err := tx.QueryRow(ctx, `
		SELECT base_url, remote_links, webhook_secret IS NOT NULL, updated_at, stator_connected_count(),
		       coalesce(client_id, ''), client_secret IS NOT NULL, token_exchange
		FROM stator_connection`,
	).Scan(&c.BaseURL, &c.RemoteLinks, &c.WebhookSecretSet, &c.UpdatedAt, &connected, &c.ClientID, &c.ClientSecretSet, &c.ExchangeOffered)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Stator connection: %w", err)
	}
	if connected != nil {
		c.Connected = *connected
	}
	return &c, nil
}

// SaveConnection sets the installation's Stator. A new address forgets every stored token in the same transaction,
// and what Stator offers is read again on every save.
func (s *Service) SaveConnection(ctx context.Context, in ConnectionInput, ip string) (*Connection, db.LSN, error) {
	return s.saveConnection(ctx, in, ip, true)
}

// ConnectFirst connects the Stator at baseURL when none is, as seed bootstrap does; one that does not answer yet is
// connected anyway, and what it offers is asked on the next save or token check. It reports whether it connected.
func (s *Service) ConnectFirst(ctx context.Context, baseURL, ip string) (bool, error) {
	before, err := s.Connection(db.PinPrimary(ctx))
	if err != nil || before != nil {
		return false, err
	}
	_, _, err = s.saveConnection(ctx, ConnectionInput{BaseURL: baseURL}, ip, false)
	return err == nil, err
}

// saveConnection is SaveConnection; answered says a new address is refused while the Stator there does not answer.
func (s *Service) saveConnection(ctx context.Context, in ConnectionInput, ip string, answered bool) (*Connection, db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	base, err := s.normalizeBaseURL(in.BaseURL)
	if err != nil {
		return nil, 0, err
	}
	secret := ""
	secretChange := "kept"
	if in.WebhookSecret != nil {
		secretChange = "removed"
		if secret = strings.TrimSpace(*in.WebhookSecret); secret != "" {
			if !webhookSecretShape.MatchString(secret) {
				return nil, 0, &FieldError{Field: "webhookSecret", Message: "That is not a Stator webhook secret. Paste the secret Stator showed when you added the webhook; it starts with " + WebhookSecretPrefix + "."}
			}
			secretChange = "set"
		}
	}
	clientID, clientSecret, clientChange := "", "", "kept"
	if in.ClientID != nil {
		if clientID = strings.TrimSpace(*in.ClientID); len(clientID) > MaxClientIDLength {
			return nil, 0, &FieldError{Field: "clientId", Message: fmt.Sprintf("A client id is at most %d characters. Paste the id Stator showed for Exciter among its connected apps.", MaxClientIDLength)}
		}
	}
	if in.ClientSecret != nil {
		clientChange = "removed"
		if clientSecret = strings.TrimSpace(*in.ClientSecret); clientSecret != "" {
			clientChange = "set"
		}
	}

	var (
		before    *Connection
		installed uuid.UUID
	)
	if err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		if installed, err = installation.ID(ctx, tx); err != nil {
			return err
		}
		before, err = s.readConnection(ctx, tx)
		return err
	}); err != nil {
		return nil, 0, err
	}
	var sealed, sealedClient []byte
	if secret != "" {
		if sealed, err = s.seal(secret, webhookContext(installed), "webhookSecret"); err != nil {
			return nil, 0, err
		}
	}
	if clientSecret != "" {
		if sealedClient, err = s.seal(clientSecret, clientContext(installed), "clientSecret"); err != nil {
			return nil, 0, err
		}
	}
	moved := before == nil || before.BaseURL != base
	meta, err := s.describe(ctx, base)
	if err != nil && moved && answered {
		return nil, 0, err
	}
	// The address people already use stays when Stator is down for a moment, with what it offered last.

	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		forgotten := 0
		if moved && before != nil {
			if err := tx.QueryRow(ctx, `SELECT coalesce(stator_connected_count(), 0)`).Scan(&forgotten); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO stator_connection (base_url, webhook_secret, updated_by, client_id, client_secret)
			VALUES ($1, $2, $3, NULLIF($5, ''), $7)
			ON CONFLICT ((true)) DO UPDATE SET
			    base_url = EXCLUDED.base_url,
			    webhook_secret = CASE WHEN $4 THEN stator_connection.webhook_secret ELSE EXCLUDED.webhook_secret END,
			    client_id = CASE WHEN $6 THEN stator_connection.client_id ELSE EXCLUDED.client_id END,
			    client_secret = CASE WHEN $8 THEN stator_connection.client_secret ELSE EXCLUDED.client_secret END,
			    updated_by = EXCLUDED.updated_by`,
			base, sealed, nullable(actor), in.WebhookSecret == nil, clientID, in.ClientID == nil, sealedClient, in.ClientSecret == nil)
		if err != nil {
			return fmt.Errorf("save the Stator connection: %w", err)
		}
		data := map[string]any{"baseUrl": base, "webhookSecret": secretChange, "moved": moved, "tokensForgotten": forgotten, "clientSecret": clientChange}
		if in.ClientID != nil {
			data["clientId"] = clientID
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: ActionConnectionSaved, TargetType: "stator_connection", Actor: actor, IP: ip, Data: data,
		})
	})
	if err != nil {
		return nil, lsn, err
	}
	if meta != nil {
		lsn = max(lsn, s.noteOffers(ctx, base, meta))
	}
	out, err := s.Connection(db.PinPrimary(ctx))
	return out, lsn, err
}

// noteOffers records what the Stator at base offers, as the admin role: a member's check reads it too, and
// exciter_app may not write it, so nobody claims a capability with raw SQL.
func (s *Service) noteOffers(ctx context.Context, base string, meta *Meta) db.LSN {
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE stator_connection SET remote_links = $1, token_exchange = $3
			WHERE base_url = $2 AND (remote_links IS DISTINCT FROM $1 OR token_exchange IS DISTINCT FROM $3)`,
			meta.Offers(RemoteLinksCapability), base, slices.Contains(meta.Capabilities, tokenexchange.Capability))
		return err
	})
	if err != nil {
		s.opts.Log.Warn("what Stator offers could not be kept", "base_url", base, "error", err)
	}
	return lsn
}

// RemoveConnection disconnects Stator; every stored token goes with it.
func (s *Service) RemoveConnection(ctx context.Context, ip string) (db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var forgotten int
		if err := tx.QueryRow(ctx, `SELECT coalesce(stator_connected_count(), 0)`).Scan(&forgotten); err != nil {
			return err
		}
		var base string
		err := tx.QueryRow(ctx, `DELETE FROM stator_connection RETURNING base_url`).Scan(&base)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("remove the Stator connection: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: ActionConnectionRemoved, TargetType: "stator_connection", Actor: actor, IP: ip,
			Data: map[string]any{"baseUrl": base, "tokensForgotten": forgotten},
		})
	})
}

// normalizeBaseURL keeps the origin only: Stator's API is always under /api/v1 of it, and its pages under /s/.
func (s *Service) normalizeBaseURL(raw string) (string, error) {
	invalid := &FieldError{Field: "baseUrl", Message: "Enter the address people open Stator at, such as https://stator.example.com, without a path."}
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(raw, "?") || strings.HasSuffix(raw, "#") || (u.Path != "" && u.Path != "/") {
		return "", invalid
	}
	if u.Scheme == "http" && !s.opts.Development && !s.opts.Allow.Names(u.Hostname()) {
		return "", &FieldError{Field: "baseUrl", Message: "Use an https address for Stator, so the members' tokens travel encrypted."}
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

// describe checks that a Stator answers at base before anybody's token is sent there, and reads what it offers.
func (s *Service) describe(ctx context.Context, base string) (*Meta, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	meta, err := s.client.Describe(ctx, base)
	switch {
	case err == nil:
		return meta, nil
	case errors.Is(err, netguard.ErrBlocked):
		s.opts.Log.Info("the SSRF guard refused a Stator address", "base_url", base, "error", err)
		return nil, &FieldError{Field: "baseUrl", Message: "Exciter may not reach this address, because it is inside the server's own network. Use Stator's public address, or ask the operator to add the host to EXCITER_OUTBOUND_ALLOW."}
	case errors.Is(err, ErrNotStator):
		return nil, &FieldError{Field: "baseUrl", Message: "Something answers at this address, but it is not Stator. Check the address, and leave out any path such as /api/v1."}
	default:
		s.opts.Log.Info("Stator did not answer at an address being connected", "base_url", base, "error", err)
		return nil, &FieldError{Field: "baseUrl", Message: "Stator did not answer at this address. Check the address, and that Stator is running, then save again."}
	}
}

func (s *Service) seal(value string, context []byte, field string) ([]byte, error) {
	sealed, err := s.box.Seal([]byte(value), context)
	if errors.Is(err, secret.ErrNoKey) {
		return nil, &FieldError{Field: field, Message: "The server has no EXCITER_SECRET_KEY, so it cannot store this. Ask the operator to set one and try again."}
	}
	return sealed, err
}

func (s *Service) open(ctx context.Context, row *tokenRow, installation uuid.UUID) (string, error) {
	user, _ := db.UserFrom(ctx)
	plain, err := s.box.Open(row.Sealed, tokenContext(installation, user))
	if err != nil {
		s.opts.Log.Warn("a stored Stator token could not be opened", "token_id", row.ID, "error", err)
		return "", err
	}
	return string(plain), nil
}

// tokenContext binds a sealed token to its installation and person, so a copy moved onto another row does not open.
func tokenContext(installation, user uuid.UUID) []byte {
	return append(append([]byte("stator.token:"), installation[:]...), user[:]...)
}

// refreshContext binds a sealed refresh token to its installation and person, apart from the access token's.
func refreshContext(installation, user uuid.UUID) []byte {
	return append(append([]byte("stator.refresh:"), installation[:]...), user[:]...)
}

// clientContext binds Exciter's client secret at Stator to its installation.
func clientContext(installation uuid.UUID) []byte {
	return append([]byte("stator.client:"), installation[:]...)
}

// webhookContext binds the webhook secret to its installation.
func webhookContext(installation uuid.UUID) []byte {
	return append([]byte("stator.webhook:"), installation[:]...)
}

// OpenWebhookSecret opens the installation's sealed webhook secret, for the receiver.
func (s *Service) OpenWebhookSecret(installation uuid.UUID, sealed []byte) (string, error) {
	plain, err := s.box.Open(sealed, webhookContext(installation))
	return string(plain), err
}

func nullable(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

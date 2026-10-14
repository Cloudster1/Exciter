package armature

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/installation"
	"github.com/cloudster1/exciter/backend/internal/netguard"
	"github.com/cloudster1/exciter/backend/internal/secret"
)

// WebhookPath is where Armature posts the installation's events, under the app's origin.
const WebhookPath = "/api/v1/armature/webhook"

// Audit actions of the connection.
const (
	ActionConnectionSaved   = audit.ActionArmatureConnected
	ActionConnectionRemoved = audit.ActionArmatureDisconnected
)

var (
	// ErrNotConfigured is an installation without an Armature connection.
	ErrNotConfigured = errors.New("the installation has not connected Armature")
	// ErrNotConnected is a caller who stored no token.
	ErrNotConnected = errors.New("the caller has not connected their Armature account")

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
	Log         *slog.Logger
}

// Service keeps the installation's connection and each member's token, and
// hands later work the caller as Armature knows them.
type Service struct {
	db     *db.Cluster
	box    *secret.Box
	client *Client
	cache  *Cache
	opts   Options
}

// NewService returns the service. A nil box refuses to store a token or a
// secret; a nil cache keeps nothing.
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
	// Installation is the id stored secrets are sealed with, unchanged since #107 so they still open.
	Installation  uuid.UUID
	BaseURL       string
	ArmatureOrgID *uuid.UUID
}

func (s *Service) endpoint(ctx context.Context, tx db.DBTX) (*endpoint, error) {
	var e endpoint
	err := tx.QueryRow(ctx, `SELECT (SELECT id FROM org), base_url, armature_org_id FROM armature_endpoint()`).
		Scan(&e.Installation, &e.BaseURL, &e.ArmatureOrgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Armature connection: %w", err)
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
	c := Connection{WebhookURL: s.opts.AppURL + WebhookPath, WebhookTopics: WebhookTopics}
	var connected *int
	err := tx.QueryRow(ctx, `
		SELECT base_url, armature_org_id, webhook_secret IS NOT NULL, updated_at, armature_connected_count()
		FROM armature_connection`,
	).Scan(&c.BaseURL, &c.ArmatureOrgID, &c.WebhookSecretSet, &c.UpdatedAt, &connected)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the Armature connection: %w", err)
	}
	if connected != nil {
		c.Connected = *connected
	}
	return &c, nil
}

// SaveConnection sets the installation's Armature. A new address forgets every stored token in the same
// transaction.
func (s *Service) SaveConnection(ctx context.Context, in ConnectionInput, ip string) (*Connection, db.LSN, error) {
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
				return nil, 0, &FieldError{Field: "webhookSecret", Message: "That is not an Armature webhook secret. Paste the secret Armature showed when you added the endpoint; it starts with " + WebhookSecretPrefix + "."}
			}
			secretChange = "set"
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
	var sealed []byte
	if secret != "" {
		if sealed, err = s.seal(secret, webhookContext(installed), "webhookSecret"); err != nil {
			return nil, 0, err
		}
	}
	moved := before == nil || before.BaseURL != base
	if moved {
		if err := s.describe(ctx, base); err != nil {
			return nil, 0, err
		}
	}

	var out *Connection
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		forgotten := 0
		if moved && before != nil {
			if err := tx.QueryRow(ctx, `SELECT coalesce(armature_connected_count(), 0)`).Scan(&forgotten); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO armature_connection (base_url, webhook_secret, updated_by)
			VALUES ($1, $2, $3)
			ON CONFLICT ON CONSTRAINT armature_connection_pkey DO UPDATE SET
			    base_url = EXCLUDED.base_url,
			    webhook_secret = CASE WHEN $4 THEN armature_connection.webhook_secret ELSE EXCLUDED.webhook_secret END,
			    updated_by = EXCLUDED.updated_by`,
			base, sealed, nullable(actor), in.WebhookSecret == nil)
		if err != nil {
			return fmt.Errorf("save the Armature connection: %w", err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{
			Action: ActionConnectionSaved, TargetType: "armature_connection", Actor: actor, IP: ip,
			Data: map[string]any{"baseUrl": base, "webhookSecret": secretChange, "moved": moved, "tokensForgotten": forgotten},
		}); err != nil {
			return err
		}
		out, err = s.readConnection(ctx, tx)
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// RemoveConnection disconnects Armature; every stored token goes with it.
func (s *Service) RemoveConnection(ctx context.Context, ip string) (db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var forgotten int
		if err := tx.QueryRow(ctx, `SELECT coalesce(armature_connected_count(), 0)`).Scan(&forgotten); err != nil {
			return err
		}
		var base string
		err := tx.QueryRow(ctx, `DELETE FROM armature_connection RETURNING base_url`).Scan(&base)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("remove the Armature connection: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: ActionConnectionRemoved, TargetType: "armature_connection", Actor: actor, IP: ip,
			Data: map[string]any{"baseUrl": base, "tokensForgotten": forgotten},
		})
	})
}

// normalizeBaseURL keeps the origin only: Armature's API is always under
// /api/v1 of it, and its issues under /issues.
func (s *Service) normalizeBaseURL(raw string) (string, error) {
	invalid := &FieldError{Field: "baseUrl", Message: "Enter the address people open Armature at, such as https://armature.example.com, without a path."}
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(raw, "?") || strings.HasSuffix(raw, "#") || (u.Path != "" && u.Path != "/") {
		return "", invalid
	}
	if u.Scheme == "http" && !s.opts.Development && !s.opts.Allow.Names(u.Hostname()) {
		return "", &FieldError{Field: "baseUrl", Message: "Use an https address for Armature, so the members' tokens travel encrypted."}
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

// describe checks that an Armature answers at base before anybody's token is
// sent there.
func (s *Service) describe(ctx context.Context, base string) error {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	err := s.client.Describe(ctx, base)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, netguard.ErrBlocked):
		s.opts.Log.Info("the SSRF guard refused an Armature address", "base_url", base, "error", err)
		return &FieldError{Field: "baseUrl", Message: "Exciter may not reach this address, because it is inside the server's own network. Use Armature's public address, or ask the operator to add the host to EXCITER_OUTBOUND_ALLOW."}
	case errors.Is(err, ErrNotArmature):
		return &FieldError{Field: "baseUrl", Message: "Something answers at this address, but it is not Armature. Check the address, and leave out any path such as /api/v1."}
	default:
		s.opts.Log.Info("Armature did not answer at an address being connected", "base_url", base, "error", err)
		return &FieldError{Field: "baseUrl", Message: "Armature did not answer at this address. Check the address, and that Armature is running, then save again."}
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
		s.opts.Log.Warn("a stored Armature token could not be opened", "token_id", row.ID, "error", err)
		return "", err
	}
	return string(plain), nil
}

// tokenContext binds a sealed token to its installation and person, so a
// copy moved onto another row does not open.
func tokenContext(installation, user uuid.UUID) []byte {
	return append(append([]byte("armature.token:"), installation[:]...), user[:]...)
}

// webhookContext binds the webhook secret to its installation.
func webhookContext(installation uuid.UUID) []byte {
	return append([]byte("armature.webhook:"), installation[:]...)
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

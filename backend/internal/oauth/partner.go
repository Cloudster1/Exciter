package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// ClientPartner is a tool beside Exciter, registered by an administrator, that trades a person's token from the
// shared identity provider for a grant (#273).
const ClientPartner ClientKind = "partner"

// The scopes a partner app may hold; read is always among them, and write lets its grants do what their person may.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

// PartnerScopes lists them, for the API document and the metadata.
var PartnerScopes = []string{ScopeRead, ScopeWrite}

// MaxKeycloakClientIDLength bounds the identity provider's client a partner names, as the column does.
const MaxKeycloakClientIDLength = 255

// PartnerApp is a partner as its administrators see it.
type PartnerApp struct {
	// ID is the client_id the app authenticates with.
	ID        string `json:"id"`
	Name      string `json:"name"`
	ClientURI string `json:"clientUri"`
	// KeycloakClientID is the identity provider's client whose tokens the app may exchange; empty exchanges none.
	KeycloakClientID string   `json:"keycloakClientId"`
	Scopes           []string `json:"scopes"`
	// Grants counts the people the app holds a grant of.
	Grants    int       `json:"grants"`
	CreatedAt time.Time `json:"createdAt"`
	// Secret is answered once, when the app is registered and when it is rotated.
	Secret string `json:"secret,omitempty"`
}

// PartnerAppInput registers or changes a partner; on a change, what is left out keeps its value.
type PartnerAppInput struct {
	Name             *string  `json:"name,omitempty"`
	ClientURI        *string  `json:"clientUri,omitempty"`
	KeycloakClientID *string  `json:"keycloakClientId,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
}

// FieldError is a partner's setting that cannot be saved as given.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// ErrNoSuchPartner is a partner that is not there, or a client of another kind.
var ErrNoSuchPartner = errors.New("that partner app was not found")

const partnerColumns = `c.id, c.name, c.uri, c.keycloak_client_id, c.scopes, c.created_at,
	(SELECT count(*) FROM oauth_grant g WHERE g.client_id = c.id)`

func scanPartner(row pgx.Row) (*PartnerApp, error) {
	var p PartnerApp
	if err := row.Scan(&p.ID, &p.Name, &p.ClientURI, &p.KeycloakClientID, &p.Scopes, &p.CreatedAt, &p.Grants); err != nil {
		return nil, err
	}
	return &p, nil
}

// cleanPartner holds an input to the rules, filling a new app's defaults; was is the app it changes, or nil.
func cleanPartner(in PartnerAppInput, was *PartnerApp) (PartnerApp, error) {
	out := PartnerApp{Scopes: []string{ScopeRead}}
	if was != nil {
		out = *was
	}
	if in.Name != nil {
		out.Name = strings.TrimSpace(*in.Name)
	}
	if out.Name == "" || utf8.RuneCountInString(out.Name) > MaxClientNameLength {
		return out, &FieldError{Field: "name", Message: fmt.Sprintf("Give the app a name of at most %d characters, such as Stator.", MaxClientNameLength)}
	}
	if in.ClientURI != nil {
		out.ClientURI = strings.TrimSpace(*in.ClientURI)
	}
	if out.ClientURI != "" {
		if u, err := url.Parse(out.ClientURI); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return out, &FieldError{Field: "clientUri", Message: "Enter the app's web address, such as https://stator.example.com, or leave it empty."}
		}
	}
	if in.KeycloakClientID != nil {
		out.KeycloakClientID = strings.TrimSpace(*in.KeycloakClientID)
	}
	if utf8.RuneCountInString(out.KeycloakClientID) > MaxKeycloakClientIDLength {
		return out, &FieldError{Field: "keycloakClientId", Message: fmt.Sprintf("A Keycloak client id is at most %d characters.", MaxKeycloakClientIDLength)}
	}
	if in.Scopes != nil {
		scopes := slices.Compact(slices.Sorted(slices.Values(in.Scopes)))
		for _, sc := range scopes {
			if !slices.Contains(PartnerScopes, sc) {
				return out, &FieldError{Field: "scopes", Message: "A partner app may read, or read and write; give read, or read and write."}
			}
		}
		if !slices.Contains(scopes, ScopeRead) {
			return out, &FieldError{Field: "scopes", Message: "Every partner app reads; give read, or read and write."}
		}
		out.Scopes = scopes
	}
	return out, nil
}

// PartnerApps are the installation's partners, by name; row level security shows them to its members alone.
func (s *Service) PartnerApps(ctx context.Context) ([]PartnerApp, error) {
	out := []PartnerApp{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+partnerColumns+` FROM oauth_client c WHERE c.kind = 'partner' ORDER BY lower(c.name), c.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPartner(rows)
			if err != nil {
				return err
			}
			out = append(out, *p)
		}
		return rows.Err()
	})
	return out, err
}

func partnerEntry(ctx context.Context, action string, p *PartnerApp) audit.Entry {
	actor, _ := db.UserFrom(ctx)
	return audit.Entry{Action: action, TargetType: "oauth_client", Actor: actor,
		Data: map[string]any{"clientId": p.ID, "name": p.Name, "keycloakClientId": p.KeycloakClientID, "scopes": p.Scopes}}
}

// CreatePartnerApp registers a partner as the administrator acting; its secret is in the answer and never again.
func (s *Service) CreatePartnerApp(ctx context.Context, in PartnerAppInput) (*PartnerApp, db.LSN, error) {
	p, err := cleanPartner(in, nil)
	if err != nil {
		return nil, 0, err
	}
	if p.ID, _, err = secret(""); err != nil {
		return nil, 0, err
	}
	plain, digest, err := secret(clientSecretPrefix)
	if err != nil {
		return nil, 0, err
	}
	var out *PartnerApp
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO oauth_client (id, kind, name, uri, redirect_uris, auth_method, secret_hash, grant_types, keycloak_client_id, scopes)
			VALUES ($1, 'partner', $2, $3, '{}', $4, $5, $6, $7, $8)`,
			p.ID, p.Name, p.ClientURI, AuthSecretBasic, digest, partnerGrantTypes, p.KeycloakClientID, p.Scopes); err != nil {
			return err
		}
		if out, err = scanPartner(tx.QueryRow(ctx, `SELECT `+partnerColumns+` FROM oauth_client c WHERE c.id = $1`, p.ID)); err != nil {
			return err
		}
		return audit.Write(ctx, tx, partnerEntry(ctx, audit.ActionOAuthAppCreated, out))
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Secret = plain
	return out, lsn, nil
}

// UpdatePartnerApp changes a partner. Taking write away narrows every grant of it at once, as they are read with
// the app's scopes each time.
func (s *Service) UpdatePartnerApp(ctx context.Context, id string, in PartnerAppInput) (*PartnerApp, db.LSN, error) {
	var out *PartnerApp
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		was, err := scanPartner(tx.QueryRow(ctx, `SELECT `+partnerColumns+` FROM oauth_client c WHERE c.id = $1 AND c.kind = 'partner'`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchPartner
		}
		if err != nil {
			return err
		}
		p, err := cleanPartner(in, was)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE oauth_client SET name = $2, uri = $3, keycloak_client_id = $4, scopes = $5 WHERE id = $1`,
			id, p.Name, p.ClientURI, p.KeycloakClientID, p.Scopes); err != nil {
			return err
		}
		if out, err = scanPartner(tx.QueryRow(ctx, `SELECT `+partnerColumns+` FROM oauth_client c WHERE c.id = $1`, id)); err != nil {
			return err
		}
		return audit.Write(ctx, tx, partnerEntry(ctx, audit.ActionOAuthAppUpdated, out))
	})
	if err != nil {
		return nil, lsn, err
	}
	return out, lsn, nil
}

// RotatePartnerSecret gives a partner a new secret, in the answer and never again; the old one stops at once.
func (s *Service) RotatePartnerSecret(ctx context.Context, id string) (*PartnerApp, db.LSN, error) {
	plain, digest, err := secret(clientSecretPrefix)
	if err != nil {
		return nil, 0, err
	}
	var out *PartnerApp
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE oauth_client SET secret_hash = $2 WHERE id = $1 AND kind = 'partner'`, id, digest)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSuchPartner
		}
		if out, err = scanPartner(tx.QueryRow(ctx, `SELECT `+partnerColumns+` FROM oauth_client c WHERE c.id = $1`, id)); err != nil {
			return err
		}
		return audit.Write(ctx, tx, partnerEntry(ctx, audit.ActionOAuthAppSecretRotated, out))
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Secret = plain
	return out, lsn, nil
}

// DeletePartnerApp removes a partner, and with it every grant it holds and their tokens.
func (s *Service) DeletePartnerApp(ctx context.Context, id string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		was, err := scanPartner(tx.QueryRow(ctx, `SELECT `+partnerColumns+` FROM oauth_client c WHERE c.id = $1 AND c.kind = 'partner'`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchPartner
		}
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM oauth_client WHERE id = $1 AND kind = 'partner'`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSuchPartner
		}
		return audit.Write(ctx, tx, partnerEntry(ctx, audit.ActionOAuthAppDeleted, was))
	})
}

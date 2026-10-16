package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/netguard"
)

// selectWebhooks reads endpoints with their owner and the projects they listen to, in the order they were named.
const selectWebhooks = `
SELECT e.id, e.name, e.url, e.topics, e.enabled, e.version, e.disabled_reason, e.failures, e.owner_id, u.name, e.created_at, e.updated_at,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', p.id, 'key', p.key, 'name', p.name) ORDER BY p.key)
                 FROM project p WHERE p.id = ANY (e.project_ids)), '[]'::jsonb)
FROM webhook_endpoint e LEFT JOIN app_user u ON u.id = e.owner_id`

func scanWebhook(row pgx.Row) (*Webhook, error) {
	var (
		w         Webhook
		ownerID   *uuid.UUID
		ownerName *string
	)
	if err := row.Scan(&w.ID, &w.Name, &w.URL, &w.Topics, &w.Enabled, &w.Version, &w.DisabledReason, &w.Failures, &ownerID, &ownerName,
		&w.CreatedAt, &w.UpdatedAt, &w.Projects); err != nil {
		return nil, err
	}
	if w.Topics == nil {
		w.Topics = []string{}
	}
	if w.Projects == nil {
		w.Projects = []WebhookProject{}
	}
	if ownerID != nil {
		w.WebhookOwner = &WebhookOwner{ID: *ownerID}
		if ownerName != nil {
			w.WebhookOwner.Name = *ownerName
		}
	}
	return &w, nil
}

func one(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Webhook, error) {
	w, err := scanWebhook(tx.QueryRow(ctx, selectWebhooks+` WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return w, err
}

// List is the installation's endpoints, by name.
func (s *Service) List(ctx context.Context) ([]Webhook, error) {
	out := []Webhook{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectWebhooks+` ORDER BY lower(e.name), e.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			w, err := scanWebhook(rows)
			if err != nil {
				return err
			}
			out = append(out, *w)
		}
		return rows.Err()
	})
	return out, err
}

// clean checks an input and returns it trimmed, its topics deduplicated in the order Subscribable gives them.
func (s *Service) clean(in WebhookInput) (WebhookInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > MaxName {
		return in, &FieldError{Field: "name", Message: fmt.Sprintf("Give the webhook a name of at most %d characters, such as the system it feeds.", MaxName)}
	}
	in.URL = strings.TrimSpace(in.URL)
	if err := s.checkURL(in.URL); err != nil {
		return in, err
	}
	if len(in.Topics) == 0 {
		return in, &FieldError{Field: "topics", Message: "Choose at least one event to send."}
	}
	for _, t := range in.Topics {
		if !slices.Contains(Subscribable, t) {
			return in, &FieldError{Field: "topics", Message: fmt.Sprintf("A webhook cannot take the event %q. Choose from %s.", t, strings.Join(Subscribable, ", "))}
		}
	}
	topics := []string{}
	for _, t := range Subscribable {
		if slices.Contains(in.Topics, t) {
			topics = append(topics, t)
		}
	}
	in.Topics = topics
	keys := []string{}
	for _, k := range in.Projects {
		if k = strings.ToUpper(strings.TrimSpace(k)); k != "" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	if len(keys) > MaxProjects {
		return in, &FieldError{Field: "projects", Message: fmt.Sprintf("A webhook listens to at most %d projects. Leave the list empty to hear every project.", MaxProjects)}
	}
	in.Projects = keys
	if in.Version != nil && !slices.Contains(Versions, *in.Version) {
		return in, &FieldError{Field: "version", Message: fmt.Sprintf("Exciter sends webhook envelope version %s. Register the webhook for one of them.", VersionList())}
	}
	return in, nil
}

// projectIDs reads the projects an input names by key as the caller sees them, refusing a key that names none.
func projectIDs(ctx context.Context, tx db.DBTX, keys []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(keys))
	for _, k := range keys {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, k).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &FieldError{Field: "projects", Message: fmt.Sprintf("There is no project %s here. Pick the projects from the list.", k)}
		}
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// checkURL refuses what is no address to post to, and an address that names the server's own network outright. A
// name is resolved only when it is posted to, where netguard decides again on every attempt.
func (s *Service) checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || len(raw) > MaxURL {
		return &FieldError{Field: "url", Message: "Enter the address to post to, starting with https://, without a user name or a # part."}
	}
	host := strings.ToLower(u.Hostname())
	if s.opts.Allow.Names(host) {
		return nil
	}
	addr, notAddr := netip.ParseAddr(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || (notAddr == nil && netguard.Reserved(addr)) {
		return &FieldError{Field: "url", Message: "Exciter may not post to an address inside the server's own network. Use the receiver's public address, or ask the operator to add the host to EXCITER_OUTBOUND_ALLOW."}
	}
	return nil
}

// hostOf is what the audit log keeps of an address: a receiver's path or query often carries a token of its own.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func actorOf(ctx context.Context) uuid.UUID {
	id, _ := db.UserFrom(ctx)
	return id
}

func isUniqueName(err error) bool {
	return err != nil && strings.Contains(err.Error(), "webhook_endpoint_name_idx")
}

var errNameTaken = &FieldError{Field: "name", Message: "A webhook with that name is already here. Choose another name."}

// installationID is the installation a secret is sealed for.
func (s *Service) installationID(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id FROM installation`).Scan(&id)
	})
	return id, err
}

// sealFor makes a secret for the endpoint id and seals it, returning both.
func (s *Service) sealFor(ctx context.Context, id uuid.UUID) (string, []byte, error) {
	if s.box == nil {
		return "", nil, ErrNoKey
	}
	installation, err := s.installationID(ctx)
	if err != nil {
		return "", nil, err
	}
	plain, err := newSecret()
	if err != nil {
		return "", nil, err
	}
	sealed, err := s.box.Seal([]byte(plain), sealContext(installation, id))
	return plain, sealed, err
}

// Create makes an endpoint and returns it with its secret, this once.
func (s *Service) Create(ctx context.Context, in WebhookInput) (*Webhook, db.LSN, error) {
	in, err := s.clean(in)
	if err != nil {
		return nil, 0, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, 0, err
	}
	plain, sealed, err := s.sealFor(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	enabled := in.Enabled == nil || *in.Enabled
	version := Version
	if in.Version != nil {
		version = *in.Version
	}
	var out *Webhook
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		projects, err := projectIDs(ctx, tx, in.Projects)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO webhook_endpoint (id, name, url, secret_sealed, topics, project_ids, enabled, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, in.Name, in.URL, sealed, in.Topics, projects, enabled, version)
		if isUniqueName(err) {
			return errNameTaken
		}
		if err != nil {
			return err
		}
		if out, err = one(ctx, tx, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWebhookCreated, TargetType: "webhook", TargetID: &id, Actor: actorOf(ctx),
			Data: map[string]any{"name": in.Name, "host": hostOf(in.URL), "topics": in.Topics, "projects": in.Projects, "enabled": enabled,
				"version": version, "secret": "set"}})
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Secret = plain
	return out, lsn, nil
}

// Update changes the name, address, topics, projects or whether it is on, and makes the caller its owner.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in WebhookInput) (*Webhook, db.LSN, error) {
	in, err := s.clean(in)
	if err != nil {
		return nil, 0, err
	}
	var out *Webhook
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		projects, err := projectIDs(ctx, tx, in.Projects)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE webhook_endpoint SET name = $2, url = $3, topics = $4, project_ids = $5, enabled = COALESCE($6, enabled),
			       version = COALESCE($7, version)
			WHERE id = $1`, id, in.Name, in.URL, in.Topics, projects, in.Enabled, in.Version)
		if isUniqueName(err) {
			return errNameTaken
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if out, err = one(ctx, tx, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWebhookUpdated, TargetType: "webhook", TargetID: &id, Actor: actorOf(ctx),
			Data: map[string]any{"name": out.Name, "host": hostOf(out.URL), "topics": out.Topics, "projects": in.Projects, "enabled": out.Enabled,
				"version": out.Version}})
	})
	return out, lsn, err
}

// RotateSecret issues a new secret; the old one stops working at once.
func (s *Service) RotateSecret(ctx context.Context, id uuid.UUID) (*Webhook, db.LSN, error) {
	plain, sealed, err := s.sealFor(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	var out *Webhook
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE webhook_endpoint SET secret_sealed = $2 WHERE id = $1`, id, sealed)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if out, err = one(ctx, tx, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWebhookSecretRotated, TargetType: "webhook", TargetID: &id, Actor: actorOf(ctx),
			Data: map[string]any{"name": out.Name, "secret": "set"}})
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Secret = plain
	return out, lsn, nil
}

// Delete removes the endpoint and its log.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var name, address string
		err := tx.QueryRow(ctx, `DELETE FROM webhook_endpoint WHERE id = $1 RETURNING name, url`, id).Scan(&name, &address)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWebhookDeleted, TargetType: "webhook", TargetID: &id, Actor: actorOf(ctx),
			Data: map[string]any{"name": name, "host": hostOf(address)}})
	})
}

const selectDeliveries = `
SELECT id, endpoint_id, event_id, topic, attempt, manual, state, status, error, next_attempt_at, attempted_at, delivered_at, created_at
FROM webhook_delivery`

func scanDelivery(row pgx.Row) (*WebhookDelivery, error) {
	var d WebhookDelivery
	if err := row.Scan(&d.ID, &d.WebhookID, &d.EventID, &d.Topic, &d.Attempt, &d.Manual, &d.DeliveryState, &d.Status, &d.Error,
		&d.NextAttemptAt, &d.AttemptedAt, &d.DeliveredAt, &d.CreatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// Deliveries is an endpoint's log, newest first.
func (s *Service) Deliveries(ctx context.Context, id uuid.UUID, limit int) ([]WebhookDelivery, error) {
	limit = min(max(limit, 1), MaxDeliveries)
	out := []WebhookDelivery{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var found bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_endpoint WHERE id = $1)`, id).Scan(&found); err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, selectDeliveries+` WHERE endpoint_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, id, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDelivery(rows)
			if err != nil {
				return err
			}
			out = append(out, *d)
		}
		return rows.Err()
	})
	return out, err
}

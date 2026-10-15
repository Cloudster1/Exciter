package stator

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// Stator's webhooks (#41), as Armature's receiver: a delivery the installation's secret vouches for clears what it
// announces and writes what it says of linked pages.

// WebhookVersion is the envelope version Exciter reads; Stator sends none yet, which reads the same.
const WebhookVersion = 1

// TopicPing is what Stator's test delivery carries.
const TopicPing = "ping"

var (
	// ErrBadSignature is any delivery that cannot be trusted, one to an installation without a secret included.
	ErrBadSignature = errors.New("the webhook signature does not match")
	// ErrUnknownVersion is an envelope of a version Exciter cannot read, refused so Stator keeps it and retries.
	ErrUnknownVersion = errors.New("the webhook envelope is of a version Exciter does not read")
)

// Sign is Stator's webhook.Sign: sha256= and the lower case hex HMAC-SHA256 of the raw body under the secret.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// absentSecret is checked against when there is no secret, so a delivery to an installation without one costs the
// same work as one with.
const absentSecret = WebhookSecretPrefix + "absent"

// WebhookEnvelope is Stator's webhook.Envelope, Armature's shape.
type WebhookEnvelope struct {
	Version    int             `json:"version"`
	ID         uuid.UUID       `json:"id"`
	Topic      string          `json:"topic"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

// WebhookPage is the page every page topic names, Stator's PageRef.
type WebhookPage struct {
	ID      uuid.UUID `json:"id"`
	Title   string    `json:"title"`
	Version int       `json:"version"`
	URL     string    `json:"url"`
	Space   struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"space"`
}

type webhookTarget struct {
	Installation uuid.UUID
	Sealed       []byte
	BaseURL      string
}

// Webhook verifies one delivery and acts on it; an event acted on before changes nothing. An error other than
// ErrBadSignature or ErrUnknownVersion is Exciter's, and Stator tries the delivery again later.
func (s *Service) Webhook(ctx context.Context, body []byte, signature string) error {
	var target *webhookTarget
	// As the admin role: nobody signs in, and only the secret vouches for the body.
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var t webhookTarget
		err := tx.QueryRow(ctx, `
			SELECT (SELECT id FROM installation), webhook_secret, base_url
			FROM stator_connection WHERE webhook_secret IS NOT NULL`).Scan(&t.Installation, &t.Sealed, &t.BaseURL)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the webhook secret: %w", err)
		}
		target = &t
		return nil
	})
	if err != nil {
		return err
	}
	secret := absentSecret
	if target != nil {
		if secret, err = s.OpenWebhookSecret(target.Installation, target.Sealed); err != nil {
			s.opts.Log.Warn("the sealed Stator webhook secret could not be opened", "error", err)
			secret, target = absentSecret, nil
		}
	}
	if !hmac.Equal([]byte(Sign(body, secret)), []byte(signature)) || target == nil {
		return ErrBadSignature
	}

	var env WebhookEnvelope
	if err := json.Unmarshal(body, &env); err != nil || env.ID == uuid.Nil {
		s.opts.Log.Info("a signed webhook delivery is not Stator's envelope; ignored", "error", err)
		return nil
	}
	if env.Version != 0 && env.Version != WebhookVersion {
		s.opts.Log.Warn("Stator sent a webhook envelope of a version Exciter does not read", "version", env.Version, "event", env.ID)
		return ErrUnknownVersion
	}
	if s.cache.Delivered(ctx, env.ID) {
		return nil
	}
	if err := s.act(ctx, target.BaseURL, env); err != nil {
		return err
	}
	s.cache.NoteDelivered(ctx, env.ID)
	return nil
}

// act does what one topic says; a topic Exciter does not act on, ping among them, does nothing.
func (s *Service) act(ctx context.Context, baseURL string, env WebhookEnvelope) error {
	var p struct {
		Page WebhookPage `json:"page"`
	}
	if len(env.Payload) > 0 {
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			s.opts.Log.Info("a webhook's payload does not read; ignored", "event", env.ID, "topic", env.Topic, "error", err)
			return nil
		}
	}
	page := p.Page
	var write func(ctx context.Context, tx db.DBTX) error
	switch env.Topic {
	case TopicPagePublished:
		write = func(ctx context.Context, tx db.DBTX) error { return notePublished(ctx, tx, baseURL, page) }
	case TopicPageMoved:
		write = func(ctx context.Context, tx db.DBTX) error { return noteMoved(ctx, tx, baseURL, page) }
	case TopicPageDeleted:
		write = func(ctx context.Context, tx db.DBTX) error { return breakLinks(ctx, tx, page.ID) }
	default:
		// The remote links' removal is #42's, which puts them there.
		return nil
	}
	if page.ID == uuid.Nil {
		return nil
	}
	if _, err := s.db.WriteAdmin(ctx, write); err != nil {
		return fmt.Errorf("act on the webhook %s: %w", env.Topic, err)
	}
	s.cache.ForgetPages(ctx, page.ID)
	s.cache.ForgetSearches(ctx)
	return nil
}

// notePublished stores a page's new version on its links, which turns each suspect whose confirmed one is older.
func notePublished(ctx context.Context, tx db.DBTX, baseURL string, page WebhookPage) error {
	if err := noteMoved(ctx, tx, baseURL, page); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE stator_page_link SET page_version = $2, page_version_seen = COALESCE(page_version_seen, $2)
		WHERE page_id = $1 AND (page_version IS NULL OR page_version < $2)`, page.ID, page.Version)
	return err
}

// noteMoved follows a page to its space now, under the connection's address rather than the one Stator writes.
func noteMoved(ctx context.Context, tx db.DBTX, baseURL string, page WebhookPage) error {
	if page.Space.Key == "" || len(page.Space.Key) > MaxSpaceKeyLength {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE stator_page_link SET space_key = $2, url = $3
		WHERE page_id = $1 AND (space_key <> $2 OR url <> $3)`, page.ID, page.Space.Key, PageURL(baseURL, page.Space.Key, page.ID))
	return err
}

// breakLinks marks the links to a page Stator deleted; they stay until somebody removes them.
func breakLinks(ctx context.Context, tx db.DBTX, pageID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE stator_page_link SET broken_at = now() WHERE page_id = $1 AND broken_at IS NULL`, pageID)
	return err
}

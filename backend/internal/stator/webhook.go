package stator

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// Stator's webhooks (#41), as Armature's receiver: a delivery the installation's secret vouches for clears what it
// announces and writes what it says of linked pages.

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

// WebhookTypePrefix leads the type of Stator's version 2 envelope, the topic after it (Stator #434).
const WebhookTypePrefix = "dev.stator."

// WebhookEnvelope is Stator's webhook.Envelope, Armature's shape, for the API document; webhook.Decode reads it.
// Stator sends no version until its #412.
type WebhookEnvelope struct {
	Version    int             `json:"version,omitempty"`
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
func (s *Service) Webhook(ctx context.Context, body []byte, signature, version string) error {
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

	env, err := webhook.Decode(body, version, WebhookTypePrefix)
	if errors.Is(err, webhook.ErrUnknownVersion) {
		s.opts.Log.Warn("Stator sent a webhook envelope of a version Exciter does not read", "error", err)
		return ErrUnknownVersion
	}
	if err != nil {
		s.opts.Log.Info("a signed webhook delivery is not Stator's envelope; ignored", "error", err)
		return nil
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
func (s *Service) act(ctx context.Context, baseURL string, env *webhook.Inbound) error {
	var p struct {
		Page       WebhookPage `json:"page"`
		RemoteLink struct {
			ID  uuid.UUID `json:"id"`
			URL string    `json:"url"`
		} `json:"remoteLink"`
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
	case TopicRemoteLinkRemoved:
		requirementID, ours := s.requirementOf(p.RemoteLink.URL)
		if !ours || page.ID == uuid.Nil {
			return nil
		}
		removed := p.RemoteLink.ID
		// The page itself did not change, so what is cached of it stands.
		if _, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			return detachLinks(ctx, tx, requirementID, page.ID, removed)
		}); err != nil {
			return fmt.Errorf("act on the webhook %s: %w", env.Topic, err)
		}
		return nil
	default:
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

// breakLinks marks the links to a page Stator deleted; they stay until somebody removes them. The remote links on it
// go to its trash with it, so there is nothing left for a sync to take off.
func breakLinks(ctx context.Context, tx db.DBTX, pageID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE stator_page_link SET broken_at = now() WHERE page_id = $1 AND broken_at IS NULL`, pageID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM stator_remote_link WHERE page_id = $1`, pageID)
	return err
}

// requirementOf reads a remote link's address as one of this installation's requirements.
func (s *Service) requirementOf(raw string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(raw, strings.TrimSuffix(s.opts.AppURL, "/")+RequirementPath)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	return id, err == nil
}

// detachLinks marks a requirement's links to a page once somebody took Exciter's link off it, so Stator's removal
// wins. The worker takes off only links it no longer wants, so a removal of a wanted one is a person's.
func detachLinks(ctx context.Context, tx db.DBTX, requirementID, pageID, remoteID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		DELETE FROM stator_remote_link rl
		WHERE rl.requirement_id = $1 AND rl.page_id = $2 AND rl.remote_link_id = $3
		  AND EXISTS (SELECT 1 FROM stator_page_link l JOIN requirement r ON r.id = l.requirement_id
		              WHERE l.requirement_id = $1 AND l.page_id = $2 AND r.trashed_at IS NULL
		                AND l.broken_at IS NULL AND l.detached_at IS NULL)`,
		requirementID, pageID, remoteID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE stator_page_link SET detached_at = now()
		WHERE requirement_id = $1 AND page_id = $2 AND detached_at IS NULL`, requirementID, pageID)
	return err
}

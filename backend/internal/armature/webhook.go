package armature

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/webhook"
)

// Armature's webhooks (#29), Stator's receiver: a delivery the installation's secret vouches for clears what it
// announces and writes what it says of linked issues.

var (
	// ErrBadSignature is any delivery that cannot be trusted, one to an installation without a secret included.
	ErrBadSignature = errors.New("the webhook signature does not match")
	// ErrUnknownVersion is an envelope of a version Exciter cannot read, refused so Armature keeps it and retries.
	ErrUnknownVersion = errors.New("the webhook envelope is of a version Exciter does not read")
)

// Webhook topics Exciter acts on; WebhookTopics lists the ones an administrator subscribes to.
const (
	TopicIssueCreated      = "issue.created"
	TopicIssueUpdated      = "issue.updated"
	TopicIssueTransitioned = "issue.transitioned"
	TopicIssueDeleted      = "issue.deleted"
	TopicRemoteLinkRemoved = "issue.remote_link.removed"
)

// Sign is Armature's webhook.Sign: sha256= and the lower case hex HMAC-SHA256 of the raw body under the secret.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// SignTimestamped is Armature's webhook.SignTimestamped: the signature of "{timestamp}.{body}".
func SignTimestamped(body []byte, secret, timestamp string) string {
	return Sign(append([]byte(timestamp+"."), body...), secret)
}

// WebhookHeaders are a delivery's signatures and version as they arrived; an older Armature sends neither the
// timestamped pair nor the version.
type WebhookHeaders struct {
	Signature            string
	Timestamp            string
	TimestampedSignature string
	Version              string
}

// Verify says whether the headers vouch for body under secret. Retries resend the stored body, so the timestamp
// proves the signature and is not judged by its age; the event id stops a replay instead.
func (h WebhookHeaders) Verify(body []byte, secret string) bool {
	ok := hmac.Equal([]byte(Sign(body, secret)), []byte(h.Signature))
	if h.Timestamp != "" || h.TimestampedSignature != "" {
		if _, err := strconv.ParseInt(h.Timestamp, decimal, int64Bits); err != nil {
			return false
		}
		ok = hmac.Equal([]byte(SignTimestamped(body, secret, h.Timestamp)), []byte(h.TimestampedSignature)) && ok
	}
	return ok
}

// decimal and int64Bits read the timestamp as Armature writes it, Unix seconds.
const (
	decimal   = 10
	int64Bits = 64
)

// absentSecret is checked against when there is no secret, so a delivery to an installation without one costs the
// same work as one with.
const absentSecret = WebhookSecretPrefix + "absent"

// WebhookTypePrefix leads the type of Armature's version 2 envelope, the topic after it (Armature #286).
const WebhookTypePrefix = "dev.armature."

// WebhookEnvelope is Armature's EnvelopeV1, for the API document; webhook.Decode reads it. An Armature before #242
// sends no version.
type WebhookEnvelope struct {
	Version int       `json:"version,omitempty"`
	ID      uuid.UUID `json:"id"`
	Topic   string    `json:"topic"`
	// OrgID is deprecated in Armature and leaves with version 2; while it comes, it has to be the connection's.
	OrgID      uuid.UUID       `json:"orgId,omitempty"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

// webhookPayload holds the fields of the topics Exciter acts on.
type webhookPayload struct {
	Key              string    `json:"key"`
	MovedFrom        string    `json:"movedFrom"`
	ToStatusCategory string    `json:"toStatusCategory"`
	LinkID           uuid.UUID `json:"linkId"`
	URL              string    `json:"url"`
}

type webhookTarget struct {
	Installation  uuid.UUID
	Sealed        []byte
	ArmatureOrgID *uuid.UUID
}

// Webhook verifies one delivery and acts on it; an event acted on before changes nothing. An error other than
// ErrBadSignature or ErrUnknownVersion is Exciter's, and Armature tries the delivery again later.
func (s *Service) Webhook(ctx context.Context, body []byte, headers WebhookHeaders) error {
	var target *webhookTarget
	// As the admin role: nobody signs in, and only the secret vouches for the body.
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var t webhookTarget
		err := tx.QueryRow(ctx, `
			SELECT (SELECT id FROM installation), webhook_secret, armature_org_id
			FROM armature_connection WHERE webhook_secret IS NOT NULL`).Scan(&t.Installation, &t.Sealed, &t.ArmatureOrgID)
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
			s.opts.Log.Warn("the sealed webhook secret could not be opened", "error", err)
			secret, target = absentSecret, nil
		}
	}
	if !headers.Verify(body, secret) || target == nil {
		return ErrBadSignature
	}

	env, err := webhook.Decode(body, headers.Version, WebhookTypePrefix)
	if errors.Is(err, webhook.ErrUnknownVersion) {
		s.opts.Log.Warn("Armature sent a webhook envelope of a version Exciter does not read", "error", err)
		return ErrUnknownVersion
	}
	if err != nil {
		s.opts.Log.Info("a signed webhook delivery is not Armature's envelope; ignored", "error", err)
		return nil
	}
	if env.OrgID != uuid.Nil && target.ArmatureOrgID != nil && env.OrgID != *target.ArmatureOrgID {
		return ErrBadSignature
	}
	if s.cache.Delivered(ctx, env.ID) {
		return nil
	}
	if err := s.act(ctx, env); err != nil {
		return err
	}
	s.cache.NoteDelivered(ctx, env.ID)
	return nil
}

// act does what one topic says; a topic Exciter does not act on, ping among them, does nothing.
func (s *Service) act(ctx context.Context, env *webhook.Inbound) error {
	var p webhookPayload
	if len(env.Payload) > 0 {
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			s.opts.Log.Info("a webhook's payload does not read; ignored", "event", env.ID, "topic", env.Topic, "error", err)
			return nil
		}
	}
	key, known := NormalizeKey(p.Key)
	if !known || !slices.Contains(WebhookTopics, env.Topic) {
		return nil
	}
	forget := []string{key}
	var write func(ctx context.Context, tx db.DBTX) error
	switch env.Topic {
	case TopicIssueUpdated:
		if old, moved := NormalizeKey(p.MovedFrom); moved && old != key {
			forget = append(forget, old)
			write = func(ctx context.Context, tx db.DBTX) error { return rekeyLinks(ctx, tx, old, key) }
		}
	case TopicIssueTransitioned:
		write = func(ctx context.Context, tx db.DBTX) error { return noteTransition(ctx, tx, key, p.ToStatusCategory) }
	case TopicIssueDeleted:
		write = func(ctx context.Context, tx db.DBTX) error { return breakLinks(ctx, tx, key) }
	case TopicRemoteLinkRemoved:
		requirementID, ours := s.requirementOf(p.URL)
		if !ours {
			return nil
		}
		write = func(ctx context.Context, tx db.DBTX) error { return detachLinks(ctx, tx, requirementID, key, p.LinkID) }
	}
	if write != nil {
		if _, err := s.db.WriteAdmin(ctx, write); err != nil {
			return fmt.Errorf("act on the webhook %s: %w", env.Topic, err)
		}
	}
	if write != nil {
		s.forgetCoverage(ctx, key)
	}
	if env.Topic != TopicRemoteLinkRemoved {
		s.cache.ForgetIssues(ctx, forget...)
		s.cache.ForgetSearches(ctx)
	}
	return nil
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

// rekeyLinks follows an issue moved to another project: its links and their sync records take the new key, and
// one that would repeat a link the requirement has under the new key already goes.
func rekeyLinks(ctx context.Context, tx db.DBTX, old, now string) error {
	for _, step := range []string{
		`DELETE FROM armature_issue_link o WHERE o.issue_key = $1 AND EXISTS (
		    SELECT 1 FROM armature_issue_link n
		    WHERE n.issue_key = $2 AND n.requirement_id = o.requirement_id AND n.relation = o.relation)`,
		`UPDATE armature_issue_link SET issue_key = $2 WHERE issue_key = $1`,
		`DELETE FROM armature_remote_link o WHERE o.issue_key = $1 AND EXISTS (
		    SELECT 1 FROM armature_remote_link n WHERE n.issue_key = $2 AND n.requirement_id = o.requirement_id)`,
		`UPDATE armature_remote_link SET issue_key = $2 WHERE issue_key = $1`,
	} {
		if _, err := tx.Exec(ctx, step, old, now); err != nil {
			return err
		}
	}
	return nil
}

// noteTransition stores the category an issue moved to, or marks the stored one stale when Armature did not say.
func noteTransition(ctx context.Context, tx db.DBTX, key, category string) error {
	if !slices.Contains(StatusCategories, category) {
		_, err := tx.Exec(ctx, `UPDATE armature_issue_link SET status_stale = true WHERE issue_key = $1 AND NOT status_stale`, key)
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE armature_issue_link SET status_category = $2, status_stale = false
		WHERE issue_key = $1 AND (status_category IS DISTINCT FROM $2 OR status_stale)`, key, category)
	return err
}

// breakLinks marks the links to a deleted issue; the links Exciter put on it went with it.
func breakLinks(ctx context.Context, tx db.DBTX, key string) error {
	if _, err := tx.Exec(ctx, `UPDATE armature_issue_link SET broken_at = now() WHERE issue_key = $1 AND broken_at IS NULL`, key); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM armature_remote_link WHERE issue_key = $1`, key)
	return err
}

// detachLinks marks a requirement's links to an issue once somebody took Exciter's link off it, so Armature's
// removal wins. The worker takes off only links it no longer wants, so a removal of a wanted one is a person's.
func detachLinks(ctx context.Context, tx db.DBTX, requirementID uuid.UUID, key string, remoteID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		DELETE FROM armature_remote_link rl
		WHERE rl.requirement_id = $1 AND rl.issue_key = $2 AND rl.remote_link_id = $3
		  AND EXISTS (SELECT 1 FROM armature_issue_link l JOIN requirement r ON r.id = l.requirement_id
		              WHERE l.requirement_id = $1 AND l.issue_key = $2 AND r.trashed_at IS NULL
		                AND l.broken_at IS NULL AND l.detached_at IS NULL)`,
		requirementID, key, remoteID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE armature_issue_link SET detached_at = now()
		WHERE requirement_id = $1 AND issue_key = $2 AND detached_at IS NULL`, requirementID, key)
	return err
}

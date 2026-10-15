package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/netguard"
)

const (
	// sendTimeout bounds one request; a slow receiver is a failed attempt.
	sendTimeout = 10 * time.Second
	// maxResponseBytes is the most of an answer that is read and dropped,
	// so the connection can be used again.
	maxResponseBytes = 4 << 10
	// ClaimLease is how long a claimed delivery is kept from other senders;
	// one that dies holding it delays the attempt by at most this much.
	ClaimLease = 2 * time.Minute
	// sendBatch is how many due deliveries one pass claims; passes repeat
	// until none is due, so a backlog is drained rather than nibbled.
	sendBatch = 100
	// SendInterval is how often the worker looks for due deliveries, and so
	// the most a first attempt waits.
	SendInterval = 2 * time.Second
	// DeliveryRetention is how long the log keeps an attempt, as Armature's.
	DeliveryRetention = 30 * 24 * time.Hour
	// pruneEvery is how often attempts past DeliveryRetention are deleted.
	pruneEvery = time.Hour
	// MaxCommentText bounds the words of a comment a payload carries.
	MaxCommentText = 1000
)

// The sentences a delivery's log shows for what stopped it.
const (
	whyBlocked    = "This address is inside the server's own network, so Exciter may not post to it. Use the receiver's public address, or ask the operator to add the host to EXCITER_OUTBOUND_ALLOW."
	whyTimeout    = "The receiver did not answer in time. Check that it is running, then send it again."
	whyUnreached  = "The receiver could not be reached. Check the address, then send it again."
	whyNoSecret   = "The webhook's secret does not open with this server's key. Rotate the secret and give the receiver the new one."
	whyOff        = "The webhook was turned off before this was sent."
	whyNoOwner    = "Nothing was sent: the administrator who saved this webhook has left or was switched off. Save it again to send as yourself."
	whyNotVisible = "Nothing was sent: the webhook's owner may not view this requirement, or it no longer exists."
	whyVersion    = "Nothing was sent: this delivery is in envelope version %d, which this Exciter does not send. Register the webhook for version %s; the events after that are sent in it."
)

// errWithheld is a payload the owner could not read; nothing is sent.
var errWithheld = errors.New("withheld")

// Handle is the outbox's half: every enabled endpoint subscribed to the topic, and listening to a project the event
// is about or to every project, gets a delivery due now. A second run for the same event adds none.
func (s *Service) Handle(ctx context.Context, e events.Event) error {
	if !slices.Contains(Topics, e.Topic) {
		return nil
	}
	occurred := e.CreatedAt
	if occurred.IsZero() {
		occurred = s.now()
	}
	payload := e.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	var in emitted
	if err := json.Unmarshal(payload, &in); err != nil {
		return fmt.Errorf("read the %s event: %w", e.Topic, err)
	}
	ctx = db.WithUser(ctx, uuid.Nil)
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// The projects an event is about: its requirement's, both ends of a link, or the project it names.
		_, err := tx.Exec(ctx, `
			WITH about AS (
			    SELECT array_agg(DISTINCT x) AS projects FROM (
			        SELECT r.project_id AS x FROM requirement r WHERE r.id = ANY ($6::uuid[])
			        UNION SELECT $7::uuid WHERE $7::uuid IS NOT NULL) found
			)
			INSERT INTO webhook_delivery (endpoint_id, event_id, topic, event, occurred_at, next_attempt_at, version)
			SELECT e.id, $1, $2, $3, $4, now(), e.version FROM webhook_endpoint e, about
			WHERE e.enabled AND ($2 = ANY (e.topics) OR $5 = ANY (e.topics))
			  AND (cardinality(e.project_ids) = 0 OR e.project_ids && COALESCE(about.projects, '{}'))
			ON CONFLICT (endpoint_id, event_id, attempt) DO NOTHING`,
			e.ID, e.Topic, payload, occurred, TopicAny, in.requirements(), in.ProjectID)
		return err
	})
	return err
}

// SendDue posts every delivery whose time has come, and says how many were
// tried. Each is leased first, so senders in several workers never post one
// twice.
func (s *Service) SendDue(ctx context.Context) (int, error) {
	tried := 0
	for {
		var found []uuid.UUID
		_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			rows, err := tx.Query(ctx, `
				UPDATE webhook_delivery d SET next_attempt_at = now() + make_interval(secs => $2)
				FROM (
				    SELECT id FROM webhook_delivery
				    WHERE state = 'pending' AND next_attempt_at <= now()
				    ORDER BY next_attempt_at
				    LIMIT $1
				    FOR UPDATE SKIP LOCKED
				) claimed
				WHERE d.id = claimed.id
				RETURNING d.id`, sendBatch, ClaimLease.Seconds())
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err != nil {
					return err
				}
				found = append(found, id)
			}
			return rows.Err()
		})
		if err != nil {
			return tried, err
		}
		for _, id := range found {
			if ctx.Err() != nil {
				return tried, nil
			}
			if _, _, err := s.attempt(ctx, id); err != nil {
				s.opts.Log.Warn("a webhook attempt could not be recorded", "delivery", id, "error", err)
			}
		}
		tried += len(found)
		if len(found) < sendBatch || ctx.Err() != nil {
			return tried, nil
		}
	}
}

// Prune deletes the attempts past DeliveryRetention.
func (s *Service) Prune(ctx context.Context) (int64, error) {
	var gone int64
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM webhook_delivery WHERE created_at < now() - make_interval(secs => $1) AND state <> 'pending'`,
			DeliveryRetention.Seconds())
		gone = tag.RowsAffected()
		return err
	})
	return gone, err
}

// Test posts a ping to the endpoint now and returns the attempt as logged.
// It is sent even while the endpoint is off, to check it before turning it on.
func (s *Service) Test(ctx context.Context, id uuid.UUID) (*WebhookDelivery, db.LSN, error) {
	if err := s.visible(ctx, id); err != nil {
		return nil, 0, err
	}
	var made uuid.UUID
	system := asSystem(ctx)
	lsn, err := s.db.WriteAdmin(system, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO webhook_delivery (endpoint_id, event_id, topic, event, occurred_at, manual, next_attempt_at, version)
			SELECT id, $2, $3, '{}', now(), true, now() + make_interval(secs => $4), version
			FROM webhook_endpoint WHERE id = $1
			RETURNING id`, id, uuid.New(), TopicPing, ClaimLease.Seconds()).Scan(&made)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	sent, recorded, err := s.attempt(system, made)
	return sent, max(lsn, recorded), err
}

// Redeliver tries a logged delivery's event again, now, as the next attempt.
// The payload is read afresh, as the owner may view it now.
func (s *Service) Redeliver(ctx context.Context, webhookID, deliveryID uuid.UUID) (*WebhookDelivery, db.LSN, error) {
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var found bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_delivery WHERE id = $1 AND endpoint_id = $2)`, deliveryID, webhookID).Scan(&found)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	var made uuid.UUID
	system := asSystem(ctx)
	lsn, err := s.db.WriteAdmin(system, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO webhook_delivery (endpoint_id, event_id, topic, event, occurred_at, attempt, manual, next_attempt_at, version)
			SELECT d.endpoint_id, d.event_id, d.topic, d.event, d.occurred_at,
			       (SELECT max(attempt) FROM webhook_delivery x WHERE x.endpoint_id = d.endpoint_id AND x.event_id = d.event_id) + 1,
			       true, now() + make_interval(secs => $2), d.version
			FROM webhook_delivery d WHERE d.id = $1
			RETURNING id`, deliveryID, ClaimLease.Seconds()).Scan(&made)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	sent, recorded, err := s.attempt(system, made)
	return sent, max(lsn, recorded), err
}

// visible says whether the caller may see the endpoint, through the policies. It reads the primary, as the write it
// guards does, so an endpoint another administrator just made is found.
func (s *Service) visible(ctx context.Context, id uuid.UUID) error {
	return s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		var found bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_endpoint WHERE id = $1)`, id).Scan(&found)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
}

// asSystem is ctx acting for nobody: the log and the counts are the
// worker's, and a request's caller must not become the endpoint's owner by
// sending a test.
func asSystem(ctx context.Context) context.Context {
	return db.PinPrimary(db.WithUser(ctx, uuid.Nil))
}

// claimed is a delivery about to be attempted, with its endpoint. installationID is what the endpoint's secret was
// sealed with.
type claimed struct {
	installationID, endpointID, eventID uuid.UUID
	url, topic                          string
	sealed                              []byte
	enabled, manual                     bool
	owner                               *uuid.UUID
	event                               json.RawMessage
	occurred                            time.Time
	attempt                             int
	state                               DeliveryState
	// version is the envelope the delivery was queued in, which its retries
	// and redeliveries keep whatever the endpoint is on by then.
	version int
}

// outcome is how one attempt went.
type outcome struct {
	state  DeliveryState
	status *int
	why    string
	// counts says whether it moves the endpoint's count of failures.
	counts bool
}

// attempt posts one logged delivery and records how it went: delivered,
// withheld, cancelled, or failed with the next attempt due later.
func (s *Service) attempt(ctx context.Context, id uuid.UUID) (*WebhookDelivery, db.LSN, error) {
	ctx = asSystem(ctx)
	var c claimed
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT (SELECT id FROM installation), d.endpoint_id, d.event_id, e.url, d.topic, e.secret_sealed, e.enabled, d.manual,
			       -- An owner switched off has left as much as one deleted: nothing is read as them.
			       CASE WHEN u.is_active AND u.role IS NOT NULL THEN e.owner_id END,
			       d.event, d.occurred_at, d.attempt, d.state, d.version
			FROM webhook_delivery d JOIN webhook_endpoint e ON e.id = d.endpoint_id LEFT JOIN app_user u ON u.id = e.owner_id
			WHERE d.id = $1`, id).
			Scan(&c.installationID, &c.endpointID, &c.eventID, &c.url, &c.topic, &c.sealed, &c.enabled, &c.manual, &c.owner,
				&c.event, &c.occurred, &c.attempt, &c.state, &c.version)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	if c.state != StatePending {
		done, err := s.read(ctx, id)
		return done, 0, err
	}
	return s.record(ctx, id, c, s.send(ctx, id, c))
}

// send builds the body as the owner may read it, signs it and posts it.
func (s *Service) send(ctx context.Context, id uuid.UUID, c claimed) outcome {
	if !c.enabled && !c.manual {
		return outcome{state: StateCancelled, why: whyOff}
	}
	body, err := s.body(ctx, c)
	if errors.Is(err, errWithheld) {
		why := whyNotVisible
		if c.owner == nil {
			why = whyNoOwner
		}
		return outcome{state: StateWithheld, why: why}
	}
	// Only a later release, taken back, leaves a delivery in a version this
	// one cannot build. Every attempt would fail alike, so it is neither
	// retried nor counted against the endpoint.
	if errors.Is(err, ErrUnknownVersion) {
		s.opts.Log.Warn("a webhook delivery is in an envelope version this release does not send", "delivery", id, "version", c.version)
		return outcome{state: StateFailed, why: fmt.Sprintf(whyVersion, c.version, versionList())}
	}
	if err != nil {
		s.opts.Log.Warn("a webhook payload could not be read", "delivery", id, "error", err)
		return outcome{state: StateFailed, why: whyUnreached, counts: true}
	}
	plain, err := s.box.Open(c.sealed, sealContext(c.installationID, c.endpointID))
	if err != nil {
		s.opts.Log.Warn("a webhook secret could not be opened", "webhook", c.endpointID, "error", err)
		return outcome{state: StateFailed, why: whyNoSecret, counts: true}
	}
	status, err := s.post(ctx, c.url, string(plain), c.topic, c.version, id, body)
	if err != nil {
		s.opts.Log.Info("a webhook delivery did not reach its receiver", "delivery", id, "error", err)
		return outcome{state: StateFailed, why: why(err), counts: true}
	}
	if status < 200 || status > 299 {
		return outcome{state: StateFailed, status: &status, why: fmt.Sprintf("The receiver answered %d. Check its log, then send it again.", status), counts: true}
	}
	return outcome{state: StateDelivered, status: &status}
}

// why says what stopped a request in a sentence. The error itself names
// hosts and ports this server can reach, which the log may say and a
// delivery row may not.
func why(err error) string {
	switch {
	case errors.Is(err, netguard.ErrBlocked):
		return whyBlocked
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "timeout"):
		return whyTimeout
	default:
		return whyUnreached
	}
}

// record writes how an attempt went, schedules the next after a failure,
// and turns the endpoint off once it has failed for DisableAfter.
func (s *Service) record(ctx context.Context, id uuid.UUID, c claimed, o outcome) (*WebhookDelivery, db.LSN, error) {
	var out *WebhookDelivery
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			UPDATE webhook_delivery
			SET state = $2, status = $3, error = $4, next_attempt_at = NULL, attempted_at = now(),
			    delivered_at = CASE WHEN $2 = 'delivered' THEN now() END
			WHERE id = $1`, id, o.state, o.status, o.why); err != nil {
			return err
		}
		switch {
		case o.state == StateDelivered:
			if _, err := tx.Exec(ctx, `UPDATE webhook_endpoint SET failures = 0, failing_since = NULL WHERE id = $1`, c.endpointID); err != nil {
				return err
			}
		case o.counts && !c.manual:
			disabled, err := s.countFailure(ctx, tx, c)
			if err != nil {
				return err
			}
			if !disabled && c.attempt < MaxAttempts {
				if _, err := tx.Exec(ctx, `
					INSERT INTO webhook_delivery (endpoint_id, event_id, topic, event, occurred_at, attempt, next_attempt_at, version)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
					ON CONFLICT (endpoint_id, event_id, attempt) DO NOTHING`,
					c.endpointID, c.eventID, c.topic, c.event, c.occurred, c.attempt+1, s.now().Add(NextWait(c.attempt)), c.version); err != nil {
					return err
				}
			}
		}
		var err error
		out, err = scanDelivery(tx.QueryRow(ctx, selectDeliveries+` WHERE id = $1`, id))
		return err
	})
	return out, lsn, err
}

// NextWait is how long after a failed attempt the next one is due.
func NextWait(attempt int) time.Duration {
	return Backoff[min(max(attempt, 1)-1, len(Backoff)-1)]
}

// ShouldDisable says whether an endpoint that has failed failures times in a
// row, since since, has failed long enough to be turned off at now.
func ShouldDisable(failures int, since, now time.Time) bool {
	return failures >= DisableAfterFailures && !since.After(now.Add(-DisableAfter))
}

// countFailure adds a failure to the endpoint and turns it off when it has
// failed long enough, cancelling what was still due and noting it in the
// audit log with nobody as the actor.
func (s *Service) countFailure(ctx context.Context, tx db.DBTX, c claimed) (bool, error) {
	var (
		failures int
		since    time.Time
		enabled  bool
		name     string
	)
	err := tx.QueryRow(ctx, `
		UPDATE webhook_endpoint SET failures = failures + 1, failing_since = COALESCE(failing_since, now())
		WHERE id = $1
		RETURNING failures, failing_since, enabled, name`, c.endpointID).Scan(&failures, &since, &enabled, &name)
	if err != nil {
		return false, err
	}
	if !enabled {
		return true, nil
	}
	if !ShouldDisable(failures, since, s.now()) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE webhook_endpoint SET enabled = false, disabled_reason = $2 WHERE id = $1`,
		c.endpointID, ReasonFailing); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE webhook_delivery SET state = 'cancelled', error = $2, next_attempt_at = NULL
		WHERE endpoint_id = $1 AND state = 'pending' AND NOT manual`, c.endpointID, whyOff); err != nil {
		return false, err
	}
	id := c.endpointID
	return true, audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWebhookDisabled, TargetType: "webhook", TargetID: &id,
		Data: map[string]any{"name": name, "reason": ReasonFailing, "failures": failures}})
}

func (s *Service) read(ctx context.Context, id uuid.UUID) (*WebhookDelivery, error) {
	var out *WebhookDelivery
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanDelivery(tx.QueryRow(ctx, selectDeliveries+` WHERE id = $1`, id))
		return err
	})
	return out, err
}

// post sends one request and returns the status, or the error that stopped it.
func (s *Service) post(ctx context.Context, address, secret, topic string, version int, deliveryID uuid.UUID, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(EventHeader, topic)
	req.Header.Set(DeliveryHeader, deliveryID.String())
	req.Header.Set(VersionHeader, strconv.Itoa(version))
	// Signed as sent, so a retry or a redelivery carries its own time.
	sent := s.now()
	req.Header.Set(SignatureHeader, Sign(body, secret))
	req.Header.Set(TimestampHeader, strconv.FormatInt(sent.Unix(), 10))
	req.Header.Set(TimestampedSignatureHeader, SignTimestamped(body, secret, sent))
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	return resp.StatusCode, nil
}

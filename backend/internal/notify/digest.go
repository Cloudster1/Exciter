package notify

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/mail"
)

// DigestInterval is how often the queue is looked at; well inside an hour so an hourly bundle goes out close to the
// hour.
const DigestInterval = time.Minute

// Due says whether a bundle whose oldest row came at oldest goes now: hourly at the next full hour, daily at
// DailyDigestHour UTC, and a leftover at once.
func Due(digest Digest, oldest, now time.Time) bool {
	oldest, now = oldest.UTC(), now.UTC()
	switch digest {
	case DigestHourly:
		return !now.Before(oldest.Truncate(time.Hour).Add(time.Hour))
	case DigestDaily:
		next := time.Date(oldest.Year(), oldest.Month(), oldest.Day(), DailyDigestHour, 0, 0, 0, time.UTC)
		if !next.After(oldest) {
			next = next.AddDate(0, 0, 1)
		}
		return !now.Before(next)
	}
	return true
}

// Digester mails each person's queue as one bundle on their schedule, read from the rows so several workers and
// restarts never send one twice.
type Digester struct {
	db     *db.Cluster
	mailer mail.Mailer
	appURL string
	log    *slog.Logger
	now    func() time.Time
}

func NewDigester(cluster *db.Cluster, mailer mail.Mailer, appURL string, log *slog.Logger) *Digester {
	return &Digester{db: cluster, mailer: mailer, appURL: strings.TrimRight(appURL, "/"), log: log, now: time.Now}
}

// WithClock replaces the clock, for a test that cannot wait for eight o'clock.
func (d *Digester) WithClock(now func() time.Time) *Digester {
	d.now = now
	return d
}

// Run sends what is due every DigestInterval until ctx ends.
func (d *Digester) Run(ctx context.Context) {
	ticker := time.NewTicker(DigestInterval)
	defer ticker.Stop()
	for {
		if n, err := d.Once(ctx); err != nil && ctx.Err() == nil {
			d.log.Warn("digests could not be sent", "error", err)
		} else if n > 0 {
			d.log.Info("digests sent", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Once sends every bundle that is due and says how many went. Finding them crosses people; each is read and cleared
// acting for its person.
func (d *Digester) Once(ctx context.Context) (int, error) {
	type waiting struct {
		user   uuid.UUID
		digest Digest
		oldest time.Time
	}
	var found []waiting
	err := d.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT q.user_id, COALESCE(p.digest, 'off'), min(n.created_at)
			FROM notification_digest q
			JOIN notification n ON n.id = q.notification_id
			LEFT JOIN notification_preference p ON p.user_id = q.user_id
			GROUP BY q.user_id, p.digest
			ORDER BY q.user_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var w waiting
			if err := rows.Scan(&w.user, &w.digest, &w.oldest); err != nil {
				return err
			}
			found = append(found, w)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	now := d.now()
	sent := 0
	for _, w := range found {
		if !Due(w.digest, w.oldest, now) {
			continue
		}
		ok, err := d.send(db.WithUser(db.PinPrimary(ctx), w.user), w.user)
		if err != nil {
			d.log.Warn("a digest could not be sent", "user", w.user, "error", err)
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

// send takes the person's queue and mails what is still unread on requirements they may still view, in the one
// transaction: a failed send leaves the queue.
func (d *Digester) send(ctx context.Context, userID uuid.UUID) (bool, error) {
	sent := false
	_, err := d.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		ids, err := people(ctx, tx, `DELETE FROM notification_digest WHERE user_id = $1 RETURNING notification_id`, userID)
		if err != nil || len(ids) == 0 {
			return err
		}
		listed, err := tx.Query(ctx, selectNotifications+` AND n.read_at IS NULL AND n.id = ANY($2) ORDER BY n.created_at, n.id`, userID, ids)
		if err != nil {
			return err
		}
		items, err := scanNotifications(listed)
		if err != nil || len(items) == 0 {
			return err
		}
		var to string
		if err := tx.QueryRow(ctx, `SELECT email FROM app_user WHERE id = $1`, userID).Scan(&to); err != nil {
			return err
		}
		words := make([]mailed, len(items))
		for i, n := range items {
			if n.Session != nil {
				id := n.Session.ID
				words[i] = mailed{kind: n.Kind, actor: n.ActorName, key: n.Session.ProjectKey, title: n.Session.Title, session: &id}
				continue
			}
			words[i] = mailed{kind: n.Kind, actor: n.ActorName, requirementID: n.Requirement.ID, key: n.Requirement.Key,
				title: n.Requirement.Title, subject: Subject{RequirementID: n.Requirement.ID, Revision: n.Revision,
					ThreadID: n.ThreadID, CommentID: n.CommentID, ReviewID: n.ReviewID, LinkID: n.LinkID, Excerpt: n.Excerpt}}
		}
		if d.mailer != nil {
			if err := d.mailer.Send(ctx, bundle(to, d.appURL, words)); err != nil {
				return err
			}
		}
		sent = true
		return nil
	})
	return sent, err
}

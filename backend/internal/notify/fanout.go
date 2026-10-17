package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/codetrace"
	"github.com/cloudster1/exciter/backend/internal/comment"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/mail"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/review"
)

// Subject is what one event tells people about: a requirement at a revision, and a thread, comment, review or link
// of it.
type Subject struct {
	RequirementID uuid.UUID
	Revision      int
	ThreadID      *uuid.UUID
	CommentID     *uuid.UUID
	ReviewID      *uuid.UUID
	LinkID        *uuid.UUID
	// SessionID names a review session the event is about instead of a requirement (#204).
	SessionID *uuid.UUID
	// CodeScanID names a code scan the event is about instead of a requirement (#201).
	CodeScanID *uuid.UUID
	Excerpt    string
}

// Tell is one person to tell, and why. About, when set, is what they are told about instead of the plan's subject.
type Tell struct {
	UserID uuid.UUID
	Kind   Kind
	About  *Subject
}

// Plan is who hears about one event.
type Plan struct {
	Actor   uuid.UUID
	Subject Subject
	Tells   []Tell
}

// Planner reads what an event is about and who could hear of it, as the worker. A nil plan tells nobody.
type Planner func(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error)

// Pick keeps one kind per person, the first of Kinds that applies, and leaves out the actor, who is never told about
// their own act.
func Pick(actor uuid.UUID, candidates []Tell) []Tell {
	best := map[uuid.UUID]Tell{}
	var order []uuid.UUID
	for _, c := range candidates {
		if c.UserID == actor || c.UserID == uuid.Nil {
			continue
		}
		rank := slices.Index(Kinds, c.Kind)
		if rank < 0 {
			continue
		}
		if prev, seen := best[c.UserID]; !seen {
			best[c.UserID] = c
			order = append(order, c.UserID)
		} else if rank < slices.Index(Kinds, prev.Kind) {
			best[c.UserID] = c
		}
	}
	out := make([]Tell, len(order))
	for i, id := range order {
		out[i] = best[id]
	}
	return out
}

// FanOut runs in the worker: each event becomes a row per person told, and a mail for those who want one, at once
// or in their digest.
type FanOut struct {
	db       *db.Cluster
	mailer   mail.Mailer
	appURL   string
	log      *slog.Logger
	planners map[string]Planner
}

// NewFanOut builds the handler. A nil mailer writes the rows alone.
func NewFanOut(cluster *db.Cluster, mailer mail.Mailer, appURL string, log *slog.Logger) *FanOut {
	return &FanOut{db: cluster, mailer: mailer, appURL: strings.TrimRight(appURL, "/"), log: log, planners: map[string]Planner{
		comment.TopicCreated:        planCommentCreated,
		comment.TopicEdited:         planCommentEdited,
		comment.TopicResolved:       planThreadResolved,
		comment.TopicReopened:       planThreadResolved,
		requirement.TopicRevised:    planRevised,
		requirement.TopicChanged:    planTransitioned,
		review.TopicRequested:       planReviewRequested,
		review.TopicDecided:         planReviewDecided,
		review.TopicSessionOpened:   planSession(KindSessionInvited, false),
		review.TopicSessionRound:    planSession(KindSessionRound, false),
		review.TopicSessionReminder: planSession(KindSessionReminder, true),
		review.TopicSessionClosed:   planSession(KindSessionClosed, false),
		codetrace.TopicUploaded:     planCodeScan,
	}}
}

// Plan adds or replaces who hears about a topic.
func (f *FanOut) Plan(topic string, planner Planner) *FanOut {
	f.planners[topic] = planner
	return f
}

// Topics are those the fan-out plans for, for the worker's routes.
func (f *FanOut) Topics() []string {
	out := make([]string, 0, len(f.planners))
	for topic := range f.planners {
		out = append(out, topic)
	}
	slices.Sort(out)
	return out
}

// Handle tells everybody an event concerns. Run twice with the same event, it finds every row already written and
// sends nothing again.
func (f *FanOut) Handle(ctx context.Context, e events.Event) error {
	planner, ok := f.planners[e.Topic]
	if !ok {
		return nil
	}
	ctx = db.PinPrimary(ctx)
	var plan *Plan
	err := f.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		plan, err = planner(ctx, tx, e)
		return err
	})
	if err != nil || plan == nil {
		return err
	}
	for _, t := range Pick(plan.Actor, plan.Tells) {
		if err := f.deliver(db.WithUser(ctx, t.UserID), e.ID, plan, t); err != nil {
			return fmt.Errorf("tell %s: %w", t.UserID, err)
		}
	}
	return nil
}

// deliver writes one row acting for its recipient, so the database refuses a row about a requirement they may not
// view, and mails or queues it as they prefer.
func (f *FanOut) deliver(ctx context.Context, eventID uuid.UUID, plan *Plan, t Tell) error {
	subject := plan.Subject
	if t.About != nil {
		subject = *t.About
	}
	var out *mail.Mail
	_, err := f.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			about      mailed
			projectKey string
			number     int
			err        error
		)
		switch {
		case subject.CodeScanID != nil:
			about.scan = subject.CodeScanID
			err = tx.QueryRow(ctx, `
				SELECT s.repository || ' ' || s.branch, p.key FROM code_scan s JOIN project p ON p.id = s.project_id
				WHERE s.id = $1 AND perm_project_holds(s.project_id, $2::uuid, 'view')`, *subject.CodeScanID, t.UserID).Scan(&about.title, &about.key)
		case subject.SessionID != nil:
			about.session = subject.SessionID
			err = tx.QueryRow(ctx, `
				SELECT s.title, p.key FROM review_session s JOIN project p ON p.id = s.project_id
				WHERE s.id = $1 AND perm_project_holds(s.project_id, $2::uuid, 'view')`, *subject.SessionID, t.UserID).Scan(&about.title, &about.key)
		default:
			err = tx.QueryRow(ctx, `
				SELECT r.id, r.title, p.key, r.number FROM requirement r JOIN project p ON p.id = r.project_id
				WHERE r.id = $1 AND r.trashed_at IS NULL AND perm_requirement_viewable(r.id, $2::uuid)`,
				subject.RequirementID, t.UserID).Scan(&about.requirementID, &about.title, &projectKey, &number)
			about.key = requirement.Key(projectKey, number)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		prefs, err := readPreferences(ctx, tx, t.UserID)
		if err != nil {
			return err
		}
		if !On(prefs.InApp, t.Kind) {
			return nil
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO notification (user_id, event_id, kind, actor_id, requirement_id, revision, thread_id, comment_id,
			                          review_id, link_id, excerpt, session_id, code_scan_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (event_id, user_id) DO NOTHING
			RETURNING id`,
			t.UserID, eventID, t.Kind, nullable(plan.Actor), nullable(subject.RequirementID), subject.Revision, subject.ThreadID,
			subject.CommentID, subject.ReviewID, subject.LinkID, subject.Excerpt, subject.SessionID, subject.CodeScanID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if f.mailer == nil || !On(prefs.Email, t.Kind) {
			return nil
		}
		if prefs.Digest != DigestOff {
			_, err := tx.Exec(ctx, `INSERT INTO notification_digest (user_id, notification_id) VALUES ($1, $2)`, t.UserID, id)
			return err
		}
		var to string
		if err := tx.QueryRow(ctx, `
			SELECT u.email, COALESCE((SELECT a.name FROM app_user a WHERE a.id = $2), '')
			FROM app_user u WHERE u.id = $1`, t.UserID, plan.Actor).Scan(&to, &about.actor); err != nil {
			return err
		}
		about.kind, about.subject = t.Kind, subject
		m := about.single(to, f.appURL)
		out = &m
		return nil
	})
	if err != nil || out == nil {
		return err
	}
	// The row is committed, so a failed send is logged rather than retried: a second run would find the row and
	// could not tell whether it went.
	if err := f.mailer.Send(ctx, *out); err != nil {
		f.log.Warn("a notification could not be mailed", "event", eventID, "user", t.UserID, "error", err)
	}
	return nil
}

func nullable(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

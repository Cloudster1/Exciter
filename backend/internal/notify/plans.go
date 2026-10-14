package notify

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// What the planners read of the events they plan for; see docs/api-contract-m3.md.
type (
	commentEvent struct {
		ActorID       uuid.UUID   `json:"actorId"`
		CommentID     uuid.UUID   `json:"commentId"`
		ThreadID      uuid.UUID   `json:"threadId"`
		RequirementID uuid.UUID   `json:"requirementId"`
		Reply         bool        `json:"reply"`
		Mentioned     []uuid.UUID `json:"mentioned"`
	}
	threadEvent struct {
		ActorID       uuid.UUID `json:"actorId"`
		ThreadID      uuid.UUID `json:"threadId"`
		RequirementID uuid.UUID `json:"requirementId"`
	}
	requirementEvent struct {
		ActorID       uuid.UUID `json:"actorId"`
		RequirementID uuid.UUID `json:"requirementId"`
		Revision      int       `json:"revision"`
		Field         string    `json:"field"`
	}
)

// commentWords are a live comment's words as an excerpt, and the revision it was written on.
func commentWords(ctx context.Context, tx db.DBTX, commentID uuid.UUID) (string, int, error) {
	var (
		body     []byte
		revision int
	)
	err := tx.QueryRow(ctx, `SELECT body, revision FROM comment WHERE id = $1 AND deleted_at IS NULL`, commentID).
		Scan(&body, &revision)
	if err != nil {
		return "", 0, err
	}
	root, err := document.ParseComment(body)
	if err != nil {
		return "", revision, nil
	}
	return Excerpt(document.PlainText(root)), revision, nil
}

// planCommentCreated tells the people the comment names, everybody who wrote in the thread when it is a reply, and
// the requirement's watchers. A comment deleted before the worker came to it tells nobody.
func planCommentCreated(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in commentEvent
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	excerpt, revision, err := commentWords(ctx, tx, in.CommentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	thread, comment := in.ThreadID, in.CommentID
	plan := &Plan{Actor: in.ActorID, Subject: Subject{RequirementID: in.RequirementID, Revision: revision, ThreadID: &thread,
		CommentID: &comment, Excerpt: excerpt}}
	for _, id := range in.Mentioned {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindMentioned})
	}
	if in.Reply {
		if err := tellThreadWriters(ctx, tx, plan, in.ThreadID, KindReplied); err != nil {
			return nil, err
		}
	}
	if err := tellWatchers(ctx, tx, plan, in.RequirementID, KindCommented); err != nil {
		return nil, err
	}
	return plan, nil
}

// planCommentEdited tells the people an edit named for the first time, leaving out anybody already told about the
// comment.
func planCommentEdited(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in commentEvent
	if err := json.Unmarshal(e.Payload, &in); err != nil || len(in.Mentioned) == 0 {
		return nil, nil
	}
	excerpt, revision, err := commentWords(ctx, tx, in.CommentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	told, err := people(ctx, tx, `SELECT user_id FROM notification WHERE comment_id = $1`, in.CommentID)
	if err != nil {
		return nil, err
	}
	thread, comment := in.ThreadID, in.CommentID
	plan := &Plan{Actor: in.ActorID, Subject: Subject{RequirementID: in.RequirementID, Revision: revision, ThreadID: &thread,
		CommentID: &comment, Excerpt: excerpt}}
	for _, id := range in.Mentioned {
		if !slices.Contains(told, id) {
			plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindMentioned})
		}
	}
	return plan, nil
}

// planThreadResolved tells everybody who wrote in a thread, unless it was wholly deleted meanwhile, that it was
// resolved or reopened, quoting its first comment while that stands.
func planThreadResolved(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in threadEvent
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var revision int
	err := tx.QueryRow(ctx, `
		SELECT r.revision FROM comment_thread t JOIN requirement r ON r.id = t.requirement_id
		WHERE t.id = $1 AND EXISTS (SELECT 1 FROM comment c WHERE c.thread_id = t.id AND c.deleted_at IS NULL)`,
		in.ThreadID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	excerpt, _, err := commentWords(ctx, tx, in.ThreadID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	thread := in.ThreadID
	plan := &Plan{Actor: in.ActorID, Subject: Subject{RequirementID: in.RequirementID, Revision: revision, ThreadID: &thread,
		Excerpt: excerpt}}
	if err := tellThreadWriters(ctx, tx, plan, in.ThreadID, KindResolved); err != nil {
		return nil, err
	}
	return plan, nil
}

// planRevised tells the requirement's watchers of a new revision, quoting its comment.
func planRevised(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in requirementEvent
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	var comment string
	err := tx.QueryRow(ctx, `SELECT comment FROM requirement_revision WHERE requirement_id = $1 AND number = $2`,
		in.RequirementID, in.Revision).Scan(&comment)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plan := &Plan{Actor: in.ActorID, Subject: Subject{RequirementID: in.RequirementID, Revision: in.Revision, Excerpt: Excerpt(comment)}}
	if err := tellWatchers(ctx, tx, plan, in.RequirementID, KindRevised); err != nil {
		return nil, err
	}
	return plan, nil
}

// planTransitioned tells the requirement's watchers that its status moved, naming the status it is in now. Every
// other change is left to the history.
func planTransitioned(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in requirementEvent
	if err := json.Unmarshal(e.Payload, &in); err != nil || in.Field != string(requirement.ChangeStatus) {
		return nil, nil
	}
	var (
		revision int
		status   string
	)
	err := tx.QueryRow(ctx, `
		SELECT r.revision, COALESCE(st.name, '') FROM requirement r LEFT JOIN req_status st ON st.id = r.status_id
		WHERE r.id = $1`, in.RequirementID).Scan(&revision, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plan := &Plan{Actor: in.ActorID, Subject: Subject{RequirementID: in.RequirementID, Revision: revision, Excerpt: Excerpt(status)}}
	if err := tellWatchers(ctx, tx, plan, in.RequirementID, KindTransitioned); err != nil {
		return nil, err
	}
	return plan, nil
}

func tellWatchers(ctx context.Context, tx db.DBTX, plan *Plan, requirementID uuid.UUID, kind Kind) error {
	watchers, err := people(ctx, tx, `SELECT user_id FROM requirement_watch_coverage($1)`, requirementID)
	for _, id := range watchers {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: kind})
	}
	return err
}

func tellThreadWriters(ctx context.Context, tx db.DBTX, plan *Plan, threadID uuid.UUID, kind Kind) error {
	writers, err := people(ctx, tx, `SELECT DISTINCT author_id FROM comment WHERE thread_id = $1 AND author_id IS NOT NULL`, threadID)
	for _, id := range writers {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: kind})
	}
	return err
}

func people(ctx context.Context, tx db.DBTX, sql string, args ...any) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

type reviewEvent struct {
	ActorID       uuid.UUID `json:"actorId"`
	ReviewID      uuid.UUID `json:"reviewId"`
	RequirementID uuid.UUID `json:"requirementId"`
	State         string    `json:"state"`
}

// reviewOf is what a notification of a review quotes and which revision it is about; nothing for a review gone.
func reviewOf(ctx context.Context, tx db.DBTX, id uuid.UUID) (string, int, error) {
	var (
		comment  string
		revision int
	)
	err := tx.QueryRow(ctx, `SELECT comment, revision FROM requirement_review WHERE id = $1`, id).Scan(&comment, &revision)
	return comment, revision, err
}

// planReviewRequested tells the approvers the review froze, every member of a group among them, but the revision's
// author when the rule keeps them from deciding. A review closed before the worker came to it tells nobody.
func planReviewRequested(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in reviewEvent
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return nil, nil
	}
	comment, revision, err := reviewOf(ctx, tx, in.ReviewID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	review := in.ReviewID
	plan := &Plan{Actor: in.ActorID, Subject: Subject{RequirementID: in.RequirementID, Revision: revision, ReviewID: &review, Excerpt: Excerpt(comment)}}
	approvers, err := people(ctx, tx, `
		SELECT DISTINCT a.user_id FROM (
			SELECT s.user_id FROM review_subject s WHERE s.review_id = $1 AND s.user_id IS NOT NULL
			UNION
			SELECT m.user_id FROM review_subject s JOIN group_member m ON m.group_id = s.group_id WHERE s.review_id = $1
		) a
		JOIN requirement_review v ON v.id = $1 AND v.state = 'pending'
		JOIN requirement_revision rv ON rv.requirement_id = v.requirement_id AND rv.number = v.revision
		WHERE v.author_may_approve OR rv.created_by IS DISTINCT FROM a.user_id`, in.ReviewID)
	for _, id := range approvers {
		plan.Tells = append(plan.Tells, Tell{UserID: id, Kind: KindReviewRequested})
	}
	return plan, err
}

// planReviewDecided tells whoever asked, and the revision's author, of a decision that closed the review, quoting
// what the deciding approver said.
func planReviewDecided(ctx context.Context, tx db.DBTX, e events.Event) (*Plan, error) {
	var in reviewEvent
	if err := json.Unmarshal(e.Payload, &in); err != nil || (in.State != "approved" && in.State != "rejected") {
		return nil, nil
	}
	var (
		revision        int
		asked, author   *uuid.UUID
		decisionComment string
	)
	err := tx.QueryRow(ctx, `
		SELECT v.revision, v.requested_by, rv.created_by,
		       COALESCE((SELECT d.comment FROM review_decision d WHERE d.review_id = v.id AND d.reviewer_id = $2), '')
		FROM requirement_review v
		JOIN requirement_revision rv ON rv.requirement_id = v.requirement_id AND rv.number = v.revision
		WHERE v.id = $1`, in.ReviewID, in.ActorID).Scan(&revision, &asked, &author, &decisionComment)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	review := in.ReviewID
	plan := &Plan{Actor: in.ActorID, Subject: Subject{RequirementID: in.RequirementID, Revision: revision, ReviewID: &review,
		Excerpt: Excerpt(decisionComment)}}
	for _, id := range []*uuid.UUID{asked, author} {
		if id != nil {
			plan.Tells = append(plan.Tells, Tell{UserID: *id, Kind: KindReviewed})
		}
	}
	return plan, nil
}

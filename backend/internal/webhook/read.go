package webhook

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
)

// payload reads what an event says as the owner. Whatever the owner may not view reads as pgx.ErrNoRows, as what is
// gone does, and the delivery is withheld.
func (s *Service) payload(ctx context.Context, tx db.DBTX, topic string, in emitted) (any, error) {
	actor := person(ctx, tx, in.ActorID)
	if actor != nil {
		actor.Via = in.Via
	}
	switch topic {
	case TopicLinkCreated, TopicLinkRemoved:
		link, err := s.link(ctx, tx, in.LinkID)
		if err != nil {
			return nil, err
		}
		return LinkEvent{Link: *link, Actor: actor}, nil
	case TopicBaselineTaken:
		b, err := s.baseline(ctx, tx, in.BaselineID)
		if err != nil {
			return nil, err
		}
		return BaselineTaken{Baseline: *b, Actor: actor}, nil
	}
	r, err := s.requirement(ctx, tx, in.RequirementID)
	if err != nil {
		return nil, err
	}
	switch topic {
	case TopicRequirementCreated, TopicRequirementTrashed, TopicRequirementRestored:
		return RequirementEvent{Requirement: *r, Actor: actor}, nil
	case TopicRequirementRevised:
		return RequirementRevised{Requirement: *r, Revision: in.Revision, Actor: actor}, nil
	case TopicRequirementChanged:
		return RequirementChanged{Requirement: *r, Field: in.Field, Actor: actor}, nil
	case TopicRequirementMoved:
		return RequirementMoved{Requirement: *r, FromKey: in.FromKey, ToKey: in.ToKey, Actor: actor}, nil
	case TopicLinkCleared:
		cleared := ClearedLink{}
		switch {
		case in.LinkID != nil:
			cleared = ClearedLink{Kind: ClearedTrace, ID: *in.LinkID}
		case in.ArmatureLinkID != nil:
			cleared = ClearedLink{Kind: ClearedArmature, ID: *in.ArmatureLinkID}
		case in.StatorLinkID != nil:
			cleared = ClearedLink{Kind: ClearedStator, ID: *in.StatorLinkID}
		default:
			return nil, pgx.ErrNoRows
		}
		return LinkCleared{Requirement: *r, Link: cleared, Actor: actor}, nil
	case TopicReviewRequested, TopicReviewDecided, TopicReviewClosed:
		review, err := s.review(ctx, tx, in.ReviewID)
		if err != nil {
			return nil, err
		}
		if topic == TopicReviewDecided {
			return ReviewDecided{Review: *review, Requirement: *r, Decision: in.Decision, Signed: in.Signed, Actor: actor}, nil
		}
		return ReviewEvent{Review: *review, Requirement: *r, Actor: actor}, nil
	case TopicCommentCreated:
		if in.CommentID == nil {
			return nil, pgx.ErrNoRows
		}
		c := CommentRef{Reply: in.Reply, URL: r.URL}
		if err := tx.QueryRow(ctx, `
			SELECT c.id, c.thread_id, left(COALESCE(requirement_plain_text(c.body), ''), $3)
			FROM comment c WHERE c.id = $1 AND c.requirement_id = $2 AND c.deleted_at IS NULL`,
			*in.CommentID, r.ID, MaxCommentText).Scan(&c.ID, &c.ThreadID, &c.Text); err != nil {
			return nil, err
		}
		return CommentCreated{Comment: c, Requirement: *r, Actor: actor}, nil
	case TopicThreadResolved:
		if in.ThreadID == nil {
			return nil, pgx.ErrNoRows
		}
		t := ThreadRef{URL: r.URL}
		if err := tx.QueryRow(ctx, `SELECT id FROM comment_thread WHERE id = $1 AND requirement_id = $2`, *in.ThreadID, r.ID).Scan(&t.ID); err != nil {
			return nil, err
		}
		return ThreadResolved{Thread: t, Requirement: *r, Actor: actor}, nil
	}
	return nil, pgx.ErrNoRows
}

// requirement reads a requirement as the owner, in the trash or out of it; one they may not view is pgx.ErrNoRows.
func (s *Service) requirement(ctx context.Context, tx db.DBTX, id *uuid.UUID) (*RequirementRef, error) {
	if id == nil {
		return nil, pgx.ErrNoRows
	}
	var (
		r                      RequirementRef
		statusName, statusKind *string
	)
	err := tx.QueryRow(ctx, `
		SELECT r.id, p.key || '-R' || r.number, r.kind, r.title, r.revision, st.name, st.category, p.key, p.name, r.trashed_at IS NOT NULL
		FROM requirement r JOIN project p ON p.id = r.project_id LEFT JOIN req_status st ON st.id = r.status_id
		WHERE r.id = $1`, *id).Scan(&r.ID, &r.Key, &r.Kind, &r.Title, &r.Revision, &statusName, &statusKind, &r.Project.Key, &r.Project.Name, &r.Trashed)
	if err != nil {
		return nil, err
	}
	if statusName != nil && statusKind != nil {
		r.Status = &StatusRef{Name: *statusName, Category: *statusKind}
	}
	r.URL = s.opts.AppURL + "/p/" + r.Project.Key + "/r/" + r.Key
	return &r, nil
}

// link reads a trace link as the owner, removed or not; an end they may not view is null, and a link of which they
// may view neither end is pgx.ErrNoRows.
func (s *Service) link(ctx context.Context, tx db.DBTX, id *uuid.UUID) (*LinkRef, error) {
	if id == nil {
		return nil, pgx.ErrNoRows
	}
	var (
		l              LinkRef
		source, target uuid.UUID
	)
	err := tx.QueryRow(ctx, `
		SELECT tl.id, lt.key, lt.name, lt.forward, lt.backward, tl.source_id, tl.target_id
		FROM trace_link tl JOIN link_type lt ON lt.id = tl.type_id WHERE tl.id = $1`, *id).
		Scan(&l.ID, &l.Type.Key, &l.Type.Name, &l.Type.Forward, &l.Type.Backward, &source, &target)
	if err != nil {
		return nil, err
	}
	end := func(id uuid.UUID) (*RequirementRef, error) {
		r, err := s.requirement(ctx, tx, &id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return r, err
	}
	if l.Source, err = end(source); err != nil {
		return nil, err
	}
	if l.Target, err = end(target); err != nil {
		return nil, err
	}
	if l.Source == nil && l.Target == nil {
		return nil, pgx.ErrNoRows
	}
	return &l, nil
}

func (s *Service) review(ctx context.Context, tx db.DBTX, id *uuid.UUID) (*ReviewRef, error) {
	if id == nil {
		return nil, pgx.ErrNoRows
	}
	var v ReviewRef
	err := tx.QueryRow(ctx, `SELECT id, revision, state FROM requirement_review WHERE id = $1`, *id).Scan(&v.ID, &v.Revision, &v.State)
	return &v, err
}

func (s *Service) baseline(ctx context.Context, tx db.DBTX, id *uuid.UUID) (*BaselineRef, error) {
	if id == nil {
		return nil, pgx.ErrNoRows
	}
	var b BaselineRef
	err := tx.QueryRow(ctx, `SELECT b.id, b.name, p.key, p.name FROM baseline b JOIN project p ON p.id = b.project_id WHERE b.id = $1`, *id).
		Scan(&b.ID, &b.Name, &b.Project.Key, &b.Project.Name)
	if err != nil {
		return nil, err
	}
	b.URL = s.opts.AppURL + "/p/" + b.Project.Key + "/baselines/" + b.ID.String()
	return &b, nil
}

func person(ctx context.Context, tx db.DBTX, id uuid.UUID) *Person {
	p := Person{}
	if err := tx.QueryRow(ctx, `SELECT id, name FROM app_user WHERE id = $1`, id).Scan(&p.ID, &p.Name); err != nil {
		return nil
	}
	return &p
}

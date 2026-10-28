package requirement

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/workflow"
)

// TransitionConflict is a transition that no longer leaves the requirement's status, which somebody moved meanwhile.
type TransitionConflict struct {
	Key    string
	Status string
}

func (e *TransitionConflict) Error() string { return fmt.Sprintf("%s is in %s now", e.Key, e.Status) }

// subjectOf is the requirement as a workflow's rules read it.
func subjectOf(ctx context.Context, tx db.DBTX, l *loaded) (workflow.Subject, error) {
	s := workflow.Subject{}
	if l.Status != nil {
		s.StatusID = &l.Status.ID
	}
	if l.Owner != nil {
		s.OwnerID = &l.Owner.ID
	}
	s.Quality = l.Quality.Score
	err := tx.QueryRow(ctx, `SELECT created_by, requirement_approved($1) FROM requirement_revision WHERE requirement_id = $1 AND number = $2`,
		l.ID, l.Revision).Scan(&s.LatestAuthor, &s.Approved)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return s, err
	}
	defs, err := attribute.Defs(ctx, tx, l.projectID)
	if err != nil {
		return s, err
	}
	var missing *attribute.FieldError
	if errors.As(attribute.Missing(defs, l.typeID(), l.Attributes), &missing) {
		s.MissingField = missing.Field
		for _, d := range defs {
			if "attributes."+d.ID.String() == missing.Field {
				s.MissingRequired = d.Name
			}
		}
	}
	return s, nil
}

// flowOf reads the workflow a requirement follows, and who asks as the workflow's rules see them.
func flowOf(ctx context.Context, tx db.DBTX, actor uuid.UUID, l *loaded) (*workflow.Workflow, workflow.Actor, error) {
	w, err := workflow.For(ctx, tx, l.projectID, l.typeID())
	if err != nil {
		return nil, workflow.Actor{}, err
	}
	roles, err := workflow.ProjectRoles(ctx, tx, l.projectID, actor)
	return w, workflow.Actor{UserID: actor, Roles: roles}, err
}

// Transitions answers the transitions the reader may take on a requirement now; a section takes none.
func (s *Service) Transitions(ctx context.Context, reader uuid.UUID, key string) ([]workflow.Available, error) {
	out := []workflow.Available{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := find(ctx, tx, reader, key)
		if err != nil {
			return err
		}
		out, err = offer(ctx, tx, reader, l)
		return err
	})
	return out, err
}

// offer is what the reader may take on a loaded requirement now: nothing on a section, in the trash or in an
// archived project, or for somebody who may not transition it.
func offer(ctx context.Context, tx db.DBTX, reader uuid.UUID, l *loaded) ([]workflow.Available, error) {
	if l.Kind == KindSection || l.TrashedAt != nil || l.archived || !l.Can.Transition {
		return []workflow.Available{}, nil
	}
	w, actor, err := flowOf(ctx, tx, reader, l)
	if err != nil {
		return nil, err
	}
	subject, err := subjectOf(ctx, tx, l)
	if err != nil {
		return nil, err
	}
	if offered := workflow.Offer(w, subject, actor); offered != nil {
		return offered, nil
	}
	return []workflow.Available{}, nil
}

// Transition takes a transition: its conditions and validators first, then the status by the database's own
// function, then what its post-functions ask for. The change is logged, never a revision.
func (s *Service) Transition(ctx context.Context, actor uuid.UUID, key string, in workflow.TransitionRequest) (*Requirement, db.LSN, error) {
	return s.transition(ctx, actor, key, in, uuid.Nil)
}

// transition takes a transition on the requirement key names, which must be of the project within unless that is nil.
func (s *Service) transition(ctx context.Context, actor uuid.UUID, key string, in workflow.TransitionRequest, within uuid.UUID) (*Requirement, db.LSN, error) {
	var out *Requirement
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := find(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		switch {
		case within != uuid.Nil && l.projectID != within:
			return ErrNotFound
		case l.archived:
			return project.ErrArchived
		case l.TrashedAt != nil:
			return ErrTrashed
		case l.Kind == KindSection:
			return &FieldError{"transitionId", "A section has no status to move."}
		case !l.Can.Transition:
			return ErrForbidden
		}
		comment, err := checkComment(in.Comment)
		if err != nil {
			return err
		}
		w, who, err := flowOf(ctx, tx, actor, l)
		if err != nil {
			return err
		}
		subject, err := subjectOf(ctx, tx, l)
		if err != nil {
			return err
		}
		if err := s.traceOf(ctx, tx, l, &subject); err != nil {
			return err
		}
		t, effect, err := workflow.Decide(w, in.TransitionID, subject, who, workflow.Input{Comment: comment})
		switch {
		case errors.Is(err, workflow.ErrUnavailable), errors.Is(err, workflow.ErrNoTransition):
			status := ""
			if l.Status != nil {
				status = l.Status.Name
			}
			return &TransitionConflict{Key: l.Key, Status: status}
		case err != nil:
			return err
		}
		if err := requiredIn(ctx, tx, l, &t.ToStatusID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT requirement_transition($1, $2, $3)`, l.ID, t.ID, comment); err != nil {
			return fmt.Errorf("take %s on %s: %w", t.Name, l.Key, err)
		}
		if err := events.Emit(ctx, tx, TopicChanged, map[string]any{"actorId": actor, "requirementId": l.ID, "field": ChangeStatus}); err != nil {
			return err
		}
		if effect.SetOwner && !samePerson(subject.OwnerID, effect.Owner) {
			if err := events.Emit(ctx, tx, TopicChanged, map[string]any{"actorId": actor, "requirementId": l.ID, "field": ChangeOwner}); err != nil {
				return err
			}
		}
		got, err := findByID(ctx, tx, actor, l.ID)
		if err == nil {
			out = &got.Requirement
		}
		return err
	})
	return out, lsn, err
}

func samePerson(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// settleStatus moves a requirement whose workflow lacks its status, after a new type, to the initial status of
// the workflow it follows now, and logs why; the status is read anew, since the revision may have moved it.
func settleStatus(ctx context.Context, tx db.DBTX, actor uuid.UUID, l *loaded, typeID *uuid.UUID, reason string) error {
	w, err := workflow.For(ctx, tx, l.projectID, typeID)
	if err != nil {
		return err
	}
	var (
		status *uuid.UUID
		from   string
	)
	if err := tx.QueryRow(ctx, `SELECT r.status_id, COALESCE(s.name, '') FROM requirement r LEFT JOIN req_status s ON s.id = r.status_id WHERE r.id = $1`,
		l.ID).Scan(&status, &from); err != nil {
		return err
	}
	initial, ok := w.Initial()
	if !ok || (status != nil && w.Has(*status)) {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT requirement_remap_status($1, $2)`, l.ID, initial.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO requirement_change (requirement_id, field, from_value, to_value, reason, actor_id)
		VALUES ($1, 'status', $2, $3, $4, $5)`, l.ID, from, initial.Name, reason, actor); err != nil {
		return err
	}
	return events.Emit(ctx, tx, TopicChanged, map[string]any{"actorId": actor, "requirementId": l.ID, "field": ChangeStatus})
}

// afterRevision sends a requirement where its step says a new revision goes, and logs why.
func afterRevision(ctx context.Context, tx db.DBTX, actor, id uuid.UUID, revision int) error {
	var left *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT requirement_revision_status($1)`, id).Scan(&left); err != nil || left == nil {
		return err
	}
	var from, to string
	if err := tx.QueryRow(ctx, `SELECT (SELECT name FROM req_status WHERE id = $2), (SELECT s.name FROM requirement r JOIN req_status s ON s.id = r.status_id WHERE r.id = $1)`,
		id, *left).Scan(&from, &to); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO requirement_change (requirement_id, field, from_value, to_value, reason, actor_id)
		VALUES ($1, 'status', $2, $3, $4, $5)`, id, from, to, fmt.Sprintf("Revision %d changed the requirement while it was %s.", revision, from), actor); err != nil {
		return err
	}
	return events.Emit(ctx, tx, TopicChanged, map[string]any{"actorId": actor, "requirementId": id, "field": ChangeStatus})
}

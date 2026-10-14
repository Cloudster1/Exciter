package watch

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// ErrSignedOut refuses a caller nobody signed in as, whose watches there are none of.
var ErrSignedOut = errors.New("watching needs somebody signed in")

// Service keeps each person's own watches, Stator's watch service over requirements and projects. Who hears about a
// requirement is read across people only by the database's requirement_watchers and the worker.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

func actorOf(ctx context.Context) (uuid.UUID, error) {
	actor, ok := db.UserFrom(ctx)
	if !ok {
		return uuid.Nil, ErrSignedOut
	}
	return actor, nil
}

// RequirementWatching is how a person follows a requirement: their own watch on it, and their watch on its project.
func RequirementWatching(ctx context.Context, tx db.DBTX, userID, requirementID uuid.UUID) (Watching, error) {
	var out Watching
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM watch WHERE user_id = $1 AND requirement_id = $2),
		       EXISTS (SELECT 1 FROM watch w JOIN requirement r ON r.project_id = w.project_id
		               WHERE w.user_id = $1 AND r.id = $2)`, userID, requirementID).Scan(&out.Requirement, &out.Project)
	return out, err
}

// WatchRequirement makes the caller watch a requirement and clears their opting out of it.
func (s *Service) WatchRequirement(ctx context.Context, reqKey string) (Watching, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return Watching{}, 0, err
	}
	var out Watching
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO watch (user_id, kind, requirement_id) VALUES ($1, 'requirement', $2)
			ON CONFLICT DO NOTHING`, actor, r.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM watch_optout WHERE user_id = $1 AND requirement_id = $2`, actor, r.ID); err != nil {
			return err
		}
		out, err = RequirementWatching(ctx, tx, actor, r.ID)
		return err
	})
	return out, lsn, err
}

// UnwatchRequirement removes the caller's own watch on a requirement and records that they opted out, so their own
// edits do not make them watch it again. A watch on its project stays.
func (s *Service) UnwatchRequirement(ctx context.Context, reqKey string) (db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM watch WHERE user_id = $1 AND requirement_id = $2`, actor, r.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO watch_optout (user_id, requirement_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, actor, r.ID)
		return err
	})
}

// Watchers lists who hears about a requirement, by name.
func (s *Service) Watchers(ctx context.Context, reqKey string, limit, offset int) ([]Watcher, int, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := []Watcher{}
	var total int
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := requirement.FindEndpoint(ctx, tx, actor, reqKey)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM requirement_watchers($1)`, r.ID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT c.user_id, u.name, u.email, c.via
			FROM requirement_watchers($1) c
			JOIN app_user u ON u.id = c.user_id
			ORDER BY lower(u.name), u.id
			LIMIT $2 OFFSET $3`, r.ID, limit, offset)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Watcher, error) {
			var w Watcher
			err := row.Scan(&w.UserID, &w.Name, &w.Email, &w.Via)
			return w, err
		})
		return err
	})
	return out, total, err
}

// WatchProject makes the caller watch every requirement of a project, now and later.
func (s *Service) WatchProject(ctx context.Context, projectKey string) (*Watch, db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	var out *Watch
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO watch (user_id, kind, project_id) VALUES ($1, 'project', $2)
			ON CONFLICT DO NOTHING`, actor, p.ID); err != nil {
			return err
		}
		w, err := scanWatch(tx.QueryRow(ctx, selectWatches+` AND w.project_id = $2`, actor, p.ID))
		out = &w
		return err
	})
	return out, lsn, err
}

// UnwatchProject stops the caller watching a project; their watches on its requirements stay.
func (s *Service) UnwatchProject(ctx context.Context, projectKey string) (db.LSN, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM watch WHERE user_id = $1 AND project_id = $2`, actor, p.ID)
		return err
	})
}

// selectWatches reads $1's watches on what they may still view, a requirement out of the trash.
const selectWatches = `
SELECT w.id, w.kind, w.created_at, p.key, r.id, r.number, r.kind, r.title, r.revision, r.trashed_at IS NOT NULL,
       st.id, COALESCE(st.name, ''), COALESCE(st.category, '')
FROM watch w
LEFT JOIN requirement r ON r.id = w.requirement_id
LEFT JOIN req_status st ON st.id = r.status_id
JOIN project p ON p.id = COALESCE(w.project_id, r.project_id)
WHERE w.user_id = $1
  AND (w.requirement_id IS NULL OR (r.trashed_at IS NULL AND perm_requirement_viewable(r.id, $1::uuid)))
  AND (w.project_id IS NULL OR perm_project_holds(p.id, $1::uuid, 'view'))`

func scanWatch(row pgx.Row) (Watch, error) {
	var (
		w        Watch
		id       *uuid.UUID
		number   *int
		kind     *string
		title    *string
		revision *int
		trashed  bool
		statusID *uuid.UUID
		status   requirement.StatusRef
	)
	if err := row.Scan(&w.ID, &w.Kind, &w.CreatedAt, &w.ProjectKey, &id, &number, &kind, &title, &revision, &trashed,
		&statusID, &status.Name, &status.Category); err != nil {
		return w, err
	}
	if id != nil {
		ref := &requirement.Ref{ID: *id, Key: requirement.Key(w.ProjectKey, *number), ProjectKey: w.ProjectKey,
			Kind: requirement.Kind(*kind), Title: *title, Revision: *revision, Trashed: trashed}
		if statusID != nil {
			status.ID = *statusID
			ref.Status = &status
		}
		w.Requirement = ref
	}
	return w, nil
}

// List is the caller's own watches, the latest first.
func (s *Service) List(ctx context.Context, limit, offset int) ([]Watch, int, error) {
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := []Watch{}
	var total int
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM (`+selectWatches+`) listed`, actor).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectWatches+` ORDER BY w.created_at DESC, w.id DESC LIMIT $2 OFFSET $3`, actor, limit, offset)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Watch, error) { return scanWatch(row) })
		return err
	})
	return out, total, err
}

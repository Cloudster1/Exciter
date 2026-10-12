package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// FieldError is a query refused on one parameter, with the sentence to show beside it.
type FieldError struct {
	Field, Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Service finds requirements; until #20 it lists one project's requirements by kind, owner and priority.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// unlisted refuses the grid's narrowing that #20 brings, by the parameter that asked for it.
func unlisted(f Filter) error {
	for field, asked := range map[string]bool{
		"q": f.Q != "", "type": len(f.Types) > 0, "status": len(f.Statuses) > 0, "category": len(f.Categories) > 0,
		"label": len(f.Labels) > 0, "attr": len(f.Attributes) > 0,
	} {
		if asked {
			return &FieldError{field, "The list does not narrow by this yet. Leave it out, and narrow by kind, owner or priority."}
		}
	}
	return nil
}

// Grid answers a window of one project's requirements out of the trash, in key order, narrowed by f, and how
// many there are in all.
func (s *Service) Grid(ctx context.Context, reader uuid.UUID, projectKey string, f Filter, limit, offset int) ([]Row, int, error) {
	if err := unlisted(f); err != nil {
		return nil, 0, err
	}
	for _, p := range f.Priorities {
		if !isPriority(p) {
			return nil, 0, &FieldError{"priority", "Narrow by must, should, could or wont."}
		}
	}
	for _, k := range f.Kinds {
		if k != requirement.KindRequirement && k != requirement.KindSection {
			return nil, 0, &FieldError{"kind", "Narrow by requirement or section."}
		}
	}
	out := []Row{}
	total := 0
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		where := `
			FROM requirement r LEFT JOIN app_user o ON o.id = r.owner_id
			WHERE r.org_id = current_org_id() AND r.project_id = $2 AND r.trashed_at IS NULL AND ` + perm.ViewableRequirement("r", 1) + `
			  AND (cardinality($3::text[]) = 0 OR r.kind = ANY ($3))
			  AND (cardinality($4::uuid[]) = 0 OR r.owner_id = ANY ($4))
			  AND (cardinality($5::text[]) = 0 OR r.priority = ANY ($5))`
		kinds := make([]string, len(f.Kinds))
		for i, k := range f.Kinds {
			kinds[i] = string(k)
		}
		owners := f.Owners
		if owners == nil {
			owners = []uuid.UUID{}
		}
		priorities := f.Priorities
		if priorities == nil {
			priorities = []string{}
		}
		args := []any{reader, p.ID, kinds, owners, priorities}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int`+where, args...).Scan(&total); err != nil {
			return fmt.Errorf("count the requirements: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT r.id, r.number, r.kind, r.title, r.revision, r.owner_id, COALESCE(o.name, ''), r.priority, r.updated_at,
			       perm_project_holds(r.project_id, $1, 'edit'), perm_project_holds(r.project_id, $1, 'approve'),
			       perm_project_holds(r.project_id, $1, 'comment')`+where+`
			ORDER BY r.number LIMIT $6 OFFSET $7`, append(args, limit, offset)...)
		if err != nil {
			return fmt.Errorf("list the requirements: %w", err)
		}
		found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Row, error) {
			var (
				r                     Row
				number                int
				ownerID               *uuid.UUID
				ownerName             string
				priority              *string
				edit, approve, remark bool
			)
			err := row.Scan(&r.ID, &number, &r.Kind, &r.Title, &r.Revision, &ownerID, &ownerName, &priority, &r.UpdatedAt, &edit, &approve, &remark)
			r.ProjectKey = p.Key
			r.Key = requirement.Key(p.Key, number)
			if ownerID != nil {
				r.Owner = &requirement.PersonRef{ID: *ownerID, Name: ownerName}
			}
			if priority != nil {
				pr := requirement.Priority(*priority)
				r.Priority = &pr
			}
			r.Labels = []string{}
			r.Attributes = map[string]json.RawMessage{}
			r.Can = requirement.Can{Edit: edit, Transition: edit || approve, Delete: edit, Link: edit, Comment: remark}
			return r, err
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if found != nil {
			out = found
		}
		return err
	})
	return out, total, err
}

func isPriority(p string) bool {
	for _, each := range requirement.Priorities {
		if string(each) == p {
			return true
		}
	}
	return false
}

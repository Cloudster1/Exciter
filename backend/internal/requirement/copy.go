package requirement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// Copy makes new requirements of a requirement, and of what is below it when asked, at their current content
// as revision 1, in its project or another; it answers the copy and how many it made.
func (s *Service) Copy(ctx context.Context, actor uuid.UUID, key string, in CopyInput) (*Requirement, int, db.LSN, error) {
	return s.copyTree(ctx, actor, key, in, false)
}

// copyTree writes the copies of Copy, or of Reuse when reuse is set: then each copy follows its original.
func (s *Service) copyTree(ctx context.Context, actor uuid.UUID, key string, in CopyInput, reuse bool) (*Requirement, int, db.LSN, error) {
	var (
		out    *Requirement
		copied int
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		src, err := find(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if src.TrashedAt != nil {
			return ErrTrashed
		}
		here, err := project.Find(ctx, tx, actor, src.ProjectKey)
		if err != nil {
			return err
		}
		if reuse {
			if err := reusable(here, in.ProjectKey); err != nil {
				return err
			}
		}
		to, err := destination(ctx, tx, actor, in.ProjectKey, here)
		if err != nil {
			return err
		}
		nodes := []uuid.UUID{src.ID}
		if in.Children {
			if nodes, err = subtree(ctx, tx, src.projectID, src.ID); err != nil {
				return err
			}
		}
		if len(nodes) > MaxCopy {
			return &FieldError{"children", fmt.Sprintf("A copy makes at most %d requirements, and this one would make %d. Copy a part of the tree.", MaxCopy, len(nodes))}
		}
		at, err := rankAt(ctx, tx, to.ID, in.Place, nil)
		if err != nil {
			return err
		}
		var match *matcher
		if to.ID != here.ID {
			if match, err = newMatcher(ctx, tx, here.ID, to); err != nil {
				return err
			}
		}
		copies := map[uuid.UUID]uuid.UUID{}
		for i, id := range nodes {
			l, err := findByID(ctx, tx, actor, id)
			if err != nil {
				return err
			}
			var files map[uuid.UUID]uuid.UUID
			if s.files != nil {
				if files, err = s.files.PlanCopy(ctx, tx, id); err != nil {
					return err
				}
			}
			made, err := copyOne(ctx, tx, actor, l, to, match, copyPlace{root: i == 0, parent: in.Place.ParentID, at: at, reuse: reuse}, copies, files)
			if err != nil {
				return err
			}
			if len(files) > 0 {
				if err := s.files.CopyFiles(ctx, tx, actor, made, files); err != nil {
					return err
				}
			}
			copies[id] = made
		}
		copied = len(nodes)
		if in.Links {
			if err := copyLinks(ctx, tx, actor, to.ID, copies); err != nil {
				return err
			}
		}
		l, err := findByID(ctx, tx, actor, copies[src.ID])
		if err == nil {
			out = &l.Requirement
		}
		return err
	})
	return out, copied, lsn, err
}

// copyPlace is where one copy goes: the root at the place asked for, the rest below the copies of their parents.
type copyPlace struct {
	root   bool
	parent *uuid.UUID
	at     string
	// reuse makes the copy follow its original (#49).
	reuse bool
}

// copyOne writes one copy. Its content shows the files named in files by their copies' ids.
func copyOne(ctx context.Context, tx db.DBTX, actor uuid.UUID, l *loaded, to *project.Project, match *matcher, place copyPlace,
	copies, files map[uuid.UUID]uuid.UUID) (uuid.UUID, error) {
	typeID, values, owner := l.typeID(), l.Attributes, (*uuid.UUID)(nil)
	if l.Owner != nil {
		owner = &l.Owner.ID
	}
	if match != nil {
		var err error
		if typeID, values, err = match.carry(l); err != nil {
			return uuid.Nil, err
		}
		if owner != nil {
			views, err := match.viewer(*owner)
			if err != nil {
				return uuid.Nil, err
			}
			if !views {
				owner = nil
			}
		}
	}
	parent, rank := place.parent, place.at
	if !place.root {
		mapped := copies[*l.ParentID]
		parent, rank = &mapped, l.rank
	}
	test, err := testFor(ctx, tx, typeID, l.test)
	if err != nil {
		return uuid.Nil, err
	}
	content := Normative{Title: l.Title, Statement: l.Statement, Rationale: l.Rationale, TypeID: typeID, Attributes: values, Test: test}
	if err := pointAtFiles(&content, files); err != nil {
		return uuid.Nil, err
	}
	hash := content.Hash()
	attrs, err := json.Marshal(content.Attributes)
	if err != nil {
		return uuid.Nil, err
	}
	pre, steps, params, err := content.Test.columns()
	if err != nil {
		return uuid.Nil, err
	}
	var (
		id                  uuid.UUID
		reuseRev, reuseFrom any
		comment             = "Copied from " + l.Key
	)
	if place.reuse {
		reuseFrom, reuseRev = l.ID, l.Revision
		comment = fmt.Sprintf("Reused from %s at revision %d", l.Key, l.Revision)
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO requirement (project_id, kind, title, statement, rationale, type_id, attributes, normative_hash, owner_id, priority,
		                         parent_id, rank, copied_from, created_by, updated_by, reuse_source, reuse_revision, reuse_state,
		                         preconditions, steps, parameters)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14, $15, $16, CASE WHEN $15::uuid IS NULL THEN NULL ELSE 'following' END,
		        $17, $18, $19)
		RETURNING id`,
		to.ID, l.Kind, l.Title, content.Statement, content.Rationale, typeID, attrs, hash, owner, l.Priority, parent, rank, l.ID, actor,
		reuseFrom, reuseRev, pre, steps, params).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("copy %s: %w", l.Key, treeRefusal(err))
	}
	if err := revisionOf(ctx, tx, actor, id, 1, content, hash, comment, nil); err != nil {
		return uuid.Nil, err
	}
	if err := copyLabels(ctx, tx, actor, l, id); err != nil {
		return uuid.Nil, err
	}
	return id, events.Emit(ctx, tx, TopicCreated, map[string]any{"actorId": actor, "projectId": to.ID, "requirementId": id, "revision": 1})
}

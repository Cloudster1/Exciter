package trace

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/events"
)

// CoverageTopics are the events that change what a project's coverage counts or how its rows read: a requirement
// written, moved, trashed or restored, a link made, removed or cleared, an Armature link changed, a project rekeyed,
// a test run done or the branch whose results count named (#200), a code scan uploaded or the tag words set (#201).
var CoverageTopics = []string{"requirement.created", "requirement.revised", "requirement.changed", "requirement.trashed",
	"requirement.restored", "requirement.moved", "link.created", "link.removed", TopicLinkCleared, "armature.links",
	"project.key_changed", "test_run.finished", "evidence_settings.saved", "code_scan.uploaded", "code_tag_words.saved"}

// CoverageForgetter clears the coverage of the projects an event touches. Clearing twice changes nothing, so it
// runs ahead of each topic's other handlers.
type CoverageForgetter struct {
	db    *db.Cluster
	cache *CoverageCache
}

func NewCoverageForgetter(cluster *db.Cluster, cache *CoverageCache) *CoverageForgetter {
	return &CoverageForgetter{db: cluster, cache: cache}
}

// touched are the ids an event's payload may name.
type touched struct {
	ProjectID     uuid.UUID `json:"projectId"`
	RequirementID uuid.UUID `json:"requirementId"`
	SourceID      uuid.UUID `json:"sourceId"`
	TargetID      uuid.UUID `json:"targetId"`
}

func (f *CoverageForgetter) Handle(ctx context.Context, e events.Event) error {
	if f.cache == nil {
		return nil
	}
	var t touched
	if err := json.Unmarshal(e.Payload, &t); err != nil {
		return nil
	}
	projects := []uuid.UUID{}
	if t.ProjectID != uuid.Nil {
		projects = append(projects, t.ProjectID)
	}
	var ids []uuid.UUID
	for _, id := range []uuid.UUID{t.RequirementID, t.SourceID, t.TargetID} {
		if id != uuid.Nil {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		// As the admin role: the projects are needed whoever may see the requirements, and a link's other end
		// may be in a project its maker does not view.
		err := f.db.ReadAdmin(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
			rows, err := tx.Query(ctx, `SELECT DISTINCT project_id FROM requirement WHERE id = ANY ($1)`, ids)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err != nil {
					return err
				}
				projects = append(projects, id)
			}
			return rows.Err()
		})
		if err != nil {
			return fmt.Errorf("find the projects whose coverage to clear: %w", err)
		}
	}
	f.cache.Forget(ctx, projects...)
	return nil
}

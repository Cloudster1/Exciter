package trace

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// OrgTypes answers the installation's link types, retired ones included.
func (s *Service) OrgTypes(ctx context.Context, reader uuid.UUID) ([]LinkType, error) {
	out := []LinkType{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		types, err := linkTypes(ctx, tx, `t.project_id IS NULL`)
		if types != nil {
			out = types
		}
		return err
	})
	return out, err
}

// ProjectTypes answers the link types a project may use, with whether it does and the pairs each may join.
func (s *Service) ProjectTypes(ctx context.Context, reader uuid.UUID, projectKey string) ([]LinkType, error) {
	out := []LinkType{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		types, err := usable(ctx, tx, p.ID)
		if types != nil {
			out = types
		}
		return err
	})
	return out, err
}

// create makes a type of the installation, or of a project when projectID is set, with what in says.
func create(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectID *uuid.UUID, in LinkTypeInput) (*LinkType, error) {
	var t LinkType
	if err := applyType(&t, in, true); err != nil {
		return nil, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM link_type WHERE project_id IS NOT DISTINCT FROM $1`,
		projectID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= MaxLinkTypes {
		return nil, &FieldError{"name", fmt.Sprintf("There are at most %d link types here. Retire or delete one first.", MaxLinkTypes)}
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO link_type (project_id, key, name, forward, backward, acyclic, suspect_end, counts, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		projectID, t.Key, t.Name, t.Forward, t.Backward, t.Acyclic, t.SuspectEnd, t.Counts, actor).Scan(&id)
	if err != nil {
		return nil, typeRefusal(err, t.Key)
	}
	made, err := linkTypeByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return made, record(ctx, tx, actor, audit.ActionLinkTypeCreated, made, map[string]any{})
}

// CreateOrgType makes a link type every project may use; the policies hold it to the installation's administrators.
func (s *Service) CreateOrgType(ctx context.Context, actor uuid.UUID, in LinkTypeInput) (*LinkType, db.LSN, error) {
	var out *LinkType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = create(ctx, tx, actor, nil, in)
		return err
	})
	return out, lsn, err
}

// CreateProjectType makes a link type of the project's own.
func (s *Service) CreateProjectType(ctx context.Context, actor uuid.UUID, projectKey string, in LinkTypeInput) (*LinkType, db.LSN, error) {
	var out *LinkType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if out, err = create(ctx, tx, actor, &p.ID, in); err != nil {
			return err
		}
		types, err := usable(ctx, tx, p.ID)
		out = pick(types, out.ID, out)
		return err
	})
	return out, lsn, err
}

func pick(types []LinkType, id uuid.UUID, fallback *LinkType) *LinkType {
	for i := range types {
		if types[i].ID == id {
			return &types[i]
		}
	}
	return fallback
}

// UpdateType changes or retires a type; a retired one keeps its links and takes no new ones.
func (s *Service) UpdateType(ctx context.Context, actor, id uuid.UUID, in LinkTypeInput) (*LinkType, db.LSN, error) {
	if in.Acyclic != nil && !*in.Acyclic {
		// A formula that reads itself along the type is safe only while the type stays acyclic (#213), in any project.
		var name string
		if err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) (err error) {
			name, err = attribute.FollowsAcyclic(ctx, tx, id)
			return err
		}); err != nil {
			return nil, 0, err
		}
		if name != "" {
			return nil, 0, &InFormulaError{Attribute: name}
		}
	}
	var out *LinkType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		t, err := managing(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if err := applyType(t, in, false); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE link_type SET name = $2, forward = $3, backward = $4, acyclic = $5, suspect_end = $6, counts = $7, retired = $8
			WHERE id = $1`, id, t.Name, t.Forward, t.Backward, t.Acyclic, t.SuspectEnd, t.Counts, t.Retired); err != nil {
			return typeRefusal(err, t.Key)
		}
		if out, err = linkTypeByID(ctx, tx, id); err != nil {
			return err
		}
		return record(ctx, tx, actor, audit.ActionLinkTypeChanged, out, map[string]any{"retired": out.Retired})
	})
	return out, lsn, err
}

// DeleteType removes a type no link ever had, tombstones included.
func (s *Service) DeleteType(ctx context.Context, actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		t, err := managing(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		var used int
		if err := tx.QueryRow(ctx, `SELECT link_type_usage($1, false)`, id).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return &InUseError{Count: used}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM link_type WHERE id = $1`, id); err != nil {
			return err
		}
		return record(ctx, tx, actor, audit.ActionLinkTypeDeleted, t, map[string]any{})
	})
}

// SetProjectType says whether a project uses a type and which pairs of its requirement types the type joins.
func (s *Service) SetProjectType(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID, in ProjectLinkTypeInput) (*LinkType, db.LSN, error) {
	var out *LinkType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		types, err := usable(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		t := pick(types, id, nil)
		if t == nil {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, `SELECT id, name FROM requirement_type WHERE project_id = $1`, p.ID)
		if err != nil {
			return err
		}
		named := map[uuid.UUID]string{}
		var (
			typeID uuid.UUID
			name   string
		)
		if _, err := pgx.ForEachRow(rows, []any{&typeID, &name}, func() error { named[typeID] = name; return nil }); err != nil {
			return err
		}
		rules, err := checkRules(in.Rules, named)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO link_type_project (project_id, link_type_id, enabled) VALUES ($1, $2, $3)
			ON CONFLICT (project_id, link_type_id) DO UPDATE SET enabled = EXCLUDED.enabled`, p.ID, id, in.Enabled); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM link_type_rule WHERE project_id = $1 AND link_type_id = $2`, p.ID, id); err != nil {
			return err
		}
		for _, rule := range rules {
			if _, err := tx.Exec(ctx, `
				INSERT INTO link_type_rule (project_id, link_type_id, source_type_id, target_type_id)
				VALUES ($1, $2, $3, $4)`, p.ID, id, rule.SourceTypeID, rule.TargetTypeID); err != nil {
				return err
			}
		}
		if types, err = usable(ctx, tx, p.ID); err != nil {
			return err
		}
		out = pick(types, id, t)
		return record(ctx, tx, actor, audit.ActionLinkTypeChanged, out, map[string]any{"project": p.Key, "enabled": out.Enabled, "rules": len(out.Rules)})
	})
	return out, lsn, err
}

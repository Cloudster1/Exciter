package trace

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/audit"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// readModel reads a project's levels in order with their types and steps, and who set them last.
func readModel(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (*Model, error) {
	out := &Model{Levels: []ModelLevel{}}
	rows, err := tx.Query(ctx, `
		SELECT l.id, l.name, l.kind, l.updated_at, l.updated_by, COALESCE(u.name, '')
		FROM trace_level l LEFT JOIN app_user u ON u.id = l.updated_by
		WHERE l.project_id = $1 ORDER BY l.position`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the trace model: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var (
			id     uuid.UUID
			level  = ModelLevel{Types: []requirement.TypeRef{}, Steps: []ModelStep{}, TypeIDs: []uuid.UUID{}}
			at     time.Time
			byID   *uuid.UUID
			byName string
		)
		if err := rows.Scan(&id, &level.Name, &level.Kind, &at, &byID, &byName); err != nil {
			rows.Close()
			return nil, err
		}
		if out.UpdatedAt == nil || at.After(*out.UpdatedAt) {
			out.UpdatedAt = &at
			out.UpdatedBy = nil
			if byID != nil {
				out.UpdatedBy = &requirement.PersonRef{ID: *byID, Name: byName}
			}
		}
		ids = append(ids, id)
		out.Levels = append(out.Levels, level)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	types, err := tx.Query(ctx, `
		SELECT lt.level_id, t.id, t.name, t.icon, t.colour
		FROM trace_level_type lt JOIN requirement_type t ON t.id = lt.type_id
		WHERE lt.project_id = $1 ORDER BY t.position, t.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the trace model's types: %w", err)
	}
	for types.Next() {
		var (
			level uuid.UUID
			t     requirement.TypeRef
		)
		if err := types.Scan(&level, &t.ID, &t.Name, &t.Icon, &t.Colour); err != nil {
			types.Close()
			return nil, err
		}
		l := &out.Levels[slices.Index(ids, level)]
		l.Types, l.TypeIDs = append(l.Types, t), append(l.TypeIDs, t.ID)
	}
	if err := types.Err(); err != nil {
		return nil, err
	}
	steps, err := tx.Query(ctx, `
		SELECT s.level_id, s.link_type_id, COALESCE(k.name, ''), COALESCE(s.direction, ''), COALESCE(s.relation, '')
		FROM trace_level_step s LEFT JOIN link_type k ON k.id = s.link_type_id
		WHERE s.project_id = $1 ORDER BY s.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the trace model's steps: %w", err)
	}
	defer steps.Close()
	for steps.Next() {
		var (
			level uuid.UUID
			step  ModelStep
		)
		if err := steps.Scan(&level, &step.TypeID, &step.TypeName, &step.Direction, &step.Relation); err != nil {
			return nil, err
		}
		l := &out.Levels[slices.Index(ids, level)]
		l.Steps = append(l.Steps, step)
	}
	return out, steps.Err()
}

// Model answers a project's trace levels, first to last; none until an administrator sets them.
func (s *Service) Model(ctx context.Context, reader uuid.UUID, projectKey string) (*Model, error) {
	var out *Model
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		if out, err = readModel(ctx, tx, p.ID); err != nil {
			return err
		}
		out.Can.Edit = p.Can.Administer && p.ArchivedAt == nil
		return nil
	})
	return out, err
}

// SaveModel replaces a project's whole trace model; no levels clears it.
func (s *Service) SaveModel(ctx context.Context, actor uuid.UUID, projectKey string, in ModelInput) (*Model, db.LSN, error) {
	if err := checkModel(&in); err != nil {
		return nil, 0, err
	}
	var out *Model
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if !p.Can.Administer {
			return ErrNotModelKeeper
		}
		if p.ArchivedAt != nil {
			return project.ErrArchived
		}
		if err := modelHeld(ctx, tx, p.ID, in); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM trace_level WHERE project_id = $1`, p.ID); err != nil {
			return fmt.Errorf("clear the trace model: %w", err)
		}
		names := make([]string, len(in.Levels))
		for i, level := range in.Levels {
			names[i] = level.Name
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO trace_level (project_id, position, name, kind, updated_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				p.ID, i, level.Name, level.Kind, actor).Scan(&id); err != nil {
				return fmt.Errorf("save the trace level %s: %w", level.Name, err)
			}
			for _, t := range level.TypeIDs {
				if _, err := tx.Exec(ctx, `INSERT INTO trace_level_type (level_id, project_id, type_id) VALUES ($1, $2, $3)`, id, p.ID, t); err != nil {
					return fmt.Errorf("save the trace level %s's types: %w", level.Name, err)
				}
			}
			for _, step := range level.Steps {
				direction, relation := nullable(string(step.Direction)), nullable(step.Relation)
				if _, err := tx.Exec(ctx, `INSERT INTO trace_level_step (level_id, project_id, link_type_id, direction, relation) VALUES ($1, $2, $3, $4, $5)`,
					id, p.ID, step.TypeID, direction, relation); err != nil {
					return fmt.Errorf("save the trace level %s's steps: %w", level.Name, err)
				}
			}
		}
		if out, err = readModel(ctx, tx, p.ID); err != nil {
			return err
		}
		out.Can.Edit = true
		return audit.Write(ctx, tx, audit.Entry{
			Action: audit.ActionTraceModelSaved, TargetType: "project", TargetID: &p.ID, Actor: actor,
			Data: map[string]any{"projectKey": p.Key, "levels": names},
		})
	})
	return out, lsn, err
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// levelField names a field of the i-th level, or of its j-th step, as a refusal points at it.
func levelField(i int, rest string) string {
	return "levels[" + strconv.Itoa(i) + "]." + rest
}

// checkModel refuses a model whose shape does not hold, before anything is read: the levels, their names and kinds,
// and how each is reached from the one before.
func checkModel(in *ModelInput) error {
	if len(in.Levels) > MaxLevels {
		return &FieldError{Field: "levels", Message: "A trace model has at most " + strconv.Itoa(MaxLevels) + " levels. Join or drop some."}
	}
	seenNames := map[string]bool{}
	seenTypes := map[uuid.UUID]bool{}
	for i := range in.Levels {
		level := &in.Levels[i]
		level.Name = strings.TrimSpace(level.Name)
		switch n := utf8.RuneCountInString(level.Name); {
		case n == 0:
			return &FieldError{Field: levelField(i, "name"), Message: "Name the level, such as Stakeholder or Test."}
		case n > MaxLevelNameRunes:
			return &FieldError{Field: levelField(i, "name"), Message: "Name the level in at most " + strconv.Itoa(MaxLevelNameRunes) + " characters."}
		case seenNames[strings.ToLower(level.Name)]:
			return &FieldError{Field: levelField(i, "name"), Message: "Another level has that name. Give each level its own."}
		}
		seenNames[strings.ToLower(level.Name)] = true
		if !slices.Contains(LevelKinds, level.Kind) {
			return &FieldError{Field: levelField(i, "kind"), Message: "A level holds requirements, Armature issues, Stator pages, tests or code."}
		}
		if i == 0 && level.Kind != LevelRequirements {
			return &FieldError{Field: levelField(i, "kind"), Message: "The first level holds requirements, which the chains start from."}
		}
		if level.Kind != LevelRequirements && i != len(in.Levels)-1 {
			return &FieldError{Field: levelField(i, "kind"), Message: "Issues, pages, tests and code end a model; move this level to the end."}
		}
		if err := checkLevelTypes(i, level, seenTypes); err != nil {
			return err
		}
		if err := checkSteps(i, level); err != nil {
			return err
		}
	}
	return nil
}

func checkLevelTypes(i int, level *ModelLevelInput, seen map[uuid.UUID]bool) error {
	if level.Kind != LevelRequirements {
		if len(level.TypeIDs) > 0 {
			return &FieldError{Field: levelField(i, "typeIds"), Message: "Only a level of requirements has types. Leave them out here."}
		}
		return nil
	}
	if len(level.TypeIDs) == 0 {
		return &FieldError{Field: levelField(i, "typeIds"), Message: "Pick the requirement types this level holds."}
	}
	for _, t := range level.TypeIDs {
		if seen[t] {
			return &FieldError{Field: levelField(i, "typeIds"), Message: "A type sits in one level only. Take it out of the other."}
		}
		seen[t] = true
	}
	return nil
}

func checkSteps(i int, level *ModelLevelInput) error {
	if i == 0 {
		if len(level.Steps) > 0 {
			return &FieldError{Field: levelField(i, "steps"), Message: "The first level is where chains start, so nothing leads to it. Leave its links out."}
		}
		return nil
	}
	if len(level.Steps) == 0 {
		return &FieldError{Field: levelField(i, "steps"), Message: "Pick the links that lead to this level from the one before."}
	}
	for j, step := range level.Steps {
		field := levelField(i, "steps["+strconv.Itoa(j)+"]")
		switch level.Kind {
		case LevelRequirements:
			if step.TypeID == nil || step.Relation != "" {
				return &FieldError{Field: field, Message: "A level of requirements is reached by a link type. Pick one."}
			}
			if !slices.Contains(Directions, step.Direction) {
				return &FieldError{Field: field + ".direction", Message: "Read the link type outgoing or incoming from the level before."}
			}
		case LevelIssues, LevelPages, LevelTests, LevelCode:
			allowed := IssueRelations
			switch level.Kind {
			case LevelPages:
				allowed = PageRelations
			case LevelTests:
				allowed = TestRelations
			case LevelCode:
				allowed = CodeRelations
			}
			if step.TypeID != nil || step.Direction != "" || !slices.Contains(allowed, step.Relation) {
				return &FieldError{Field: field + ".relation", Message: "Pick " + strings.Join(allowed, ", ") + " as the relation."}
			}
		}
	}
	return nil
}

// modelHeld refuses types that are not the project's own or are retired, and link types it may not use.
func modelHeld(ctx context.Context, tx db.DBTX, projectID uuid.UUID, in ModelInput) error {
	for i, level := range in.Levels {
		if len(level.TypeIDs) > 0 {
			var held int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM requirement_type WHERE id = ANY ($1) AND project_id = $2 AND NOT retired`,
				level.TypeIDs, projectID).Scan(&held); err != nil {
				return err
			}
			if held != len(level.TypeIDs) {
				return &FieldError{Field: levelField(i, "typeIds"), Message: "Pick types this project has and still uses."}
			}
		}
		for j, step := range level.Steps {
			if step.TypeID == nil {
				continue
			}
			var usable bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM link_type t WHERE t.id = $1 AND NOT t.retired AND (t.project_id IS NULL OR t.project_id = $2)
				    AND NOT EXISTS (SELECT 1 FROM link_type_project o WHERE o.project_id = $2 AND o.link_type_id = t.id AND NOT o.enabled))`,
				*step.TypeID, projectID).Scan(&usable); err != nil {
				return err
			}
			if !usable {
				return &FieldError{Field: levelField(i, "steps["+strconv.Itoa(j)+"]"), Message: "Pick a link type this project uses."}
			}
		}
	}
	return nil
}

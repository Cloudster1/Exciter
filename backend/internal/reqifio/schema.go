package reqifio

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// The look of a type an import makes, the project's default type's, which an administrator changes later.
const (
	madeTypeIcon   = "requirement"
	madeTypeColour = "#4f6bed"
)

// tieRow ties an identifier of the file to what it became.
type tieRow struct {
	kind, identifier string
	target           uuid.UUID
	scope            *uuid.UUID
	// word replaces the identity an earlier import of a Word document gave the same requirement, since a part's
	// identity changes as the document is edited and a requirement keeps one.
	word bool
}

// tie records ties as the import's maker, the latest for an identifier winning.
func (s *Service) tie(ctx context.Context, j *job, ties []tieRow) error {
	if len(ties) == 0 {
		return nil
	}
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return writeTies(ctx, tx, j, ties)
	})
	return err
}

func writeTies(ctx context.Context, tx db.DBTX, j *job, ties []tieRow) error {
	for _, t := range ties {
		if t.word {
			if err := writeWordTie(ctx, tx, j, t); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO reqif_identity (project_id, kind, identifier, target_id, scope_id) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (project_id, kind, identifier) DO UPDATE SET target_id = EXCLUDED.target_id, scope_id = EXCLUDED.scope_id,
			    created_at = now()`,
			j.projectID, t.kind, t.identifier, t.target, t.scope); err != nil {
			return fmt.Errorf("remember what %s became: %w", t.identifier, err)
		}
	}
	return nil
}

// writeWordTie ties a part of a Word document to its requirement, unless an exchange of another kind tied the
// requirement first, which keeps its own.
func writeWordTie(ctx context.Context, tx db.DBTX, j *job, t tieRow) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM reqif_identity WHERE project_id = $1 AND kind = $2
		AND ((target_id = $3 AND identifier LIKE 'word:%' AND identifier <> $4) OR (identifier = $4 AND target_id <> $3))`,
		j.projectID, t.kind, t.target, t.identifier); err != nil {
		return fmt.Errorf("forget what %s was: %w", t.identifier, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO reqif_identity (project_id, kind, identifier, target_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		j.projectID, t.kind, t.identifier, t.target); err != nil {
		return fmt.Errorf("remember what %s became: %w", t.identifier, err)
	}
	return nil
}

// makeSchema makes the types, attributes, options and link types the analysis said to make, widens the
// attributes it maps to so they show on the types that use them, and ties every identifier to what it is.
func (s *Service) makeSchema(ctx context.Context, j *job, a *analysis) error {
	actor, key := j.createdBy, j.ProjectKey
	var ties []tieRow
	typeOf := map[string]*uuid.UUID{}
	for _, t := range a.file.Types {
		target := a.schema.types[t.Identifier]
		if target == nil {
			continue
		}
		if target.create != "" {
			name, icon, colour := target.create, madeTypeIcon, madeTypeColour
			in := attribute.TypeInput{Name: &name, Icon: &icon, Colour: &colour}
			if target.testCases {
				role := attribute.RoleTestCase
				in.Role = &role
			} else if target.risks {
				role := attribute.RoleRisk
				in.Role = &role
			}
			made, _, err := s.attributes.CreateType(ctx, actor, key, in)
			if err != nil {
				return fmt.Errorf("make the type %s: %w", name, err)
			}
			target.id = &made.ID
		}
		typeOf[t.Identifier] = target.id
		if target.id != nil {
			ties = append(ties, tieRow{kind: "type", identifier: t.Identifier, target: *target.id})
		}
	}
	defs := map[uuid.UUID]*defChange{}
	for _, t := range a.file.Types {
		for _, d := range t.Attributes {
			target := a.schema.attrs[d.Identifier]
			if target == nil || target.field != "" || target.skip || target.lost || (target.def == nil && target.create == nil) {
				continue
			}
			if target.create != nil && target.create.made == nil {
				made, err := s.makeAttribute(ctx, j, target.create, typeOf)
				if err != nil {
					return err
				}
				target.create.made = made
			}
			def := target.def
			if def == nil {
				def = target.create.made
			}
			change := defs[def.ID]
			if change == nil {
				change = &defChange{def: def}
				defs[def.ID] = change
			}
			change.missing = append(change.missing, target.missing...)
			if typeID := typeOf[t.Identifier]; typeID != nil {
				change.types = append(change.types, *typeID)
				ties = append(ties, tieRow{kind: "attribute", identifier: d.Identifier, target: def.ID, scope: typeID})
			}
			change.enums = append(change.enums, a.index.dataTypes[d.DataType])
		}
	}
	for _, change := range defs {
		def, err := s.widen(ctx, j, change)
		if err != nil {
			return err
		}
		ties = append(ties, optionTies(def, change.enums)...)
	}
	for _, t := range a.file.Types {
		target := a.schema.links[t.Identifier]
		if target == nil {
			continue
		}
		if target.create != nil {
			name, forward, backward := target.create.name, truncate(target.create.name, trace.MaxPhraseLength),
				truncate("inverse of "+target.create.name, trace.MaxPhraseLength)
			made, _, err := s.trace.CreateProjectType(ctx, actor, key, trace.LinkTypeInput{Key: target.create.key, Name: &name, Forward: &forward, Backward: &backward})
			if err != nil {
				return fmt.Errorf("make the link type %s: %w", name, err)
			}
			target.id = &made.ID
		}
		if target.id != nil {
			ties = append(ties, tieRow{kind: "relation_type", identifier: t.Identifier, target: *target.id})
		}
	}
	return s.tie(ctx, j, ties)
}

// defChange is what an import needs of one attribute of the project: options it lacks and types it must show on.
type defChange struct {
	def     *attribute.Def
	missing []string
	types   []uuid.UUID
	enums   []*DataType
}

func (s *Service) makeAttribute(ctx context.Context, j *job, c *createAttr, typeOf map[string]*uuid.UUID) (*attribute.Def, error) {
	in := attribute.DefInput{Name: &c.name, Kind: c.kind}
	if c.kind == attribute.Select || c.kind == attribute.MultiSelect {
		options := make([]attribute.OptionInput, 0, len(c.options))
		for _, o := range c.options {
			options = append(options, attribute.OptionInput{Name: o})
		}
		in.Options = &options
	}
	var types []uuid.UUID
	for _, t := range c.types {
		if typeOf[t] == nil {
			// A spec type that becomes no type takes the attribute on every type.
			types = nil
			break
		}
		types = append(types, *typeOf[t])
	}
	in.TypeIDs = &types
	made, _, err := s.attributes.CreateAttribute(ctx, j.createdBy, j.ProjectKey, in)
	if err != nil {
		return nil, fmt.Errorf("make the attribute %s: %w", c.name, err)
	}
	return made, nil
}

// widen adds the options and types an attribute lacks for the file, and answers it as it is then.
func (s *Service) widen(ctx context.Context, j *job, c *defChange) (*attribute.Def, error) {
	def := c.def
	var in attribute.DefInput
	if len(def.TypeIDs) > 0 {
		types := slices.Clone(def.TypeIDs)
		for _, t := range c.types {
			if !slices.Contains(types, t) {
				types = append(types, t)
			}
		}
		if len(types) > len(def.TypeIDs) {
			in.TypeIDs = &types
		}
	}
	var added []attribute.OptionInput
	for _, name := range c.missing {
		if !slices.ContainsFunc(def.Options, func(o attribute.Option) bool { return strings.EqualFold(o.Name, name) && !o.Retired }) &&
			!slices.ContainsFunc(added, func(o attribute.OptionInput) bool { return strings.EqualFold(o.Name, name) }) {
			added = append(added, attribute.OptionInput{Name: name})
		}
	}
	if len(added) > 0 {
		options := make([]attribute.OptionInput, 0, len(def.Options)+len(added))
		for _, o := range def.Options {
			options = append(options, attribute.OptionInput{ID: &o.ID, Name: o.Name, Retired: o.Retired})
		}
		options = append(options, added...)
		in.Options = &options
	}
	if in.TypeIDs == nil && in.Options == nil {
		return def, nil
	}
	changed, _, err := s.attributes.UpdateAttribute(ctx, j.createdBy, j.ProjectKey, def.ID, in)
	if err != nil {
		return nil, fmt.Errorf("widen the attribute %s: %w", def.Name, err)
	}
	return changed, nil
}

// optionTies tie the enumeration values of the file to the options of an attribute of their names.
func optionTies(def *attribute.Def, enums []*DataType) []tieRow {
	var ties []tieRow
	seen := map[string]bool{}
	for _, dt := range enums {
		if dt == nil {
			continue
		}
		for _, v := range dt.Values {
			if seen[v.Identifier] {
				continue
			}
			seen[v.Identifier] = true
			name := strings.TrimSpace(truncate(nameOf(v.Identity), attribute.MaxNameLength))
			for _, o := range def.Options {
				if strings.EqualFold(o.Name, name) && !o.Retired {
					ties = append(ties, tieRow{kind: "option", identifier: v.Identifier, target: o.ID})
					break
				}
			}
		}
	}
	return ties
}

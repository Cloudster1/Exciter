package attribute

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute/formula"
	"github.com/cloudster1/exciter/backend/internal/db"
)

// Computed attributes (#213): a formula is checked against the project's attributes and link types when saved,
// kept with them named by id, and spelled with today's names when read.

// InFormulaError refuses retiring or deleting an attribute a formula reads.
type InFormulaError struct {
	// Name is the computed attribute whose formula reads it.
	Name string
}

func (e *InFormulaError) Error() string { return "the formula of " + e.Name + " reads it" }

// ComputedKinds are the kinds a formula gives.
var ComputedKinds = []Kind{Number, Text, Boolean, Date}

// formulaKind is how a formula reads an attribute of a kind: a choice, a person or rich text as its words.
func formulaKind(k Kind) formula.Kind {
	switch k {
	case Number:
		return formula.Number
	case Boolean:
		return formula.Boolean
	case Date:
		return formula.Date
	}
	return formula.Text
}

// kindOf is the attribute kind that keeps what a formula gives.
func kindOf(k formula.Kind) Kind {
	switch k {
	case formula.Number:
		return Number
	case formula.Boolean:
		return Boolean
	case formula.Date:
		return Date
	}
	return Text
}

// linkTypesOf reads the link types a project's formulas may follow, the installation's and its own.
func linkTypesOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]formula.LinkType, error) {
	rows, err := tx.Query(ctx, `SELECT id, key, acyclic FROM link_type WHERE project_id IS NULL OR project_id = $1 ORDER BY key`, projectID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (formula.LinkType, error) {
		var l formula.LinkType
		return l, row.Scan(&l.ID, &l.Key, &l.Acyclic)
	})
}

func catalogOf(defs []Def, links []formula.LinkType, self uuid.UUID) formula.Catalog {
	cat := formula.Catalog{LinkTypes: links, Self: self}
	for _, d := range defs {
		cat.Attributes = append(cat.Attributes, formula.Attribute{ID: d.ID, Name: d.Name, Kind: formulaKind(d.Kind)})
	}
	return cat
}

// spell fills in each computed attribute's formula with the names its attributes and link types have now.
func spell(ctx context.Context, tx db.DBTX, projectID uuid.UUID, defs []Def) error {
	if !slices.ContainsFunc(defs, Def.Computed) {
		return nil
	}
	links, err := linkTypesOf(ctx, tx, projectID)
	if err != nil {
		return err
	}
	cat := catalogOf(defs, links, uuid.Nil)
	for i := range defs {
		if defs[i].Computed() {
			shown := formula.Display(defs[i].stored, cat)
			defs[i].Formula = &shown
		}
	}
	return nil
}

// programs compiles every computed attribute of a project as kept, by its id.
func programs(defs []Def, links []formula.LinkType) map[uuid.UUID]*formula.Program {
	out := map[uuid.UUID]*formula.Program{}
	for _, d := range defs {
		if !d.Computed() {
			continue
		}
		if p, err := formula.Compile(d.stored, catalogOf(defs, links, d.ID)); err == nil {
			out[d.ID] = p
		}
	}
	return out
}

// compileFor checks a formula for an attribute of a project, among the others as they would be once it is saved,
// and refuses one that would read itself round; it answers the formula as it is kept.
func compileFor(ctx context.Context, tx db.DBTX, projectID uuid.UUID, self Def, text string) (*formula.Program, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('computed ' || $1::text, 0))`, projectID); err != nil {
		return nil, err
	}
	defs, err := Defs(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(defs, func(d Def) bool { return d.ID == self.ID }) {
		defs = append(defs, self)
	}
	links, err := linkTypesOf(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	prog, ferr := formula.Compile(text, catalogOf(defs, links, self.ID))
	if ferr != nil {
		return nil, &FieldError{"formula", ferr.Message}
	}
	all := programs(defs, links)
	all[self.ID] = prog
	if err := cyclesOf(defs, links, all); err != nil {
		return nil, err
	}
	return prog, nil
}

// cyclesOf refuses formulas that would read themselves round on one requirement.
func cyclesOf(defs []Def, links []formula.LinkType, all map[uuid.UUID]*formula.Program) error {
	var checked []formula.Def
	for _, d := range defs {
		if p, ok := all[d.ID]; ok {
			checked = append(checked, formula.Def{ID: d.ID, Name: d.Name, Reads: p.Reads})
		}
	}
	acyclic := func(id uuid.UUID) bool {
		return slices.ContainsFunc(links, func(l formula.LinkType) bool { return l.ID == id && l.Acyclic })
	}
	var cycle *formula.CycleError
	if err := formula.Cycles(checked, acyclic); errors.As(err, &cycle) {
		return &FieldError{"formula", cycle.Error()}
	}
	return nil
}

// readBy names the computed attribute whose formula reads id, empty when none does.
func readBy(ctx context.Context, tx db.DBTX, projectID, id uuid.UUID) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM attribute_def WHERE project_id = $1 AND id <> $2 AND formula LIKE '%"@' || $2::text || '"%'
		ORDER BY position LIMIT 1`, projectID, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// recompute marks a project's requirements to work its attributes out again, as its administrator.
func recompute(ctx context.Context, tx db.DBTX, projectID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT computed_recompute($1)`, projectID)
	return err
}

// FollowsAcyclic names a computed attribute whose formula is only safe while a link type stays acyclic, empty when
// none is, for the link type's change to refuse.
func FollowsAcyclic(ctx context.Context, tx db.DBTX, typeID uuid.UUID) (string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT project_id FROM attribute_def WHERE formula LIKE '%''@' || $1::text || '''%'`, typeID)
	if err != nil {
		return "", err
	}
	projects, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return "", err
	}
	for _, p := range projects {
		defs, err := Defs(ctx, tx, p)
		if err != nil {
			return "", err
		}
		links, err := linkTypesOf(ctx, tx, p)
		if err != nil {
			return "", err
		}
		for i := range links {
			if links[i].ID == typeID {
				links[i].Acyclic = false
			}
		}
		if cyclesOf(defs, links, programs(defs, links)) != nil {
			for _, d := range defs {
				if d.Computed() && strings.Contains(d.stored, "'@"+typeID.String()+"'") {
					return d.Name, nil
				}
			}
		}
	}
	return "", nil
}

// checkComputed refuses on a computed attribute what only a typed one has.
func checkComputed(in DefInput) error {
	switch {
	case in.Required != nil && *in.Required:
		return &FieldError{"required", "A computed attribute is never typed, so it cannot be required."}
	case len(in.RequiredFrom) > 0 && string(in.RequiredFrom) != "null":
		return &FieldError{"requiredFrom", "A computed attribute is never typed, so no status asks for it."}
	case len(in.DependsOn) > 0 && string(in.DependsOn) != "null":
		return &FieldError{"dependsOn", "A computed attribute offers no choices to narrow."}
	case in.Options != nil && len(*in.Options) > 0:
		return &FieldError{"options", "A computed attribute offers no options."}
	case in.EditableBy != nil && len(*in.EditableBy) > 0:
		return &FieldError{"editableBy", "Nobody edits a computed attribute; its formula works it out."}
	}
	return nil
}

// progressOf reads how far a project's recompute has got, nil when none is running.
func progressOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (*Recompute, error) {
	var r Recompute
	err := tx.QueryRow(ctx, `SELECT total, done, started_at FROM computed_progress($1)`, projectID).Scan(&r.Total, &r.Done, &r.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the recompute: %w", err)
	}
	return &r, nil
}

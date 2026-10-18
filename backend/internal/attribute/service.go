package attribute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
)

var (
	ErrNotFound = errors.New("type or attribute not found")
	// ErrForbidden is somebody who may view a project changing its types and attributes, its administrators' to change.
	ErrForbidden = errors.New("only the project's administrators change its types and attributes")
	// ErrDefaultType is deleting the type a project gives new requirements.
	ErrDefaultType = errors.New("the project's default type cannot go")
)

// InUseError refuses deleting what a requirement or a revision has, which is retired instead.
type InUseError struct {
	// What is "type" or "attribute"; Count is how many requirements and revisions have it.
	What  string
	Count int
}

func (e *InUseError) Error() string {
	return fmt.Sprintf("the %s is used by %d requirements and revisions", e.What, e.Count)
}

var (
	iconRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	colourRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

// kinds describe each kind for the form that defines an attribute, in the order it offers them.
var kinds = []KindInfo{
	{Text, "Text", "A line of text, up to 500 characters.", false},
	{RichText, "Rich text", "Formatted text, as in a statement.", false},
	{Number, "Number", "A number, such as 12 or 2.5.", false},
	{Select, "Select", "One of a list of options.", true},
	{MultiSelect, "Multi-select", "Any of a list of options.", true},
	{Date, "Date", "A day.", false},
	{User, "Person", "A member who may view the project.", false},
	{Boolean, "Yes or no", "True or false.", false},
	{URL, "Web address", "An http or https address.", false},
}

// KindInfos describes what an attribute can be.
func KindInfos() []KindInfo { return slices.Clone(kinds) }

// Service keeps a project's requirement types and attributes.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// shaping reads a project for a change to its types and attributes, which only its administrators make.
func shaping(ctx context.Context, tx db.DBTX, actor uuid.UUID, projectKey string) (*project.Project, error) {
	p, err := project.Find(ctx, tx, actor, projectKey)
	switch {
	case err != nil:
		return nil, err
	case p.ArchivedAt != nil:
		return nil, project.ErrArchived
	case !p.Can.Administer:
		return nil, ErrForbidden
	}
	return p, nil
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", &FieldError{"name", "Give it a name."}
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return "", &FieldError{"name", fmt.Sprintf("Keep the name to %d characters.", MaxNameLength)}
	}
	return name, nil
}

func checkDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > MaxDescriptionLength {
		return "", &FieldError{"description", fmt.Sprintf("Keep the description to %d characters.", MaxDescriptionLength)}
	}
	return description, nil
}

func nameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// reorder moves one row of a project's types or attributes to a position, and numbers the rest after it.
func reorder(ctx context.Context, tx db.DBTX, table string, projectID, id uuid.UUID, to int) error {
	rows, err := tx.Query(ctx, `SELECT id FROM `+table+` WHERE project_id = $1 ORDER BY position, lower(name)`, projectID)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	ids = slices.DeleteFunc(ids, func(each uuid.UUID) bool { return each == id })
	if id != uuid.Nil {
		ids = slices.Insert(ids, min(max(to, 0), len(ids)), id)
	}
	for i, each := range ids {
		if _, err := tx.Exec(ctx, `UPDATE `+table+` SET position = $2 WHERE id = $1 AND position <> $2`, each, i); err != nil {
			return err
		}
	}
	return nil
}

const selectDef = `
SELECT a.id, a.name, a.description, a.kind, a.required, a.options, a.position, a.retired, a.created_at, a.updated_at,
       COALESCE((SELECT array_agg(dt.type_id ORDER BY t.position) FROM attribute_def_type dt
                 JOIN requirement_type t ON t.id = dt.type_id WHERE dt.attribute_id = a.id), '{}'),
       a.required_from, a.depends_on, a.choices, a.editable_by, a.formula, a.on_sections
FROM attribute_def a`

func scanDef(row pgx.CollectableRow) (Def, error) {
	var (
		d                         Def
		options, choices, permits []byte
		dependsOn                 *uuid.UUID
	)
	var stored *string
	err := row.Scan(&d.ID, &d.Name, &d.Description, &d.Kind, &d.Required, &options, &d.Position, &d.Retired, &d.CreatedAt, &d.UpdatedAt, &d.TypeIDs,
		&d.RequiredFrom, &dependsOn, &choices, &permits, &stored, &d.OnSections)
	if err != nil {
		return d, err
	}
	if stored != nil {
		d.stored, d.Formula = *stored, stored
	}
	if dependsOn != nil {
		d.DependsOn = &Dependency{AttributeID: *dependsOn}
		if err := json.Unmarshal(choices, &d.DependsOn.Choices); err != nil {
			return d, fmt.Errorf("read the choices of %s: %w", d.Name, err)
		}
		if d.DependsOn.Choices == nil {
			d.DependsOn.Choices = map[string][]uuid.UUID{}
		}
	}
	if err := json.Unmarshal(permits, &d.EditableBy); err != nil {
		return d, fmt.Errorf("read who changes %s: %w", d.Name, err)
	}
	if d.EditableBy == nil {
		d.EditableBy = []Permit{}
	}
	if err := json.Unmarshal(options, &d.Options); err != nil {
		return d, fmt.Errorf("read the options of %s: %w", d.Name, err)
	}
	if d.Options == nil {
		d.Options = []Option{}
	}
	return d, nil
}

// Defs reads, in the caller's transaction, a project's attributes in order, retired ones included.
func Defs(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]Def, error) {
	rows, err := tx.Query(ctx, selectDef+` WHERE a.project_id = $1 ORDER BY a.position, lower(a.name)`, projectID)
	if err != nil {
		return nil, err
	}
	defs, err := pgx.CollectRows(rows, scanDef)
	if err != nil {
		return nil, err
	}
	return defs, spell(ctx, tx, projectID, defs)
}

func defByID(ctx context.Context, tx db.DBTX, projectID, id uuid.UUID) (*Def, error) {
	defs, err := Defs(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(defs, func(d Def) bool { return d.ID == id })
	if i < 0 {
		return nil, ErrNotFound
	}
	return &defs[i], nil
}

// Progress answers how far a project's recompute has got after a formula changed, nil when none is running.
func (s *Service) Progress(ctx context.Context, reader uuid.UUID, projectKey string) (*Recompute, error) {
	var out *Recompute
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		out, err = progressOf(ctx, tx, p.ID)
		return err
	})
	return out, err
}

// Attributes answers a project's attributes in order, retired ones included.
func (s *Service) Attributes(ctx context.Context, reader uuid.UUID, projectKey string) ([]Def, error) {
	out := []Def{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		defs, err := Defs(ctx, tx, p.ID)
		if defs != nil {
			out = defs
		}
		return err
	})
	return out, err
}

// mergeOptions replaces an attribute's options with the list sent: one without an id is new, one left out is
// retired, and every id a value may name is kept.
func mergeOptions(kind Kind, current []Option, sent []OptionInput) ([]Option, error) {
	if kind != Select && kind != MultiSelect {
		if len(sent) > 0 {
			return nil, &FieldError{"options", "Only a select or a multi-select attribute has options."}
		}
		return []Option{}, nil
	}
	out := make([]Option, 0, len(sent))
	names := map[string]bool{}
	kept := map[uuid.UUID]bool{}
	live := 0
	for _, in := range sent {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return nil, &FieldError{"options", "Give every option a name, or leave it out."}
		}
		if utf8.RuneCountInString(name) > MaxNameLength {
			return nil, &FieldError{"options", fmt.Sprintf("Keep each option to %d characters.", MaxNameLength)}
		}
		if names[strings.ToLower(name)] {
			return nil, &FieldError{"options", fmt.Sprintf("%s is there twice. Give each option its own name.", name)}
		}
		names[strings.ToLower(name)] = true
		o := Option{ID: uuid.Must(uuid.NewV7()), Name: name, Retired: in.Retired}
		if in.ID != nil {
			if !slices.ContainsFunc(current, func(c Option) bool { return c.ID == *in.ID }) {
				return nil, &FieldError{"options", "An option names an id this attribute never had. Leave the id out to add a new one."}
			}
			o.ID = *in.ID
			kept[o.ID] = true
		}
		if !o.Retired {
			live++
		}
		out = append(out, o)
	}
	for _, c := range current {
		if !kept[c.ID] {
			c.Retired = true
			out = append(out, c)
		}
	}
	if live == 0 {
		return nil, &FieldError{"options", "A select attribute needs at least one option to choose."}
	}
	if live > MaxOptions {
		return nil, &FieldError{"options", fmt.Sprintf("An attribute offers at most %d options.", MaxOptions)}
	}
	return out, nil
}

// setTypes replaces the types an attribute shows on; each must be one of the project's.
func setTypes(ctx context.Context, tx db.DBTX, projectID, attributeID uuid.UUID, typeIDs []uuid.UUID) error {
	for _, id := range typeIDs {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM requirement_type WHERE project_id = $1 AND id = $2)`, projectID, id).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return &FieldError{"typeIds", "A type named is not one of this project's. Pick them from its types."}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM attribute_def_type WHERE attribute_id = $1`, attributeID); err != nil {
		return err
	}
	for _, id := range typeIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO attribute_def_type (attribute_id, type_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			attributeID, id); err != nil {
			return err
		}
	}
	return nil
}

// CreateAttribute defines an attribute, last in the project's order unless a position says otherwise.
func (s *Service) CreateAttribute(ctx context.Context, actor uuid.UUID, projectKey string, in DefInput) (*Def, db.LSN, error) {
	computed := in.Formula != nil
	if computed {
		if err := checkComputed(in); err != nil {
			return nil, 0, err
		}
		// The formula decides the kind, once it is checked.
		if in.Kind == "" {
			in.Kind = Number
		}
	} else if in.OnSections != nil && *in.OnSections {
		return nil, 0, &FieldError{"onSections", "Only a computed attribute is worked out for sections."}
	}
	if !slices.Contains(Kinds, in.Kind) {
		return nil, 0, &FieldError{"kind", "Pick what kind of value the attribute holds."}
	}
	if in.Name == nil {
		return nil, 0, &FieldError{"name", "Give it a name."}
	}
	name, err := checkName(*in.Name)
	if err != nil {
		return nil, 0, err
	}
	description := ""
	if in.Description != nil {
		if description, err = checkDescription(*in.Description); err != nil {
			return nil, 0, err
		}
	}
	var sent []OptionInput
	if in.Options != nil {
		sent = *in.Options
	}
	for _, o := range sent {
		if o.ID != nil {
			return nil, 0, &FieldError{"options", "A new attribute's options have no ids yet. Leave them out."}
		}
	}
	options, err := mergeOptions(in.Kind, nil, sent)
	if err != nil {
		return nil, 0, err
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return nil, 0, err
	}
	var out *Def
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM attribute_def WHERE project_id = $1`, p.ID).Scan(&count); err != nil {
			return err
		}
		if count >= MaxAttributes {
			return &FieldError{"name", fmt.Sprintf("A project has at most %d attributes. Retire or delete one first.", MaxAttributes)}
		}
		id := uuid.Must(uuid.NewV7())
		var stored *string
		if computed {
			prog, err := compileFor(ctx, tx, p.ID, Def{ID: id, Name: name, Kind: in.Kind}, *in.Formula)
			if err != nil {
				return err
			}
			text := prog.Stored()
			stored, in.Kind = &text, kindOf(prog.Kind)
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO attribute_def (id, project_id, name, description, kind, required, options, position, formula, on_sections)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			id, p.ID, name, description, in.Kind, in.Required != nil && *in.Required, encoded, count, stored,
			computed && in.OnSections != nil && *in.OnSections).Scan(&id)
		if nameTaken(err) {
			return &FieldError{"name", "Another attribute of this project is called that. Pick another name."}
		}
		if err != nil {
			return err
		}
		if in.TypeIDs != nil {
			if err := setTypes(ctx, tx, p.ID, id, *in.TypeIDs); err != nil {
				return err
			}
		}
		if in.Position != nil {
			if err := reorder(ctx, tx, "attribute_def", p.ID, id, *in.Position); err != nil {
				return err
			}
		}
		if out, err = defByID(ctx, tx, p.ID, id); err != nil {
			return err
		}
		if err := setRules(ctx, tx, actor, p.ID, p.Key, out, in); err != nil {
			return err
		}
		if computed {
			if err := recompute(ctx, tx, p.ID); err != nil {
				return err
			}
		}
		out, err = defByID(ctx, tx, p.ID, id)
		return err
	})
	return out, lsn, err
}

// UpdateAttribute changes an attribute's name, options, types, order, or whether it is required or retired.
func (s *Service) UpdateAttribute(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID, in DefInput) (*Def, db.LSN, error) {
	var out *Def
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		d, err := defByID(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		if in.Kind != "" && in.Kind != d.Kind {
			return &FieldError{"kind", "The kind is set when an attribute is made. Make a new attribute for another kind."}
		}
		changedFormula := false
		switch {
		case d.Computed():
			if err := checkComputed(in); err != nil {
				return err
			}
			if in.Formula != nil {
				prog, err := compileFor(ctx, tx, p.ID, *d, *in.Formula)
				if err != nil {
					return err
				}
				if kindOf(prog.Kind) != d.Kind {
					return &FieldError{"formula", fmt.Sprintf("This formula gives %s, and %s holds %s. Make a new attribute for the other kind.", prog.Kind, d.Name, d.Kind)}
				}
				changedFormula = prog.Stored() != d.stored
				d.stored = prog.Stored()
			}
			if in.OnSections != nil && *in.OnSections != d.OnSections {
				d.OnSections, changedFormula = *in.OnSections, true
			}
		case in.Formula != nil:
			return &FieldError{"formula", "A typed attribute stays typed. Make a new computed attribute instead."}
		case in.OnSections != nil && *in.OnSections:
			return &FieldError{"onSections", "Only a computed attribute is worked out for sections."}
		}
		if in.Retired != nil && *in.Retired && !d.Retired {
			name, err := readBy(ctx, tx, p.ID, id)
			if err != nil {
				return err
			}
			if name != "" {
				return &InFormulaError{Name: name}
			}
		}
		if in.Name != nil {
			if d.Name, err = checkName(*in.Name); err != nil {
				return err
			}
		}
		if in.Description != nil {
			if d.Description, err = checkDescription(*in.Description); err != nil {
				return err
			}
		}
		if in.Required != nil {
			d.Required = *in.Required
		}
		if in.Retired != nil {
			d.Retired = *in.Retired
		}
		if in.Options != nil {
			if d.Options, err = mergeOptions(d.Kind, d.Options, *in.Options); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(d.Options)
		if err != nil {
			return err
		}
		var stored *string
		if d.Computed() {
			stored = &d.stored
		}
		_, err = tx.Exec(ctx, `UPDATE attribute_def SET name = $2, description = $3, required = $4, retired = $5, options = $6, formula = $7,
			on_sections = $8 WHERE id = $1`,
			id, d.Name, d.Description, d.Required, d.Retired, encoded, stored, d.OnSections)
		if nameTaken(err) {
			return &FieldError{"name", "Another attribute of this project is called that. Pick another name."}
		}
		if err != nil {
			return err
		}
		if in.TypeIDs != nil {
			if err := setTypes(ctx, tx, p.ID, id, *in.TypeIDs); err != nil {
				return err
			}
			changedFormula = changedFormula || d.Computed()
		}
		if in.Position != nil {
			if err := reorder(ctx, tx, "attribute_def", p.ID, id, *in.Position); err != nil {
				return err
			}
		}
		if out, err = defByID(ctx, tx, p.ID, id); err != nil {
			return err
		}
		if err := setRules(ctx, tx, actor, p.ID, p.Key, out, in); err != nil {
			return err
		}
		if changedFormula {
			if err := recompute(ctx, tx, p.ID); err != nil {
				return err
			}
		}
		out, err = defByID(ctx, tx, p.ID, id)
		return err
	})
	return out, lsn, err
}

// DeleteAttribute removes an attribute no requirement and no revision ever had a value for.
func (s *Service) DeleteAttribute(ctx context.Context, actor uuid.UUID, projectKey string, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := shaping(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		d, err := defByID(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		name, err := readBy(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		if name != "" {
			return &InFormulaError{Name: name}
		}
		var used int
		if err := tx.QueryRow(ctx, `SELECT attribute_usage($1)`, id).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return &InUseError{What: "attribute", Count: used}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM attribute_def WHERE id = $1`, id); err != nil {
			return err
		}
		if d.Computed() {
			if err := recompute(ctx, tx, p.ID); err != nil {
				return err
			}
		}
		return reorder(ctx, tx, "attribute_def", p.ID, uuid.Nil, 0)
	})
}

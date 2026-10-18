package attribute

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute/formula"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
)

// ErrNoSuchRequirement is a formula tried on a requirement the project does not have.
var ErrNoSuchRequirement = errors.New("no such requirement in the project")

// CheckFormula checks a formula as it is written, for the project's administrators: what is wrong and where, what
// it gives and reads, and its value for a requirement of the project.
func (s *Service) CheckFormula(ctx context.Context, actor uuid.UUID, projectKey string, in formula.CheckInput) (*formula.Check, error) {
	out := &formula.Check{Formula: in.Formula, Errors: []formula.Error{}, Reads: []string{}, LinkTypes: []string{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, actor, projectKey)
		if err != nil {
			return err
		}
		if !p.Can.Administer {
			return ErrForbidden
		}
		defs, err := Defs(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		links, err := linkTypesOf(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		self := Def{ID: uuid.Must(uuid.NewV7()), Name: "this attribute", OnSections: in.OnSections}
		if id, err := uuid.Parse(in.AttributeID); err == nil {
			if i := slices.IndexFunc(defs, func(d Def) bool { return d.ID == id }); i >= 0 {
				self = defs[i]
				self.OnSections = in.OnSections
			}
		}
		if !slices.ContainsFunc(defs, func(d Def) bool { return d.ID == self.ID }) {
			defs = append(defs, self)
		}
		cat := catalogOf(defs, links, self.ID)
		prog, ferr := formula.Compile(in.Formula, cat)
		if ferr != nil {
			out.Errors = append(out.Errors, *ferr)
			return nil
		}
		out.Kind, out.Formula = prog.Kind, formula.Display(prog.Stored(), cat)
		out.Reads, out.LinkTypes = readNames(prog.Reads, defs, links)
		all := programs(defs, links)
		all[self.ID] = prog
		var field *FieldError
		if err := cyclesOf(defs, links, all); errors.As(err, &field) {
			out.Errors = append(out.Errors, formula.Error{From: 1, To: max(1, utf8.RuneCountInString(in.Formula)), Message: field.Message})
			return nil
		}
		if strings.TrimSpace(in.RequirementKey) == "" {
			return nil
		}
		return tryOn(ctx, tx, p.ID, defs, self, prog, in.RequirementKey, out)
	})
	return out, err
}

// readNames are what a formula reads, its attributes by name and its link types by key.
func readNames(r formula.Reads, defs []Def, links []formula.LinkType) (names, keys []string) {
	names, keys = []string{}, []string{}
	add := func(id uuid.UUID) {
		if i := slices.IndexFunc(defs, func(d Def) bool { return d.ID == id }); i >= 0 && !slices.Contains(names, defs[i].Name) {
			names = append(names, defs[i].Name)
		}
	}
	for _, id := range slices.Concat(r.Self, r.Children) {
		add(id)
	}
	for _, l := range r.Links {
		for _, id := range l.Attributes {
			add(id)
		}
		if i := slices.IndexFunc(links, func(t formula.LinkType) bool { return t.ID == l.TypeID }); i >= 0 && !slices.Contains(keys, links[i].Key) {
			keys = append(keys, links[i].Key)
		}
	}
	return names, keys
}

// tryOn works a formula not yet saved out for one requirement of the project, reading the others' values as kept.
func tryOn(ctx context.Context, tx db.DBTX, projectID uuid.UUID, defs []Def, self Def, prog *formula.Program, key string, out *formula.Check) error {
	var id uuid.UUID
	var section bool
	err := tx.QueryRow(ctx, `SELECT r.id, r.kind = 'section' FROM requirement r JOIN project p ON p.id = r.project_id
		WHERE r.project_id = $1 AND p.key || '-R' || r.number = upper(btrim($2))`, projectID, key).Scan(&id, &section)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSuchRequirement
	}
	if err != nil {
		return err
	}
	if section && !self.OnSections {
		out.ValueError = "This formula is worked out for requirements; let it work out for sections too to try it on one."
		return nil
	}
	self.TypeIDs = nil
	w := &world{tx: tx, project: projectID, byID: map[uuid.UUID]Def{}, programs: map[uuid.UUID]*formula.Program{self.ID: prog},
		people: map[uuid.UUID]string{}, today: day(time.Now()), order: []Def{self}, children: prog.Reads.ReadsChildren}
	for _, d := range defs {
		w.byID[d.ID] = d
	}
	w.byID[self.ID] = self
	for _, l := range prog.Reads.Links {
		w.ways = append(w.ways, linkWay{l.TypeID, l.Out})
	}
	results, err := w.work(ctx, []uuid.UUID{id}, nil)
	if err != nil {
		return err
	}
	out.Value = results[id].values[self.ID.String()]
	return nil
}

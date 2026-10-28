package risk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/search"
)

// RowsQuery narrows the FMEA table: the grid's filter and order, the side the matrix reads, a cell of it, and a page.
type RowsQuery struct {
	Filter search.Filter
	Sort   search.Sort
	Side   Side
	// Severity and Probability keep the risks of one cell of the risk matrix on Side, when both are set.
	Severity, Probability *int
	Limit, Offset         int
}

// risky is one risk the reader sees, as the table and the matrix read it.
type risky struct {
	row  Row
	view requirement.RiskView
}

// effective is the side a risk counts with: after once it is mitigated and that side has a level, before otherwise.
func (r risky) effective() requirement.Side {
	if r.view.State != nil && *r.view.State == requirement.Mitigated && r.view.AfterLevel != nil {
		return r.view.After
	}
	return r.view.Before
}

// on is the side a query reads.
func (r risky) on(side Side) requirement.Side {
	if side == SideBefore {
		return r.view.Before
	}
	return r.effective()
}

// rated says whether a side gives the severity and the probability a cell of the matrix needs.
func rated(s requirement.Side) bool { return s.Severity != nil && s.Probability != nil }

// readRisks reads the risks of a project the filter keeps, in its order, each with its mitigations and the levels
// and state its rating and they give under the scheme.
func readRisks(ctx context.Context, tx db.DBTX, reader uuid.UUID, p *project.Project, f search.Filter, sort search.Sort) ([]risky, error) {
	kept, err := search.Keep(ctx, tx, reader, p, f, sort)
	if err != nil {
		return nil, err
	}
	ids, edit := make([]uuid.UUID, len(kept)), map[uuid.UUID]bool{}
	for i, k := range kept {
		ids[i], edit[k.Ref.ID] = k.Ref.ID, k.Edit
	}
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.number, r.title, r.revision, t.id, t.name, t.icon, t.colour, r.attributes || requirement_computed_of(r.id), r.risk
		FROM unnest($1::uuid[]) WITH ORDINALITY k(id, n)
		JOIN requirement r ON r.id = k.id AND r.kind = 'requirement'
		JOIN requirement_type t ON t.id = r.type_id AND t.role = 'risk'
		ORDER BY k.n`, ids)
	if err != nil {
		return nil, fmt.Errorf("read the risks: %w", err)
	}
	var out []risky
	for rows.Next() {
		var (
			x             risky
			number        int
			attrs, rating []byte
		)
		if err := rows.Scan(&x.row.Risk.ID, &number, &x.row.Risk.Title, &x.row.Risk.Revision, &x.row.Risk.Type.ID, &x.row.Risk.Type.Name,
			&x.row.Risk.Type.Icon, &x.row.Risk.Type.Colour, &attrs, &rating); err != nil {
			rows.Close()
			return nil, err
		}
		x.row.Risk.Key = requirement.Key(p.Key, number)
		if err := json.Unmarshal(attrs, &x.row.Attributes); err != nil {
			rows.Close()
			return nil, err
		}
		if x.view.Rating, err = requirement.ReadRating(rating); err != nil {
			rows.Close()
			return nil, err
		}
		x.row.Can.Edit = edit[x.row.Risk.ID] && p.ArchivedAt == nil
		x.row.Mitigations = []Mitigation{}
		out = append(out, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	scheme, err := ReadScheme(ctx, tx, p.ID)
	if err != nil {
		return nil, err
	}
	all := make([]*Row, len(out))
	for i := range out {
		all[i] = &out[i].row
	}
	if err := readMitigations(ctx, tx, all); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].view = viewOf(scheme, out[i].view.Rating, out[i].row.Mitigations)
		out[i].row.Rating = out[i].view
	}
	return out, nil
}

// stateOf is how far a risk's mitigations take it: none, some not in effect, or all in effect.
func stateOf(ms []Mitigation) requirement.RiskState {
	switch {
	case len(ms) == 0:
		return requirement.Unmitigated
	case slices.ContainsFunc(ms, func(m Mitigation) bool { return !m.InEffect }):
		return requirement.Claimed
	}
	return requirement.Mitigated
}

// viewOf works out what a rating and its mitigations give under a scheme, as risk_view does in SQL.
func viewOf(scheme *Scheme, r requirement.Rating, ms []Mitigation) requirement.RiskView {
	state := stateOf(ms)
	v := requirement.RiskView{Rating: r, State: &state}
	if scheme == nil {
		return v
	}
	ref := func(l *Level) *requirement.RiskLevel {
		if l == nil {
			return nil
		}
		return &requirement.RiskLevel{Name: l.Name, Colour: l.Colour, Acceptable: l.Acceptable}
	}
	v.BeforeRPN, v.AfterRPN = scheme.RPN(r.Before), scheme.RPN(r.After)
	v.BeforeLevel, v.AfterLevel = ref(scheme.Level(r.Before)), ref(scheme.Level(r.After))
	v.Level = v.BeforeLevel
	if state == requirement.Mitigated && v.AfterLevel != nil {
		v.Level = v.AfterLevel
	}
	return v
}

// readMitigations gives each row its mitigations, whoever the reader is, sources in order of their keys; a source the
// reader may not view is hidden.
func readMitigations(ctx context.Context, tx db.DBTX, rows []*Row) error {
	at := map[uuid.UUID]*Row{}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i], at[r.Risk.ID] = r.Risk.ID, r
	}
	found, err := tx.Query(ctx, `
		SELECT risk_id, link_id, source_id, source_project, source_number, source_title, visible, implemented, verified, suspect, in_effect
		FROM risk_mitigation_rows($1)`, ids)
	if err != nil {
		return fmt.Errorf("read the mitigations: %w", err)
	}
	defer found.Close()
	for found.Next() {
		var (
			risk    uuid.UUID
			m       Mitigation
			number  int
			visible bool
		)
		if err := found.Scan(&risk, &m.LinkID, &m.Source.ID, &m.Source.ProjectKey, &number, &m.Source.Title, &visible, &m.Implemented, &m.Verified,
			&m.Suspect, &m.InEffect); err != nil {
			return err
		}
		if visible {
			m.Source.Key = requirement.Key(m.Source.ProjectKey, number)
		} else {
			m.Source = SourceRef{Hidden: true}
		}
		at[risk].Mitigations = append(at[risk].Mitigations, m)
	}
	return found.Err()
}

// Rows reads a page of the FMEA table: the project's risks the query keeps, with their mitigations.
func (s *Service) Rows(ctx context.Context, reader uuid.UUID, projectKey string, q RowsQuery) (*Rows, error) {
	out := &Rows{Risks: []Row{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		all, err := readRisks(ctx, tx, reader, p, q.Filter, q.Sort)
		if err != nil {
			return err
		}
		var kept []Row
		for _, r := range all {
			if !rated(r.view.Before) {
				out.Unrated++
			}
			if q.Severity != nil && q.Probability != nil {
				side := r.on(q.Side)
				if !rated(side) || *side.Severity != *q.Severity || *side.Probability != *q.Probability {
					continue
				}
			}
			kept = append(kept, r.row)
		}
		out.Total = len(kept)
		start, end := min(q.Offset, len(kept)), min(q.Offset+q.Limit, len(kept))
		out.Risks = append(out.Risks, kept[start:end]...)
		return nil
	})
	return out, err
}

// Matrix counts the project's risks the filter keeps in each cell of severity and probability on one side.
func (s *Service) Matrix(ctx context.Context, reader uuid.UUID, projectKey string, f search.Filter, sort search.Sort, side Side) (*Matrix, error) {
	// Without a scheme the scales are empty but still name their dimensions, as the API document says they do.
	out := &Matrix{Side: side, Cells: []MatrixCount{},
		Severity:    Scale{Dimension: Severity, Label: defaultLabels[Severity], Values: []ScaleValue{}, Held: []int{}},
		Probability: Scale{Dimension: Probability, Label: defaultLabels[Probability], Values: []ScaleValue{}, Held: []int{}}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		scheme, err := ReadScheme(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		all, err := readRisks(ctx, tx, reader, p, f, sort)
		if err != nil {
			return err
		}
		if scheme == nil {
			out.Unrated = len(all)
			return nil
		}
		for _, sc := range scheme.Scales {
			switch sc.Dimension {
			case Severity:
				out.Severity = sc
			case Probability:
				out.Probability = sc
			}
		}
		counts := map[[2]int]int{}
		for _, r := range all {
			on := r.on(side)
			if !rated(on) {
				out.Unrated++
				continue
			}
			counts[[2]int{*on.Severity, *on.Probability}]++
		}
		for _, pv := range out.Probability.Values {
			for _, sv := range out.Severity.Values {
				cell := MatrixCount{Severity: sv.Value, Probability: pv.Value, Count: counts[[2]int{sv.Value, pv.Value}]}
				if scheme.Method == MethodMatrix {
					severity, probability := sv.Value, pv.Value
					if l := scheme.Level(requirement.Side{Severity: &severity, Probability: &probability}); l != nil {
						cell.Level = &requirement.RiskLevel{Name: l.Name, Colour: l.Colour, Acceptable: l.Acceptable}
					}
				}
				out.Cells = append(out.Cells, cell)
			}
		}
		return nil
	})
	return out, err
}

// Mitigations reads the mitigations of a risk the reader sees and the state they put it in.
func (s *Service) Mitigations(ctx context.Context, reader uuid.UUID, key string) (*Mitigations, error) {
	var out *Mitigations
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := requirement.FindEndpoint(ctx, tx, reader, key)
		if errors.Is(err, requirement.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var isRisk bool
		if err := tx.QueryRow(ctx, `SELECT requirement_is_risk($1)`, e.ID).Scan(&isRisk); err != nil {
			return err
		}
		if !isRisk {
			return ErrNotRisk
		}
		row := &Row{Risk: RiskRef{ID: e.ID}, Mitigations: []Mitigation{}}
		if err := readMitigations(ctx, tx, []*Row{row}); err != nil {
			return err
		}
		out = &Mitigations{Mitigations: row.Mitigations, State: stateOf(row.Mitigations)}
		return nil
	})
	return out, err
}

// Export is the FMEA table whole as a workbook writes it, with the attributes and the people its cells name.
type Export struct {
	ProjectID uuid.UUID
	Rows      []Row
	Defs      []attribute.Def
	People    map[uuid.UUID]string
}

// Export reads every risk the query keeps, up to max, with what a workbook needs to name their attributes.
func (s *Service) Export(ctx context.Context, reader uuid.UUID, projectKey string, q RowsQuery, max int) (*Export, error) {
	out := &Export{People: map[uuid.UUID]string{}}
	q.Limit, q.Offset = max, 0
	rows, err := s.Rows(ctx, reader, projectKey, q)
	if err != nil {
		return nil, err
	}
	out.Rows = rows.Risks
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		out.ProjectID = p.ID
		if out.Defs, err = attribute.Defs(ctx, tx, p.ID); err != nil {
			return err
		}
		var ids []uuid.UUID
		for _, d := range out.Defs {
			if d.Kind != attribute.User {
				continue
			}
			for _, r := range out.Rows {
				var id uuid.UUID
				if json.Unmarshal(r.Attributes[d.ID.String()], &id) == nil {
					ids = append(ids, id)
				}
			}
		}
		found, err := tx.Query(ctx, `SELECT id, COALESCE(NULLIF(name, ''), email::text) FROM app_user WHERE id = ANY ($1)`, ids)
		if err != nil {
			return err
		}
		defer found.Close()
		for found.Next() {
			var (
				id   uuid.UUID
				name string
			)
			if err := found.Scan(&id, &name); err != nil {
				return err
			}
			out.People[id] = name
		}
		return found.Err()
	})
	return out, err
}

package attribute

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute/formula"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
)

// What a project's formulas read, worked out for its requirements (#213).

// item is one requirement as a formula reads it.
type item struct {
	id       uuid.UUID
	parent   *uuid.UUID
	section  bool
	trashed  bool
	typeID   *uuid.UUID
	fields   map[string]formula.Value
	typed    map[string]json.RawMessage
	computed map[string]json.RawMessage
	w        *world
}

func (i *item) Field(name string) formula.Value {
	if v, ok := i.fields[name]; ok {
		return v
	}
	return formula.None
}

func (i *item) Attribute(id uuid.UUID) formula.Value {
	d, ok := i.w.byID[id]
	if !ok {
		return formula.None
	}
	key := id.String()
	if d.Computed() {
		return formula.FromJSON(formulaKind(d.Kind), i.computed[key])
	}
	raw := i.typed[key]
	if len(raw) == 0 {
		return formula.None
	}
	switch d.Kind {
	case Number, Boolean, Date, Text, URL:
		return formula.FromJSON(formulaKind(d.Kind), raw)
	case RichText:
		node, err := document.Parse(raw)
		if err != nil {
			return formula.None
		}
		return formula.TextValue(document.PlainText(node))
	case Select:
		var option uuid.UUID
		if json.Unmarshal(raw, &option) == nil {
			return formula.TextValue(optionName(d, option.String()))
		}
	case MultiSelect:
		var options []uuid.UUID
		if json.Unmarshal(raw, &options) == nil {
			names := make([]string, len(options))
			for n, o := range options {
				names[n] = optionName(d, o.String())
			}
			return formula.TextValue(strings.Join(names, ", "))
		}
	case User:
		var person uuid.UUID
		if json.Unmarshal(raw, &person) == nil {
			return formula.TextValue(i.w.people[person])
		}
	}
	return formula.None
}

// scope is an item with what its aggregates read.
type scope struct {
	*item
	children []formula.Item
	linked   map[linkWay][]formula.Item
	today    time.Time
}

type linkWay struct {
	typeID uuid.UUID
	out    bool
}

func (s scope) Children() []formula.Item { return s.children }
func (s scope) Linked(typeID uuid.UUID, out bool) []formula.Item {
	return s.linked[linkWay{typeID, out}]
}
func (s scope) Today() time.Time { return s.today }

// world is a project's computed attributes, compiled, in the order one requirement works them out.
type world struct {
	tx       db.DBTX
	project  uuid.UUID
	byID     map[uuid.UUID]Def
	order    []Def
	programs map[uuid.UUID]*formula.Program
	children bool
	ways     []linkWay
	people   map[uuid.UUID]string
	today    time.Time
}

// newWorld reads and compiles a project's computed attributes; nil when it has none.
func newWorld(ctx context.Context, tx db.DBTX, projectID uuid.UUID, today time.Time) (*world, error) {
	defs, err := Defs(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	links, err := linkTypesOf(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	w := &world{tx: tx, project: projectID, byID: map[uuid.UUID]Def{}, programs: programs(defs, links), people: map[uuid.UUID]string{}, today: today}
	for _, d := range defs {
		w.byID[d.ID] = d
	}
	if len(w.programs) == 0 {
		return nil, nil
	}
	for _, p := range w.programs {
		w.children = w.children || p.Reads.ReadsChildren
		for _, l := range p.Reads.Links {
			if way := (linkWay{l.TypeID, l.Out}); !slices.Contains(w.ways, way) {
				w.ways = append(w.ways, way)
			}
		}
	}
	w.order = selfOrder(defs, w.programs)
	return w, nil
}

// selfOrder lays the computed attributes out so each comes after those it reads of the same requirement.
func selfOrder(defs []Def, programs map[uuid.UUID]*formula.Program) []Def {
	var out []Def
	done := map[uuid.UUID]bool{}
	var visit func(d Def, depth int)
	visit = func(d Def, depth int) {
		if done[d.ID] || depth > len(defs) {
			return
		}
		done[d.ID] = true
		for _, id := range programs[d.ID].Reads.Self {
			if next, ok := findDef(defs, id); ok && programs[id] != nil {
				visit(next, depth+1)
			}
		}
		out = append(out, d)
	}
	for _, d := range defs {
		if programs[d.ID] != nil {
			visit(d, 0)
		}
	}
	return out
}

func findDef(defs []Def, id uuid.UUID) (Def, bool) {
	i := slices.IndexFunc(defs, func(d Def) bool { return d.ID == id })
	if i < 0 {
		return Def{}, false
	}
	return defs[i], true
}

// items reads requirements of the project as formulas read them.
func (w *world) items(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*item, error) {
	out := map[uuid.UUID]*item{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := w.tx.Query(ctx, `
		SELECT r.id, r.parent_id, r.kind = 'section', r.trashed_at IS NOT NULL, r.type_id, r.title, COALESCE(st.name, ''),
		       COALESCE(st.category, ''), COALESCE(t.name, ''), COALESCE(u.name, ''), COALESCE(r.priority, ''), r.revision,
		       r.created_at, r.updated_at, (SELECT q.score FROM requirement_quality_of(r.id) q),
		       (SELECT CASE WHEN count(*) = 0 THEN 'none' WHEN bool_or(tr.outcome IN ('failed', 'errored')) THEN 'failed' ELSE 'passed' END
		        FROM test_result tr JOIN test_run_latest l ON l.run_id = tr.run_id AND l.branch = evidence_counting_branch(l.project_id)
		        WHERE tr.requirement_id = r.id),
		       r.attributes, COALESCE(c."values", '{}'::jsonb)
		FROM requirement r
		LEFT JOIN req_status st ON st.id = r.status_id
		LEFT JOIN requirement_type t ON t.id = r.type_id
		LEFT JOIN app_user u ON u.id = r.owner_id
		LEFT JOIN requirement_computed c ON c.requirement_id = r.id
		WHERE r.id = ANY ($1) AND r.project_id = $2`, ids, w.project)
	if err != nil {
		return nil, fmt.Errorf("read what the formulas read: %w", err)
	}
	defer rows.Close()
	var people []uuid.UUID
	for rows.Next() {
		var (
			it                                                       = &item{w: w, fields: map[string]formula.Value{}}
			title, status, category, typeName, owner, priority, test string
			revision                                                 int
			created, updated                                         time.Time
			quality                                                  *int
		)
		if err := rows.Scan(&it.id, &it.parent, &it.section, &it.trashed, &it.typeID, &title, &status, &category, &typeName, &owner,
			&priority, &revision, &created, &updated, &quality, &test, &it.typed, &it.computed); err != nil {
			return nil, err
		}
		for name, text := range map[string]string{"title": title, "status": status, "category": category, "type": typeName, "owner": owner, "priority": priority} {
			if text != "" {
				it.fields[name] = formula.TextValue(text)
			}
		}
		it.fields["testResult"] = formula.TextValue(test)
		it.fields["revision"] = formula.NumberValue(float64(revision))
		it.fields["created"] = formula.DayValue(day(created))
		it.fields["updated"] = formula.DayValue(day(updated))
		if quality != nil {
			it.fields["quality"] = formula.NumberValue(float64(*quality))
		}
		for key, raw := range it.typed {
			if d, ok := w.byID[uuid.MustParse(key)]; ok && d.Kind == User {
				var person uuid.UUID
				if json.Unmarshal(raw, &person) == nil {
					people = append(people, person)
				}
			}
		}
		out[it.id] = it
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, w.namePeople(ctx, people)
}

func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

func (w *world) namePeople(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := w.tx.Query(ctx, `SELECT id, name FROM app_user WHERE id = ANY ($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		w.people[id] = name
	}
	return rows.Err()
}

// result is what working a requirement out gave: its values, why some could not be had, and whether they changed.
type result struct {
	values  map[string]json.RawMessage
	errors  map[string]string
	changed bool
	found   bool
}

// tooOften is why a value is none when what it reads kept changing.
const tooOften = "It kept changing as what it reads changed, so a cycle of links may lead back to it."

// work works the formulas out for requirements of the project, those marked too often going without a value.
func (w *world) work(ctx context.Context, ids []uuid.UUID, stopped map[uuid.UUID]bool) (map[uuid.UUID]result, error) {
	items, err := w.items(ctx, ids)
	if err != nil {
		return nil, err
	}
	kids := map[uuid.UUID][]uuid.UUID{}
	var more []uuid.UUID
	if w.children {
		rows, err := w.tx.Query(ctx, `SELECT parent_id, id FROM requirement WHERE parent_id = ANY ($1) AND project_id = $2 AND trashed_at IS NULL
			ORDER BY rank`, ids, w.project)
		if err != nil {
			return nil, err
		}
		pairs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]uuid.UUID, error) {
			var p [2]uuid.UUID
			return p, row.Scan(&p[0], &p[1])
		})
		if err != nil {
			return nil, err
		}
		for _, p := range pairs {
			kids[p[0]] = append(kids[p[0]], p[1])
			more = append(more, p[1])
		}
	}
	along := map[linkWay]map[uuid.UUID][]uuid.UUID{}
	for _, way := range w.ways {
		from, to := "target_id", "source_id"
		if way.out {
			from, to = "source_id", "target_id"
		}
		rows, err := w.tx.Query(ctx, `
			SELECT l.`+from+`, l.`+to+` FROM trace_link l JOIN requirement o ON o.id = l.`+to+`
			WHERE l.deleted_at IS NULL AND l.type_id = $2 AND l.`+from+` = ANY ($1) AND o.project_id = $3 AND o.trashed_at IS NULL`,
			ids, way.typeID, w.project)
		if err != nil {
			return nil, err
		}
		pairs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]uuid.UUID, error) {
			var p [2]uuid.UUID
			return p, row.Scan(&p[0], &p[1])
		})
		if err != nil {
			return nil, err
		}
		along[way] = map[uuid.UUID][]uuid.UUID{}
		for _, p := range pairs {
			along[way][p[0]] = append(along[way][p[0]], p[1])
			more = append(more, p[1])
		}
	}
	others, err := w.items(ctx, more)
	if err != nil {
		return nil, err
	}
	pick := func(ids []uuid.UUID) []formula.Item {
		out := make([]formula.Item, 0, len(ids))
		for _, id := range ids {
			if it, ok := others[id]; ok {
				out = append(out, it)
			}
		}
		return out
	}
	results := map[uuid.UUID]result{}
	for _, id := range ids {
		it, ok := items[id]
		if !ok {
			continue
		}
		res := result{values: map[string]json.RawMessage{}, errors: map[string]string{}, found: true}
		before := it.computed
		it.computed = map[string]json.RawMessage{}
		s := scope{item: it, children: pick(kids[id]), linked: map[linkWay][]formula.Item{}, today: w.today}
		for way, byFrom := range along {
			s.linked[way] = pick(byFrom[id])
		}
		for _, d := range w.order {
			if it.trashed || (it.section && !d.OnSections) || (!it.section && !d.Shows(it.typeID)) {
				continue
			}
			key := d.ID.String()
			p := w.programs[d.ID]
			if stopped[id] && (p.Reads.ReadsChildren || len(p.Reads.Links) > 0) {
				res.errors[key] = tooOften
				continue
			}
			if raw := p.Eval(s).JSON(); raw != nil {
				it.computed[key], res.values[key] = raw, raw
			}
		}
		res.changed = !sameValues(before, res.values)
		results[id] = res
	}
	return results, nil
}

// sameValues compares values by what they say, since the database and Go spell some JSON differently.
func sameValues(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		var x, y any
		if json.Unmarshal(v, &x) != nil || json.Unmarshal(b[k], &y) != nil || !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}

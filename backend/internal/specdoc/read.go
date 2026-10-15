package specdoc

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// rowsOf is what a links query answers.
type rowsOf = pgx.Rows

// liveLinks are the links of the requirements now, in either direction, to requirements out of the trash.
func liveLinks(ctx context.Context, tx db.DBTX, ids []uuid.UUID) (rowsOf, error) {
	return tx.Query(ctx, `
		SELECT l.source_id, l.target_id, lt.forward, lt.backward,
		       sp.key, s.number, s.title, tp.key, t.number, t.title
		FROM trace_link l
		JOIN link_type lt ON lt.id = l.type_id
		JOIN requirement s ON s.id = l.source_id JOIN project sp ON sp.id = s.project_id
		JOIN requirement t ON t.id = l.target_id JOIN project tp ON tp.id = t.project_id
		WHERE l.deleted_at IS NULL AND s.trashed_at IS NULL AND t.trashed_at IS NULL
		  AND (l.source_id = ANY($1) OR l.target_id = ANY($1))
		ORDER BY lt.key, l.created_at, l.id`, ids)
}

// baselineLinks are the links a baseline held, each end titled as the baseline holds it when it holds it.
const baselineLinks = `
	SELECT l.source_id, l.target_id, COALESCE(lt.forward, l.type_name), COALESCE(lt.backward, l.type_name),
	       sp.key, s.number, COALESCE(sv.title, s.title), tp.key, t.number, COALESCE(tv.title, t.title)
	FROM baseline_link l
	LEFT JOIN link_type lt ON lt.id = l.type_id
	JOIN requirement s ON s.id = l.source_id JOIN project sp ON sp.id = s.project_id
	JOIN requirement t ON t.id = l.target_id JOIN project tp ON tp.id = t.project_id
	LEFT JOIN baseline_item si ON si.baseline_id = $2 AND si.requirement_id = l.source_id
	LEFT JOIN requirement_revision sv ON sv.requirement_id = si.requirement_id AND sv.number = si.revision
	LEFT JOIN baseline_item ti ON ti.baseline_id = $2 AND ti.requirement_id = l.target_id
	LEFT JOIN requirement_revision tv ON tv.requirement_id = ti.requirement_id AND tv.number = ti.revision
	WHERE l.baseline_id = $2 AND (l.source_id = ANY($1) OR l.target_id = ANY($1))
	ORDER BY l.type_name, l.link_id`

// links reads each requirement's links from it: the forward phrase where it is the source, the backward where it
// is the target.
func (r *reading) links(doc *Document, ids []uuid.UUID, query func(context.Context, db.DBTX, []uuid.UUID) (rowsOf, error)) error {
	rows, err := query(r.ctx, r.tx, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	at := map[uuid.UUID]int{}
	for i, e := range doc.Entries {
		at[e.ID] = i
	}
	for rows.Next() {
		var (
			source, target                uuid.UUID
			forward, backward, sKey, tKey string
			sNumber, tNumber              int
			sTitle, tTitle                string
		)
		if err := rows.Scan(&source, &target, &forward, &backward, &sKey, &sNumber, &sTitle, &tKey, &tNumber, &tTitle); err != nil {
			return err
		}
		if i, ok := at[source]; ok {
			doc.Entries[i].Links = append(doc.Entries[i].Links, Link{Phrase: forward, Key: requirement.Key(tKey, tNumber), Title: tTitle})
		}
		if i, ok := at[target]; ok && target != source {
			doc.Entries[i].Links = append(doc.Entries[i].Links, Link{Phrase: backward, Key: requirement.Key(sKey, sNumber), Title: sTitle})
		}
	}
	return rows.Err()
}

// baselineItems reads a baseline's items in their outline, at the revisions they hold, up to MaxEntries.
func (r *reading) baselineItems(doc *Document, id uuid.UUID, projectKey string) ([]uuid.UUID, error) {
	rows, err := r.tx.Query(r.ctx, `
		SELECT i.requirement_id, i.number, i.kind, i.revision, i.path,
		       cardinality(i.path) - (SELECT min(cardinality(m.path)) FROM baseline_item m WHERE m.baseline_id = $1),
		       COALESCE(v.title, ''), v.statement, v.rationale, v.attributes, COALESCE(t.name, ''), COALESCE(i.status_name, '')
		FROM baseline_item i
		LEFT JOIN requirement_revision v ON v.requirement_id = i.requirement_id AND v.number = i.revision
		LEFT JOIN requirement_type t ON t.id = v.type_id
		WHERE i.baseline_id = $1 ORDER BY i.path LIMIT $2`, id, MaxEntries)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var (
			e                                Entry
			number                           int
			path                             []int32
			statement, rationale, attributes []byte
			typeName, status                 string
		)
		if err := rows.Scan(&e.ID, &number, &e.Kind, &e.Revision, &path, &e.Depth, &e.Title, &statement, &rationale, &attributes, &typeName, &status); err != nil {
			return nil, err
		}
		e.Key, e.Outline = requirement.Key(projectKey, number), requirement.OutlineOf(path)
		e.Statement, e.Rationale, e.Properties, e.Links = document(statement), rationaleOf(rationale), []Property{}, []Link{}
		if e.Kind == requirement.KindRequirement {
			e.Properties = appendText(e.Properties, FieldType, typeName)
			e.Properties = appendText(e.Properties, FieldStatus, status)
			attrs, err := r.attributes(attributes)
			if err != nil {
				return nil, err
			}
			e.Properties = append(e.Properties, attrs...)
		}
		doc.Entries = append(doc.Entries, e)
		ids = append(ids, e.ID)
	}
	return ids, rows.Err()
}

// attributes reads stored values as a reader reads them, in the project's order of its attributes.
func (r *reading) attributes(raw []byte) ([]Property, error) {
	var values map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, err
		}
	}
	var out []Property
	for _, d := range r.defs {
		value, ok := values[d.ID.String()]
		if !ok || len(value) == 0 || string(value) == "null" {
			continue
		}
		p, err := r.property(d, value)
		if err != nil {
			return nil, err
		}
		if p.Text != "" || p.Document != nil || p.Flag != nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *reading) property(d attribute.Def, value json.RawMessage) (Property, error) {
	p := Property{Name: d.Name}
	var (
		s string
		n float64
		b bool
	)
	switch d.Kind {
	case attribute.RichText:
		p.Document = value
	case attribute.Number:
		if json.Unmarshal(value, &n) == nil {
			p.Text = strconv.FormatFloat(n, 'g', -1, 64)
		}
	case attribute.Boolean:
		if json.Unmarshal(value, &b) == nil {
			p.Flag = &b
		}
	case attribute.Select, attribute.MultiSelect:
		var ids []string
		if json.Unmarshal(value, &s) == nil {
			ids = []string{s}
		} else {
			_ = json.Unmarshal(value, &ids)
		}
		var names []string
		for _, id := range ids {
			for _, o := range d.Options {
				if o.ID.String() == id {
					names = append(names, o.Name)
				}
			}
		}
		p.Text = strings.Join(names, ", ")
	case attribute.User:
		if json.Unmarshal(value, &s) == nil {
			name, err := r.person(s)
			if err != nil {
				return p, err
			}
			p.Text = name
		}
	default:
		_ = json.Unmarshal(value, &s)
		p.Text = s
	}
	return p, nil
}

func (r *reading) person(id string) (string, error) {
	if r.people == nil {
		r.people = map[string]string{}
	}
	if name, ok := r.people[id]; ok {
		return name, nil
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", nil
	}
	name, err := nameOf(r.ctx, r.tx, parsed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	r.people[id] = name
	return name, nil
}

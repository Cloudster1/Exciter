package requirement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/document"
	"github.com/cloudster1/exciter/backend/internal/events"
	"github.com/cloudster1/exciter/backend/internal/keyset"
	"github.com/cloudster1/exciter/backend/internal/project"
)

var (
	// ErrNoRevision is a revision number the requirement never reached.
	ErrNoRevision = errors.New("no such revision")
	// ErrNoChange is a restore of what the requirement says now.
	ErrNoChange = errors.New("the revision says what the requirement says now")
)

// emptyDoc is the document of a side that has no statement or rationale: the empty requirement, or a null rationale.
var emptyDoc = json.RawMessage(`{"type":"doc","content":[]}`)

const selectRevision = `
SELECT v.number, v.title, v.comment, v.created_by, COALESCE(u.name, ''), v.created_at, v.restored_from, v.hash,
       v.statement, v.rationale, v.attributes, v.type_id, COALESCE(t.name, ''), COALESCE(t.icon, ''), COALESCE(t.colour, ''), t.project_id
FROM requirement_revision v
LEFT JOIN app_user u ON u.id = v.created_by
LEFT JOIN requirement_type t ON t.id = v.type_id`

// stored is a revision as read, with the project its type belongs to, which a move may have left behind.
type stored struct {
	Revision
	typeProject *uuid.UUID
}

func scanRevision(row pgx.Row) (*stored, error) {
	var (
		v         stored
		rationale []byte
		attrs     []byte
		typeID    *uuid.UUID
		typ       TypeRef
	)
	err := row.Scan(&v.Number, &v.Title, &v.Comment, &v.AuthorID, &v.AuthorName, &v.CreatedAt, &v.RestoredFrom, &v.Hash,
		&v.Statement, &rationale, &attrs, &typeID, &typ.Name, &typ.Icon, &typ.Colour, &v.typeProject)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoRevision
	}
	if err != nil {
		return nil, err
	}
	if len(rationale) > 0 && string(rationale) != "null" {
		v.Rationale = rationale
	}
	if typeID != nil {
		typ.ID = *typeID
		v.Type = &typ
	}
	return &v, json.Unmarshal(attrs, &v.Attributes)
}

func revisionAt(ctx context.Context, tx db.DBTX, id uuid.UUID, number int) (*stored, error) {
	return scanRevision(tx.QueryRow(ctx, selectRevision+` WHERE v.org_id = current_org_id() AND v.requirement_id = $1 AND v.number = $2`, id, number))
}

// Revisions answers a window of a requirement's revisions, the latest first, and how many it has.
func (s *Service) Revisions(ctx context.Context, reader uuid.UUID, key string, limit, offset int) ([]RevisionEntry, int, error) {
	out := []RevisionEntry{}
	total := 0
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := find(ctx, tx, reader, key)
		if err != nil {
			return err
		}
		total = l.Revision
		rows, err := tx.Query(ctx, selectRevision+` WHERE v.org_id = current_org_id() AND v.requirement_id = $1 ORDER BY v.number DESC LIMIT $2 OFFSET $3`,
			l.ID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanRevision(rows)
			if err != nil {
				return err
			}
			out = append(out, v.RevisionEntry)
		}
		return rows.Err()
	})
	return out, total, err
}

// Revision answers one revision with what it said.
func (s *Service) Revision(ctx context.Context, reader uuid.UUID, key string, number int) (*Revision, error) {
	var out *Revision
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := find(ctx, tx, reader, key)
		if err != nil {
			return err
		}
		v, err := revisionAt(ctx, tx, l.ID, number)
		if err == nil {
			out = &v.Revision
		}
		return err
	})
	return out, err
}

// side is one end of a comparison: a revision, or the empty requirement for number 0.
func side(ctx context.Context, tx db.DBTX, id uuid.UUID, number int) (*stored, error) {
	if number == 0 {
		return &stored{Revision: Revision{Statement: emptyDoc, Attributes: map[string]json.RawMessage{}}}, nil
	}
	return revisionAt(ctx, tx, id, number)
}

func orEmpty(doc json.RawMessage) json.RawMessage {
	if len(doc) == 0 {
		return emptyDoc
	}
	return doc
}

// Compare answers what changed between two revisions; to defaults to the latest, from to the one before to.
func (s *Service) Compare(ctx context.Context, reader uuid.UUID, key string, from, to *int) (*Comparison, error) {
	var out *Comparison
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := find(ctx, tx, reader, key)
		if err != nil {
			return err
		}
		later := l.Revision
		if to != nil {
			later = *to
		}
		earlier := later - 1
		if from != nil {
			earlier = *from
		}
		for _, n := range []int{earlier, later} {
			if n < 0 || n > l.Revision {
				return ErrNoRevision
			}
		}
		a, err := side(ctx, tx, l.ID, earlier)
		if err != nil {
			return err
		}
		b, err := side(ctx, tx, l.ID, later)
		if err != nil {
			return err
		}
		c := &Comparison{From: compareSide(a), To: compareSide(b)}
		if c.Statement, err = Diff(orEmpty(a.Statement), orEmpty(b.Statement)); err != nil {
			return err
		}
		if c.Rationale, err = Diff(orEmpty(a.Rationale), orEmpty(b.Rationale)); err != nil {
			return err
		}
		if c.Attributes, err = attributeDiff(ctx, tx, l.projectID, a.Attributes, b.Attributes); err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

func compareSide(v *stored) CompareSide {
	return CompareSide{Number: v.Number, Title: v.Title, Type: v.Type, AuthorName: v.AuthorName, CreatedAt: v.CreatedAt}
}

// attributeDiff lists each attribute set on either side, in the project's order, with the values as a reader
// sees them; one that differs is changed, one set on both alike is equal.
func attributeDiff(ctx context.Context, tx db.DBTX, projectID uuid.UUID, from, to map[string]json.RawMessage) ([]AttributeDiff, error) {
	defs, err := attribute.Defs(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	names, err := peopleIn(ctx, tx, from, to)
	if err != nil {
		return nil, err
	}
	out := []AttributeDiff{}
	seen := map[string]bool{}
	add := func(key string, d *attribute.Def) {
		if seen[key] {
			return
		}
		a, b := from[key], to[key]
		if a == nil && b == nil {
			return
		}
		seen[key] = true
		id, _ := uuid.Parse(key)
		diff := AttributeDiff{AttributeID: id, Name: key, From: a, To: b, Change: DiffEqual}
		if d != nil {
			diff.Name = d.Name
		}
		diff.FromText, diff.ToText = shown(d, a, names), shown(d, b, names)
		switch {
		case a == nil:
			diff.Change = DiffInserted
		case b == nil:
			diff.Change = DiffDeleted
		case !bytes.Equal(a, b):
			diff.Change = DiffModified
		}
		out = append(out, diff)
	}
	for i := range defs {
		add(defs[i].ID.String(), &defs[i])
	}
	// What no attribute of the project names any more, such as values a move left behind, comes last by key.
	rest := []string{}
	for key := range from {
		rest = append(rest, key)
	}
	for key := range to {
		rest = append(rest, key)
	}
	slices.Sort(rest)
	for _, key := range rest {
		add(key, nil)
	}
	return out, nil
}

// peopleIn names everybody a user value on either side holds.
func peopleIn(ctx context.Context, tx db.DBTX, sides ...map[string]json.RawMessage) (map[string]string, error) {
	var ids []uuid.UUID
	for _, values := range sides {
		for _, raw := range values {
			var id uuid.UUID
			if json.Unmarshal(raw, &id) == nil {
				ids = append(ids, id)
			}
		}
	}
	names := map[string]string{}
	if len(ids) == 0 {
		return names, nil
	}
	rows, err := tx.Query(ctx, `SELECT id, name FROM app_user WHERE id = ANY ($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id.String()] = name
	}
	return names, rows.Err()
}

// shown is a value as a reader sees it: options and people by their names now, dates as written.
func shown(d *attribute.Def, raw json.RawMessage, people map[string]string) string {
	if raw == nil {
		return ""
	}
	if d == nil {
		return string(raw)
	}
	optionName := func(id string) string {
		for _, o := range d.Options {
			if o.ID.String() == id {
				return o.Name
			}
		}
		return id
	}
	switch d.Kind {
	case attribute.Select, attribute.User, attribute.Text, attribute.Date, attribute.URL:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return string(raw)
		}
		if d.Kind == attribute.Select {
			return optionName(s)
		}
		if d.Kind == attribute.User {
			if name, ok := people[s]; ok {
				return name
			}
		}
		return s
	case attribute.MultiSelect:
		var ids []string
		if json.Unmarshal(raw, &ids) != nil {
			return string(raw)
		}
		names := make([]string, len(ids))
		for i, id := range ids {
			names[i] = optionName(id)
		}
		return strings.Join(names, ", ")
	case attribute.Boolean:
		if string(raw) == "true" {
			return "Yes"
		}
		return "No"
	case attribute.RichText:
		doc, err := document.Parse(raw)
		if err != nil {
			return ""
		}
		return document.PlainText(doc)
	}
	return string(raw)
}

// RestoreRevision writes an older revision's content as the next revision, recording which it restored. What was
// retired since comes back as it was; a value a move left behind comes back matched by name.
func (s *Service) RestoreRevision(ctx context.Context, actor uuid.UUID, key string, number int, in RestoreInput) (*Requirement, *RevisionEntry, db.LSN, error) {
	var (
		out     *Requirement
		written *RevisionEntry
	)
	comment, err := checkComment(in.Comment)
	if err != nil {
		return nil, nil, 0, err
	}
	if comment == "" {
		comment = fmt.Sprintf("Restored revision %d", number)
	}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		cur, err := changing(ctx, tx, actor, key)
		if err != nil {
			return err
		}
		if cur.TrashedAt != nil {
			return ErrTrashed
		}
		if in.BaseRevision != cur.Revision {
			return &ConflictError{Key: cur.Key, Revision: cur.Revision}
		}
		if number < 1 || number > cur.Revision {
			return ErrNoRevision
		}
		old, err := revisionAt(ctx, tx, cur.ID, number)
		if err != nil {
			return err
		}
		content := Normative{Title: old.Title, Statement: old.Statement, Rationale: old.Rationale, Attributes: old.Attributes}
		if old.Type != nil {
			content.TypeID = &old.Type.ID
		}
		if old.typeProject != nil && *old.typeProject != cur.projectID {
			if content.TypeID, content.Attributes, err = carryBack(ctx, tx, actor, cur, old); err != nil {
				return err
			}
		}
		hash := content.Hash()
		if hash == cur.hash {
			return ErrNoChange
		}
		attrs, err := json.Marshal(content.Attributes)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE requirement SET title = $2, statement = $3, rationale = $4, type_id = $5, attributes = $6, normative_hash = $7, updated_by = $8
			WHERE id = $1`, cur.ID, content.Title, content.Statement, content.Rationale, content.TypeID, attrs, hash, actor); err != nil {
			return fmt.Errorf("restore revision %d of %s: %w", number, cur.Key, err)
		}
		next := cur.Revision + 1
		if err := revisionOf(ctx, tx, actor, cur.ID, next, content, hash, comment, &number); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, TopicRevised, map[string]any{"actorId": actor, "requirementId": cur.ID, "revision": next}); err != nil {
			return err
		}
		l, err := findByID(ctx, tx, actor, cur.ID)
		if err != nil {
			return err
		}
		v, err := revisionAt(ctx, tx, cur.ID, next)
		if err != nil {
			return err
		}
		out, written = &l.Requirement, &v.RevisionEntry
		return nil
	})
	return out, written, lsn, err
}

// carryBack matches a revision written before a move into the project the requirement is in now.
func carryBack(ctx context.Context, tx db.DBTX, actor uuid.UUID, cur *loaded, old *stored) (*uuid.UUID, map[string]json.RawMessage, error) {
	here, err := project.Find(ctx, tx, actor, cur.ProjectKey)
	if err != nil {
		return nil, nil, err
	}
	match, err := newMatcher(ctx, tx, *old.typeProject, here)
	if err != nil {
		return nil, nil, err
	}
	then := &loaded{Requirement: Requirement{Kind: cur.Kind, Type: old.Type, Attributes: old.Attributes}}
	return match.carry(then)
}

// revisionEntryID names a revision in the activity list, which keys its windows by id; revisions have none.
const revisionEntryID = `md5(requirement_id::text || ':' || number)::uuid`

// Activity answers a requirement's revisions and logged changes in one list, newest first, a window at a time.
func (s *Service) Activity(ctx context.Context, reader uuid.UUID, key string, after *keyset.Cursor, limit int) ([]ActivityEntry, *string, error) {
	out := []ActivityEntry{}
	at, id := after.Args()
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		l, err := find(ctx, tx, reader, key)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT * FROM (
				SELECT 'revision' AS kind, v.created_at, `+revisionEntryID+` AS id, COALESCE(u.name, '') AS actor,
				       v.number, v.title, v.comment, v.created_by, v.restored_from, v.hash, '' AS field, '' AS from_value, '' AS to_value, '' AS reason
				FROM requirement_revision v LEFT JOIN app_user u ON u.id = v.created_by
				WHERE v.org_id = current_org_id() AND v.requirement_id = $1
				UNION ALL
				SELECT 'change', c.created_at, c.id, COALESCE(u.name, ''),
				       0, '', '', c.actor_id, NULL, '', c.field, c.from_value, c.to_value, c.reason
				FROM requirement_change c LEFT JOIN app_user u ON u.id = c.actor_id
				WHERE c.org_id = current_org_id() AND c.requirement_id = $1
			) a
			WHERE $2::timestamptz IS NULL OR (a.created_at, a.id) < ($2, $3)
			ORDER BY a.created_at DESC, a.id DESC LIMIT $4`, l.ID, at, id, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				e                         ActivityEntry
				v                         RevisionEntry
				c                         Change
				authorID                  *uuid.UUID
				field, fromValue, toValue string
			)
			if err := rows.Scan(&e.Kind, &e.At, &e.ID, &e.ActorName, &v.Number, &v.Title, &v.Comment, &authorID, &v.RestoredFrom, &v.Hash,
				&field, &fromValue, &toValue, &c.Reason); err != nil {
				return err
			}
			if e.Kind == ActivityRevision {
				v.AuthorID, v.AuthorName, v.CreatedAt = authorID, e.ActorName, e.At
				e.Revision = &v
			} else {
				c.Field, c.From, c.To = ChangeField(field), fromValue, toValue
				e.Change = &c
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	var next *string
	if len(out) > limit {
		last := out[limit-1]
		next = keyset.Next(limit, len(out), keyset.Cursor{At: last.At, ID: last.ID})
		out = out[:limit]
	}
	return out, next, nil
}

package reqifio

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
)

// named is something of the project a name of the file may name.
type named struct {
	ID   uuid.UUID
	Name string
}

// linkType is a link type the project may use.
type linkType struct {
	ID   uuid.UUID
	Key  string
	Name string
}

// existing is a requirement or section of the project an object of the file may be.
type existing struct {
	ID       uuid.UUID
	Key      string
	Kind     requirement.Kind
	Revision int
	Status   *uuid.UUID
	TypeID   *uuid.UUID
	Parent   *uuid.UUID
	// Files are the ids of its attachments, which an image it already shows names.
	Files map[uuid.UUID]bool
}

// linkKey is one live link: its type and its ends.
type linkKey struct{ typeID, source, target uuid.UUID }

// tie is what an identifier of an earlier exchange names, with the type an attribute's belongs to.
type tie struct {
	Target uuid.UUID
	Scope  *uuid.UUID
}

// world is what a file is read against: the project as the person importing it sees it.
type world struct {
	project *project.Project
	// defs are the project's attributes, retired ones included, since an identifier may name one.
	defs      []attribute.Def
	types     map[uuid.UUID]named
	linkTypes map[uuid.UUID]linkType
	statuses  map[uuid.UUID]named
	// workflow lists the statuses of the workflow each type follows, and uuid.Nil the project's own.
	workflow map[uuid.UUID]map[uuid.UUID]bool
	// members are the people who may view the project, by their email in lower case.
	members  map[string]uuid.UUID
	existing map[uuid.UUID]*existing
	// children are each parent's children in order, uuid.Nil the top, as the run moves them.
	children map[uuid.UUID][]uuid.UUID
	// links are the live links of the project, by type, source and target.
	links map[linkKey]bool
	ties  map[string]map[string]tie
}

// loadWorld reads the project as actor sees it, in the transaction tx.
func loadWorld(ctx context.Context, tx db.DBTX, p *project.Project) (*world, error) {
	w := &world{project: p, types: map[uuid.UUID]named{}, linkTypes: map[uuid.UUID]linkType{}, statuses: map[uuid.UUID]named{},
		workflow: map[uuid.UUID]map[uuid.UUID]bool{}, members: map[string]uuid.UUID{}, existing: map[uuid.UUID]*existing{},
		children: map[uuid.UUID][]uuid.UUID{}, links: map[linkKey]bool{}, ties: map[string]map[string]tie{}}
	var err error
	if w.defs, err = attribute.Defs(ctx, tx, p.ID); err != nil {
		return nil, err
	}
	for _, read := range []func(context.Context, db.DBTX) error{w.readNamed, w.readLinkTypes, w.readWorkflows, w.readRequirements, w.readLinks, w.readTies} {
		if err := read(ctx, tx); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func (w *world) readNamed(ctx context.Context, tx db.DBTX) error {
	for _, read := range []struct {
		sql  string
		into func(uuid.UUID, string)
	}{
		{`SELECT id, name FROM requirement_type WHERE project_id = $1 AND NOT retired`, func(id uuid.UUID, name string) { w.types[id] = named{id, name} }},
		{`SELECT id, name FROM req_status WHERE project_id = $1`, func(id uuid.UUID, name string) { w.statuses[id] = named{id, name} }},
		{`SELECT u.id, lower(u.email::text) FROM app_user u
		  WHERE u.role IS NOT NULL AND u.is_active AND perm_project_holds($1, u.id, 'view')`, func(id uuid.UUID, email string) { w.members[email] = id }},
	} {
		rows, err := tx.Query(ctx, read.sql, w.project.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var (
				id   uuid.UUID
				name string
			)
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return err
			}
			read.into(id, name)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (w *world) readLinkTypes(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT lt.id, lt.key, lt.name FROM link_type lt
		LEFT JOIN link_type_project lp ON lp.project_id = $1 AND lp.link_type_id = lt.id
		WHERE (lt.project_id IS NULL OR lt.project_id = $1) AND NOT lt.retired AND COALESCE(lp.enabled, true)`, w.project.ID)
	if err != nil {
		return err
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (linkType, error) {
		var l linkType
		return l, row.Scan(&l.ID, &l.Key, &l.Name)
	})
	for _, l := range found {
		w.linkTypes[l.ID] = l
	}
	return err
}

func (w *world) readWorkflows(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT COALESCE(t.id, $2::uuid), ws.status_id
		FROM (SELECT id FROM requirement_type WHERE project_id = $1 UNION ALL SELECT NULL) t
		JOIN workflow_step ws ON ws.workflow_id = requirement_workflow(t.id, $1)`, w.project.ID, uuid.Nil)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var typeID, status uuid.UUID
		if err := rows.Scan(&typeID, &status); err != nil {
			return err
		}
		if w.workflow[typeID] == nil {
			w.workflow[typeID] = map[uuid.UUID]bool{}
		}
		w.workflow[typeID][status] = true
	}
	return rows.Err()
}

func (w *world) readRequirements(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.number, r.kind, r.revision, r.status_id, r.type_id, r.parent_id,
		       COALESCE((SELECT array_agg(a.id) FROM attachment a WHERE a.requirement_id = r.id AND a.deleted_at IS NULL), '{}')
		FROM requirement r WHERE r.project_id = $1 AND r.trashed_at IS NULL
		ORDER BY r.parent_id NULLS FIRST, r.rank, r.id`, w.project.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			e      existing
			number int
			files  []uuid.UUID
		)
		if err := rows.Scan(&e.ID, &number, &e.Kind, &e.Revision, &e.Status, &e.TypeID, &e.Parent, &files); err != nil {
			return err
		}
		e.Key = requirement.Key(w.project.Key, number)
		e.Files = map[uuid.UUID]bool{}
		for _, f := range files {
			e.Files[f] = true
		}
		w.existing[e.ID] = &e
		parent := uuid.Nil
		if e.Parent != nil {
			parent = *e.Parent
		}
		w.children[parent] = append(w.children[parent], e.ID)
	}
	return rows.Err()
}

func (w *world) readLinks(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT l.type_id, l.source_id, l.target_id FROM trace_link l
		JOIN requirement r ON r.id = l.source_id
		WHERE r.project_id = $1 AND l.deleted_at IS NULL`, w.project.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k linkKey
		if err := rows.Scan(&k.typeID, &k.source, &k.target); err != nil {
			return err
		}
		w.links[k] = true
	}
	return rows.Err()
}

func (w *world) readTies(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `SELECT kind, identifier, target_id, scope_id FROM reqif_identity WHERE project_id = $1`, w.project.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			kind, identifier string
			t                tie
		)
		if err := rows.Scan(&kind, &identifier, &t.Target, &t.Scope); err != nil {
			return err
		}
		if w.ties[kind] == nil {
			w.ties[kind] = map[string]tie{}
		}
		w.ties[kind][identifier] = t
	}
	return rows.Err()
}

func (w *world) def(id uuid.UUID) *attribute.Def {
	for i := range w.defs {
		if w.defs[i].ID == id {
			return &w.defs[i]
		}
	}
	return nil
}

// defNamed is the live attribute of the name, nil when there is none.
func (w *world) defNamed(name string) *attribute.Def {
	for i := range w.defs {
		if !w.defs[i].Retired && strings.EqualFold(w.defs[i].Name, name) {
			return &w.defs[i]
		}
	}
	return nil
}

func (w *world) typeNamed(name string) (named, bool) {
	for _, t := range w.types {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return named{}, false
}

func (w *world) statusNamed(name string) (named, bool) {
	for _, s := range w.statuses {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return named{}, false
}

// tied is what an identifier of the kind names in the project: tied by an earlier exchange, or one of the
// project's own ids an export wrote with prefix.
func (w *world) tied(kind, identifier, prefix string) (tie, bool) {
	if t, ok := w.ties[kind][identifier]; ok {
		return t, true
	}
	if rest, ok := strings.CutPrefix(identifier, prefix); ok && prefix != "" {
		if id, err := uuid.Parse(rest); err == nil {
			return tie{Target: id}, true
		}
	}
	return tie{}, false
}

// place moves id in the run's picture of the tree: below parent, uuid.Nil the top, after after, or first.
func (w *world) place(id, parent uuid.UUID, after *uuid.UUID) {
	for p, kids := range w.children {
		for i, k := range kids {
			if k == id {
				w.children[p] = append(kids[:i:i], kids[i+1:]...)
				break
			}
		}
	}
	kids := w.children[parent]
	at := 0
	if after != nil {
		for i, k := range kids {
			if k == *after {
				at = i + 1
				break
			}
		}
	}
	w.children[parent] = append(kids[:at:at], append([]uuid.UUID{id}, kids[at:]...)...)
}

// placedAt says id already sits below parent right after after, or first when after is nil.
func (w *world) placedAt(id, parent uuid.UUID, after *uuid.UUID) bool {
	kids := w.children[parent]
	for i, k := range kids {
		if k != id {
			continue
		}
		if after == nil {
			return i == 0
		}
		return i > 0 && kids[i-1] == *after
	}
	return false
}

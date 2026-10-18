package csvio

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

// named is something of the project a word of the file may name.
type named struct {
	ID   uuid.UUID
	Name string
}

// existing is a requirement of the project a key of the file may name.
type existing struct {
	ID       uuid.UUID
	Key      string
	Kind     requirement.Kind
	Status   string
	Revision int
}

// world is what a file is read against: the project as the person importing it sees it.
type world struct {
	project *project.Project
	// defs are the attributes a column may fill, retired ones left out.
	defs     []attribute.Def
	types    map[string]named
	statuses map[string]named
	// members are the people who may view the project, by their email in lower case.
	members map[string]named
	// existing are the requirements of the project out of the trash, by key.
	existing map[string]existing
	// linkTypes are the keys of the link types the project uses.
	linkTypes map[string]bool
	// elsewhere says whether a key names a requirement of another project the importer may link to.
	elsewhere func(key string) bool
}

// loadWorld reads the project as actor sees it, in the transaction tx, which elsewhere goes on reading.
func loadWorld(ctx context.Context, tx db.DBTX, actor uuid.UUID, p *project.Project) (*world, error) {
	w := &world{project: p, types: map[string]named{}, statuses: map[string]named{}, members: map[string]named{},
		existing: map[string]existing{}, linkTypes: map[string]bool{}}
	defs, err := attribute.Defs(ctx, tx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, d := range defs {
		// A computed attribute is worked out, never typed (#213), so no column fills it.
		if !d.Retired && !d.Computed() {
			w.defs = append(w.defs, d)
		}
	}
	reads := []struct {
		sql  string
		into map[string]named
	}{
		{`SELECT id, name, lower(name) FROM requirement_type WHERE project_id = $1 AND NOT retired`, w.types},
		{`SELECT id, name, lower(name) FROM req_status WHERE project_id = $1`, w.statuses},
		{`SELECT u.id, u.email::text, lower(u.email::text) FROM app_user u
		  WHERE u.role IS NOT NULL AND u.is_active AND perm_project_holds($1, u.id, 'view')`, w.members},
	}
	for _, read := range reads {
		if err := readNamed(ctx, tx, read.sql, p.ID, read.into); err != nil {
			return nil, err
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.number, r.kind, COALESCE(st.name, ''), r.revision FROM requirement r
		LEFT JOIN req_status st ON st.id = r.status_id
		WHERE r.project_id = $1 AND r.trashed_at IS NULL`, p.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			e      existing
			number int
		)
		if err := rows.Scan(&e.ID, &number, &e.Kind, &e.Status, &e.Revision); err != nil {
			rows.Close()
			return nil, err
		}
		e.Key = requirement.Key(p.Key, number)
		w.existing[e.Key] = e
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	keys, err := tx.Query(ctx, `
		SELECT lt.key FROM link_type lt
		LEFT JOIN link_type_project lp ON lp.project_id = $1 AND lp.link_type_id = lt.id
		WHERE (lt.project_id IS NULL OR lt.project_id = $1) AND NOT lt.retired
		  AND COALESCE(lp.enabled, true)`, p.ID)
	if err != nil {
		return nil, err
	}
	usable, err := pgx.CollectRows(keys, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, k := range usable {
		w.linkTypes[k] = true
	}
	seen := map[string]bool{}
	w.elsewhere = func(key string) bool {
		if found, ok := seen[key]; ok {
			return found
		}
		_, err := requirement.FindEndpoint(ctx, tx, actor, key)
		seen[key] = err == nil
		return seen[key]
	}
	return w, nil
}

func readNamed(ctx context.Context, tx db.DBTX, sql string, projectID uuid.UUID, into map[string]named) error {
	rows, err := tx.Query(ctx, sql, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			n     named
			lower string
		)
		if err := rows.Scan(&n.ID, &n.Name, &lower); err != nil {
			return err
		}
		into[lower] = n
	}
	return rows.Err()
}

// def is the attribute a target such as attr:Risk fills.
func (w *world) def(target string) *attribute.Def {
	name, ok := strings.CutPrefix(target, AttributePrefix)
	if !ok {
		return nil
	}
	for i := range w.defs {
		if strings.EqualFold(w.defs[i].Name, name) {
			return &w.defs[i]
		}
	}
	return nil
}

package search

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/rql"
)

// The query language (#47) narrows the grid as one line of text, beside its words and controls; it is compiled here
// against what the project has, so a field per link type and the attributes by name are this project's.

// catalogOf is what a query may say in project p: its attributes and the link types its requirements may be joined
// by, the installation's first, so that a name two types would take goes to the same one every time.
func catalogOf(ctx context.Context, tx db.DBTX, p *project.Project, defs []attribute.Def) (*rql.Catalog, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id, t.key, t.forward, t.backward FROM link_type t
		WHERE t.project_id IS NULL OR t.project_id = $1
		ORDER BY t.project_id NULLS FIRST, t.key`, p.ID)
	if err != nil {
		return nil, err
	}
	links, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (rql.LinkType, error) {
		var lt rql.LinkType
		return lt, row.Scan(&lt.ID, &lt.Key, &lt.Forward, &lt.Backward)
	})
	if err != nil {
		return nil, err
	}
	return rql.NewCatalog(defs, links), nil
}

// compileQuery compiles a filter's query as the reader means it now; a query of only spaces is none.
func compileQuery(ctx context.Context, tx db.DBTX, reader uuid.UUID, p *project.Project, defs []attribute.Def, text string) (*rql.Compiled, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	q, err := rql.Parse(text)
	if err != nil {
		return nil, err
	}
	cat, err := catalogOf(ctx, tx, p, defs)
	if err != nil {
		return nil, err
	}
	return q.Compile(cat, rql.Env{UserID: reader, Now: time.Now()})
}

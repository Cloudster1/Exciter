package dashboard

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/rql"
	"github.com/cloudster1/exciter/backend/internal/search"
	"github.com/cloudster1/exciter/backend/internal/trace"
)

// What a widget shows, read every time as the reader: only the requirements they may see count, and every bucket
// carries the condition that keeps its requirements, so the grid opens with the same ones.

// Data reads a saved widget as the reader.
func (s *Service) Data(ctx context.Context, reader, widgetID uuid.UUID) (*Data, error) {
	var (
		key string
		w   *Widget
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, found, err := widgetOf(ctx, tx, widgetID)
		if err != nil {
			return err
		}
		key, w = f.ProjectKey, found
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.read(ctx, reader, key, w.Kind, w.Config)
}

// Preview reads a widget as it is being set up, before it is saved.
func (s *Service) Preview(ctx context.Context, reader uuid.UUID, projectKey string, in PreviewInput) (*Data, error) {
	if _, ok := infoOf(in.Kind); !ok {
		return nil, &FieldError{"kind", "Pick a kind of widget the dashboard offers."}
	}
	c, err := checkConfig(in.Kind, in.Config)
	if err != nil {
		return nil, err
	}
	return s.read(ctx, reader, projectKey, in.Kind, c)
}

// orderBy is a query's own order, which a bucket's condition joined to it would leave stranded in the middle.
var orderBy = regexp.MustCompile(`(?is)\s+ORDER\s+BY\s+.*$`)

// and joins a condition to a filter's query.
func and(f search.Filter, cond string) search.Filter {
	q := strings.TrimSpace(orderBy.ReplaceAllString(f.Query, ""))
	if q == "" {
		f.Query = cond
	} else {
		f.Query = "(" + q + ") AND " + cond
	}
	return f
}

// quote writes text as the query language reads it.
func quote(text string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(text, `\`, `\\`), `"`, `\"`) + `"`
}

func (s *Service) read(ctx context.Context, reader uuid.UUID, projectKey string, kind Kind, c Config) (*Data, error) {
	out := &Data{Kind: kind, Groups: []Group{}, Lines: []Line{}, Rows: []search.Row{}, Query: c.Source.Query, ViewID: c.Source.ViewID}
	var (
		filter search.Filter
		p      *project.Project
		defs   []attribute.Def
		ids    []uuid.UUID
	)
	count := func(f search.Filter) (int, error) {
		n := 0
		err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			kept, err := search.Keep(ctx, tx, reader, p, f, search.Sort{})
			n = len(kept)
			return err
		})
		return n, err
	}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if p, err = project.Find(ctx, tx, reader, projectKey); err != nil {
			return err
		}
		if c.Source.ViewID != nil {
			v, err := search.SharedGridView(ctx, tx, reader, p.ID, *c.Source.ViewID)
			if err != nil {
				return &FieldError{"config.source.viewId", "The view this widget reads is gone or no longer shared. Pick another, or write a query."}
			}
			filter = v.Filter
		} else {
			filter = search.Filter{Query: c.Source.Query}
		}
		if defs, err = attribute.Defs(ctx, tx, p.ID); err != nil {
			return err
		}
		kept, err := search.Keep(ctx, tx, reader, p, filter, search.Sort{})
		if err != nil {
			return err
		}
		for _, k := range kept {
			ids = append(ids, k.Ref.ID)
		}
		out.Total = len(ids)
		switch kind {
		case Breakdown:
			group, err := breakdown(ctx, tx, defs, ids, c)
			if err != nil {
				return err
			}
			out.Groups = []Group{group}
		case Changes:
			out.Lines, err = changes(ctx, tx, ids, c)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	switch kind {
	case List:
		rows, _, err := s.search.Grid(ctx, reader, projectKey, filter, search.Sort{}, c.Rows, 0)
		if err != nil {
			return nil, err
		}
		out.Rows = rows
	case Coverage:
		summary, _, _, err := s.trace.Coverage(ctx, reader, projectKey, filter, trace.LevelFilter{}, 0, 0)
		if err != nil {
			return nil, err
		}
		for _, d := range []struct {
			name   trace.Dimension
			totals trace.Totals
		}{{trace.Decomposed, summary.Decomposed}, {trace.Implemented, summary.Implemented}, {trace.Verified, summary.Verified}} {
			g := Group{Name: string(d.name), Total: d.totals.Total}
			for _, b := range []struct {
				level trace.Level
				n     int
			}{{trace.LevelFull, d.totals.Full}, {trace.LevelPartial, d.totals.Partial}, {trace.LevelNone, d.totals.None}} {
				g.Buckets = append(g.Buckets, Bucket{Label: string(b.level), Count: b.n, Query: fmt.Sprintf("%s = %s", d.name, b.level)})
			}
			out.Groups = append(out.Groups, g)
		}
	case Review:
		g := Group{Name: "approval", Total: out.Total}
		for _, state := range []string{"approved", "pending", "outdated", "none"} {
			cond := "approval = " + state
			n, err := count(and(filter, cond))
			if err != nil {
				return nil, err
			}
			g.Buckets = append(g.Buckets, Bucket{Label: state, Count: n, Query: cond})
		}
		out.Groups = []Group{g}
	case Suspects:
		cond := "suspect > 0"
		n, err := count(and(filter, cond))
		if err != nil {
			return nil, err
		}
		out.Groups = []Group{{Name: "suspect", Total: out.Total, Buckets: []Bucket{{Label: "suspect", Count: n, Query: cond}}}}
		listed := and(filter, cond)
		listed.Query += " ORDER BY suspect DESC"
		rows, _, err := s.search.Grid(ctx, reader, projectKey, listed, search.Sort{}, DefaultListRows, 0)
		if err != nil {
			return nil, err
		}
		out.Rows = rows
	case Quality:
		summary, err := s.search.Quality(ctx, reader, projectKey, filter, 1, 0)
		if err != nil {
			return nil, err
		}
		out.Average = summary.Average
		g := Group{Name: "quality", Total: summary.Checked}
		for _, b := range summary.Bands {
			g.Buckets = append(g.Buckets, Bucket{Label: fmt.Sprintf("%d-%d", b.From, b.To), Count: b.Count,
				Query: fmt.Sprintf("quality >= %d AND quality <= %d", b.From, b.To)})
		}
		out.Groups = []Group{g}
	}
	return out, nil
}

// grouping is how a breakdown reads one requirement r's value: its SQL, a join it needs, and the condition a value
// keeps, the empty value's too.
type grouping struct {
	value, join string
	cond        func(value string) string
	none        string
}

func groupingOf(defs []attribute.Def, c Config) (grouping, *attribute.Def, error) {
	switch c.GroupBy {
	case ByStatus:
		return grouping{`st.name`, `LEFT JOIN req_status st ON st.id = r.status_id`, func(v string) string { return "status = " + quote(v) }, "status IS EMPTY"}, nil, nil
	case ByCategory:
		return grouping{`st.category`, `LEFT JOIN req_status st ON st.id = r.status_id`, func(v string) string { return "category = " + v }, ""}, nil, nil
	case ByType:
		return grouping{`t.name`, `LEFT JOIN requirement_type t ON t.id = r.type_id`, func(v string) string { return "type = " + quote(v) }, "type IS EMPTY"}, nil, nil
	case ByOwner:
		return grouping{`u.email::text`, `LEFT JOIN app_user u ON u.id = r.owner_id`, func(v string) string { return "owner = " + quote(v) }, "owner IS EMPTY"}, nil, nil
	case ByPriority:
		return grouping{`r.priority`, ``, func(v string) string { return "priority = " + v }, "priority IS EMPTY"}, nil, nil
	case ByLabel:
		return grouping{`l.name`, `LEFT JOIN requirement_label l ON l.requirement_id = r.id`, func(v string) string { return "labels = " + quote(v) }, "labels IS EMPTY"}, nil, nil
	}
	i := slices.IndexFunc(defs, func(d attribute.Def) bool { return c.AttributeID != nil && d.ID == *c.AttributeID })
	if i < 0 {
		return grouping{}, nil, &FieldError{"config.attributeId", "That attribute is not one of this project's. Pick another."}
	}
	d := &defs[i]
	src, id := rql.Store(d), "'"+d.ID.String()+"'"
	name := quote(d.Name)
	g := grouping{value: src + ` ->> ` + id, none: name + " IS EMPTY"}
	switch d.Kind {
	case attribute.RichText:
		return g, nil, &FieldError{"config.attributeId", d.Name + " holds formatted text, which is not counted by value. Pick another attribute."}
	case attribute.MultiSelect:
		g.value, g.join = `ms.value`, `LEFT JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(`+src+` -> `+id+`) = 'array' THEN `+src+` -> `+id+` END) AS ms (value) ON true`
	case attribute.User:
		g.value, g.join = `au.email::text`, `LEFT JOIN app_user au ON au.id::text = `+src+` ->> `+id
	}
	g.cond = func(v string) string {
		switch d.Kind {
		case attribute.Number, attribute.Boolean, attribute.Date:
			return name + " = " + v
		case attribute.Select, attribute.MultiSelect:
			return name + " = " + quote(optionLabel(d, v))
		}
		return name + " = " + quote(v)
	}
	return g, d, nil
}

func optionLabel(d *attribute.Def, id string) string {
	for _, o := range d.Options {
		if o.ID.String() == id {
			return o.Name
		}
	}
	return id
}

// breakdown counts the requirements by one field, largest first, the tail past MaxBuckets counted as one.
func breakdown(ctx context.Context, tx db.DBTX, defs []attribute.Def, ids []uuid.UUID, c Config) (Group, error) {
	g, d, err := groupingOf(defs, c)
	group := Group{Name: string(c.GroupBy), Total: len(ids), Buckets: []Bucket{}}
	if err != nil || len(ids) == 0 {
		return group, err
	}
	category := `''`
	if c.GroupBy == ByStatus {
		category = `COALESCE(st.category, '')`
	}
	rows, err := tx.Query(ctx, `SELECT `+g.value+`, `+category+`, count(DISTINCT r.id)::int FROM requirement r `+g.join+`
		WHERE r.id = ANY ($1) GROUP BY 1, 2 ORDER BY 3 DESC, 1`, ids)
	if err != nil {
		return group, fmt.Errorf("count the breakdown: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			value *string
			b     Bucket
		)
		if err := rows.Scan(&value, &b.Category, &b.Count); err != nil {
			return group, err
		}
		switch {
		case value == nil:
			b.Query = g.none
		case d != nil && (d.Kind == attribute.Select || d.Kind == attribute.MultiSelect):
			b.Label, b.Query = optionLabel(d, *value), g.cond(*value)
		default:
			b.Label, b.Query = *value, g.cond(*value)
		}
		group.Buckets = append(group.Buckets, b)
	}
	if err := rows.Err(); err != nil {
		return group, err
	}
	if len(group.Buckets) > MaxBuckets {
		rest := Bucket{}
		for _, b := range group.Buckets[MaxBuckets-1:] {
			rest.Count += b.Count
		}
		group.Buckets = append(group.Buckets[:MaxBuckets-1], rest)
	}
	return group, nil
}

// changes counts per week or month what happened to the requirements: made, revised, moved to another status.
func changes(ctx context.Context, tx db.DBTX, ids []uuid.UUID, c Config) ([]Line, error) {
	rows, err := tx.Query(ctx, `
		WITH b AS (SELECT generate_series(date_trunc($2, now() - make_interval(days => $3)), date_trunc($2, now()), ('1 ' || $2)::interval) AS start)
		SELECT b.start, b.start + ('1 ' || $2)::interval,
		       (SELECT count(*)::int FROM requirement r WHERE r.id = ANY ($1) AND r.created_at >= b.start AND r.created_at < b.start + ('1 ' || $2)::interval),
		       (SELECT count(DISTINCT v.requirement_id)::int FROM requirement_revision v WHERE v.requirement_id = ANY ($1) AND v.number > 1
		          AND v.created_at >= b.start AND v.created_at < b.start + ('1 ' || $2)::interval),
		       (SELECT count(DISTINCT x.requirement_id)::int FROM requirement_change x WHERE x.requirement_id = ANY ($1) AND x.field = 'status'
		          AND x.created_at >= b.start AND x.created_at < b.start + ('1 ' || $2)::interval)
		FROM b ORDER BY b.start`, ids, string(c.Interval), c.Days)
	if err != nil {
		return nil, fmt.Errorf("count the changes: %w", err)
	}
	defer rows.Close()
	lines := map[Series]*Line{}
	for _, s := range c.Series {
		lines[s] = &Line{Series: s, Points: []Point{}}
	}
	for rows.Next() {
		var (
			start, end                time.Time
			created, revised, changed int
		)
		if err := rows.Scan(&start, &end, &created, &revised, &changed); err != nil {
			return nil, err
		}
		within := func(field string) string {
			return fmt.Sprintf("%s >= %s AND %s < %s", field, start.UTC().Format(time.DateOnly), field, end.UTC().Format(time.DateOnly))
		}
		for s, l := range lines {
			p := Point{Start: start}
			switch s {
			case Created:
				p.Count, p.Query = created, within("created")
			case Revised:
				p.Count, p.Query = revised, within("updated")
			case StatusChanged:
				p.Count = changed
			}
			l.Points = append(l.Points, p)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Line, 0, len(c.Series))
	for _, s := range c.Series {
		out = append(out, *lines[s])
	}
	return out, nil
}

package search

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/exciter/backend/internal/attribute"
	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/perm"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/requirement"
	"github.com/cloudster1/exciter/backend/internal/rql"
)

// Service finds requirements by their words and their fields, and keeps the views people save of a grid.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// config is the text search configuration of migration 00200.
const config = `'exciter_search'::regconfig`

// gridConditions are the SQL conditions of f over requirement r with status st in project p, binding through bind;
// f is checked already.
func gridConditions(f Filter, p *project.Project, defs []attribute.Def, bind func(any) string) []string {
	var where []string
	if f.Q != "" {
		words := `r.search_vector @@ websearch_to_tsquery(` + config + `, ` + bind(f.Q) + `)`
		if key, number, ok := requirement.ParseKey(f.Q); ok && key == p.Key {
			words = `(` + words + ` OR r.number = ` + bind(number) + `)`
		}
		where = append(where, words)
	}
	if len(f.Kinds) > 0 {
		kinds := make([]string, len(f.Kinds))
		for i, k := range f.Kinds {
			kinds[i] = string(k)
		}
		where = append(where, `r.kind = ANY (`+bind(kinds)+`::text[])`)
	}
	if len(f.Types) > 0 {
		where = append(where, `r.type_id = ANY (`+bind(f.Types)+`::uuid[])`)
	}
	if len(f.Statuses) > 0 {
		where = append(where, `r.status_id = ANY (`+bind(f.Statuses)+`::uuid[])`)
	}
	if len(f.Categories) > 0 {
		where = append(where, `st.category = ANY (`+bind(f.Categories)+`::text[])`)
	}
	if len(f.Labels) > 0 {
		where = append(where, `EXISTS (SELECT 1 FROM requirement_label l WHERE l.requirement_id = r.id AND l.name = ANY (`+
			bind(f.Labels)+`::text[]))`)
	}
	if len(f.Owners) > 0 {
		where = append(where, `r.owner_id = ANY (`+bind(f.Owners)+`::uuid[])`)
	}
	if len(f.Priorities) > 0 {
		where = append(where, `r.priority = ANY (`+bind(f.Priorities)+`::text[])`)
	}
	for _, c := range f.Attributes {
		sql, _ := condition(c, defs, bind)
		where = append(where, sql)
	}
	if f.Suspect {
		where = append(where, requirement.SuspectLinks+` > 0`)
	}
	return where
}

// Grid answers a window of one project's requirements out of the trash, narrowed by f and in the order sort
// asks, outline order otherwise, and how many there are in all.
func (s *Service) Grid(ctx context.Context, reader uuid.UUID, projectKey string, f Filter, sort Sort, limit, offset int) ([]Row, int, error) {
	out := []Row{}
	total := 0
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		defs, err := attribute.Defs(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if f, err = check(f, sort, defs); err != nil {
			return err
		}
		query, err := compileQuery(ctx, tx, reader, p, defs, f.Query)
		if err != nil {
			return err
		}
		args := []any{p.ID, reader}
		bind := func(v any) string {
			args = append(args, v)
			return "$" + strconv.Itoa(len(args))
		}
		where := append([]string{perm.ViewableRequirement("r", 2)}, gridConditions(f, p, defs, bind)...)
		if clause := query.Place(bind); clause != "" {
			where = append(where, clause)
		}
		from := `
			FROM walk w JOIN requirement r ON r.id = w.id
			LEFT JOIN app_user o ON o.id = r.owner_id
			LEFT JOIN requirement_type t ON t.id = r.type_id
			LEFT JOIN req_status st ON st.id = r.status_id
			WHERE ` + strings.Join(where, ` AND `)
		if err := tx.QueryRow(ctx, requirement.WalkTree+` SELECT count(*)::int`+from, args...).Scan(&total); err != nil {
			return fmt.Errorf("count the requirements: %w", err)
		}
		// The order binds after the count, which has no use for what it binds.
		order := orderBy(query, sort, defs, bind)
		limitAt, offsetAt := bind(limit), bind(offset)
		rows, err := tx.Query(ctx, requirement.WalkTree+`
			SELECT r.id, r.number, r.kind, r.title, r.revision, w.path, r.owner_id, COALESCE(o.name, ''), r.priority, r.updated_at,
			       r.type_id, COALESCE(t.name, ''), COALESCE(t.icon, ''), COALESCE(t.colour, ''), r.attributes,
			       r.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''),
			       perm_project_holds(r.project_id, $2, 'edit'), perm_project_holds(r.project_id, $2, 'approve'),
			       perm_project_holds(r.project_id, $2, 'comment'),
			       ARRAY(SELECT l.name FROM requirement_label l WHERE l.requirement_id = r.id ORDER BY l.name),`+requirement.SuspectLinks+from+`
			ORDER BY `+order+` LIMIT `+limitAt+` OFFSET `+offsetAt, args...)
		if err != nil {
			return fmt.Errorf("list the requirements: %w", err)
		}
		found, err := pgx.CollectRows(rows, scanRow(p.Key))
		if found != nil {
			out = found
		}
		return err
	})
	return out, total, err
}

func scanRow(projectKey string) pgx.RowToFunc[Row] {
	return func(row pgx.CollectableRow) (Row, error) {
		var (
			r                     Row
			number                int
			path                  []int32
			ownerID               *uuid.UUID
			ownerName             string
			priority              *string
			edit, approve, remark bool
			typeID                *uuid.UUID
			typ                   requirement.TypeRef
			attrs                 []byte
			statusID              *uuid.UUID
			status                requirement.StatusRef
		)
		err := row.Scan(&r.ID, &number, &r.Kind, &r.Title, &r.Revision, &path, &ownerID, &ownerName, &priority, &r.UpdatedAt,
			&typeID, &typ.Name, &typ.Icon, &typ.Colour, &attrs, &statusID, &status.Name, &status.Category, &edit, &approve, &remark, &r.Labels,
			&r.SuspectLinks)
		if err != nil {
			return r, err
		}
		r.ProjectKey = projectKey
		r.Key = requirement.Key(projectKey, number)
		r.Outline = requirement.OutlineOf(path)
		if ownerID != nil {
			r.Owner = &requirement.PersonRef{ID: *ownerID, Name: ownerName}
		}
		if priority != nil {
			pr := requirement.Priority(*priority)
			r.Priority = &pr
		}
		if r.Labels == nil {
			r.Labels = []string{}
		}
		if typeID != nil {
			typ.ID = *typeID
			r.Type = &typ
		}
		if statusID != nil {
			status.ID = *statusID
			r.Status = &status
		}
		if err := json.Unmarshal(attrs, &r.Attributes); err != nil {
			return r, fmt.Errorf("read the attributes of %s: %w", r.Key, err)
		}
		r.Can = requirement.Can{Edit: edit, Transition: edit || approve, Delete: edit, Link: edit, Comment: remark}
		return r, nil
	}
}

// orderBy is the grid's order: a query's ORDER BY, which outranks the column as Armature's outranks its list's, or
// the column sort asks for, then outline order. It reads r, w, t, st and o.
func orderBy(query *rql.Compiled, sort Sort, defs []attribute.Def, bind func(any) string) string {
	order := `w.path`
	if query.Sorts() {
		keys := []string{}
		for _, o := range query.PlaceOrder(bind) {
			direction := ` ASC`
			if o.Desc {
				direction = ` DESC`
			}
			keys = append(keys, o.SQL+direction+` NULLS LAST`)
		}
		return strings.Join(keys, `, `) + `, ` + order
	}
	if key := sortKey(sort.Column, defs, bind); key != "" {
		direction := ` ASC`
		if sort.Desc {
			direction = ` DESC`
		}
		return key + direction + ` NULLS LAST, ` + order
	}
	return order
}

// Kept is one requirement a filter keeps, as coverage and the traceability matrix (#38) list it.
type Kept struct {
	Ref     requirement.Ref
	TypeID  *uuid.UUID
	Outline string
	// Edit is the reader's right to edit it, which links it while its project is open.
	Edit bool
}

// Keep answers every requirement of p out of the trash that the reader sees and f keeps, never a section, in the
// order sort and the query ask for, outline order otherwise; f is checked as the grid checks it.
func Keep(ctx context.Context, tx db.DBTX, reader uuid.UUID, p *project.Project, f Filter, sort Sort) ([]Kept, error) {
	defs, err := attribute.Defs(ctx, tx, p.ID)
	if err != nil {
		return nil, err
	}
	if f, err = check(f, sort, defs); err != nil {
		return nil, err
	}
	query, err := compileQuery(ctx, tx, reader, p, defs, f.Query)
	if err != nil {
		return nil, err
	}
	if len(f.Kinds) > 0 && !slices.Contains(f.Kinds, requirement.KindRequirement) {
		return []Kept{}, nil
	}
	f.Kinds = nil
	args := []any{p.ID, reader}
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	where := append([]string{perm.ViewableRequirement("r", 2), `r.kind = 'requirement'`}, gridConditions(f, p, defs, bind)...)
	if clause := query.Place(bind); clause != "" {
		where = append(where, clause)
	}
	order := orderBy(query, sort, defs, bind)
	rows, err := tx.Query(ctx, requirement.WalkTree+`
		SELECT r.id, r.number, r.title, r.revision, r.type_id, w.path, r.status_id, COALESCE(st.name, ''), COALESCE(st.category, ''),
		       perm_project_holds(r.project_id, $2, 'edit')
		FROM walk w JOIN requirement r ON r.id = w.id
		LEFT JOIN app_user o ON o.id = r.owner_id
		LEFT JOIN requirement_type t ON t.id = r.type_id
		LEFT JOIN req_status st ON st.id = r.status_id
		WHERE `+strings.Join(where, ` AND `)+`
		ORDER BY `+order, args...)
	if err != nil {
		return nil, fmt.Errorf("list the requirements: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Kept, error) {
		var (
			k        Kept
			number   int
			path     []int32
			statusID *uuid.UUID
			status   requirement.StatusRef
		)
		if err := row.Scan(&k.Ref.ID, &number, &k.Ref.Title, &k.Ref.Revision, &k.TypeID, &path, &statusID, &status.Name, &status.Category,
			&k.Edit); err != nil {
			return k, err
		}
		k.Ref.ProjectKey, k.Ref.Kind = p.Key, requirement.KindRequirement
		k.Ref.Key = requirement.Key(p.Key, number)
		k.Outline = requirement.OutlineOf(path)
		if statusID != nil {
			status.ID = *statusID
			k.Ref.Status = &status
		}
		return k, nil
	})
}

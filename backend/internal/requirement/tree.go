package requirement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/exciter/backend/internal/db"
	"github.com/cloudster1/exciter/backend/internal/project"
	"github.com/cloudster1/exciter/backend/internal/rank"
)

// walkTree numbers a project's tree out of the trash: each node's place among its siblings, by rank, makes
// its path, and the path is the outline and the reading order. $1 is the project.
const walkTree = `
WITH RECURSIVE live AS (
	SELECT id, parent_id, row_number() OVER (PARTITION BY parent_id ORDER BY rank, number) AS pos
	FROM requirement WHERE org_id = current_org_id() AND project_id = $1 AND trashed_at IS NULL
), walk AS (
	SELECT id, ARRAY[pos]::int[] AS path FROM live WHERE parent_id IS NULL
	UNION ALL
	SELECT l.id, w.path || l.pos::int FROM live l JOIN walk w ON l.parent_id = w.id
)`

// outlineOf writes a path as the reader numbers it, such as 2.3.1.
func outlineOf(path []int32) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strconv.Itoa(int(p))
	}
	return strings.Join(parts, ".")
}

// outline answers one requirement's outline number, empty when it is in the trash.
func outline(ctx context.Context, tx db.DBTX, projectID, id uuid.UUID) (string, error) {
	var path []int32
	err := tx.QueryRow(ctx, walkTree+` SELECT path FROM walk WHERE id = $2`, projectID, id).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return outlineOf(path), err
}

const selectNodes = walkTree + `
SELECT r.id, r.number, r.kind, r.title, r.parent_id, cardinality(w.path) - 1, w.path, r.type_id, r.revision,
       (SELECT count(*)::int FROM requirement c WHERE c.org_id = r.org_id AND c.parent_id = r.id AND c.trashed_at IS NULL),
       r.status_id, COALESCE(st.name, ''), COALESCE(st.category, '')
FROM walk w JOIN requirement r ON r.id = w.id LEFT JOIN req_status st ON st.id = r.status_id
ORDER BY w.path`

func scanNode(projectKey string) pgx.RowToFunc[TreeNode] {
	return func(row pgx.CollectableRow) (TreeNode, error) {
		var (
			n        TreeNode
			number   int
			path     []int32
			statusID *uuid.UUID
			status   StatusRef
		)
		err := row.Scan(&n.ID, &number, &n.Kind, &n.Title, &n.ParentID, &n.Depth, &path, &n.TypeID, &n.Revision, &n.Children,
			&statusID, &status.Name, &status.Category)
		if statusID != nil {
			status.ID = *statusID
			n.Status = &status
		}
		n.Key = Key(projectKey, number)
		n.Outline = outlineOf(path)
		return n, err
	}
}

// Tree answers a project's whole tree out of the trash, in reading order, without content.
func (s *Service) Tree(ctx context.Context, reader uuid.UUID, projectKey string) ([]TreeNode, error) {
	out := []TreeNode{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectNodes, p.ID)
		if err != nil {
			return fmt.Errorf("read the tree: %w", err)
		}
		nodes, err := pgx.CollectRows(rows, scanNode(p.Key))
		if nodes != nil {
			out = nodes
		}
		return err
	})
	return out, err
}

// Spec answers a window of a project's tree in reading order, with what each node says.
func (s *Service) Spec(ctx context.Context, reader uuid.UUID, projectKey string, limit, offset int) ([]SpecEntry, int, error) {
	out := []SpecEntry{}
	total := 0
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := project.Find(ctx, tx, reader, projectKey)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM requirement WHERE org_id = current_org_id() AND project_id = $1 AND trashed_at IS NULL`,
			p.ID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, walkTree+`
			SELECT r.id, r.number, r.kind, r.title, r.parent_id, cardinality(w.path) - 1, w.path, r.type_id, r.revision,
			       (SELECT count(*)::int FROM requirement c WHERE c.org_id = r.org_id AND c.parent_id = r.id AND c.trashed_at IS NULL),
			       r.statement, r.rationale, r.attributes,
			       ARRAY(SELECT l.name FROM requirement_label l WHERE l.org_id = r.org_id AND l.requirement_id = r.id ORDER BY l.name)
			FROM walk w JOIN requirement r ON r.id = w.id
			ORDER BY w.path LIMIT $2 OFFSET $3`, p.ID, limit, offset)
		if err != nil {
			return fmt.Errorf("read the specification: %w", err)
		}
		entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SpecEntry, error) {
			var (
				e      SpecEntry
				number int
				path   []int32
				attrs  []byte
			)
			n := &e.Node
			err := row.Scan(&n.ID, &number, &n.Kind, &n.Title, &n.ParentID, &n.Depth, &path, &n.TypeID, &n.Revision, &n.Children,
				&e.Statement, &e.Rationale, &attrs, &e.Labels)
			if err != nil {
				return e, err
			}
			n.Key = Key(p.Key, number)
			n.Outline = outlineOf(path)
			if len(e.Rationale) == 0 || string(e.Rationale) == "null" {
				e.Rationale = nil
			}
			if e.Labels == nil {
				e.Labels = []string{}
			}
			open := p.ArchivedAt == nil
			e.Can = Can{Edit: p.Can.Edit && open, Transition: (p.Can.Edit || p.Can.Approve) && open, Delete: p.Can.Edit && open,
				Link: p.Can.Edit && open, Comment: p.Can.Comment && open}
			return e, json.Unmarshal(attrs, &e.Attributes)
		})
		if entries != nil {
			out = entries
		}
		return err
	})
	return out, total, err
}

// placeError is the refusal of a place, on the field that named it.
func placeError(field, message string) error { return &FieldError{"place." + field, message} }

// noCycle is the sentence the tree guard's refusal is answered with.
const noCycle = "A requirement cannot go below itself or below one of the requirements under it."

// treeRefusal answers the database's refusal of a parent with the sentence the author reads.
func treeRefusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.ConstraintName == "requirement_tree_no_cycle" || pgErr.ConstraintName == "requirement_parent_same_project") {
		return placeError("parentId", noCycle)
	}
	return err
}

// rankAt finds where a node goes: below parent, after the sibling after names (first when nil). moving is the
// node being moved, which is no sibling of itself and no parent of anything it lands under.
func rankAt(ctx context.Context, tx db.DBTX, projectID uuid.UUID, place Place, moving *uuid.UUID) (string, error) {
	if place.ParentID != nil {
		if moving != nil && *place.ParentID == *moving {
			return "", placeError("parentId", noCycle)
		}
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM requirement WHERE org_id = current_org_id() AND project_id = $1 AND id = $2 AND trashed_at IS NULL)`,
			projectID, *place.ParentID).Scan(&live); err != nil {
			return "", err
		}
		if !live {
			return "", placeError("parentId", "That parent is not in this project, or it is in the trash. Pick another place.")
		}
	}
	siblings := `FROM requirement WHERE org_id = current_org_id() AND project_id = $1 AND parent_id IS NOT DISTINCT FROM $2
		AND trashed_at IS NULL AND ($3::uuid IS NULL OR id <> $3)`
	var before, after string
	if place.AfterID != nil {
		if moving != nil && *place.AfterID == *moving {
			return "", placeError("afterId", "A requirement cannot follow itself. Pick the sibling it goes after.")
		}
		var afterRank string
		var afterNumber int
		err := tx.QueryRow(ctx, `SELECT rank, number `+siblings+` AND id = $4`, projectID, place.ParentID, moving, *place.AfterID).Scan(&afterRank, &afterNumber)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", placeError("afterId", "The requirement it should follow is not below that parent. Pick the place again.")
		}
		if err != nil {
			return "", err
		}
		before = afterRank
		err = tx.QueryRow(ctx, `SELECT rank `+siblings+` AND (rank, number) > ($4, $5) ORDER BY rank, number LIMIT 1`,
			projectID, place.ParentID, moving, afterRank, afterNumber).Scan(&after)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	} else if err := tx.QueryRow(ctx, `SELECT rank `+siblings+` ORDER BY rank, number LIMIT 1`, projectID, place.ParentID, moving).Scan(&after); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	// Two siblings of one rank, as rows written straight to the table may be, leave no room between them.
	if before != "" && before == after {
		after = ""
	}
	return rank.Between(before, after)
}

// lastAtTop is the rank after everything at the top of a project, where a node without a place goes.
func lastAtTop(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (string, error) {
	var last string
	err := tx.QueryRow(ctx, `SELECT rank FROM requirement WHERE org_id = current_org_id() AND project_id = $1 AND parent_id IS NULL
		AND trashed_at IS NULL ORDER BY rank DESC, number DESC LIMIT 1`, projectID).Scan(&last)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	return rank.Between(last, "")
}

// subtree lists a node and everything below it that is out of the trash, in reading order.
func subtree(ctx context.Context, tx db.DBTX, projectID, root uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE live AS (
			SELECT id, parent_id, row_number() OVER (PARTITION BY parent_id ORDER BY rank, number) AS pos
			FROM requirement WHERE org_id = current_org_id() AND project_id = $1 AND (trashed_at IS NULL OR id = $2)
		), below AS (
			SELECT id, ARRAY[0]::int[] AS path FROM live WHERE id = $2
			UNION ALL
			SELECT l.id, b.path || l.pos::int FROM live l JOIN below b ON l.parent_id = b.id
		)
		SELECT id FROM below ORDER BY path`, projectID, root)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
